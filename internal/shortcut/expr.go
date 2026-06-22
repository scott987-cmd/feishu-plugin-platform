package shortcut

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The value expressions in ResultProp.Expr are a deliberately tiny grammar:
//   atoms    = number | rand() | in.<formKey> | res.<dotted.json.path>
//   operators= + - * / ( )
// Everything else is rejected. This keeps the LLM/DSL from smuggling arbitrary
// JS into the generated execute() body — the expression is the ONE place a
// generator could otherwise inject code, so it is allowlisted, not eval'd.

var (
	exprIdentRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.]*`)
	exprNumRe   = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	exprOpsRe   = regexp.MustCompile(`[^\s+\-*/()]`)
	placeholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// validateExpr rejects anything outside the grammar and checks that every
// in.<key> references a declared form item.
func validateExpr(expr string, formKeys map[string]bool) error {
	e := strings.TrimSpace(expr)
	if e == "" {
		return errors.New("empty")
	}
	for _, bad := range []string{";", "=", "[", "]", "{", "}", "$", "`", "\"", "'", "\\", "//", "/*", "?", ":", ",", "&", "|", "!", "<", ">"} {
		if strings.Contains(e, bad) {
			return fmt.Errorf("contains forbidden token %q", bad)
		}
	}
	if strings.Contains(e, "rand") && !strings.Contains(e, "rand()") {
		return errors.New("rand must be used as rand()")
	}
	for _, id := range exprIdentRe.FindAllString(e, -1) {
		switch {
		case id == "rand":
		case strings.HasPrefix(id, "in."):
			seg := strings.Split(strings.TrimPrefix(id, "in."), ".")[0]
			if !formKeys[seg] {
				return fmt.Errorf("in.%s references unknown form item", seg)
			}
		case strings.HasPrefix(id, "res."):
			// any dotted response path is allowed
		default:
			return fmt.Errorf("identifier %q not allowed (use in.<key>, res.<path>, or rand())", id)
		}
	}
	// Whatever remains after stripping idents and numbers must be operators only.
	stripped := exprIdentRe.ReplaceAllString(e, "")
	stripped = exprNumRe.ReplaceAllString(stripped, "")
	if exprOpsRe.MatchString(stripped) {
		return errors.New("contains characters outside the allowed grammar (+ - * / ( ) numbers in.<key> res.<path> rand())")
	}
	return nil
}

// translateExpr lowers a validated expression to the JS emitted inside execute():
//   rand()        -> String(Math.random())
//   in.<key>      -> inp.<key>
//   res.a.b.c     -> res?.a?.b?.c           (optional chaining; response is untrusted)
//   res.list.0.x  -> res?.list?.[0]?.x      (numeric segments are array indices)
func translateExpr(expr string) string {
	e := strings.TrimSpace(expr)
	e = strings.ReplaceAll(e, "rand()", "String(Math.random())")
	e = regexp.MustCompile(`\bin\.([A-Za-z_][A-Za-z0-9_]*)`).ReplaceAllString(e, "inp.$1")
	e = regexp.MustCompile(`\bres((?:\.[A-Za-z0-9_]+)+)`).ReplaceAllStringFunc(e, func(m string) string {
		segs := strings.Split(strings.TrimPrefix(m, "res"), ".")[1:] // drop leading ""
		var b strings.Builder
		b.WriteString("res")
		for _, s := range segs {
			if isAllDigits(s) {
				b.WriteString("?.[" + s + "]")
			} else {
				b.WriteString("?." + s)
			}
		}
		return b.String()
	})
	return e
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validateURLTemplate checks {placeholders} resolve to form items and that the
// URL host is covered by the addDomainList allowlist (the SDK hard-rejects any
// fetch to a host outside it — we catch that at compile time instead).
func validateURLTemplate(u string, domains []string, formKeys map[string]bool) error {
	for _, m := range placeholder.FindAllStringSubmatch(u, -1) {
		if !formKeys[m[1]] {
			return fmt.Errorf("url placeholder {%s} references unknown form item", m[1])
		}
	}
	host := u
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	for _, c := range []string{"/", "?", "{", ":", "#"} {
		if i := strings.Index(host, c); i >= 0 {
			host = host[:i]
		}
	}
	if host == "" {
		return errors.New("cannot determine host")
	}
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return nil
		}
	}
	return fmt.Errorf("host %q is not covered by domains allowlist %v (addDomainList would reject the request)", host, domains)
}

var resRefRe = regexp.MustCompile(`\bres\.`)

// validateExprMode is validateExpr plus a mode gate: in compute-only mode there
// is no fetched response, so res.* is forbidden (reference inputs or a template).
func validateExprMode(expr string, formKeys map[string]bool, allowRes bool) error {
	if !allowRes && resRefRe.MatchString(expr) {
		return errors.New("res.* not allowed without a fetch (compute-only): reference inputs (in.*) or use a template")
	}
	return validateExpr(expr, formKeys)
}

// validatePlaceholders gates a template string (rendered later as a JS template
// literal): no backtick / backslash / ${ injection, and every {key} must be a
// declared form item.
func validatePlaceholders(tpl string, formKeys map[string]bool) error {
	if strings.ContainsAny(tpl, "`\\") || strings.Contains(tpl, "${") {
		return errors.New("template must not contain a backtick, backslash, or ${")
	}
	for _, m := range placeholder.FindAllStringSubmatch(tpl, -1) {
		if !formKeys[m[1]] {
			return fmt.Errorf("template placeholder {%s} references unknown form item", m[1])
		}
	}
	return nil
}

var bodyRefRe = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// bodyRef returns the form-item key if v is exactly "{key}" (an input injection),
// or "" when v is a literal string.
func bodyRef(v string) string {
	if m := bodyRefRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
		return m[1]
	}
	return ""
}

// renderURLTemplate converts {key} placeholders into a JS template literal body
// (without the backticks): "https://x/{a}" -> "https://x/${inp.a}".
func renderURLTemplate(u string) string {
	return placeholder.ReplaceAllString(u, "${inp.$1}")
}

package shortcut

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The value expressions in ResultProp.Expr are a deliberately tiny grammar:
//   atoms    = number | 'string' | rand() | in.<formKey> | res.<dotted.json.path>
//   operators= + - * / % ( ) ,
//   functions= an allowlist (see exprFuncs) — including comparison/boolean/
//              conditional logic in FUNCTION form (eq/gt/and/if/…), which is why
//              raw comparison operators (< > = ? : & | !) stay FORBIDDEN below:
//              conditionals go through allowlisted helpers, never raw JS.
// Everything else is rejected. This keeps the LLM/DSL from smuggling arbitrary
// JS into the generated execute() body — the expression is the ONE place a
// generator could otherwise inject code, so it is allowlisted, not eval'd.

var (
	exprIdentRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.]*`)
	exprNumRe   = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	exprOpsRe   = regexp.MustCompile(`[^\s+\-*/()]`)
	placeholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// exprFuncs is the allowlist of helper functions usable in expressions. Each is
// rendered as a small pure JS helper (see exprHelperDefs) — `name(` becomes
// `_name(`. No other function names are permitted. Comparison/boolean/conditional
// logic is offered as FUNCTIONS (eq/gt/and/if/…) so the grammar needs no raw
// `< > = ? : & |` operators (those remain forbidden), keeping the no-eval invariant.
var exprFuncs = []string{
	// string / number helpers
	"concat", "upper", "lower", "trim", "substr", "slice", "replace", "len", "urlencode", "round",
	// math
	"floor", "ceil", "abs", "min", "max",
	// comparison → boolean
	"eq", "ne", "gt", "gte", "lt", "lte",
	// boolean logic
	"and", "or", "not",
	// conditional
	"if", "coalesce", "default",
	// array (for MultiSelect columns): split a delimited string into a string[]
	"split",
}

func isExprFunc(name string) bool {
	for _, f := range exprFuncs {
		if f == name {
			return true
		}
	}
	return false
}

// strLitRe matches a single-quoted string literal with safe content (no quote,
// backtick, backslash or $ inside) — so literals can never inject code.
var strLitRe = regexp.MustCompile(`'[^'` + "`" + `\\$]*'`)

// validateExpr rejects anything outside the grammar: numbers, single-quoted
// string literals, in.<key>, res.<path>, rand(), the allowlisted functions, and
// the operators + - * / % ( ) , — nothing else.
func validateExpr(expr string, formKeys map[string]bool) error {
	e := strings.TrimSpace(expr)
	if e == "" {
		return errors.New("empty")
	}
	// Mask safe string literals to "0" so they don't interfere with scanning;
	// a leftover quote means an unbalanced/unsafe literal.
	masked := strLitRe.ReplaceAllString(e, "0")
	if strings.Contains(masked, "'") {
		return errors.New("unbalanced or unsafe string literal (use 'plain text' only)")
	}
	for _, bad := range []string{";", "=", "[", "]", "{", "}", "$", "`", "\"", "\\", "//", "/*", ":", "?", "&", "|", "!", "<", ">"} {
		if strings.Contains(masked, bad) {
			return fmt.Errorf("contains forbidden token %q", bad)
		}
	}
	if strings.Contains(masked, "rand") && !strings.Contains(masked, "rand()") {
		return errors.New("rand must be used as rand()")
	}
	for _, id := range exprIdentRe.FindAllString(masked, -1) {
		switch {
		case id == "rand", isExprFunc(id):
		case strings.HasPrefix(id, "in."):
			seg := strings.Split(strings.TrimPrefix(id, "in."), ".")[0]
			if !formKeys[seg] {
				return fmt.Errorf("in.%s references unknown form item", seg)
			}
		case strings.HasPrefix(id, "res."):
			// any dotted response path is allowed
		default:
			return fmt.Errorf("identifier %q not allowed (use in.<key>, res.<path>, rand(), or %v)", id, exprFuncs)
		}
	}
	// Whatever remains after stripping idents and numbers must be operators only.
	stripped := exprIdentRe.ReplaceAllString(masked, "")
	stripped = exprNumRe.ReplaceAllString(stripped, "")
	if regexp.MustCompile(`[^\s+\-*/%(),]`).MatchString(stripped) {
		return errors.New("contains characters outside the allowed grammar (+ - * / % ( ) , numbers 'strings' in.<key> res.<path> rand() functions)")
	}
	return nil
}

// translateExpr lowers a validated expression to the JS emitted inside execute():
//
//	rand()        -> String(Math.random())
//	in.<key>      -> inp.<key>
//	res.a.b.c     -> res?.a?.b?.c           (optional chaining; response is untrusted)
//	res.list.0.x  -> res?.list?.[0]?.x      (numeric segments are array indices)
func translateExpr(expr string) string {
	e := strings.TrimSpace(expr)
	// Transform only non-literal segments so 'string literals' pass through verbatim.
	var out strings.Builder
	last := 0
	for _, loc := range strLitRe.FindAllStringIndex(e, -1) {
		out.WriteString(transformSeg(e[last:loc[0]]))
		out.WriteString(e[loc[0]:loc[1]])
		last = loc[1]
	}
	out.WriteString(transformSeg(e[last:]))
	return out.String()
}

func transformSeg(s string) string {
	s = strings.ReplaceAll(s, "rand()", "String(Math.random())")
	for _, fn := range exprFuncs { // allowlisted function name -> pure JS helper call
		s = regexp.MustCompile(`\b`+fn+`\(`).ReplaceAllString(s, "_"+fn+"(")
	}
	s = regexp.MustCompile(`\bin\.([A-Za-z_][A-Za-z0-9_]*)`).ReplaceAllString(s, "inp.$1")
	s = regexp.MustCompile(`\bres((?:\.[A-Za-z0-9_]+)+)`).ReplaceAllStringFunc(s, func(m string) string {
		segs := strings.Split(strings.TrimPrefix(m, "res"), ".")[1:]
		var b strings.Builder
		b.WriteString("res")
		for _, x := range segs {
			if isAllDigits(x) {
				b.WriteString("?.[" + x + "]")
			} else {
				b.WriteString("?." + x)
			}
		}
		return b.String()
	})
	return s
}

// exprHelperDefs are the pure JS implementations of the allowlisted functions.
// All helpers are annotated `: any` so their results compose freely with the
// arithmetic operators (+ - * / %) and with each other — without TS's strict
// "left-hand side of an arithmetic operation must be number" complaint. Runtime
// semantics are unchanged; only the static type is relaxed (params are `any` too).
var exprHelperDefs = map[string]string{
	"concat":    "const _concat = (...a: any[]): any => a.map(String).join('');",
	"upper":     "const _upper = (s: any): any => String(s).toUpperCase();",
	"lower":     "const _lower = (s: any): any => String(s).toLowerCase();",
	"trim":      "const _trim = (s: any): any => String(s).trim();",
	"substr":    "const _substr = (s: any, a: any, l: any): any => String(s).slice(Number(a), Number(a) + Number(l));",
	"slice":     "const _slice = (s: any, a: any, b: any): any => String(s).slice(Number(a), Number(b));",
	"replace":   "const _replace = (s: any, a: any, b: any): any => String(s).split(String(a)).join(String(b));",
	"len":       "const _len = (s: any): any => String(s).length;",
	"urlencode": "const _urlencode = (s: any): any => encodeURIComponent(String(s));",
	"round":     "const _round = (n: any, d: any = 0): any => Number(Number(n).toFixed(Number(d)));",
	"floor":     "const _floor = (n: any): any => Math.floor(Number(n));",
	"ceil":      "const _ceil = (n: any): any => Math.ceil(Number(n));",
	"abs":       "const _abs = (n: any): any => Math.abs(Number(n));",
	"min":       "const _min = (...a: any[]): any => Math.min(...a.map(Number));",
	"max":       "const _max = (...a: any[]): any => Math.max(...a.map(Number));",
	// comparison: gt/gte/lt/lte are numeric; eq/ne compare by string so they work
	// for both numbers and text. All return a boolean (typed any) for if/and/or.
	"eq":  "const _eq = (a: any, b: any): any => String(a) === String(b);",
	"ne":  "const _ne = (a: any, b: any): any => String(a) !== String(b);",
	"gt":  "const _gt = (a: any, b: any): any => Number(a) > Number(b);",
	"gte": "const _gte = (a: any, b: any): any => Number(a) >= Number(b);",
	"lt":  "const _lt = (a: any, b: any): any => Number(a) < Number(b);",
	"lte": "const _lte = (a: any, b: any): any => Number(a) <= Number(b);",
	"and": "const _and = (...a: any[]): any => a.every(Boolean);",
	"or":  "const _or = (...a: any[]): any => a.some(Boolean);",
	"not": "const _not = (a: any): any => !a;",
	// conditional: if(cond, then, else); coalesce/default fall back on empty/null.
	"if":       "const _if = (c: any, a: any, b: any): any => (c ? a : b);",
	"coalesce": "const _coalesce = (...a: any[]): any => { for (const v of a) { if (v !== undefined && v !== null && v !== '') return v; } return ''; };",
	"default":  "const _default = (v: any, d: any): any => (v === undefined || v === null || v === '') ? d : v;",
	// split a delimited string into a trimmed, non-empty string[] (for MultiSelect).
	"split": "const _split = (s: any, d: any): any => String(s).split(String(d)).map((x: any) => x.trim()).filter(Boolean);",
}

// emitExprHelpers writes (indented) the helper defs actually referenced by vals.
func emitExprHelpers(b *strings.Builder, indent string, vals []string) {
	for _, fn := range exprFuncs {
		needle := "_" + fn + "("
		for _, v := range vals {
			if strings.Contains(v, needle) {
				b.WriteString(indent + exprHelperDefs[fn] + "\n")
				break
			}
		}
	}
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
	return checkURLHost(u, domains)
}

// urlHost extracts the host from a URL template, ignoring scheme, path, query,
// {placeholders}, and port; "" if it cannot be determined.
func urlHost(u string) string {
	// Parse with net/url so the host we allowlist-check is exactly the host the
	// dialer (and Go's redirect handling) uses. A hand-rolled splitter is unsafe:
	// it disagrees with net/url on userinfo, letting `http://good.com:80@evil.com/`
	// pass as good.com while the request actually dials evil.com (allowlist bypass
	// / confused-deputy exfil). url.Parse cleanly handles ports, paths and
	// {formKey} placeholders in path/query; an unresolved placeholder IN the host
	// makes it error → empty host → rejected (host placeholders are unsupported,
	// matching prior behavior).
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return ""
	}
	// Userinfo in the authority has no legitimate use in a field-shortcut URL and
	// is the bypass vector above — refuse it outright.
	if parsed.User != nil {
		return ""
	}
	return parsed.Hostname()
}

// checkURLHost verifies the URL's host is covered by the domains allowlist (the
// SDK hard-rejects any fetch to a host outside addDomainList).
func checkURLHost(u string, domains []string) error {
	host := urlHost(u)
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

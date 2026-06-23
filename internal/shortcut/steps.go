package shortcut

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Multi-step pipelines: chain several requests where a later step uses an earlier
// step's JSON response (e.g. fetch a token, then call an API with it; look up an
// id, then fetch its detail). Each step's response binds to s_<ID>; the last is
// also aliased to `res` so result expressions map it unchanged. Cross-step and
// input data flow through placeholders: {formKey} → an input; {priorStepID.path}
// → that step's response value. All of this is allowlisted/validated — never eval.

// dottedPlaceholderRe matches {name} and {stepID.json.path} (dots allowed, unlike
// the single-step `placeholder` which only allows a bare key).
var dottedPlaceholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_.]*)\}`)

// chainPath turns a dotted JSON path into safe optional-chaining: "a.0.b" →
// "?.a?.[0]?.b" (numeric segments are array indices), matching res.* lowering.
func chainPath(path string) string {
	var b strings.Builder
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		if isAllDigits(seg) {
			b.WriteString("?.[" + seg + "]")
		} else {
			b.WriteString("?." + seg)
		}
	}
	return b.String()
}

// makeRefResolver maps a placeholder name to its JS expression: an input → inp.<x>;
// a prior-step ref <id>.<path> → s_<id><chain>. Errors if the name resolves to
// neither (so unknown refs are rejected at validate time).
func makeRefResolver(formKeys, priorSteps map[string]bool) func(string) (string, error) {
	return func(name string) (string, error) {
		if i := strings.IndexByte(name, '.'); i >= 0 {
			head, rest := name[:i], name[i+1:]
			if !priorSteps[head] {
				return "", fmt.Errorf("{%s} references unknown prior step %q", name, head)
			}
			return "s_" + head + chainPath(rest), nil
		}
		if formKeys[name] {
			return "inp." + name, nil
		}
		return "", fmt.Errorf("{%s} is not a declared input or a prior-step ref", name)
	}
}

// renderTemplateMulti turns a string with {refs} into a JS template-literal BODY
// (no backticks): "Bearer {auth.token}" → "Bearer ${s_auth?.token}".
func renderTemplateMulti(s string, resolve func(string) (string, error)) (string, error) {
	var firstErr error
	out := dottedPlaceholderRe.ReplaceAllStringFunc(s, func(m string) string {
		js, err := resolve(m[1 : len(m)-1])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return m
		}
		return "${" + js + "}"
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// validateTplValue rejects template injection and checks every {ref} resolves.
func validateTplValue(s string, resolve func(string) (string, error)) error {
	if strings.ContainsAny(s, "`\\") || strings.Contains(s, "${") {
		return errors.New("must not contain a backtick, backslash, or ${")
	}
	if len(s) > MaxStrLen {
		return errors.New("too long")
	}
	for _, m := range dottedPlaceholderRe.FindAllStringSubmatch(s, -1) {
		if _, err := resolve(m[1]); err != nil {
			return err
		}
	}
	return nil
}

// renderTplValue renders a header/body value as a backtick template when it has
// refs, else a quoted literal.
func renderTplValue(v string, resolve func(string) (string, error)) (string, error) {
	if dottedPlaceholderRe.MatchString(v) {
		r, err := renderTemplateMulti(v, resolve)
		if err != nil {
			return "", err
		}
		return "`" + r + "`", nil
	}
	return jsStr(v), nil
}

// validateSteps checks an ordered pipeline: ids ascii+unique, methods/body rules,
// every URL host in the domains allowlist, and every placeholder resolving to an
// input or a PRIOR step (forward refs are rejected — a step can't use a later one).
func validateSteps(steps []Step, formKeys map[string]bool, domains []string) error {
	var errs []error
	if len(steps) > MaxSteps {
		errs = append(errs, fmt.Errorf("too many steps (%d > %d)", len(steps), MaxSteps))
	}
	prior := map[string]bool{}
	seen := map[string]bool{}
	for i, s := range steps {
		if !keyRe.MatchString(s.ID) {
			errs = append(errs, fmt.Errorf("steps[%d].id %q invalid", i, s.ID))
		}
		if seen[s.ID] {
			errs = append(errs, fmt.Errorf("steps[%d].id %q duplicated", i, s.ID))
		}
		seen[s.ID] = true
		if !slices.Contains(ValidMethods, s.Method) {
			errs = append(errs, fmt.Errorf("steps[%d].method %q invalid (want %s)", i, s.Method, strings.Join(ValidMethods, ", ")))
		}
		resolve := makeRefResolver(formKeys, prior) // refs: inputs + PRIOR steps only
		if strings.TrimSpace(s.URL) == "" {
			errs = append(errs, fmt.Errorf("steps[%d].url is required", i))
		} else {
			if err := validateTplValue(s.URL, resolve); err != nil {
				errs = append(errs, fmt.Errorf("steps[%d].url: %w", i, err))
			}
			if err := checkURLHost(s.URL, domains); err != nil {
				errs = append(errs, fmt.Errorf("steps[%d].url: %w", i, err))
			}
		}
		for k, v := range s.Headers {
			if !paramNameRe.MatchString(k) {
				errs = append(errs, fmt.Errorf("steps[%d].headers key %q invalid", i, k))
			}
			if err := validateTplValue(v, resolve); err != nil {
				errs = append(errs, fmt.Errorf("steps[%d].headers[%s]: %w", i, k, err))
			}
		}
		if len(s.Body) > 0 {
			if !methodHasBody(s.Method) {
				errs = append(errs, fmt.Errorf("steps[%d].body is only valid for POST/PUT/PATCH", i))
			}
			for k, v := range s.Body {
				if !keyRe.MatchString(k) {
					errs = append(errs, fmt.Errorf("steps[%d].body key %q invalid", i, k))
				}
				if err := validateTplValue(v, resolve); err != nil {
					errs = append(errs, fmt.Errorf("steps[%d].body[%s]: %w", i, k, err))
				}
			}
		}
		if len(s.BodyJSON) > 0 {
			if !methodHasBody(s.Method) {
				errs = append(errs, fmt.Errorf("steps[%d].bodyJson is only valid for POST/PUT/PATCH", i))
			}
			if len(s.Body) > 0 {
				errs = append(errs, fmt.Errorf("steps[%d]: set either body or bodyJson, not both", i))
			}
			if err := validateBodyJSONWith(s.BodyJSON, func(str string) error { return validateTplValue(str, resolve) }); err != nil {
				errs = append(errs, fmt.Errorf("steps[%d].bodyJson: %w", i, err))
			}
		}
		prior[s.ID] = true
	}
	return errors.Join(errs...)
}

// renderStepInit builds a step's fetch init (method + merged headers + body),
// resolving refs through resolve (inp.<x> / s_<id>...). inp. binding; the action
// renderer rebinds inp.→args. on the whole block.
func renderStepInit(s Step, resolve func(string) (string, error)) (string, error) {
	parts := []string{"method: " + jsStr(s.Method)}

	var hdr []string
	if len(s.Body) > 0 || len(s.BodyJSON) > 0 {
		hdr = append(hdr, "'Content-Type': 'application/json'")
	}
	for _, k := range sortedStrKeys(s.Headers) {
		val, err := renderTplValue(s.Headers[k], resolve)
		if err != nil {
			return "", err
		}
		hdr = append(hdr, jsStr(k)+": "+val)
	}
	if len(hdr) > 0 {
		parts = append(parts, "headers: { "+strings.Join(hdr, ", ")+" }")
	}

	if len(s.Body) > 0 {
		bp := make([]string, 0, len(s.Body))
		for _, k := range sortedStrKeys(s.Body) {
			val, err := renderTplValue(s.Body[k], resolve)
			if err != nil {
				return "", err
			}
			bp = append(bp, k+": "+val)
		}
		parts = append(parts, "body: JSON.stringify({ "+strings.Join(bp, ", ")+" })")
	} else if len(s.BodyJSON) > 0 {
		renderStr := func(str string) string {
			if dottedPlaceholderRe.MatchString(str) {
				r, _ := renderTemplateMulti(str, resolve)
				return "`" + r + "`"
			}
			return jsonStr(str)
		}
		bj, err := renderBodyJSONWith(s.BodyJSON, renderStr)
		if err != nil {
			return "", err
		}
		parts = append(parts, "body: JSON.stringify("+bj+")")
	}
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

// renderStepsJS emits the pipeline body: one `const s_<id> = await fetch(...)`
// per step (each resolving refs to inputs + already-bound prior steps), then
// `const res = s_<lastID>` so result expressions map the final response.
func renderStepsJS(steps []Step, formKeys map[string]bool) (string, error) {
	var b strings.Builder
	prior := map[string]bool{}
	for _, s := range steps {
		resolve := makeRefResolver(formKeys, prior)
		url, err := renderTemplateMulti(s.URL, resolve)
		if err != nil {
			return "", err
		}
		init, err := renderStepInit(s, resolve)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "      const s_%s: any = await fetch(`%s`, %s);\n", s.ID, url, init)
		prior[s.ID] = true
	}
	fmt.Fprintf(&b, "      const res: any = s_%s;\n", steps[len(steps)-1].ID)
	return b.String(), nil
}

package shortcut

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// bodyJSON support: APIs like AI chat completions need a structured/nested JSON
// body (e.g. {"model":"…","messages":[{"role":"user","content":"…{text}…"}]}).
// The DSL carries that as a JSON template; string values may contain {formKey}
// placeholders. We render it to a JS object literal (so the generated source is
// readable + auditable), substituting placeholders with input references.

// validateBodyJSON checks the template is valid JSON and that every {key}
// placeholder inside string values references a declared form item (single-step).
func validateBodyJSON(raw []byte, formKeys map[string]bool) error {
	return validateBodyJSONWith(raw, func(s string) error {
		if placeholder.MatchString(s) {
			return validatePlaceholders(s, formKeys)
		}
		return nil
	})
}

// validateBodyJSONWith is validateBodyJSON with a pluggable per-string check, so
// the multi-step path can validate {stepID.path} refs alongside {input}.
func validateBodyJSONWith(raw []byte, checkStr func(string) error) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return walkBodyJSON(v, checkStr)
}

func walkBodyJSON(v any, checkStr func(string) error) error {
	switch t := v.(type) {
	case map[string]any:
		for _, val := range t {
			if err := walkBodyJSON(val, checkStr); err != nil {
				return err
			}
		}
	case []any:
		for _, val := range t {
			if err := walkBodyJSON(val, checkStr); err != nil {
				return err
			}
		}
	case string:
		return checkStr(t)
	}
	return nil
}

// renderBodyJSON lowers the JSON template to a JS literal: string values with
// {key} placeholders become template literals (`…${inp.key}…`), other strings
// stay JSON-quoted, objects/arrays recurse. Keys are emitted in sorted order for
// deterministic output. Single-step binding (inp.).
func renderBodyJSON(raw []byte) (string, error) {
	return renderBodyJSONWith(raw, func(s string) string {
		if placeholder.MatchString(s) {
			return "`" + renderURLTemplate(s) + "`" // {key} -> ${inp.key}
		}
		return jsonStr(s)
	})
}

// renderBodyJSONWith is renderBodyJSON with a pluggable per-string renderer, so
// the multi-step path can resolve {stepID.path} refs to s_<id> bindings.
func renderBodyJSONWith(raw []byte, renderStr func(string) string) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	return renderBodyNode(v, renderStr), nil
}

func renderBodyNode(v any, renderStr func(string) string) string {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = jsonStr(k) + ": " + renderBodyNode(t[k], renderStr)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	case []any:
		parts := make([]string, len(t))
		for i, val := range t {
			parts[i] = renderBodyNode(val, renderStr)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		return renderStr(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return "null"
	default:
		return "null"
	}
}

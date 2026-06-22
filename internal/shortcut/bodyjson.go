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
// placeholder inside string values references a declared form item.
func validateBodyJSON(raw []byte, formKeys map[string]bool) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return walkBodyJSON(v, formKeys)
}

func walkBodyJSON(v any, formKeys map[string]bool) error {
	switch t := v.(type) {
	case map[string]any:
		for _, val := range t {
			if err := walkBodyJSON(val, formKeys); err != nil {
				return err
			}
		}
	case []any:
		for _, val := range t {
			if err := walkBodyJSON(val, formKeys); err != nil {
				return err
			}
		}
	case string:
		if placeholder.MatchString(t) {
			return validatePlaceholders(t, formKeys)
		}
	}
	return nil
}

// renderBodyJSON lowers the JSON template to a JS literal: string values with
// {key} placeholders become template literals (`…${inp.key}…`), other strings
// stay JSON-quoted, objects/arrays recurse. Keys are emitted in sorted order for
// deterministic output.
func renderBodyJSON(raw []byte) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	return renderBodyNode(v), nil
}

func renderBodyNode(v any) string {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = jsonStr(k) + ": " + renderBodyNode(t[k])
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	case []any:
		parts := make([]string, len(t))
		for i, val := range t {
			parts[i] = renderBodyNode(val)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		if placeholder.MatchString(t) {
			return "`" + renderURLTemplate(t) + "`" // {key} -> ${inp.key}
		}
		return jsonStr(t)
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

// Package execrt is the self-hosted execute runtime: it INTERPRETS a
// shortcut.FieldShortcut at request time (fetch external APIs + map the response
// into output values) instead of compiling it to a basekit TypeScript project.
//
// This is the runtime that replaces Feishu's basekit FaaS for private-deployment
// customers, who have no FaaS service (see EXECUTE_RUNTIME.md). The same
// declarative DSL drives both paths; here it is interpreted, never eval'd:
//   - expressions go through the allowlisted grammar (shortcut.ValidateExpr),
//   - every outbound URL host is checked against the plugin's domain allowlist,
//   - the runtime only fetches + maps + returns; it never writes host data.
package execrt

import (
	"math"
	"strconv"
	"strings"
)

// getPath resolves a dotted JSON path over a value decoded from json.Unmarshal
// (map[string]any / []any / scalar). Numeric segments index arrays, matching the
// compiler's `res.list.0.x` → optional-chaining lowering. A missing/!ok segment
// yields nil (like JS optional chaining returning undefined), never a panic.
func getPath(root any, path string) any {
	cur := root
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]any:
			cur = node[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil
			}
			cur = node[i]
		default:
			return nil
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}

// toStr mirrors JS String(x) closely enough for value mapping: numbers print
// without a trailing ".0", booleans as true/false, nil as "" (we treat a missing
// value as empty rather than the JS literal "undefined", which is more useful for
// writeback). Strings pass through; anything else falls back to a numeric/blank.
func toStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}

// toNum mirrors JS Number(x): numeric strings parse, "" is 0, booleans are 1/0,
// nil/non-numeric is NaN. ok=false signals a NaN result so arithmetic can
// propagate it (JS: undefined + 1 === NaN).
func toNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, true
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return math.NaN(), false
		}
		return f, true
	default:
		return math.NaN(), false
	}
}

// truthy mirrors JS truthiness for and/or/not/if: false, 0, "", nil are falsy.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	case int:
		return x != 0
	default:
		return true
	}
}

// isNumberLike reports whether v should be treated as a number by the JS `+`
// operator rule (string operands trigger concatenation instead of addition).
func isNumberLike(v any) bool {
	switch v.(type) {
	case float64, int, int64, bool, nil:
		return true
	default:
		return false
	}
}

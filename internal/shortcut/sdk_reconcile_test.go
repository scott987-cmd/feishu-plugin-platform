package shortcut

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// This test is the "trustworthy generator" gate. Every SDK-facing allowlist in this
// package is emitted by render.go as a reference into a basekit SDK enum — e.g.
// `FieldType.<KEY>`, `NumberFormatter.<KEY>`, `AuthorizationType.<KEY>`,
// `FieldComponent.<KEY>`, or an addAction `authorization.type` string literal. If an
// allowlist carries a value the SDK does not define, the rendered TypeScript resolves
// to `undefined` and the published plugin breaks at runtime with no compile error.
// That is exactly how a phantom "PERCENT_ROUNDED_2" formatter once slipped in.
//
// We reconcile each allowlist against authoritative enum keys extracted from the SDK's
// dist/index.d.ts into testdata/basekit_sdk_enums.json (regenerate with
// scripts/refresh-sdk-enums.sh). Two failure modes are both covered:
//   - Go allowlist drifts away from the SDK  -> this test fails.
//   - golden drifts away from the real SDK    -> the CI "sdk-drift" job (refresh +
//     git diff) fails. See .github/workflows/ci.yml.

type sdkEnums struct {
	SDKVersion        string   `json:"_sdkVersion"`
	FieldType         []string `json:"FieldType"`
	NumberFormatter   []string `json:"NumberFormatter"`
	AuthorizationType []string `json:"AuthorizationType"`
	FieldComponent    []string `json:"FieldComponent"`
	Component         []string `json:"Component"`
	FieldCode         []string `json:"FieldCode"`
	ActionAuthType    []string `json:"ActionAuthType"`
}

func loadSDKEnums(t *testing.T) sdkEnums {
	t.Helper()
	p := filepath.Join("testdata", "basekit_sdk_enums.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v — run scripts/refresh-sdk-enums.sh", p, err)
	}
	var e sdkEnums
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("parse %s: %v", p, err)
	}
	return e
}

func TestAllowlistsAreSubsetOfSDK(t *testing.T) {
	e := loadSDKEnums(t)
	cases := []struct {
		name    string   // the Go allowlist under test
		emit    string   // how the renderer references it (for the failure hint)
		values  []string // the Go allowlist values
		sdkName string   // matching SDK set name in the golden
		sdk     []string // authoritative SDK values
	}{
		{"ValidFieldTypes", "FieldType.<KEY>", ValidFieldTypes, "FieldType", e.FieldType},
		{"primaryFieldTypes", "FieldType.<KEY>", primaryFieldTypes, "FieldType", e.FieldType},
		{"ValidFormatters", "NumberFormatter.<KEY>", ValidFormatters, "NumberFormatter", e.NumberFormatter},
		{"ValidAuthTypes", "AuthorizationType.<KEY>", ValidAuthTypes, "AuthorizationType", e.AuthorizationType},
		{"ValidComponents", "FieldComponent.<KEY>", ValidComponents, "FieldComponent", e.FieldComponent},
		{"ValidActionAuthTypes", "addAction authorization.type literal", ValidActionAuthTypes, "ActionAuthType", e.ActionAuthType},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.values) == 0 {
				t.Fatalf("%s is empty", c.name)
			}
			if len(c.sdk) == 0 {
				t.Fatalf("golden set %q is empty — scripts/refresh-sdk-enums.sh likely failed to parse the SDK", c.sdkName)
			}
			set := toSet(c.sdk)
			for _, v := range c.values {
				if set[v] {
					continue
				}
				hint := ""
				if s := closest(v, c.sdk); s != "" {
					hint = " (closest valid: " + s + ")"
				}
				t.Errorf("%s contains %q which is not a valid SDK %s value%s.\n"+
					"  The renderer emits `%s`, so this resolves to undefined and breaks the plugin at runtime.\n"+
					"  Fix: remove/correct it in the allowlist, or if the SDK changed run scripts/refresh-sdk-enums.sh.",
					c.name, v, c.sdkName, hint, c.emit)
			}
		})
	}
}

// TestHardcodedSDKConstantsExist guards the SDK enum keys the renderer emits as fixed
// constants (not via an allowlist). They are valid for the pinned SDK today, but if a
// future SDK renames or removes one, the generated plugin silently emits `Enum.undefined`
// at runtime with no other signal — the same failure class the allowlist gate prevents.
// Keep this table in sync with the literal `Enum.Key` emissions in render.go/action.go.
func TestHardcodedSDKConstantsExist(t *testing.T) {
	e := loadSDKEnums(t)
	byEnum := map[string][]string{
		"FieldType": e.FieldType,
		"FieldCode": e.FieldCode,
		"Component": e.Component,
	}
	emitted := []struct{ enum, key, where string }{
		{"FieldType", "Object", "render.go resultType — `type: FieldType.Object`"},
		{"FieldCode", "Success", "render.go execute success — `code: FieldCode.Success`"},
		{"FieldCode", "Error", "render.go execute catch — `return { code: FieldCode.Error }`"},
		{"Component", "Input", "action.go addAction formItem — `component: Component.Input`"},
	}
	for _, c := range emitted {
		set := toSet(byEnum[c.enum])
		if len(set) == 0 {
			t.Fatalf("golden has no %s set — scripts/refresh-sdk-enums.sh must extract it", c.enum)
		}
		if !set[c.key] {
			t.Errorf("renderer hardcodes %s.%s (%s) but %q is not in SDK enum %s — it resolves to undefined at runtime. "+
				"Update the emission and run scripts/refresh-sdk-enums.sh.", c.enum, c.key, c.where, c.key, c.enum)
		}
	}
}

// TestSDKCoverageGaps never fails; it reports the SDK values the generator does not
// yet support, so the supported surface can be grown deliberately (run `go test
// ./internal/shortcut -run Coverage -v`).
func TestSDKCoverageGaps(t *testing.T) {
	e := loadSDKEnums(t)
	report := []struct {
		label string
		goSet []string
		sdk   []string
	}{
		{"FieldType         (vs ValidFieldTypes)", ValidFieldTypes, e.FieldType},
		{"NumberFormatter   (vs ValidFormatters)", ValidFormatters, e.NumberFormatter},
		{"AuthorizationType (vs ValidAuthTypes)", ValidAuthTypes, e.AuthorizationType},
		{"FieldComponent    (vs ValidComponents)", ValidComponents, e.FieldComponent},
	}
	t.Logf("basekit SDK %s — generator coverage gaps (informational, not a failure):", e.SDKVersion)
	for _, r := range report {
		have := toSet(r.goSet)
		var gap []string
		for _, v := range r.sdk {
			if !have[v] {
				gap = append(gap, v)
			}
		}
		sort.Strings(gap)
		t.Logf("  %s supported %d/%d, unsupported: %v", r.label, len(r.goSet), len(r.sdk), gap)
	}
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// closest returns the SDK value with the smallest edit distance to v, but only when
// it is reasonably close — to make a failure message actionable without guessing wildly.
func closest(v string, xs []string) string {
	best, bestD := "", 1<<30
	for _, x := range xs {
		if d := levenshtein(v, x); d < bestD {
			best, bestD = x, d
		}
	}
	if best != "" && bestD <= len(v)/2+1 {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

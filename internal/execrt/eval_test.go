package execrt

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad fixture json: %v", err)
	}
	return v
}

func evalNum(t *testing.T, expr string, in map[string]any, res any) float64 {
	t.Helper()
	v, err := evalExpr(expr, in, res)
	if err != nil {
		t.Fatalf("evalExpr(%q) error: %v", expr, err)
	}
	n, ok := toNum(v)
	if !ok {
		t.Fatalf("evalExpr(%q) = %v, not numeric", expr, v)
	}
	return n
}

func evalStr(t *testing.T, expr string, in map[string]any, res any) string {
	t.Helper()
	v, err := evalExpr(expr, in, res)
	if err != nil {
		t.Fatalf("evalExpr(%q) error: %v", expr, err)
	}
	return toStr(v)
}

func TestEvalArithmetic(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"1 + 2", 3},
		{"2 * 3 + 4", 10},
		{"2 + 3 * 4", 14},
		{"(2 + 3) * 4", 20},
		{"10 / 4", 2.5},
		{"10 % 3", 1},
		{"-5 + 8", 3},
	}
	for _, c := range cases {
		if got := evalNum(t, c.expr, nil, nil); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%q = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalResAndInPaths(t *testing.T) {
	res := mustJSON(t, `{"current":{"temperature_2m":28.8,"wind_speed_10m":13.4},"results":[{"latitude":39.9075,"longitude":116.4}]}`)
	in := map[string]any{"city": "Beijing", "factor": 2.0}

	if got := evalNum(t, "res.current.temperature_2m", in, res); math.Abs(got-28.8) > 1e-9 {
		t.Errorf("temperature = %v, want 28.8", got)
	}
	if got := evalNum(t, "res.results.0.latitude", in, res); math.Abs(got-39.9075) > 1e-9 {
		t.Errorf("latitude = %v, want 39.9075", got)
	}
	if got := evalStr(t, "in.city", in, res); got != "Beijing" {
		t.Errorf("in.city = %q, want Beijing", got)
	}
	if got := evalNum(t, "res.current.temperature_2m * in.factor", in, res); math.Abs(got-57.6) > 1e-9 {
		t.Errorf("temp*factor = %v, want 57.6", got)
	}
	// missing path -> nil -> "" / NaN, never a panic
	if v, err := evalExpr("res.nope.deep.path", in, res); err != nil || v != nil {
		t.Errorf("missing path = (%v,%v), want (nil,nil)", v, err)
	}
}

func TestEvalFunctions(t *testing.T) {
	res := mustJSON(t, `{"name":"  Sunny  ","code":3}`)
	in := map[string]any{"n": 7.0, "s": "hello", "csv": "a, b ,c"}

	str := map[string]string{
		"upper(in.s)":                     "HELLO",
		"trim(res.name)":                  "Sunny",
		"concat('a', 'b', in.s)":          "abhello",
		"if(gt(in.n, 5), 'big', 'small')": "big",
		"if(lt(in.n, 5), 'big', 'small')": "small",
		"coalesce(res.missing, 'fb')":     "fb",
		"default(res.missing, 'd')":       "d",
		"slice('abcdef', 1, 3)":           "bc",
		"replace('a-b-c', '-', '_')":      "a_b_c",
	}
	for expr, want := range str {
		if got := evalStr(t, expr, in, res); got != want {
			t.Errorf("%q = %q, want %q", expr, got, want)
		}
	}

	// numeric / boolean
	if got := evalNum(t, "round(28.846, 1)", in, res); math.Abs(got-28.8) > 1e-9 {
		t.Errorf("round = %v, want 28.8", got)
	}
	if got := evalNum(t, "len(in.s)", in, res); got != 5 {
		t.Errorf("len = %v, want 5", got)
	}
	if v, _ := evalExpr("and(gt(in.n,5), lt(in.n,10))", in, res); v != true {
		t.Errorf("and(...) = %v, want true", v)
	}

	// split -> []any
	v, err := evalExpr("split(in.csv, ',')", in, res)
	if err != nil {
		t.Fatalf("split error: %v", err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 3 || arr[0] != "a" || arr[1] != "b" || arr[2] != "c" {
		t.Errorf("split = %#v, want [a b c]", v)
	}
}

func TestEvalRand(t *testing.T) {
	// rand() is the grammar atom used for the stable _id group key: a string in
	// [0,1), and distinct across calls.
	a := evalStr(t, "rand()", nil, nil)
	b := evalStr(t, "rand()", nil, nil)
	if a == "" || b == "" {
		t.Fatal("rand() returned empty")
	}
	if f, err := strconv.ParseFloat(a, 64); err != nil || f < 0 || f >= 1 {
		t.Errorf("rand() = %q, want a float in [0,1)", a)
	}
	if a == b {
		t.Errorf("rand() returned the same value twice (%q)", a)
	}
}

func TestEvalRejectsGarbage(t *testing.T) {
	for _, expr := range []string{"foo.bar", "1 +", "unknownfn(1)", "(1 + 2"} {
		if _, err := evalExpr(expr, nil, nil); err == nil {
			t.Errorf("evalExpr(%q) should error", expr)
		}
	}
}

// TestExprFuncParity asserts the interpreter implements EXACTLY the functions the
// compiler (shortcut/expr.go) allowlists — so the runtime can never silently
// diverge from the generated TypeScript.
func TestExprFuncParity(t *testing.T) {
	want := map[string]bool{}
	for _, n := range shortcut.ExprFuncNames() {
		want[n] = true
		if _, ok := exprFns[n]; !ok {
			t.Errorf("interpreter is missing allowlisted function %q", n)
		}
	}
	for n := range exprFns {
		if !want[n] {
			t.Errorf("interpreter implements %q which is NOT in the compiler allowlist", n)
		}
	}
}

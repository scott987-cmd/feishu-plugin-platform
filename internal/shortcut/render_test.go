package shortcut_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

func loadExchangeRate(t *testing.T) shortcut.FieldShortcut {
	t.Helper()
	data, err := os.ReadFile("testdata/exchange_rate.json")
	if err != nil {
		t.Fatal(err)
	}
	var f shortcut.FieldShortcut
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestExchangeRateValidAndRenders(t *testing.T) {
	f := loadExchangeRate(t)
	if err := f.Validate(); err != nil {
		t.Fatalf("exchange_rate.json should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	wants := []string{
		"basekit.addDomainList(['api.exchangerate-api.com']);",
		"import { basekit, FieldType, field, FieldComponent, FieldCode, NumberFormatter, AuthorizationType }",
		"component: FieldComponent.FieldSelect,",
		"props: { supportType: [FieldType.Number] },",
		"type: FieldType.Object,",
		"isGroupByKey: true,",
		"NumberFormatter.DIGITAL_ROUNDED_2",
		// expression translation: in.* -> inp.*, res.* -> optional chaining, rand() -> Math.random
		"usd: inp.account * res?.rates?.USD,",
		"rate: res?.rates?.USD,",
		"id: String(Math.random()),",
		"await fetch(`https://api.exchangerate-api.com/v4/latest/CNY`, { method: 'GET' });",
		"export default basekit;",
	}
	for _, w := range wants {
		if !strings.Contains(ts, w) {
			t.Errorf("rendered TS missing:\n  %s", w)
		}
	}
}

func TestArrayIndexExpr(t *testing.T) {
	f := loadExchangeRate(t)
	// A response array path, as weather/list APIs return.
	f.Result.Properties[1].Expr = "res.weather.0.description"
	if err := f.Validate(); err != nil {
		t.Fatalf("array-index expr should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if !strings.Contains(ts, "res?.weather?.[0]?.description") {
		t.Errorf("array index not translated correctly; want res?.weather?.[0]?.description in:\n%s", ts)
	}
}

func TestQueryParamAuthRenders(t *testing.T) {
	f := loadExchangeRate(t)
	f.Auth = &shortcut.Auth{
		ID: "owmKey", Type: "QueryParamToken", Label: "OpenWeatherMap API Key",
		Platform: "OpenWeatherMap", InstructionsURL: "https://openweathermap.org/api", ParamName: "appid",
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("query-param auth should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, w := range []string{
		"authorizations: [",
		"type: AuthorizationType.QueryParamToken,",
		"params: { paramName: 'appid' },",
		"icon: { light:",
		// execute passes the auth id so the runtime injects the credential
		"{ method: 'GET' }, 'owmKey');",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("auth render missing: %s", w)
		}
	}
}

func TestCustomHeaderAndBasicAuth(t *testing.T) {
	// CustomHeaderToken renders params.headerName (hyphenated header allowed).
	f := loadExchangeRate(t)
	f.Auth = &shortcut.Auth{ID: "k", Type: "CustomHeaderToken", Label: "API Key", Platform: "Acme", InstructionsURL: "https://acme/keys", ParamName: "X-API-Key"}
	if err := f.Validate(); err != nil {
		t.Fatalf("CustomHeaderToken should validate, got: %v", err)
	}
	ts, _ := shortcut.RenderIndexTS(f)
	if !strings.Contains(ts, "type: AuthorizationType.CustomHeaderToken,") || !strings.Contains(ts, "params: { headerName: 'X-API-Key' },") {
		t.Errorf("CustomHeaderToken render wrong:\n%s", ts)
	}
	// Basic renders no params block.
	f.Auth = &shortcut.Auth{ID: "b", Type: "Basic", Label: "Login", Platform: "Acme", InstructionsURL: "https://acme/login"}
	if err := f.Validate(); err != nil {
		t.Fatalf("Basic should validate without paramName, got: %v", err)
	}
	ts, _ = shortcut.RenderIndexTS(f)
	if !strings.Contains(ts, "type: AuthorizationType.Basic,") || strings.Contains(ts, "params:") {
		t.Errorf("Basic render wrong (should have no params):\n%s", ts)
	}
}

func TestBearerAuthValidatesNoParamName(t *testing.T) {
	f := loadExchangeRate(t)
	f.Auth = &shortcut.Auth{ID: "tok", Type: "HeaderBearerToken", Label: "Token", Platform: "X", InstructionsURL: "https://x"}
	if err := f.Validate(); err != nil {
		t.Fatalf("bearer auth should validate without paramName, got: %v", err)
	}
	// QueryParamToken without paramName must fail.
	f.Auth.Type = "QueryParamToken"
	f.Auth.ParamName = ""
	if err := f.Validate(); err == nil {
		t.Error("QueryParamToken without paramName should fail validation")
	}
}

func TestPostBodyRenders(t *testing.T) {
	f := loadExchangeRate(t)
	f.Domains = []string{"httpbin.org"}
	f.Execute = shortcut.Execute{
		URL: "https://httpbin.org/post", Method: "POST",
		Body: map[string]string{"amount": "{account}", "currency": "CNY"},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("POST body should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, w := range []string{
		"'Content-Type': 'application/json'",
		// keys rendered in sorted order; {account} → inp.account, literal → quoted
		"body: JSON.stringify({ amount: inp.account, currency: 'CNY' })",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("POST body render missing: %s", w)
		}
	}
}

func TestPostBodyRejectedOnGet(t *testing.T) {
	f := loadExchangeRate(t)
	f.Execute.Body = map[string]string{"x": "{account}"} // method is GET → invalid
	if err := f.Validate(); err == nil {
		t.Error("body with GET method should fail validation")
	}
}

func TestComputeOnlyTemplate(t *testing.T) {
	// No execute/domains → compute-only; a template output renders as a JS template literal.
	f := shortcut.FieldShortcut{
		ID: "qr", Title: shortcut.I18n{ZhCN: "二维码"},
		FormItems: []shortcut.FormItem{{Key: "text", Label: shortcut.I18n{ZhCN: "文本"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true}},
		Result: shortcut.Result{Kind: "object", Properties: []shortcut.ResultProp{
			{Key: "url", Type: "Text", Primary: true, Template: "https://api.qrserver.com/v1/create-qr-code/?data={text}"},
		}},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("compute-only template should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if strings.Contains(ts, "addDomainList") {
		t.Error("compute-only should not emit addDomainList")
	}
	if strings.Contains(ts, "await fetch") {
		t.Error("compute-only should not fetch")
	}
	if !strings.Contains(ts, "url: `https://api.qrserver.com/v1/create-qr-code/?data=${inp.text}`") {
		t.Errorf("template not rendered as literal:\n%s", ts)
	}
}

func TestExprFunctions(t *testing.T) {
	f := loadExchangeRate(t)
	f.Domains = nil
	f.Execute = shortcut.Execute{} // compute-only
	f.Result.Properties = []shortcut.ResultProp{
		{Key: "out", Type: "Text", Primary: true, Expr: "upper(trim(in.account))"},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("function expr should validate, got: %v", err)
	}
	ts, _ := shortcut.RenderIndexTS(f)
	for _, w := range []string{
		"const _upper =", "const _trim =", // only used helpers emitted
		"out: _upper(_trim(inp.account)),",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("function render missing: %s", w)
		}
	}
	// res.* must be rejected in compute-only mode
	f.Result.Properties[0].Expr = "res.foo"
	if err := f.Validate(); err == nil {
		t.Error("res.* should be rejected without a fetch")
	}
	// arbitrary code still rejected
	f.Result.Properties[0].Expr = "process.exit(1)"
	if err := f.Validate(); err == nil {
		t.Error("arbitrary identifier should be rejected")
	}
}

func TestConditionalExpr(t *testing.T) {
	f := loadExchangeRate(t)
	f.Domains = nil
	f.Execute = shortcut.Execute{} // compute-only
	f.FormItems = []shortcut.FormItem{
		{Key: "idcard", Label: shortcut.I18n{ZhCN: "身份证号"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true},
	}
	// ID-card gender by parity of the 17th digit — exercises if/eq/substr + the % operator.
	f.Result.Properties = []shortcut.ResultProp{
		{Key: "gender", Type: "Text", Primary: true, Expr: "if(eq(substr(in.idcard, 16, 1) % 2, 1), '男', '女')"},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("conditional expr should validate, got: %v", err)
	}
	ts, _ := shortcut.RenderIndexTS(f)
	for _, w := range []string{
		"const _if =", "const _eq =", "const _substr =", // only used helpers emitted
		"gender: _if(_eq(_substr(inp.idcard, 16, 1) % 2, 1), '男', '女'),",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("conditional render missing:\n  %s\n--- got ---\n%s", w, ts)
		}
	}
	// unused conditional helpers must NOT be emitted
	if strings.Contains(ts, "const _gt =") || strings.Contains(ts, "const _coalesce =") {
		t.Error("unused conditional helpers should not be emitted")
	}
	// boolean/coalesce combo also validates and renders its helpers
	f.Result.Properties[0].Expr = "if(and(gte(len(in.idcard), 18), not(eq(in.idcard, ''))), coalesce(in.idcard, '空'), '非法')"
	if err := f.Validate(); err != nil {
		t.Fatalf("boolean/coalesce expr should validate, got: %v", err)
	}
	// RAW comparison/boolean/ternary operators MUST still be rejected (no-eval invariant);
	// every ident below is valid, so rejection is purely due to the operator.
	for _, bad := range []string{
		"in.idcard > 5",          // raw >
		"len(in.idcard) == 18",   // raw ==
		"len(in.idcard) ? 1 : 0", // raw ternary ? :
		"not(in.idcard) | 0",     // raw |
	} {
		f.Result.Properties[0].Expr = bad
		if err := f.Validate(); err == nil {
			t.Errorf("raw-operator expr %q must be rejected", bad)
		}
	}
	// non-allowlisted identifiers (incl. function-call-shaped injection) must be rejected:
	// conditionals add new funcs, but the allowlist must stay closed.
	for _, bad := range []string{
		"eval('x')",                  // not allowlisted
		"if(eq(in.idcard,1), constructor, 0)", // constructor ident
		"coalesce(process.env, '0')", // process ident
		"require('fs')",              // require ident
	} {
		f.Result.Properties[0].Expr = bad
		if err := f.Validate(); err == nil {
			t.Errorf("injection expr %q must be rejected", bad)
		}
	}
}

func TestWritePathPutHeadersBodyJSON(t *testing.T) {
	f := loadExchangeRate(t)
	f.Domains = []string{"api.example.com"}
	f.FormItems = []shortcut.FormItem{
		{Key: "recordId", Label: shortcut.I18n{ZhCN: "记录ID"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true},
		{Key: "status", Label: shortcut.I18n{ZhCN: "状态"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true},
	}
	f.Result.Properties = []shortcut.ResultProp{{Key: "ok", Type: "Text", Primary: true, Expr: "res.status"}}
	f.Execute = shortcut.Execute{
		URL: "https://api.example.com/records/{recordId}", Method: "PUT",
		Headers:  map[string]string{"X-Api-Version": "2024-01", "X-Record": "{recordId}"},
		BodyJSON: []byte(`{"status":"{status}"}`),
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("PUT + headers + bodyJson should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, w := range []string{
		"method: 'PUT'",
		"'Content-Type': 'application/json'",        // auto-added for a body
		"'X-Api-Version': '2024-01'",                // literal header
		"'X-Record': inp.recordId",                  // header with {key} injection
		"${inp.status}",                             // bodyJson placeholder interpolation
		"await fetch(`https://api.example.com/records/${inp.recordId}`", // url placeholder
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("write-path render missing:\n  %s\n--- got ---\n%s", w, ts)
		}
	}
	// GET must not carry a body.
	f.Execute.Method = "GET"
	if err := f.Validate(); err == nil {
		t.Error("GET with a bodyJson must be rejected")
	}
	// DELETE without a body is fine.
	f.Execute.Method = "DELETE"
	f.Execute.BodyJSON = nil
	f.Execute.Headers = nil
	if err := f.Validate(); err != nil {
		t.Errorf("DELETE without a body should validate, got: %v", err)
	}
}

func TestMultiStepChain(t *testing.T) {
	f := loadExchangeRate(t)
	f.Auth = nil
	f.Domains = []string{"httpbin.org"}
	f.Execute = shortcut.Execute{} // no single request — use steps
	f.FormItems = []shortcut.FormItem{
		{Key: "seed", Label: shortcut.I18n{ZhCN: "种子"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true},
	}
	f.Result.Properties = []shortcut.ResultProp{
		{Key: "final", Type: "Text", Primary: true, Expr: "res.json.echoed"},
		{Key: "_id", Type: "Text", Hidden: true, GroupByKey: true, Expr: "rand()"},
	}
	f.Steps = []shortcut.Step{
		{ID: "first", URL: "https://httpbin.org/anything/{seed}", Method: "GET"},
		{ID: "second", URL: "https://httpbin.org/anything", Method: "POST",
			Headers:  map[string]string{"X-From-First": "{first.method}"},   // header uses step 1's output
			BodyJSON: []byte(`{"echoed":"{first.url}"}`)},                    // body uses step 1's output
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("multi-step should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, w := range []string{
		"const s_first: any = await fetch(`https://httpbin.org/anything/${inp.seed}`, { method: 'GET' });",
		"const s_second: any = await fetch(`https://httpbin.org/anything`,",
		"${s_first?.method}", // header cross-step ref
		"${s_first?.url}",    // bodyJson cross-step ref
		"const res: any = s_second;",
		"final: res?.json?.echoed,",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("multi-step render missing:\n  %s\n--- got ---\n%s", w, ts)
		}
	}
	// a step may not reference a LATER step (forward ref).
	bad := f
	bad.Steps = []shortcut.Step{
		{ID: "first", URL: "https://httpbin.org/anything/{second.x}", Method: "GET"},
		{ID: "second", URL: "https://httpbin.org/anything", Method: "GET"},
	}
	if err := bad.Validate(); err == nil {
		t.Error("a step referencing a later step must be rejected")
	}
	// steps and a single execute.url are mutually exclusive.
	bad2 := f
	bad2.Execute = shortcut.Execute{URL: "https://httpbin.org/x", Method: "GET"}
	if err := bad2.Validate(); err == nil {
		t.Error("steps + execute.url together must be rejected")
	}
}

func TestBodyJSONNested(t *testing.T) {
	f := loadExchangeRate(t)
	f.Domains = []string{"api.deepseek.com"}
	f.FormItems = []shortcut.FormItem{{Key: "text", Label: shortcut.I18n{ZhCN: "文本"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true}}
	f.Result.Properties = []shortcut.ResultProp{{Key: "out", Type: "Text", Primary: true, Expr: "res.choices.0.message.content"}}
	f.Execute = shortcut.Execute{
		URL: "https://api.deepseek.com/chat/completions", Method: "POST",
		BodyJSON: []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"{text}"}]}`),
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("nested bodyJson should validate, got: %v", err)
	}
	ts, err := shortcut.RenderIndexTS(f)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	// nested literal with placeholder→${inp.text}; keys sorted; array index path
	for _, w := range []string{
		"body: JSON.stringify({ ",
		"\"messages\": [{ \"content\": `${inp.text}`, \"role\": \"user\" }]",
		"\"model\": \"deepseek-chat\"",
		"out: res?.choices?.[0]?.message?.content,",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("bodyJson render missing:\n  %s\n--- got ---\n%s", w, ts)
		}
	}
	// placeholder referencing an unknown input must be rejected
	f.Execute.BodyJSON = []byte(`{"messages":[{"content":"{nope}"}]}`)
	if err := f.Validate(); err == nil {
		t.Error("bodyJson with unknown placeholder should fail validation")
	}
}

func TestRejections(t *testing.T) {
	base := loadExchangeRate(t)

	cases := []struct {
		name string
		mut  func(*shortcut.FieldShortcut)
	}{
		{"empty domains", func(f *shortcut.FieldShortcut) { f.Domains = nil }},
		{"host not in allowlist", func(f *shortcut.FieldShortcut) {
			f.Execute.URL = "https://evil.example.com/x"
		}},
		{"expr arbitrary code", func(f *shortcut.FieldShortcut) {
			f.Result.Properties[1].Expr = "process.exit(1)"
		}},
		{"expr unknown form item", func(f *shortcut.FieldShortcut) {
			f.Result.Properties[1].Expr = "in.nope * 2"
		}},
		{"expr forbidden token", func(f *shortcut.FieldShortcut) {
			f.Result.Properties[1].Expr = "res.a; in.account"
		}},
		{"bad component", func(f *shortcut.FieldShortcut) { f.FormItems[0].Component = "Wat" }},
		{"bad field type", func(f *shortcut.FieldShortcut) { f.Result.Properties[1].Type = "Blob" }},
		{"bad method", func(f *shortcut.FieldShortcut) { f.Execute.Method = "TRACE" }},
		{"url placeholder unknown", func(f *shortcut.FieldShortcut) {
			f.Execute.URL = "https://api.exchangerate-api.com/{nope}"
		}},
	}
	for _, c := range cases {
		f := base // shallow copy; deep enough since muts target distinct fields
		// deep copy slices we mutate
		f.Domains = append([]string(nil), base.Domains...)
		f.FormItems = append([]shortcut.FormItem(nil), base.FormItems...)
		f.Result.Properties = append([]shortcut.ResultProp(nil), base.Result.Properties...)
		c.mut(&f)
		if err := f.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", c.name)
		}
	}
}

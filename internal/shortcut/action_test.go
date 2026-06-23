package shortcut_test

import (
	"strings"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

func validAction() shortcut.Action {
	return shortcut.Action{
		ID:      "cny-usd-action",
		Title:   shortcut.I18n{ZhCN: "汇率换算动作"},
		Domains: []string{"api.exchangerate-api.com"},
		Inputs:  []shortcut.ActionInput{{Key: "amount", Label: "人民币金额", Required: true}},
		Result: []shortcut.ActionOutput{
			{Key: "usd", Label: "美元金额", Type: "Number", Expr: "in.amount * res.rates.USD"},
			{Key: "rate", Label: "当前汇率", Type: "Number", Expr: "res.rates.USD"},
		},
		Execute: shortcut.Execute{URL: "https://api.exchangerate-api.com/v4/latest/CNY", Method: "GET"},
	}
}

func TestActionValidatesAndRenders(t *testing.T) {
	a := validAction()
	if err := a.Validate(); err != nil {
		t.Fatalf("valid action should pass, got: %v", err)
	}
	ts, err := shortcut.RenderActionTS(a)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	wants := []string{
		"import { basekit, Component } from '@lark-opdev/block-basekit-server-api';",
		"basekit.addAction({",
		"{ itemId: 'amount', label: '人民币金额', required: true, component: Component.Input },",
		// inputs bind to args.* (not inp.*), response to optional chaining
		"usd: args.amount * res?.rates?.USD,",
		"rate: res?.rates?.USD,",
		// action resultType uses string types in a keyed object
		"type: 'object',",
		"usd: { label: '美元金额', type: 'number' },",
		"return {", // plain object, no FieldCode
	}
	for _, w := range wants {
		if !strings.Contains(ts, w) {
			t.Errorf("action render missing:\n  %s", w)
		}
	}
	// Field-shortcut artifacts must NOT leak into an action.
	for _, bad := range []string{"FieldCode", "addField(", "resultType:\n    type: FieldType.Object"} {
		if strings.Contains(ts, bad) {
			t.Errorf("action render unexpectedly contains field artifact: %s", bad)
		}
	}
}

func TestActionAuthAndURLTemplate(t *testing.T) {
	a := validAction()
	a.Domains = []string{"api.openweathermap.org"}
	a.Auth = &shortcut.ActionAuth{Type: "APIKey", Label: "OWM API Key", Placeholder: "appid"}
	a.Inputs = []shortcut.ActionInput{{Key: "city", Label: "城市", Required: true}}
	a.Result = []shortcut.ActionOutput{{Key: "temp", Label: "温度", Type: "Number", Expr: "res.main.temp"}}
	a.Execute = shortcut.Execute{URL: "https://api.openweathermap.org/data/2.5/weather?q={city}", Method: "GET"}
	if err := a.Validate(); err != nil {
		t.Fatalf("action with auth should validate, got: %v", err)
	}
	ts, err := shortcut.RenderActionTS(a)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, w := range []string{
		"authorization: {",
		"type: 'APIKey',",
		"label: 'OWM API Key',",
		"componentProps: { placeholder: 'appid' },",
		// URL placeholder binds to args (action param), not inp
		"q=${args.city}`",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("action auth/url render missing:\n  %s\n%s", w, ts)
		}
	}
	// bad auth type rejected
	a.Auth.Type = "Nope"
	if err := a.Validate(); err == nil {
		t.Error("invalid auth.type should fail validation")
	}
}

func TestActionRejections(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*shortcut.Action)
	}{
		{"bad id", func(a *shortcut.Action) { a.ID = "Bad ID" }},
		{"no domains", func(a *shortcut.Action) { a.Domains = nil }},
		{"no inputs", func(a *shortcut.Action) { a.Inputs = nil }},
		{"bad result type", func(a *shortcut.Action) { a.Result[0].Type = "Nope" }},
		{"expr unknown input", func(a *shortcut.Action) { a.Result[0].Expr = "in.nope * 2" }},
		{"host not in domains", func(a *shortcut.Action) { a.Execute.URL = "https://evil.com/x" }},
		{"body on GET", func(a *shortcut.Action) { a.Execute.Body = map[string]string{"x": "1"} }},
		{"bodyJson on DELETE", func(a *shortcut.Action) { a.Execute.Method = "DELETE"; a.Execute.BodyJSON = []byte(`{"x":"1"}`) }},
		{"headers unknown input", func(a *shortcut.Action) { a.Execute.Headers = map[string]string{"X-Id": "{nope}"} }},
	}
	for _, c := range cases {
		a := validAction()
		a.Domains = append([]string(nil), a.Domains...)
		a.Inputs = append([]shortcut.ActionInput(nil), a.Inputs...)
		a.Result = append([]shortcut.ActionOutput(nil), a.Result...)
		c.mut(&a)
		if err := a.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", c.name)
		}
	}
}

func TestActionWritePathPatchBodyJSON(t *testing.T) {
	a := validAction()
	a.Domains = []string{"api.example.com"}
	a.Inputs = []shortcut.ActionInput{
		{Key: "ticketId", Label: "工单ID", Required: true},
		{Key: "status", Label: "新状态", Required: true},
	}
	a.Result = []shortcut.ActionOutput{{Key: "ok", Label: "结果", Type: "Text", Expr: "res.status"}}
	a.Execute = shortcut.Execute{
		URL: "https://api.example.com/tickets/{ticketId}", Method: "PATCH",
		Headers:  map[string]string{"X-Api-Version": "v2"},
		BodyJSON: []byte(`{"status":"{status}"}`),
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("action PATCH + bodyJson + headers should validate, got: %v", err)
	}
	ts, err := shortcut.RenderActionTS(a)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, w := range []string{
		"method: 'PATCH'",
		"'Content-Type': 'application/json'",
		"'X-Api-Version': 'v2'",
		"${args.status}", // bodyJson input refs rebind inp.→args. for actions
		"await fetch(`https://api.example.com/tickets/${args.ticketId}`",
	} {
		if !strings.Contains(ts, w) {
			t.Errorf("action write-path render missing:\n  %s\n--- got ---\n%s", w, ts)
		}
	}
	// the action source must never carry the field `inp.` binding.
	if strings.Contains(ts, "inp.") {
		t.Errorf("action render leaked the inp. binding:\n%s", ts)
	}
}

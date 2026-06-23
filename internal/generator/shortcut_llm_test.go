package generator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

func validShortcut() shortcut.FieldShortcut {
	return shortcut.FieldShortcut{
		ID:      "exchange-rate",
		Title:   shortcut.I18n{ZhCN: "汇率换算"},
		Domains: []string{"api.exchangerate-api.com"},
		FormItems: []shortcut.FormItem{{
			Key: "account", Label: shortcut.I18n{ZhCN: "人民币金额"},
			Component: "FieldSelect", SupportType: []string{"Number"}, Required: true,
		}},
		Result: shortcut.Result{Kind: "object", Properties: []shortcut.ResultProp{
			{Key: "id", Type: "Text", GroupByKey: true, Hidden: true, Expr: "rand()"},
			{Key: "usd", Type: "Number", Primary: true, Expr: "in.account * res.rates.USD"},
		}},
		Execute: shortcut.Execute{URL: "https://api.exchangerate-api.com/v4/latest/CNY", Method: "GET"},
	}
}

func chatShortcutResp(v any) oaResponse {
	b, _ := json.Marshal(v)
	return oaResponse{Choices: []oaChoice{{
		Message: oaMessage{Role: "assistant", ToolCalls: []oaToolCall{{
			ID: "call1", Type: "function",
			Function: oaFunctionCall{Name: emitShortcutTool, Arguments: string(b)},
		}}},
		FinishReason: "tool_calls",
	}}}
}

func TestShortcutSuccessFirstRound(t *testing.T) {
	api := &fakeChat{responses: []oaResponse{chatShortcutResp(validShortcut())}}
	f, err := generateShortcutViaChat(context.Background(), api, "deepseek-chat", "汇率换算", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.calls != 1 {
		t.Errorf("calls = %d, want 1", api.calls)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("returned shortcut invalid: %v", err)
	}
}

func TestShortcutAutoRepair(t *testing.T) {
	bad := validShortcut()
	bad.Execute.URL = "https://evil.example.com/x" // host not in domains → rejected
	api := &fakeChat{responses: []oaResponse{chatShortcutResp(bad), chatShortcutResp(validShortcut())}}
	f, err := generateShortcutViaChat(context.Background(), api, "deepseek-chat", "x", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.calls != 2 {
		t.Errorf("calls = %d, want 2 (one repair round)", api.calls)
	}
	if f.ID != "exchange-rate" {
		t.Errorf("expected repaired shortcut, got id %q", f.ID)
	}
}

func TestShortcutExhaustsRepairs(t *testing.T) {
	bad := validShortcut()
	bad.Domains = nil // always invalid
	api := &fakeChat{responses: []oaResponse{
		chatShortcutResp(bad), chatShortcutResp(bad), chatShortcutResp(bad),
	}}
	if _, err := generateShortcutViaChat(context.Background(), api, "deepseek-chat", "x", nil); err == nil {
		t.Error("expected error after exhausting repair rounds")
	}
}

// fakeVerifier stands in for the build verifier: it fails the first failCalls
// VerifyField calls (a compile error), then passes — unless unavailable, in
// which case it always signals "skip".
type fakeVerifier struct {
	failCalls   int
	unavailable bool
	calls       int
}

func (v *fakeVerifier) VerifyField(_ context.Context, _ shortcut.FieldShortcut) error {
	v.calls++
	if v.unavailable {
		return errVerifyUnavailable
	}
	if v.calls <= v.failCalls {
		return errors.New("rendered project did not compile against the real SDK: error TS2362")
	}
	return nil
}
func (v *fakeVerifier) VerifyAction(_ context.Context, _ shortcut.Action) error {
	return errVerifyUnavailable
}

func TestShortcutBuildVerifierDrivesRepair(t *testing.T) {
	// DSL is valid both rounds, but the build verifier fails once → forces a
	// repair round driven by the compile error, then passes.
	v := &fakeVerifier{failCalls: 1}
	api := &fakeChat{responses: []oaResponse{chatShortcutResp(validShortcut()), chatShortcutResp(validShortcut())}}
	if _, err := generateShortcutViaChat(context.Background(), api, "deepseek-chat", "x", v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.calls != 2 {
		t.Errorf("chat calls = %d, want 2 (compile failure forced one repair)", api.calls)
	}
	if v.calls != 2 {
		t.Errorf("verifier calls = %d, want 2", v.calls)
	}
}

func TestShortcutVerifierUnavailableAccepts(t *testing.T) {
	// An unavailable verifier (no toolchain) must NOT block generation.
	v := &fakeVerifier{unavailable: true}
	api := &fakeChat{responses: []oaResponse{chatShortcutResp(validShortcut())}}
	if _, err := generateShortcutViaChat(context.Background(), api, "deepseek-chat", "x", v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.calls != 1 {
		t.Errorf("chat calls = %d, want 1 (unavailable verifier = accept)", api.calls)
	}
}

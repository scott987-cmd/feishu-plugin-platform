package generator

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeChat returns scripted OpenAI-compatible responses, one per create() call.
type fakeChat struct {
	responses []oaResponse
	calls     int
}

func (f *fakeChat) create(_ context.Context, _ oaRequest) (oaResponse, error) {
	r := f.responses[f.calls]
	f.calls++
	return r, nil
}

// chatToolResp wraps a value as a forced-function-call response.
func chatToolResp(v any) oaResponse {
	b, _ := json.Marshal(v)
	return oaResponse{Choices: []oaChoice{{
		Message: oaMessage{Role: "assistant", ToolCalls: []oaToolCall{{
			ID: "call1", Type: "function",
			Function: oaFunctionCall{Name: emitToolName, Arguments: string(b)},
		}}},
		FinishReason: "tool_calls",
	}}}
}

func TestDeepSeekSuccessFirstRound(t *testing.T) {
	api := &fakeChat{responses: []oaResponse{chatToolResp(validDef())}}
	def, err := generateViaChat(context.Background(), api, "deepseek-chat", "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.calls != 1 {
		t.Errorf("calls = %d, want 1", api.calls)
	}
	if err := def.Validate(); err != nil {
		t.Errorf("returned def invalid: %v", err)
	}
}

func TestDeepSeekAutoRepair(t *testing.T) {
	bad := validDef()
	bad.Type = "not_a_type"
	api := &fakeChat{responses: []oaResponse{chatToolResp(bad), chatToolResp(validDef())}}
	def, err := generateViaChat(context.Background(), api, "deepseek-chat", "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.calls != 2 {
		t.Errorf("calls = %d, want 2 (one repair round)", api.calls)
	}
	if def.Type != "view_extension" {
		t.Errorf("expected repaired def, got type %q", def.Type)
	}
}

func TestDeepSeekExhaustsRepairs(t *testing.T) {
	bad := validDef()
	bad.UI.Components = nil
	api := &fakeChat{responses: []oaResponse{chatToolResp(bad), chatToolResp(bad), chatToolResp(bad)}}
	if _, err := generateViaChat(context.Background(), api, "deepseek-chat", "anything"); err == nil {
		t.Fatal("expected error after exhausting repair rounds")
	}
	if api.calls != maxRepairRounds+1 {
		t.Errorf("calls = %d, want %d", api.calls, maxRepairRounds+1)
	}
}

func TestDeepSeekNoToolCall(t *testing.T) {
	api := &fakeChat{responses: []oaResponse{{Choices: []oaChoice{{Message: oaMessage{Role: "assistant", Content: "hi"}}}}}}
	if _, err := generateViaChat(context.Background(), api, "deepseek-chat", "anything"); err == nil {
		t.Fatal("expected error when model does not call the function")
	}
}

package generator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// fakeAPI returns scripted responses, one per create() call.
type fakeAPI struct {
	responses []anthropicResponse
	calls     int
}

func (f *fakeAPI) create(_ context.Context, _ anthropicRequest) (anthropicResponse, error) {
	r := f.responses[f.calls]
	f.calls++
	return r, nil
}

// toolUse wraps a value as a forced-tool response.
func toolUse(v any) anthropicResponse {
	b, _ := json.Marshal(v)
	return anthropicResponse{Content: []contentBlock{{Type: "tool_use", ID: "tu", Name: emitToolName, Input: b}}}
}

func validDef() dsl.AppDefinition {
	return dsl.AppDefinition{
		ID: "app-x", Name: "x", Type: "view_extension",
		UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{{Type: "stat", Title: "t"}}},
	}
}

func TestClaudeSuccessFirstRound(t *testing.T) {
	api := &fakeAPI{responses: []anthropicResponse{toolUse(validDef())}}
	def, err := generateViaMessages(context.Background(), api, "m", "anything")
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

func TestClaudeAutoRepair(t *testing.T) {
	bad := validDef()
	bad.Type = "not_a_type" // fails validation -> triggers one repair round
	api := &fakeAPI{responses: []anthropicResponse{toolUse(bad), toolUse(validDef())}}
	def, err := generateViaMessages(context.Background(), api, "m", "anything")
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

func TestClaudeExhaustsRepairs(t *testing.T) {
	bad := validDef()
	bad.UI.Components = nil // always invalid
	api := &fakeAPI{responses: []anthropicResponse{toolUse(bad), toolUse(bad), toolUse(bad)}}
	if _, err := generateViaMessages(context.Background(), api, "m", "anything"); err == nil {
		t.Fatal("expected error after exhausting repair rounds")
	}
	if api.calls != maxRepairRounds+1 {
		t.Errorf("calls = %d, want %d", api.calls, maxRepairRounds+1)
	}
}

func TestClaudeNoToolUse(t *testing.T) {
	api := &fakeAPI{responses: []anthropicResponse{{Content: []contentBlock{{Type: "text", Text: "hi"}}}}}
	if _, err := generateViaMessages(context.Background(), api, "m", "anything"); err == nil {
		t.Fatal("expected error when model does not call the tool")
	}
}

// The generated schema must stay in sync with the validator's enum sources.
func TestSchemaUsesValidatorEnums(t *testing.T) {
	schema := appDefinitionSchema()
	props := schema["properties"].(map[string]any)
	typ := props["type"].(map[string]any)
	got := typ["enum"].([]string)
	if len(got) != len(dsl.ValidTypes) {
		t.Errorf("schema type enum (%v) drifted from dsl.ValidTypes (%v)", got, dsl.ValidTypes)
	}
}

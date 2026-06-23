package generator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// The embedded few-shot exemplars are injected verbatim into the prompt as the
// "exact JSON to emit", so they MUST stay valid — this guards against drift.
func TestEmbeddedFieldExemplarsValidate(t *testing.T) {
	if len(fieldExemplars) == 0 {
		t.Fatal("no field exemplars embedded")
	}
	for i, ex := range fieldExemplars {
		var f shortcut.FieldShortcut
		if err := json.Unmarshal(ex.DSL, &f); err != nil {
			t.Fatalf("field exemplar %d (%s): bad JSON: %v", i, ex.NL, err)
		}
		if err := f.Validate(); err != nil {
			t.Errorf("field exemplar %d (%s) does not Validate: %v", i, ex.NL, err)
		}
	}
}

func TestEmbeddedActionExemplarsValidate(t *testing.T) {
	if len(actionExemplars) == 0 {
		t.Fatal("no action exemplars embedded")
	}
	for i, ex := range actionExemplars {
		var a shortcut.Action
		if err := json.Unmarshal(ex.DSL, &a); err != nil {
			t.Fatalf("action exemplar %d (%s): bad JSON: %v", i, ex.NL, err)
		}
		if err := a.Validate(); err != nil {
			t.Errorf("action exemplar %d (%s) does not Validate: %v", i, ex.NL, err)
		}
	}
}

func TestRetrieveExemplarsRanksByOverlap(t *testing.T) {
	got := retrieveExemplars("身份证号判断性别男女", fieldExemplars, 3)
	if len(got) != 3 {
		t.Fatalf("want 3 exemplars, got %d", len(got))
	}
	if !strings.Contains(got[0].NL, "身份证") {
		t.Errorf("top exemplar = %q, expected the ID-card one for an ID-card prompt", got[0].NL)
	}
	block := fewShotBlock(got)
	if !strings.Contains(block, "WORKED EXAMPLES") {
		t.Error("few-shot block missing header")
	}
	if !strings.Contains(block, "if(eq(") {
		t.Errorf("few-shot block should carry the conditional exemplar:\n%s", block)
	}
	// k larger than the pool must not panic and must cap at pool size.
	if all := retrieveExemplars("x", fieldExemplars, 99); len(all) != len(fieldExemplars) {
		t.Errorf("retrieve k>pool: got %d, want %d", len(all), len(fieldExemplars))
	}
}

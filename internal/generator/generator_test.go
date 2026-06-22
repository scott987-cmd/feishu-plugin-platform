package generator_test

import (
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/generator"
)

// TestTemplatesProduceValid: every advertised template yields a definition that
// passes DSL validation (the generate->store gate must never produce off-schema).
func TestTemplatesProduceValid(t *testing.T) {
	for _, tpl := range generator.Templates {
		d, err := generator.Generate(generator.Request{
			Mode:     "template",
			Template: tpl,
			Params:   map[string]string{"title": "测试", "field": "金额", "x": "销售员"},
		})
		if err != nil {
			t.Fatalf("template %s: %v", tpl, err)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("template %s produced invalid definition: %v", tpl, err)
		}
	}
}

// TestNLRouting: the Phase-0 keyword router picks the expected component shape.
func TestNLRouting(t *testing.T) {
	cases := []struct {
		prompt   string
		wantType string
	}{
		{"我想要一个按销售员看业绩的柱状图", "chart"},
		{"统计本月新增客户数量", "stat"},
	}
	for _, c := range cases {
		d, err := generator.Generate(generator.Request{Mode: "nl", Prompt: c.prompt})
		if err != nil {
			t.Fatalf("nl %q: %v", c.prompt, err)
		}
		if got := d.UI.Components[0].Type; got != c.wantType {
			t.Errorf("nl %q: first component type = %q, want %q", c.prompt, got, c.wantType)
		}
	}
}

// TestUniqueIDsForDifferentNames guards the slug bug we fixed: distinct Chinese
// names must produce distinct ids, not collapse to a shared fallback.
func TestUniqueIDsForDifferentNames(t *testing.T) {
	a, _ := generator.Generate(generator.Request{Mode: "template", Template: "stat_card", Params: map[string]string{"title": "本月新增客户"}})
	b, _ := generator.Generate(generator.Request{Mode: "template", Template: "stat_card", Params: map[string]string{"title": "本月流失客户"}})
	if a.ID == b.ID {
		t.Errorf("different names produced the same id %q", a.ID)
	}
	if a.ID == "app" || a.ID == "" {
		t.Errorf("id collapsed to fallback: %q", a.ID)
	}
}

// TestUnknownMode and unknown template are rejected.
func TestRejectsUnknown(t *testing.T) {
	if _, err := generator.Generate(generator.Request{Mode: "xxx"}); err == nil {
		t.Error("unknown mode should error")
	}
	if _, err := generator.Generate(generator.Request{Mode: "template", Template: "nope"}); err == nil {
		t.Error("unknown template should error")
	}
}

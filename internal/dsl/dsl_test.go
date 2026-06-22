package dsl_test

import (
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// validBase is a minimal definition that must pass Validate.
func validBase() dsl.AppDefinition {
	return dsl.AppDefinition{
		ID:   "app-x",
		Name: "x",
		Type: "view_extension",
		UI:   dsl.UI{Layout: "dashboard", Components: []dsl.Component{{Type: "stat", Title: "t"}}},
	}
}

// TestValidatePlanExample asserts the plan's verbatim example DSL is accepted.
func TestValidatePlanExample(t *testing.T) {
	d := dsl.AppDefinition{
		ID:   "app_sales_board",
		Name: "销售看板",
		Type: "view_extension",
		Bind: dsl.Bind{BaseID: "current", TableID: "tbl_orders"},
		UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{
			{Type: "stat", Title: "本月销售额", Agg: "sum", Field: "金额", Filter: "month(下单时间)=THIS_MONTH"},
			{Type: "chart", ChartType: "bar", X: "销售员", Y: &dsl.AggSpec{Agg: "sum", Field: "金额"}},
		}},
		Actions: []dsl.Action{{ID: "export", Trigger: "button", Label: "导出 Excel", Do: "exportXlsx", Scope: "currentView"}},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("plan example should be valid, got: %v", err)
	}
}

// TestValidateLenientBinding documents that field/agg may be deferred (host-bound
// later), so a stat without field and a chart with only a chartType still pass.
func TestValidateLenientBinding(t *testing.T) {
	d := validBase()
	d.UI.Components = []dsl.Component{
		{Type: "stat", Title: "待绑定"},               // no field/agg
		{Type: "chart", ChartType: "bar", Title: "图"}, // no x/y
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("deferred binding should be valid, got: %v", err)
	}
}

func TestValidateRejections(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*dsl.AppDefinition)
	}{
		{"empty id", func(d *dsl.AppDefinition) { d.ID = "" }},
		{"bad id charset", func(d *dsl.AppDefinition) { d.ID = "bad id!" }},
		{"empty name", func(d *dsl.AppDefinition) { d.Name = "" }},
		{"bad type", func(d *dsl.AppDefinition) { d.Type = "nope" }},
		{"no components", func(d *dsl.AppDefinition) { d.UI.Components = nil }},
		{"bad component type", func(d *dsl.AppDefinition) { d.UI.Components[0].Type = "weird" }},
		{"bad agg", func(d *dsl.AppDefinition) { d.UI.Components[0].Agg = "p99" }},
		{"chart missing chartType", func(d *dsl.AppDefinition) { d.UI.Components[0] = dsl.Component{Type: "chart"} }},
		{"bad chartType", func(d *dsl.AppDefinition) { d.UI.Components[0] = dsl.Component{Type: "chart", ChartType: "radar"} }},
		{"bad y.agg", func(d *dsl.AppDefinition) {
			d.UI.Components[0] = dsl.Component{Type: "chart", ChartType: "bar", Y: &dsl.AggSpec{Agg: "p99"}}
		}},
		{"action missing trigger", func(d *dsl.AppDefinition) { d.Actions = []dsl.Action{{ID: "a", Do: "notify"}} }},
		{"action missing do", func(d *dsl.AppDefinition) { d.Actions = []dsl.Action{{ID: "a", Trigger: "button"}} }},
		{"action bad do", func(d *dsl.AppDefinition) { d.Actions = []dsl.Action{{ID: "a", Trigger: "button", Do: "hack"}} }},
	}
	for _, c := range cases {
		d := validBase()
		c.mut(&d)
		if err := d.Validate(); err == nil {
			t.Errorf("%s: expected a validation error, got nil", c.name)
		}
	}
}

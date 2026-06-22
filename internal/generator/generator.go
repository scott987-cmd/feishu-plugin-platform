// Package generator turns a user request into a validated AppDefinition (DSL).
// Two tracks, mirroring the plan:
//
//   - template: deterministic fill of a known parameterized template (zero
//     hallucination). This is the main track and covers ~80% of needs.
//   - nl: natural-language. Phase 0 ships a deterministic keyword heuristic that
//     routes to a template; the Claude seam (see generateWithClaude) is where a
//     real LLM call goes — constrained to emit DSL, never arbitrary code.
//
// Whatever the track, output is run through dsl.Validate before returning.
package generator

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// Request is the generation input accepted by POST /generate.
type Request struct {
	Mode     string            `json:"mode"` // template | nl
	Template string            `json:"template,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Prompt   string            `json:"prompt,omitempty"`
	Bind     dsl.Bind          `json:"bind,omitempty"`
}

// Templates lists the parameterized templates the form-based track exposes.
var Templates = []string{"stat_card", "bar_chart", "sales_dashboard"}

// Generate dispatches on Mode and returns a validated AppDefinition.
func Generate(req Request) (dsl.AppDefinition, error) {
	var (
		def dsl.AppDefinition
		err error
	)
	switch req.Mode {
	case "template":
		def, err = fromTemplate(req)
	case "nl":
		def, err = fromNL(req)
	default:
		return dsl.AppDefinition{}, fmt.Errorf("unknown mode %q (want template|nl)", req.Mode)
	}
	if err != nil {
		return dsl.AppDefinition{}, err
	}
	if def.Bind == (dsl.Bind{}) {
		def.Bind = req.Bind
	}
	if def.Bind.BaseID == "" {
		def.Bind.BaseID = "current"
	}
	if err := def.Validate(); err != nil {
		return dsl.AppDefinition{}, fmt.Errorf("generated definition failed validation: %w", err)
	}
	return def, nil
}

func fromTemplate(req Request) (dsl.AppDefinition, error) {
	p := req.Params
	switch req.Template {
	case "stat_card":
		title := param(p,"title", "统计")
		return dsl.AppDefinition{
			ID:   slug(p["id"], title),
			Name: title,
			Type: "view_extension",
			UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{{
				Type:   "stat",
				Title:  title,
				Agg:    param(p,"agg", "sum"),
				Field:  p["field"],
				Filter: p["filter"],
			}}},
		}, nil
	case "bar_chart":
		title := param(p,"title", "图表")
		return dsl.AppDefinition{
			ID:   slug(p["id"], title),
			Name: title,
			Type: "view_extension",
			UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{{
				Type:      "chart",
				Title:     title,
				ChartType: param(p,"chartType", "bar"),
				X:         p["x"],
				Y:         &dsl.AggSpec{Agg: param(p,"yAgg", "sum"), Field: p["yField"]},
			}}},
		}, nil
	case "sales_dashboard":
		title := param(p,"title", "销售看板")
		return dsl.AppDefinition{
			ID:   slug(p["id"], title),
			Name: title,
			Type: "view_extension",
			UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{
				{Type: "stat", Title: "本月销售额", Agg: "sum", Field: param(p,"amountField", "金额"), Filter: "month(下单时间)=THIS_MONTH"},
				{Type: "chart", Title: "销售员业绩", ChartType: "bar", X: param(p,"groupField", "销售员"), Y: &dsl.AggSpec{Agg: "sum", Field: param(p,"amountField", "金额")}},
			}},
			Actions: []dsl.Action{{ID: "export", Trigger: "button", Label: "导出 Excel", Do: "exportXlsx", Scope: "currentView"}},
		}, nil
	default:
		return dsl.AppDefinition{}, fmt.Errorf("unknown template %q (want one of: %s)", req.Template, strings.Join(Templates, ", "))
	}
}

// fromNL is the deterministic Phase-0 stand-in for the LLM track. It routes the
// prompt to a template by keyword. Replace/augment with generateWithClaude.
func fromNL(req Request) (dsl.AppDefinition, error) {
	if d, ok, err := generateWithLLM(req.Prompt); ok {
		return d, err
	}
	prompt := strings.ToLower(req.Prompt)
	switch {
	case containsAny(prompt, "图", "chart", "柱", "趋势", "bar", "line"):
		return fromTemplate(Request{Template: "bar_chart", Params: map[string]string{"title": firstLine(req.Prompt)}})
	case containsAny(prompt, "看板", "dashboard", "销售", "业绩"):
		return fromTemplate(Request{Template: "sales_dashboard", Params: map[string]string{"title": firstLine(req.Prompt)}})
	default:
		return fromTemplate(Request{Template: "stat_card", Params: map[string]string{"title": firstLine(req.Prompt)}})
	}
}

// param returns m[key] or fallback when empty.
func param(m map[string]string, key, fallback string) string {
	if v, ok := m[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n。.;；"); i > 0 {
		s = s[:i]
	}
	if s == "" {
		return "未命名应用"
	}
	return s
}

// slug builds a stable, URL-safe id. Prefers an explicit id, else derives from
// the name; non-ASCII names fall back to a fixed prefix so the id stays valid.
func slug(explicit, name string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	if asciiSlug := strings.Trim(b.String(), "-"); asciiSlug != "" {
		return "app-" + asciiSlug
	}
	// Non-ASCII name (e.g. Chinese): derive a stable short id from the name so
	// different names get distinct ids and the same name maps to an update.
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return fmt.Sprintf("app-%08x", h.Sum32())
}

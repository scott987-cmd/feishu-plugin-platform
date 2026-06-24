package execrt

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// TestRoundMirrorsToFixed locks the interpreter's round() to the compiled JS helper
// Number(n.toFixed(d)). The same DSL runs on BOTH the basekit-on-FaaS path (toFixed)
// and this interpreter, so a divergence writes a different number for the same input.
func TestRoundMirrorsToFixed(t *testing.T) {
	cases := []struct {
		n    float64
		d    int
		want float64
	}{
		{2.675, 2, 2.67}, // float artifact: math.Round gives 2.68 (wrong)
		{2.5, 0, 3},      // exact tie -> larger magnitude: strconv half-even gives 2 (wrong)
		{0.5, 0, 1},
		{1.5, 0, 2},
		{-2.5, 0, -3}, // sign restored after half-up on magnitude
		{8.575, 2, 8.57},
		{1.005, 2, 1.0},
		{123.456, 2, 123.46},
		{0, 0, 0},
		{2, 2, 2},
	}
	for _, c := range cases {
		if got := roundTo(c.n, c.d); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("roundTo(%v, %d) = %v, want %v (JS toFixed)", c.n, c.d, got, c.want)
		}
	}
}

// TestUrlResultWrappedAsLinkCell locks Url-property parity: the compiler writes a Url
// column as a Base URL cell { text, link } (shortcut.renderPropValue), so the self-hosted
// interpreter must too — otherwise the same DSL yields a plain string here and a clickable
// link on FaaS.
func TestUrlResultWrappedAsLinkCell(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"link": "https://x.test/foo"})
	}))
	defer srv.Close()

	dsl := shortcut.FieldShortcut{
		ID:        "urlcell",
		Title:     shortcut.I18n{ZhCN: "x"},
		Domains:   []string{hostNoPort(srv.URL)},
		FormItems: []shortcut.FormItem{{Key: "q", Label: shortcut.I18n{ZhCN: "q"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true}},
		Execute:   shortcut.Execute{URL: srv.URL + "/?q={q}", Method: "GET"},
		Result: shortcut.Result{Kind: "object", Properties: []shortcut.ResultProp{
			{Key: "label", Type: "Text", Primary: true, Expr: "'link'"}, // primary must be Text/Number
			{Key: "u", Type: "Url", Expr: "res.link"},
		}},
	}
	out, err := testEngine().Run(context.Background(), dsl, map[string]any{"q": "x"}, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	cell, ok := out["u"].(map[string]any)
	if !ok {
		t.Fatalf("Url result = %T (%v), want map{text,link}", out["u"], out["u"])
	}
	if cell["text"] != "https://x.test/foo" || cell["link"] != "https://x.test/foo" {
		t.Errorf("Url cell = %v, want text==link==https://x.test/foo", cell)
	}
}

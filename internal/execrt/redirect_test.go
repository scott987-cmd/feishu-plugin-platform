package execrt

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

func redirectDSL(base, host string) shortcut.FieldShortcut {
	return shortcut.FieldShortcut{
		ID:        "redir",
		Title:     shortcut.I18n{ZhCN: "x"},
		Domains:   []string{host},
		FormItems: []shortcut.FormItem{{Key: "q", Label: shortcut.I18n{ZhCN: "q"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true}},
		Execute:   shortcut.Execute{URL: base + "/start?q={q}", Method: "GET"},
		Result: shortcut.Result{Kind: "object", Properties: []shortcut.ResultProp{
			{Key: "v", Type: "Number", Primary: true, Expr: "res.v"},
		}},
	}
}

// TestRedirectToOffAllowlistHostBlocked is the regression test for the headline exfil
// control: an allowlisted host that 302s to an off-allowlist host must NOT be followed.
// The dial-time IP guard only blocks PRIVATE targets, so without per-hop allowlist
// re-validation any plugin becomes a confused-deputy exfil channel for the injected
// credential. This control previously shipped untested.
func TestRedirectToOffAllowlistHostBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example.com/pwned", http.StatusFound)
	}))
	defer srv.Close()

	_, err := testEngine().Run(context.Background(), redirectDSL(srv.URL, hostNoPort(srv.URL)), map[string]any{"q": "x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "redirect blocked") {
		t.Fatalf("expected off-allowlist redirect to be blocked, got %v", err)
	}
}

// TestRedirectSameHostFollowed proves the guard is not over-broad: a redirect that stays
// on the allowlisted host is followed normally.
func TestRedirectSameHostFollowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"v": 42.0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out, err := testEngine().Run(context.Background(), redirectDSL(srv.URL, hostNoPort(srv.URL)), map[string]any{"q": "x"}, nil)
	if err != nil {
		t.Fatalf("same-host redirect should succeed, got %v", err)
	}
	if v, _ := toNum(out["v"]); math.Abs(v-42) > 1e-9 {
		t.Errorf("v = %v, want 42 (same-host redirect not followed)", out["v"])
	}
}

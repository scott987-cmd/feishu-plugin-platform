package execrt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// fakeRecorder collects egress events for assertions (safe for concurrent use).
type fakeRecorder struct {
	mu     sync.Mutex
	events []EgressEvent
}

func (f *fakeRecorder) RecordEgress(_ context.Context, e EgressEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeRecorder) snapshot() []EgressEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]EgressEvent(nil), f.events...)
}

func recordingEngine(rec EgressRecorder) *Engine {
	return New(Options{AllowPrivate: true, Recorder: rec})
}

// TestEgressRecordedPerStep proves one event is emitted per outbound hop, with the
// host, step, and a successful outcome — the per-call egress ledger the runner persists.
func TestEgressRecordedPerStep(t *testing.T) {
	srv := mockWeatherServer()
	defer srv.Close()

	rec := &fakeRecorder{}
	out, err := recordingEngine(rec).Run(context.Background(), weatherDSL(srv.URL), map[string]any{"city": "Beijing"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["temperature"] == nil {
		t.Fatalf("expected a result, got %v", out)
	}
	ev := rec.snapshot()
	if len(ev) != 2 { // two-step pipeline → two outbound hops
		t.Fatalf("got %d egress events, want 2 (one per step): %+v", len(ev), ev)
	}
	for _, e := range ev {
		if e.Outcome != "allowed" || e.Host != hostNoPort(srv.URL) || e.PluginID != "city-weather" {
			t.Errorf("event = %+v, want allowed/%s/city-weather", e, hostNoPort(srv.URL))
		}
	}
	if ev[0].Step != "geo" || ev[1].Step != "weather" {
		t.Errorf("steps = %q,%q, want geo,weather", ev[0].Step, ev[1].Step)
	}
}

// TestEgressPluginIDOverride proves the runner-supplied plugin id (via WithPluginID)
// is used for attribution instead of the shortcut id.
func TestEgressPluginIDOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"main": map[string]any{"temp": 21.0}})
	}))
	defer srv.Close()

	dsl := shortcut.FieldShortcut{
		ID:        "owm",
		Title:     shortcut.I18n{ZhCN: "x"},
		Domains:   []string{hostNoPort(srv.URL)},
		FormItems: []shortcut.FormItem{{Key: "city", Label: shortcut.I18n{ZhCN: "城市"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true}},
		Execute:   shortcut.Execute{URL: srv.URL + "/weather?q={city}", Method: "GET"},
		Result: shortcut.Result{Kind: "object", Properties: []shortcut.ResultProp{
			{Key: "t", Type: "Number", Primary: true, Expr: "res.main.temp"},
		}},
	}
	rec := &fakeRecorder{}
	ctx := WithPluginID(context.Background(), "pl_platform_123")
	if _, err := recordingEngine(rec).Run(ctx, dsl, map[string]any{"city": "x"}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ev := rec.snapshot()
	if len(ev) != 1 || ev[0].PluginID != "pl_platform_123" {
		t.Errorf("events = %+v, want one attributed to pl_platform_123", ev)
	}
}

// TestEgressRecordsBlockedRedirect proves a refused redirect (off-allowlist host) is
// captured in the ledger as an "error" outcome — the exfil-attempt evidence.
func TestEgressRecordsBlockedRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example.com/pwned", http.StatusFound)
	}))
	defer srv.Close()

	rec := &fakeRecorder{}
	_, err := recordingEngine(rec).Run(context.Background(), redirectDSL(srv.URL, hostNoPort(srv.URL)), map[string]any{"q": "x"}, nil)
	if err == nil {
		t.Fatal("expected the blocked redirect to fail Run")
	}
	ev := rec.snapshot()
	if len(ev) != 1 || ev[0].Outcome != "error" {
		t.Fatalf("events = %+v, want one error event for the blocked redirect", ev)
	}
}

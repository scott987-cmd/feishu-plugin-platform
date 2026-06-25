package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

// fakeRunner stands in for the execute-runner: it records the forwarded payload
// and returns a fixed mapped result.
func fakeRunner(t *testing.T, gotAuth *string, gotBody *map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/execute" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		raw, _ := io.ReadAll(r.Body)
		if gotBody != nil {
			_ = json.Unmarshal(raw, gotBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"temperature": 28.8}})
	}))
}

func TestExecuteDisabledWhenNoRunner(t *testing.T) {
	ts := newTestServer(Config{}) // no ExecuteRunnerURL
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/execute", "application/json", strings.NewReader(`{"dsl":{},"inputs":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when runner unconfigured", resp.StatusCode)
	}
}

func TestExecuteForwardsInlineDSL(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	runner := fakeRunner(t, &gotAuth, &gotBody)
	defer runner.Close()

	ts := newTestServer(Config{ExecuteRunnerURL: runner.URL, ExecuteRunnerToken: "rtok"})
	defer ts.Close()

	body := `{"dsl":{"id":"city-weather","title":{"zh_CN":"天气"}},"inputs":{"city":"Beijing"},"auth":{"k":"v"}}`
	resp, err := http.Post(ts.URL+"/api/execute", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if !out.OK || out.Data["temperature"] != 28.8 {
		t.Errorf("response = %+v, want ok+temperature 28.8", out)
	}
	// Forwarded with the runner's bearer + the inline dsl/inputs.
	if gotAuth != "Bearer rtok" {
		t.Errorf("runner saw auth %q, want 'Bearer rtok'", gotAuth)
	}
	if gotBody["dsl"] == nil || gotBody["inputs"] == nil {
		t.Errorf("forwarded body missing dsl/inputs: %+v", gotBody)
	}
}

func TestExecuteForwardsPluginID(t *testing.T) {
	var gotBody map[string]any
	runner := fakeRunner(t, nil, &gotBody)
	defer runner.Close()
	ts := newTestServer(Config{ExecuteRunnerURL: runner.URL})
	defer ts.Close()

	// Inline DSL + a platform plugin id: the id must be forwarded so the runner can
	// attribute egress-ledger events to it (not just the shortcut's own id).
	body := `{"pluginId":"pl_platform_9","dsl":{"id":"city-weather","title":{"zh_CN":"天气"}},"inputs":{}}`
	resp, err := http.Post(ts.URL+"/api/execute", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gotBody["pluginId"] != "pl_platform_9" {
		t.Errorf("runner saw pluginId %v, want pl_platform_9", gotBody["pluginId"])
	}
}

func TestExecuteRequiresDSLOrPluginID(t *testing.T) {
	runner := fakeRunner(t, nil, nil)
	defer runner.Close()
	ts := newTestServer(Config{ExecuteRunnerURL: runner.URL})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/execute", "application/json", strings.NewReader(`{"inputs":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when neither dsl nor pluginId", resp.StatusCode)
	}
}

func TestExecutePluginIDRequiresLogin(t *testing.T) {
	runner := fakeRunner(t, nil, nil)
	defer runner.Close()
	// plugins store enabled but no session → pluginId path must 401.
	h := New(store.NewMemory(), Config{ExecuteRunnerURL: runner.URL}).
		WithPlugins(store.NewMemoryPluginStore()).Routes()
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/execute", "application/json", strings.NewReader(`{"pluginId":"p1","inputs":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// With plugins set but authn nil, currentUser is false → 401.
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 401 (no session) or 503 (store disabled)", resp.StatusCode)
	}
}

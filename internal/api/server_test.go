package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

func validDefJSON() string {
	d := dsl.AppDefinition{
		ID: "app-x", Name: "x", Type: "view_extension",
		UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{{Type: "stat", Title: "t"}}},
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func newTestServer(cfg Config) *httptest.Server {
	return httptest.NewServer(New(store.NewMemory(), cfg).Routes())
}

// do issues a request and returns the status code.
func do(t *testing.T, method, url, token, body string) int {
	t.Helper()
	var r *http.Request
	var err error
	if body != "" {
		r, err = http.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestReadyzAndHealthz(t *testing.T) {
	ts := newTestServer(Config{})
	defer ts.Close()
	if c := do(t, "GET", ts.URL+"/healthz", "", ""); c != 200 {
		t.Errorf("healthz = %d, want 200", c)
	}
	if c := do(t, "GET", ts.URL+"/readyz", "", ""); c != 200 {
		t.Errorf("readyz = %d, want 200", c)
	}
}

func TestAuthEnforced(t *testing.T) {
	ts := newTestServer(Config{APIToken: "secret"})
	defer ts.Close()
	// health/readiness stay open (probes).
	if c := do(t, "GET", ts.URL+"/healthz", "", ""); c != 200 {
		t.Errorf("healthz should be open, got %d", c)
	}
	// /api/* requires the token.
	if c := do(t, "GET", ts.URL+"/api/apps", "", ""); c != 401 {
		t.Errorf("no token = %d, want 401", c)
	}
	if c := do(t, "GET", ts.URL+"/api/apps", "wrong", ""); c != 401 {
		t.Errorf("bad token = %d, want 401", c)
	}
	if c := do(t, "GET", ts.URL+"/api/apps", "secret", ""); c != 200 {
		t.Errorf("good token = %d, want 200", c)
	}
}

func TestAuthOpenWhenUnset(t *testing.T) {
	ts := newTestServer(Config{}) // no APIToken
	defer ts.Close()
	if c := do(t, "GET", ts.URL+"/api/apps", "", ""); c != 200 {
		t.Errorf("unauth mode /api/apps = %d, want 200", c)
	}
}

func TestGenerateRateLimit(t *testing.T) {
	gen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validDefJSON()))
	}))
	defer gen.Close()
	ts := newTestServer(Config{GenURL: gen.URL, GenerateRPM: 1})
	defer ts.Close()
	body := `{"mode":"template","template":"stat_card"}`
	if c := do(t, "POST", ts.URL+"/api/generate", "", body); c != 200 {
		t.Errorf("first generate = %d, want 200", c)
	}
	if c := do(t, "POST", ts.URL+"/api/generate", "", body); c != 429 {
		t.Errorf("second generate = %d, want 429 (rate limited)", c)
	}
}

func TestCRUDValidation(t *testing.T) {
	ts := newTestServer(Config{})
	defer ts.Close()
	// invalid: empty components -> 422
	bad := `{"id":"bad","name":"x","type":"view_extension","ui":{"layout":"dashboard","components":[]}}`
	if c := do(t, "POST", ts.URL+"/api/apps", "", bad); c != 422 {
		t.Errorf("invalid put = %d, want 422", c)
	}
	// valid -> 200, then GET -> 200
	if c := do(t, "POST", ts.URL+"/api/apps", "", validDefJSON()); c != 200 {
		t.Errorf("valid put = %d, want 200", c)
	}
	if c := do(t, "GET", ts.URL+"/api/apps/app-x", "", ""); c != 200 {
		t.Errorf("get = %d, want 200", c)
	}
	if c := do(t, "GET", ts.URL+"/api/apps/missing", "", ""); c != 404 {
		t.Errorf("get missing = %d, want 404", c)
	}
}

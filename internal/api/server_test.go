package api

import (
	"context"
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

// TestTokenCapabilitySplit verifies B1: the read-only token can render (GET
// /api/apps, POST /api/execute) but CANNOT mutate the catalog (POST/DELETE
// /api/apps) or drive the paid generate endpoints — only the admin token can.
func TestTokenCapabilitySplit(t *testing.T) {
	ts := newTestServer(Config{APIToken: "admin", ReadToken: "readonly"})
	defer ts.Close()

	// read token: reads OK, mutations + generate rejected.
	if c := do(t, "GET", ts.URL+"/api/apps", "readonly", ""); c != 200 {
		t.Errorf("read token GET /api/apps = %d, want 200", c)
	}
	if c := do(t, "POST", ts.URL+"/api/apps", "readonly", validDefJSON()); c != 401 {
		t.Errorf("read token POST /api/apps = %d, want 401 (admin-only)", c)
	}
	if c := do(t, "DELETE", ts.URL+"/api/apps/app-x", "readonly", ""); c != 401 {
		t.Errorf("read token DELETE /api/apps = %d, want 401 (admin-only)", c)
	}
	if c := do(t, "POST", ts.URL+"/api/generate", "readonly", `{"mode":"template","template":"sales_dashboard"}`); c != 401 {
		t.Errorf("read token POST /api/generate = %d, want 401 (admin/session-only)", c)
	}
	// read token may reach /api/execute (auth passes; 503 because no runner configured, NOT 401).
	if c := do(t, "POST", ts.URL+"/api/execute", "readonly", `{"dsl":{},"inputs":{}}`); c == 401 {
		t.Errorf("read token POST /api/execute = 401, want auth to pass (any non-401)")
	}

	// admin token: full access.
	if c := do(t, "POST", ts.URL+"/api/apps", "admin", validDefJSON()); c != 200 {
		t.Errorf("admin token POST /api/apps = %d, want 200", c)
	}
	if c := do(t, "DELETE", ts.URL+"/api/apps/app-x", "admin", ""); c != 204 {
		t.Errorf("admin token DELETE /api/apps = %d, want 204", c)
	}

	// wrong token: rejected everywhere.
	if c := do(t, "GET", ts.URL+"/api/apps", "nope", ""); c != 401 {
		t.Errorf("bad token GET /api/apps = %d, want 401", c)
	}

	// Dirty-path evasion: a doubled/relative slash must NOT skip auth. With the
	// read token, POST //api/apps and /./api/apps must be auth-checked, never
	// dispatched to the mutating handler (200/204).
	for _, p := range []string{"//api/apps", "/./api/apps"} {
		if c := do(t, "POST", ts.URL+p, "readonly", validDefJSON()); c == 200 || c == 204 {
			t.Errorf("read token POST %s = %d, want auth-checked (not a successful mutation)", p, c)
		}
	}
}

// TestListScopedByTable verifies GET /api/apps?tableId= returns only the apps
// bound to that table (B2: no full-catalog download / read-leak to the widget).
func TestListScopedByTable(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	mk := func(id, tbl string) dsl.AppDefinition {
		return dsl.AppDefinition{
			ID: id, Name: id, Type: "view_extension", Bind: dsl.Bind{TableID: tbl},
			UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{{Type: "stat", Title: "t"}}},
		}
	}
	for _, d := range []dsl.AppDefinition{mk("a", "tbl1"), mk("b", "tbl2"), mk("c", "tbl1")} {
		if _, err := st.Put(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(New(st, Config{}).Routes())
	defer ts.Close()

	get := func(path string) []dsl.AppDefinition {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out []dsl.AppDefinition
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	if all := get("/api/apps"); len(all) != 3 {
		t.Errorf("GET /api/apps = %d apps, want 3", len(all))
	}
	scoped := get("/api/apps?tableId=tbl1")
	if len(scoped) != 2 {
		t.Fatalf("GET /api/apps?tableId=tbl1 = %d apps, want 2", len(scoped))
	}
	for _, d := range scoped {
		if d.Bind.TableID != "tbl1" {
			t.Errorf("scoped result leaked app bound to %q", d.Bind.TableID)
		}
	}
}

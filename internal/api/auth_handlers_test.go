package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/auth"
	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

func authedServer(t *testing.T) (http.Handler, *auth.Authenticator) {
	t.Helper()
	a, ok := auth.New(auth.Config{
		AppID: "cli_x", AppSecret: "sec", RedirectURI: "https://h/auth/callback",
		SessionSecret: []byte("test-key"), SessionTTL: time.Hour,
	})
	if !ok {
		t.Fatal("auth disabled")
	}
	h := New(store.NewMemory(), Config{}).WithAuth(a).WithPlugins(store.NewMemoryPluginStore()).Routes()
	return h, a
}

func req(t *testing.T, h http.Handler, method, path, cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMeAnonymousVsLoggedIn(t *testing.T) {
	h, a := authedServer(t)
	w := req(t, h, "GET", "/api/me", "", "")
	if !strings.Contains(w.Body.String(), `"logged_in":false`) {
		t.Errorf("anon /api/me should be logged_in:false, got %s", w.Body.String())
	}
	cookie := a.Sign(auth.User{OpenID: "ou_alice", Name: "Alice"})
	w = req(t, h, "GET", "/api/me", cookie, "")
	if !strings.Contains(w.Body.String(), `"logged_in":true`) || !strings.Contains(w.Body.String(), "Alice") {
		t.Errorf("logged-in /api/me wrong: %s", w.Body.String())
	}
}

func TestMyPluginsSaveListIsolation(t *testing.T) {
	h, a := authedServer(t)
	alice := a.Sign(auth.User{OpenID: "ou_alice", Name: "Alice"})
	bob := a.Sign(auth.User{OpenID: "ou_bob", Name: "Bob"})

	// unauthenticated save → 401
	if w := req(t, h, "POST", "/api/my/plugins", "", `{"kind":"field","dsl":{}}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("anon save should 401, got %d", w.Code)
	}

	// Alice saves one
	w := req(t, h, "POST", "/api/my/plugins", alice, `{"title":"汇率","kind":"field","dsl":{"id":"fx"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("alice save: %d %s", w.Code, w.Body.String())
	}
	var saved store.PluginRecord
	_ = json.Unmarshal(w.Body.Bytes(), &saved)
	if saved.ID == "" || saved.Owner.OpenID != "ou_alice" {
		t.Fatalf("saved record wrong: %+v", saved)
	}

	// Alice lists → sees it; Bob lists → empty (isolation)
	if w := req(t, h, "GET", "/api/my/plugins", alice, ""); !strings.Contains(w.Body.String(), "汇率") {
		t.Errorf("alice should see her plugin: %s", w.Body.String())
	}
	if w := req(t, h, "GET", "/api/my/plugins", bob, ""); strings.Contains(w.Body.String(), "汇率") {
		t.Errorf("bob must NOT see alice's plugin: %s", w.Body.String())
	}

	// bad kind rejected
	if w := req(t, h, "POST", "/api/my/plugins", alice, `{"kind":"nope","dsl":{}}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad kind should 400, got %d", w.Code)
	}

	// Alice deletes; then her list is empty
	req(t, h, "DELETE", "/api/my/plugins/"+saved.ID, alice, "")
	if w := req(t, h, "GET", "/api/my/plugins", alice, ""); strings.Contains(w.Body.String(), "汇率") {
		t.Errorf("after delete alice list should be empty: %s", w.Body.String())
	}
}

func TestLoginRedirects(t *testing.T) {
	h, _ := authedServer(t)
	w := req(t, h, "GET", "/auth/login", "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("login should 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "accounts.feishu.cn/open-apis/authen/v1/authorize") {
		t.Errorf("login should redirect to Feishu authorize, got %s", loc)
	}
}

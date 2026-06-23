package auth

import (
	"strings"
	"testing"
	"time"
)

func testAuth(t *testing.T) *Authenticator {
	t.Helper()
	a, ok := New(Config{
		AppID: "cli_x", AppSecret: "sec", BaseDomain: "feishu.cn",
		RedirectURI: "https://host/auth/callback", SessionSecret: []byte("test-secret-key"),
		SessionTTL: time.Hour,
	})
	if !ok {
		t.Fatal("New should enable with full config")
	}
	return a
}

func TestNewDisabledWithoutConfig(t *testing.T) {
	if _, ok := New(Config{}); ok {
		t.Error("New must be disabled without config")
	}
	if _, ok := New(Config{AppID: "x", AppSecret: "y", RedirectURI: "z"}); ok {
		t.Error("New must be disabled without a session secret")
	}
}

func TestAuthorizeURL(t *testing.T) {
	a := testAuth(t)
	u := a.AuthorizeURL("st8")
	for _, want := range []string{
		"https://accounts.feishu.cn/open-apis/authen/v1/authorize?",
		"client_id=cli_x", "response_type=code", "state=st8",
		"redirect_uri=https%3A%2F%2Fhost%2Fauth%2Fcallback",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("authorize URL missing %q\n got: %s", want, u)
		}
	}
}

func TestSessionRoundTrip(t *testing.T) {
	a := testAuth(t)
	u := User{OpenID: "ou_123", Name: "张三"}
	got, ok := a.Verify(a.Sign(u))
	if !ok || got.OpenID != "ou_123" || got.Name != "张三" {
		t.Fatalf("round-trip failed: ok=%v got=%+v", ok, got)
	}
}

func TestSessionRejectsTampering(t *testing.T) {
	a := testAuth(t)
	val := a.Sign(User{OpenID: "ou_1", Name: "a"})
	// flip a char in the payload → signature must fail
	bad := "x" + val[1:]
	if _, ok := a.Verify(bad); ok {
		t.Error("tampered payload must be rejected")
	}
	// a different key must not verify
	other, _ := New(Config{AppID: "x", AppSecret: "y", RedirectURI: "z", SessionSecret: []byte("different"), SessionTTL: time.Hour})
	if _, ok := other.Verify(val); ok {
		t.Error("session signed with a different key must be rejected")
	}
	if _, ok := a.Verify("garbage"); ok {
		t.Error("garbage must be rejected")
	}
}

func TestSessionExpiry(t *testing.T) {
	a, _ := New(Config{AppID: "x", AppSecret: "y", RedirectURI: "z", SessionSecret: []byte("k"), SessionTTL: -time.Second})
	if _, ok := a.Verify(a.Sign(User{OpenID: "ou_1"})); ok {
		t.Error("expired session must be rejected")
	}
}

func TestNewStateUnique(t *testing.T) {
	if NewState() == NewState() {
		t.Error("state should be random")
	}
}

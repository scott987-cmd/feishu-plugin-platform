// Package auth gives the web platform a per-user Feishu identity: a standard
// OAuth authorization-code login, and a stateless HMAC-signed session cookie so
// every generated plugin can be attributed to and owned by the individual who
// created it. Stdlib-only; the Feishu HTTP client deliberately bypasses any
// proxy (domestic endpoint), matching internal/store.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// User is the minimal identity the platform needs: a stable Feishu open_id and a
// display name for attribution / "my plugins".
type User struct {
	OpenID string `json:"open_id"`
	Name   string `json:"name"`
}

// Config configures the OAuth + session layer.
type Config struct {
	AppID         string // Feishu App ID
	AppSecret     string // Feishu App Secret
	BaseDomain    string // e.g. feishu.cn (private deploy: a custom domain)
	RedirectURI   string // absolute callback URL registered in the Feishu app, e.g. https://host/auth/callback
	SessionSecret []byte // HMAC key for signing the session cookie
	SessionTTL    time.Duration
}

// Authenticator performs the OAuth flow and issues/verifies sessions.
type Authenticator struct {
	cfg  Config
	http *http.Client
}

// New builds an Authenticator. Returns false (disabled) when required config is
// absent, so the platform stays usable (anonymous) without login configured.
func New(cfg Config) (*Authenticator, bool) {
	if cfg.AppID == "" || cfg.AppSecret == "" || cfg.RedirectURI == "" || len(cfg.SessionSecret) == 0 {
		return nil, false
	}
	if cfg.BaseDomain == "" {
		cfg.BaseDomain = "feishu.cn"
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 7 * 24 * time.Hour
	}
	return &Authenticator{
		cfg:  cfg,
		http: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}},
	}, true
}

// AuthorizeURL builds the Feishu authorization-page URL for the login redirect.
func (a *Authenticator) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", a.cfg.AppID)
	q.Set("redirect_uri", a.cfg.RedirectURI)
	q.Set("response_type", "code")
	q.Set("state", state)
	return fmt.Sprintf("https://accounts.%s/open-apis/authen/v1/authorize?%s", a.cfg.BaseDomain, q.Encode())
}

// Exchange swaps an authorization code for a user access token and resolves the
// user's identity (open_id + name).
func (a *Authenticator) Exchange(ctx context.Context, code string) (User, error) {
	tok, err := a.userAccessToken(ctx, code)
	if err != nil {
		return User{}, err
	}
	return a.userInfo(ctx, tok)
}

func (a *Authenticator) userAccessToken(ctx context.Context, code string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     a.cfg.AppID,
		"client_secret": a.cfg.AppSecret,
		"code":          code,
		"redirect_uri":  a.cfg.RedirectURI,
	})
	endpoint := fmt.Sprintf("https://open.%s/open-apis/authen/v2/oauth/token", a.cfg.BaseDomain)
	raw, err := a.post(ctx, endpoint, "", body)
	if err != nil {
		return "", err
	}
	// v2 returns a flat OAuth2 body; tolerate a {code,msg,data:{access_token}} wrapper too.
	var out struct {
		Code        int    `json:"code"`
		Msg         string `json:"msg"`
		AccessToken string `json:"access_token"`
		Data        struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("oauth token: bad response: %w", err)
	}
	if out.Code != 0 && out.AccessToken == "" && out.Data.AccessToken == "" {
		return "", fmt.Errorf("oauth token: code %d: %s", out.Code, out.Msg)
	}
	if out.AccessToken != "" {
		return out.AccessToken, nil
	}
	if out.Data.AccessToken != "" {
		return out.Data.AccessToken, nil
	}
	return "", errors.New("oauth token: no access_token in response")
}

func (a *Authenticator) userInfo(ctx context.Context, userToken string) (User, error) {
	endpoint := fmt.Sprintf("https://open.%s/open-apis/authen/v1/user_info", a.cfg.BaseDomain)
	raw, err := a.get(ctx, endpoint, userToken)
	if err != nil {
		return User{}, err
	}
	var out struct {
		Code int `json:"code"`
		Msg  string
		Data struct {
			OpenID string `json:"open_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return User{}, fmt.Errorf("user_info: bad response: %w", err)
	}
	if out.Code != 0 || out.Data.OpenID == "" {
		return User{}, fmt.Errorf("user_info: code %d: %s", out.Code, out.Msg)
	}
	return User{OpenID: out.Data.OpenID, Name: out.Data.Name}, nil
}

func (a *Authenticator) post(ctx context.Context, endpoint, bearer string, body []byte) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return a.do(req)
}

func (a *Authenticator) get(ctx context.Context, endpoint, bearer string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return a.do(req)
}

func (a *Authenticator) do(req *http.Request) ([]byte, error) {
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// ── Sessions: HMAC-signed, stateless cookie value "<b64(payload)>.<b64(sig)>" ──

type sessionPayload struct {
	OpenID string `json:"o"`
	Name   string `json:"n"`
	Exp    int64  `json:"e"` // unix seconds
}

// Sign produces the signed cookie value for a user.
func (a *Authenticator) Sign(u User) string {
	p := sessionPayload{OpenID: u.OpenID, Name: u.Name, Exp: time.Now().Add(a.cfg.SessionTTL).Unix()}
	raw, _ := json.Marshal(p)
	b := base64.RawURLEncoding.EncodeToString(raw)
	return b + "." + base64.RawURLEncoding.EncodeToString(a.mac([]byte(b)))
}

// Verify validates a signed cookie value and returns the user if valid + unexpired.
func (a *Authenticator) Verify(value string) (User, bool) {
	b, sigB, ok := strings.Cut(value, ".")
	if !ok {
		return User{}, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB)
	if err != nil || subtle.ConstantTimeCompare(sig, a.mac([]byte(b))) != 1 {
		return User{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(b)
	if err != nil {
		return User{}, false
	}
	var p sessionPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.OpenID == "" {
		return User{}, false
	}
	if time.Now().Unix() > p.Exp {
		return User{}, false
	}
	return User{OpenID: p.OpenID, Name: p.Name}, true
}

func (a *Authenticator) mac(b []byte) []byte {
	h := hmac.New(sha256.New, a.cfg.SessionSecret)
	h.Write(b)
	return h.Sum(nil)
}

// SessionMaxAge is the cookie Max-Age (seconds) callers should set.
func (a *Authenticator) SessionMaxAge() int { return int(a.cfg.SessionTTL.Seconds()) }

// NewState returns a random opaque value for CSRF protection of the OAuth round-trip.
func NewState() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Package api is the platform's BFF/gateway: app-definition CRUD consumed by the
// in-Feishu container plugin, a generation proxy to the generator service, and
// (optionally) the mock renderer in web/.
//
// Production middleware chain: logging → CORS → auth. Auth is a shared bearer
// token (PLATFORM_API_TOKEN); when unset the API is open (dev only) and main
// logs a loud warning. /generate is rate-limited to protect the LLM budget.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/auth"
	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

// readinessTTL caches the readiness result so probe frequency does not amplify
// into backend (Feishu) calls.
const readinessTTL = 10 * time.Second

// Config holds the server's runtime configuration.
type Config struct {
	GenURL        string // generator base URL, e.g. http://generator:8090
	GenToken      string // bearer the generator requires (GENERATOR_TOKEN); sent on every proxied call
	WebDir        string // directory served at "/" when ServeWeb is true
	ServeWeb      bool   // serve the mock renderer (dev); keep false in production
	AllowedOrigin string // CORS origin ("*" for dev)
	APIToken      string // ADMIN/write bearer: required for catalog mutations (POST/DELETE /api/apps) and (with a session) generate. Server-to-server only — never ship it to a browser.
	ReadToken     string // READ-ONLY bearer for the in-Bitable widget + web renderer: grants GET /api/apps* and POST /api/execute only. Safe(r) to embed in a client bundle — leaking it cannot mutate the catalog or spend the LLM budget.
	GenerateRPM   int    // max POST /api/generate per minute (0 = unlimited)

	// ExecuteRunnerURL is the internal base URL of the self-hosted execute-runner
	// (the FaaS replacement). Empty disables POST /api/execute (503). See
	// EXECUTE_RUNTIME.md. ExecuteRunnerToken is the bearer the runner expects.
	ExecuteRunnerURL   string
	ExecuteRunnerToken string
}

// Server holds the API dependencies.
type Server struct {
	store   store.Store
	cfg     Config
	client  *http.Client
	limiter *rateLimiter
	authn   *auth.Authenticator // nil = login disabled (platform stays anonymous)
	plugins store.PluginStore   // per-user owned plugins (nil = ownership disabled)

	readyMu   sync.Mutex
	readyAt   time.Time
	readyErr  error
	readyInit bool
}

// New constructs a Server.
func New(s store.Store, cfg Config) *Server {
	if cfg.AllowedOrigin == "" {
		cfg.AllowedOrigin = "*"
	}
	return &Server{
		store:   s,
		cfg:     cfg,
		client:  &http.Client{Timeout: 60 * time.Second},
		limiter: newRateLimiter(cfg.GenerateRPM),
	}
}

// WithAuth attaches a Feishu OAuth authenticator (enables login + ownership). Chainable.
func (s *Server) WithAuth(a *auth.Authenticator) *Server { s.authn = a; return s }

// WithPlugins attaches the per-user plugin store (enables "my plugins"). Chainable.
func (s *Server) WithPlugins(p store.PluginStore) *Server { s.plugins = p; return s }

// Routes wires handlers and the middleware chain.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth) // liveness
	mux.HandleFunc("GET /readyz", s.handleReady)   // readiness (cheap, cached)
	mux.HandleFunc("GET /api/apps", s.handleList)
	mux.HandleFunc("POST /api/apps", s.handlePut)
	mux.HandleFunc("GET /api/apps/{id}", s.handleGet)
	mux.HandleFunc("DELETE /api/apps/{id}", s.handleDelete)
	mux.HandleFunc("POST /api/generate", s.handleGenerate)
	mux.HandleFunc("POST /api/shortcut/generate", s.handleShortcutGenerate)
	mux.HandleFunc("POST /api/shortcut/zip", s.handleShortcutZip)
	mux.HandleFunc("POST /api/action/generate", func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow() {
			writeErr(w, http.StatusTooManyRequests, "generate rate limit exceeded")
			return
		}
		s.proxyGenerator(w, r, "/action/generate")
	})
	mux.HandleFunc("POST /api/action/zip", func(w http.ResponseWriter, r *http.Request) {
		s.proxyGenerator(w, r, "/action/zip")
	})
	mux.HandleFunc("POST /api/execute", s.handleExecute) // forward to self-hosted execute-runner (call-chain B)
	// Identity (Feishu OAuth) + per-user plugin ownership. Registered only when
	// login is configured; all are no-ops/anonymous otherwise.
	if s.authn != nil {
		mux.HandleFunc("GET /auth/login", s.handleAuthLogin)
		mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)
		mux.HandleFunc("POST /auth/logout", s.handleAuthLogout)
		mux.HandleFunc("GET /api/me", s.handleMe)
		if s.plugins != nil {
			mux.HandleFunc("GET /api/my/plugins", s.handleMyList)
			mux.HandleFunc("POST /api/my/plugins", s.handleMySave)
			mux.HandleFunc("DELETE /api/my/plugins/{id}", s.handleMyDelete)
		}
	}
	if s.cfg.ServeWeb {
		mux.Handle("GET /", http.FileServer(http.Dir(s.cfg.WebDir)))
	}
	return withLogging(s.withCORS(s.withAuth(mux)))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "api"})
}

// handleReady reports readiness using a cheap, TTL-cached backend check so a
// transient backend blip (or probe spam) does not hammer Feishu or flap pods.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.checkReady(r.Context()); err != nil {
		log.Printf("readiness: %v", err) // detail to logs only
		writeErr(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) checkReady(ctx context.Context) error {
	s.readyMu.Lock()
	defer s.readyMu.Unlock()
	if s.readyInit && time.Since(s.readyAt) < readinessTTL {
		return s.readyErr
	}
	var err error
	if p, ok := s.store.(store.Pinger); ok {
		err = p.Ping(ctx) // cheap reachability check (no full scan)
	} else {
		_, err = s.store.List(ctx)
	}
	s.readyAt, s.readyErr, s.readyInit = time.Now(), err, true
	return err
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	defs, err := s.store.List(r.Context())
	if err != nil {
		log.Printf("list: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, defs)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	d, ok, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		log.Printf("get: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// actor identifies who performed a request for audit logs: the logged-in user's
// open_id when a session is present, else the admin token (the only other way to
// reach a mutating route).
func (s *Server) actor(r *http.Request) string {
	if u, ok := s.currentUser(r); ok {
		return "user:" + u.OpenID
	}
	return "admin-token"
}

// clientIP returns the best-effort caller IP (honors a single X-Forwarded-For hop
// from the TLS-terminating proxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	var d dsl.AppDefinition
	if err := readJSON(r, &d); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if err := d.Validate(); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error()) // input error: safe to echo
		return
	}
	stored, err := s.store.Put(r.Context(), d)
	if err != nil {
		log.Printf("put: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	log.Printf("AUDIT actor=%s action=put.app id=%s ip=%s", s.actor(r), stored.ID, clientIP(r))
	writeJSON(w, http.StatusOK, stored)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(r.Context(), id); err != nil {
		log.Printf("delete: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	log.Printf("AUDIT actor=%s action=delete.app id=%s ip=%s", s.actor(r), id, clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleGenerate forwards to the generator service (cancellable via request
// context). Rate-limited to protect the LLM budget. With ?save=true the
// re-validated result is persisted.
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow() {
		writeErr(w, http.StatusTooManyRequests, "generate rate limit exceeded")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read request: "+err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.GenURL+"/generate", bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.GenToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.GenToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("generate proxy: %v", err)
		writeErr(w, http.StatusBadGateway, "generator unreachable")
		return
	}
	defer resp.Body.Close()
	out, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		log.Printf("generate read: %v", readErr)
		writeErr(w, http.StatusBadGateway, "bad generator response")
		return
	}
	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(out)
		return
	}
	var d dsl.AppDefinition
	if err := json.Unmarshal(out, &d); err != nil {
		writeErr(w, http.StatusBadGateway, "bad generator response")
		return
	}
	// Defense in depth: re-validate everything crossing into our store.
	if err := d.Validate(); err != nil {
		writeErr(w, http.StatusBadGateway, "generator returned invalid definition: "+err.Error())
		return
	}
	if r.URL.Query().Get("save") == "true" {
		stored, err := s.store.Put(r.Context(), d)
		if err != nil {
			log.Printf("generate save: %v", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		d = stored
	}
	writeJSON(w, http.StatusOK, d)
}

// handleShortcutGenerate proxies NL → field-shortcut generation to the generator
// service. Rate-limited (it hits the LLM).
func (s *Server) handleShortcutGenerate(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow() {
		writeErr(w, http.StatusTooManyRequests, "generate rate limit exceeded")
		return
	}
	s.proxyGenerator(w, r, "/shortcut/generate")
}

// handleShortcutZip proxies DSL → project-zip rendering (deterministic, no LLM).
func (s *Server) handleShortcutZip(w http.ResponseWriter, r *http.Request) {
	s.proxyGenerator(w, r, "/shortcut/zip")
}

// proxyGenerator forwards the request body to the generator service and streams
// the response back verbatim (status, content-type, content-disposition, body),
// so it works for both JSON and binary (zip) responses.
func (s *Server) proxyGenerator(w http.ResponseWriter, r *http.Request, path string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read request: "+err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.GenURL+path, bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.GenToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.GenToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("shortcut proxy %s: %v", path, err)
		writeErr(w, http.StatusBadGateway, "generator unreachable")
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 8<<20))
}

// ─── middleware ───

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowedOrigin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAuth requires a bearer token on /api/* when APIToken is set. Health,
// readiness and static assets stay open (probes, mock renderer).
// apiCapability classifies an /api/* route by the privilege it requires:
//   - "user":  /api/me + /api/my/* — the handler enforces the session cookie.
//   - "read":  GET /api/apps* and POST /api/execute — render + compute. Accepts the
//     read-only token, the admin token, or a logged-in session.
//   - "admin": POST/DELETE /api/apps — mutating the curated container catalog.
//     Admin token only (the CLI publishes; browsers never do).
//   - "generate": the LLM-backed endpoints — admin token or a logged-in session.
func apiCapability(method, path string) string {
	switch {
	case path == "/api/me" || strings.HasPrefix(path, "/api/my/"):
		return "user"
	case path == "/api/execute":
		return "read"
	case strings.HasPrefix(path, "/api/apps"):
		if method == http.MethodGet {
			return "read"
		}
		return "admin"
	default:
		return "generate"
	}
}

// withAuth gates /api/* by capability (apiCapability) against three credentials:
// the admin token (cfg.APIToken), the read-only token (cfg.ReadToken), and a
// logged-in user session. This replaces the old single shared token so a token
// shipped in a browser bundle can be read-only — leaking it can no longer delete
// or overwrite the catalog, nor drive the paid generate endpoints.
func (s *Server) withAuth(next http.Handler) http.Handler {
	adminWant := []byte("Bearer " + s.cfg.APIToken)
	readWant := []byte("Bearer " + s.cfg.ReadToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalize the path BEFORE any prefix/capability decision: a dirty path
		// like "//api/apps" or "/./api/apps" makes HasPrefix("/api/") false, which
		// would skip auth entirely and rely on the mux's redirect to re-auth — a
		// fragile invariant. path.Clean collapses these so the gate can't be evaded.
		reqPath := path.Clean(r.URL.Path)
		if !strings.HasPrefix(reqPath, "/api/") || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		// No tokens configured at all → dev/anonymous mode (handlers still enforce
		// the session on /api/my/*). Keeps local dev frictionless.
		if s.cfg.APIToken == "" && s.cfg.ReadToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		cap := apiCapability(r.Method, reqPath)
		if cap == "user" {
			next.ServeHTTP(w, r) // session enforced downstream (currentUser)
			return
		}
		got := []byte(r.Header.Get("Authorization"))
		admin := s.cfg.APIToken != "" && subtle.ConstantTimeCompare(got, adminWant) == 1
		read := s.cfg.ReadToken != "" && subtle.ConstantTimeCompare(got, readWant) == 1
		_, session := s.currentUser(r) // currentUser is nil-safe when login is disabled
		ok := false
		switch cap {
		case "read":
			// If no read token is configured, the admin token still satisfies reads
			// (back-compat: widget keeps working until it's rebuilt with the read token).
			ok = admin || read || session
		case "generate":
			ok = admin || session
		case "admin":
			ok = admin
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withLogging logs each request except the frequent probes (/healthz, /readyz).
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// rateLimiter is a token bucket (per process): refills perMin tokens/minute, so
// it enforces a steady rate without the fixed-window boundary burst. Note: with
// N replicas the effective cap is N×perMin — set GENERATE_RPM accordingly.
type rateLimiter struct {
	mu     sync.Mutex
	perMin int
	tokens float64
	max    float64
	last   time.Time
}

func newRateLimiter(perMin int) *rateLimiter {
	return &rateLimiter{perMin: perMin, tokens: float64(perMin), max: float64(perMin), last: time.Now()}
}

func (r *rateLimiter) allow() bool {
	if r.perMin <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.tokens += now.Sub(r.last).Minutes() * float64(r.perMin)
	r.last = now
	if r.tokens > r.max {
		r.tokens = r.max
	}
	if r.tokens < 1 {
		return false
	}
	r.tokens--
	return true
}

// ─── helpers ───

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// readJSON decodes the size-limited body. Unknown fields are tolerated so the DSL
// can grow additively without breaking older servers (forward compatible).
func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

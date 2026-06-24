// Command execute-runner is the self-hosted execute runtime: it interprets a
// field-shortcut DSL at request time (fetch external APIs + map the response)
// for private-deployment customers who have no Feishu basekit FaaS. See
// EXECUTE_RUNTIME.md.
//
// It is the FaaS-replacement: the container renderer (or the api BFF, call-chain
// B) POSTs a (DSL, inputs, auth) triple to /execute and gets back the mapped
// output. The runtime never writes host data, enforces the per-plugin domain
// allowlist on every request, and interprets the allowlisted expression grammar
// rather than eval'ing code.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/execrt"
	"github.com/dushibing/feishu-plugin-platform/internal/httpx"
	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

func main() {
	port := getenv("PORT", "8095")
	token := os.Getenv("PLATFORM_API_TOKEN") // optional bearer; required when set

	eng := execrt.New(execrt.Options{
		Timeout:      durEnv("EXECUTE_TIMEOUT_SECONDS", 10*time.Second),
		MaxBodyBytes: int64(intEnv("EXECUTE_MAX_BODY_BYTES", 1<<20)),
		AllowPrivate: boolEnv("EXECUTE_ALLOW_PRIVATE", false), // SSRF guard; only loosen for local dev
	})
	maxConc := intEnv("EXECUTE_MAX_CONCURRENCY", 64)
	if maxConc < 1 {
		maxConc = 1
	}
	h := &handler{eng: eng, token: token, sem: make(chan struct{}, maxConc)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "execute-runner"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /execute", h.execute)

	if token == "" {
		log.Printf("WARNING: PLATFORM_API_TOKEN not set — /execute is UNAUTHENTICATED (local dev only)")
	}
	srv := httpx.NewServer(":"+port, mux)
	log.Printf("execute-runner listening on :%s (auth=%t, ssrfGuard=%t, maxConcurrency=%d)", port, token != "", !boolEnv("EXECUTE_ALLOW_PRIVATE", false), maxConc)
	if err := httpx.Run(srv); err != nil {
		log.Fatal(err)
	}
}

type handler struct {
	eng   *execrt.Engine
	token string
	sem   chan struct{} // bounds concurrent /execute work; full ⇒ 429 (load shedding)
}

// executeRequest is the /execute contract. DSL is the inline field-shortcut
// definition (call-chain B: the api fetches the stored DSL and forwards it).
// Inputs are the FormItem values (the renderer reads them from host cells); Auth
// holds user-supplied credentials keyed by Auth.ID (used, never stored).
type executeRequest struct {
	PluginID string                 `json:"pluginId,omitempty"`
	DSL      shortcut.FieldShortcut `json:"dsl"`
	Inputs   map[string]any         `json:"inputs"`
	Auth     map[string]string      `json:"auth,omitempty"`
}

func (h *handler) execute(w http.ResponseWriter, r *http.Request) {
	if h.token != "" && !bearerOK(r, h.token) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	// Decode first (cheap, size-limited) so a slow client trickling the body does
	// not hold a concurrency slot — the slot is acquired only around the expensive
	// outbound work below.
	var req executeRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json: " + err.Error()})
		return
	}
	// Bound concurrent executes: each spawns up to MaxSteps outbound fetches, so
	// an unbounded burst would exhaust fds/memory on the shared runner and could
	// turn it into a DDoS amplifier. Shed load with 429 when saturated.
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	default:
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "execute runtime at capacity, retry shortly"})
		return
	}
	// Explicit panic safety: make the slot-release + a clean 500 refactor-proof
	// (today net/http recovers per-connection, but a future goroutine fan-out would
	// not — a panic there would crash the shared runner and leak the slot).
	defer func() {
		if p := recover(); p != nil {
			log.Printf("execute panic: %v", p)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "internal error"})
		}
	}()
	data, err := h.eng.Run(r.Context(), req.DSL, req.Inputs, req.Auth)
	if err != nil {
		// 422: the DSL/inputs/upstream produced a handled failure (not a bug).
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func bearerOK(r *http.Request, want string) bool {
	const p = "Bearer "
	got := r.Header.Get("Authorization")
	if len(got) <= len(p) || got[:len(p)] != p {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got[len(p):]), []byte(want)) == 1
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func intEnv(k string, fallback int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func durEnv(k string, fallback time.Duration) time.Duration {
	if n := intEnv(k, 0); n > 0 {
		return time.Duration(n) * time.Second
	}
	return fallback
}

func boolEnv(k string, fallback bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

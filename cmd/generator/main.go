// Command generator is the generation service: it turns a template+params or a
// natural-language prompt into a validated AppDefinition (DSL). See README.md.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/dushibing/feishu-plugin-platform/internal/generator"
	"github.com/dushibing/feishu-plugin-platform/internal/httpx"
	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// egressHost returns the host NL prompts are sent to for the active provider, for
// the boot-time data-egress transparency log.
func egressHost(provider string) string {
	if provider == "anthropic" {
		return "api.anthropic.com"
	}
	if b := os.Getenv("DEEPSEEK_BASE_URL"); b != "" {
		if p, err := url.Parse(b); err == nil && p.Host != "" {
			return p.Host
		}
		return b
	}
	return "api.deepseek.com"
}

func main() {
	port := getenv("PORT", "8090")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "generator"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /generate", handleGenerate)
	mux.HandleFunc("POST /shortcut/generate", handleShortcutGenerate)
	mux.HandleFunc("POST /shortcut/zip", handleShortcutZip)
	mux.HandleFunc("POST /action/generate", handleActionGenerate)
	mux.HandleFunc("POST /action/zip", handleActionZip)

	provider := getenv("LLM_PROVIDER", "deepseek")
	// Data-egress transparency: make it explicit at boot whether (and where) any
	// natural-language prompt leaves this server. Compliance teams read this line.
	switch {
	case !generator.AIEnabled():
		log.Printf("AI generation DISABLED (AI_ENABLED=false) — NL prompts NEVER leave this server; only templates + the deterministic keyword router are used")
	case !aiConfigured(provider):
		log.Printf("WARNING: no API key for LLM_PROVIDER=%s — AI (nl) generation will fall back to the keyword router", provider)
	default:
		log.Printf("AI generation ON — NL prompts EGRESS to provider=%s endpoint=%s. Only the typed prompt + static exemplars are sent (NOT Bitable row data or credentials). Disable with AI_ENABLED=false; pin a region/self-hosted model with DEEPSEEK_BASE_URL.", provider, egressHost(provider))
	}

	// The generator holds the LLM API key and makes paid calls; it must not be an
	// open relay. When GENERATOR_TOKEN is set, require it on every non-health route
	// (the api BFF sends it). NetworkPolicy is defense-in-depth, not the sole gate
	// — the documented k3s/flannel default does not enforce NetworkPolicy.
	token := os.Getenv("GENERATOR_TOKEN")
	if token == "" {
		log.Printf("WARNING: GENERATOR_TOKEN not set — generator endpoints are UNAUTHENTICATED (anyone who can reach the port can spend the LLM budget)")
	}

	srv := httpx.NewServer(":"+port, authMiddleware(token, mux))
	log.Printf("generator listening on :%s (provider=%s, model=%s, aiConfigured=%t, aiEnabled=%t, auth=%t)",
		port, provider, getenv("MODEL", "deepseek-chat"), aiConfigured(provider), generator.AIEnabled(), token != "")
	if err := httpx.Run(srv); err != nil {
		log.Fatal(err)
	}
}

// aiConfigured reports whether the active provider has its API key set. Readiness
// stays 200 even when false — template generation does not need a key, and NL
// degrades gracefully — but the missing key is logged loudly at startup.
func aiConfigured(provider string) bool {
	switch provider {
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY") != ""
	default:
		return os.Getenv("DEEPSEEK_API_KEY") != ""
	}
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req generator.Request
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	def, err := generator.Generate(req)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, def)
}

// handleShortcutGenerate turns a natural-language request into a field-shortcut
// DSL plus its rendered, auditable src/index.ts (the deliverable customers
// review). Body: {"prompt": "..."}.
func handleShortcutGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	f, ok, err := generator.GenerateShortcut(req.Prompt)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "generation failed: " + err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI generation unavailable (DEEPSEEK_API_KEY not set)"})
		return
	}
	index, err := shortcut.RenderIndexTS(f)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dsl": f, "indexTs": index})
}

// handleShortcutZip renders a (client-supplied, re-validated) field-shortcut DSL
// into a downloadable basekit project archive. No LLM call — deterministic.
func handleShortcutZip(w http.ResponseWriter, r *http.Request) {
	var f shortcut.FieldShortcut
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&f); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	if err := f.Validate(); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	archive, err := shortcut.Zip(f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "zip failed"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.ID+`.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

// handleActionGenerate: NL → automation Action DSL + its rendered src/register.ts.
func handleActionGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	a, ok, err := generator.GenerateAction(req.Prompt)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "generation failed: " + err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI generation unavailable (DEEPSEEK_API_KEY not set)"})
		return
	}
	src, err := shortcut.RenderActionRegisterTS(a)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dsl": a, "registerTs": src})
}

// handleActionZip: re-validated Action DSL → downloadable basekit action project.
func handleActionZip(w http.ResponseWriter, r *http.Request) {
	var a shortcut.Action
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&a); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	if err := a.Validate(); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	archive, err := shortcut.ZipAction(a)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "zip failed"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+a.ID+`-action.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

// authMiddleware requires a bearer token on every route except health/readiness
// when token is non-empty. No-op when token is "" (local dev).
func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p == "/healthz" || p == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
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

// Command api is the BFF/gateway service: app-definition CRUD, generation proxy,
// and the mock renderer (web/). See README.md / PRODUCTION.md.
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/api"
	"github.com/dushibing/feishu-plugin-platform/internal/auth"
	"github.com/dushibing/feishu-plugin-platform/internal/generator"
	"github.com/dushibing/feishu-plugin-platform/internal/httpx"
	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

func main() {
	port := getenv("PORT", "8080")
	cfg := api.Config{
		GenURL:        getenv("GENERATOR_URL", "http://localhost:8090"),
		WebDir:        getenv("WEB_DIR", "./web"),
		ServeWeb:      getbool("SERVE_WEB", true), // dev serves the mock renderer; set false in prod
		AllowedOrigin: getenv("ALLOWED_ORIGIN", "*"),
		APIToken:      os.Getenv("PLATFORM_API_TOKEN"),
		ReadToken:     os.Getenv("PLATFORM_READ_TOKEN"),
		GenToken:      os.Getenv("GENERATOR_TOKEN"),
		GenerateRPM:   atoiOr("GENERATE_RPM", 60),

		ExecuteRunnerURL:   os.Getenv("EXECUTE_RUNNER_URL"),
		ExecuteRunnerToken: os.Getenv("EXECUTE_RUNNER_TOKEN"),
	}
	mustValidateConfig(cfg)

	st := buildStore()
	seed(st)

	if cfg.APIToken == "" {
		log.Printf("WARNING: PLATFORM_API_TOKEN not set — /api is UNAUTHENTICATED (acceptable for local dev only)")
	}
	if cfg.AllowedOrigin == "*" {
		log.Printf("WARNING: ALLOWED_ORIGIN=* (open CORS) — set a specific origin in production")
	}
	if cfg.GenToken == "" {
		log.Printf("WARNING: GENERATOR_TOKEN not set — calls to the generator are UNAUTHENTICATED (set it + the generator's GENERATOR_TOKEN to the same value in production)")
	}
	if cfg.APIToken != "" && cfg.ReadToken == "" {
		log.Printf("WARNING: PLATFORM_READ_TOKEN not set — the widget/web bundle must embed the ADMIN token (PLATFORM_API_TOKEN), which is then extractable by any user. Set a separate read-only token and rebuild the widget with it.")
	}

	server := api.New(st, cfg)
	authn := buildAuth()
	if authn != nil {
		server = server.WithAuth(authn).WithPlugins(buildPluginStore())
	}

	srv := httpx.NewServer(":"+port, server.Routes())
	log.Printf("api listening on :%s (generator=%s, serveWeb=%t, generateRPM=%d, auth=%t, readToken=%t, genAuth=%t, login=%t)",
		port, cfg.GenURL, cfg.ServeWeb, cfg.GenerateRPM, cfg.APIToken != "", cfg.ReadToken != "", cfg.GenToken != "", authn != nil)
	if err := httpx.Run(srv); err != nil {
		log.Fatal(err)
	}
}

// mustValidateConfig fails fast on misconfiguration that would otherwise "fail
// open" (placeholder secrets, Bitable selected without its credentials).
func mustValidateConfig(cfg api.Config) {
	if placeholder(cfg.APIToken) {
		log.Fatal("PLATFORM_API_TOKEN is a placeholder (REPLACE_ME) — set a real token or leave it empty for dev")
	}
	// The api↔generator and api↔execute-runner bearers must not ship as the
	// REPLACE_ME placeholder (k8s Secrets default to it); a placeholder is a known
	// weak credential, never an intended value. Empty is allowed (dev / no auth).
	for _, t := range []struct{ name, val string }{
		{"GENERATOR_TOKEN", cfg.GenToken},
		{"EXECUTE_RUNNER_TOKEN", cfg.ExecuteRunnerToken},
		{"PLATFORM_READ_TOKEN", cfg.ReadToken},
	} {
		if placeholder(t.val) {
			log.Fatalf("%s is a placeholder (REPLACE_ME) — set a real token or leave it empty for dev", t.name)
		}
	}
	// A read token must differ from the admin token, else the "read-only" bundle
	// would actually carry full admin privileges.
	if cfg.ReadToken != "" && cfg.ReadToken == cfg.APIToken {
		log.Fatal("PLATFORM_READ_TOKEN must differ from PLATFORM_API_TOKEN (the read token ships in the client bundle and must be read-only)")
	}
	if os.Getenv("STORE") == "bitable" {
		for _, k := range []string{"FEISHU_APP_ID", "FEISHU_APP_SECRET", "FEISHU_BITABLE_APP_TOKEN", "FEISHU_BITABLE_TABLE_ID"} {
			if v := os.Getenv(k); v == "" || placeholder(v) {
				log.Fatalf("STORE=bitable requires %s (currently empty or placeholder)", k)
			}
		}
	}
	// Wildcard CORS combined with a credential-bearing API (auth on) lets ANY web
	// origin script the API on a user's behalf. Refuse this combo — require an
	// explicit origin allowlist in production. Set ALLOWED_ORIGIN_INSECURE=true to
	// override (local tunnels/dev only).
	if cfg.APIToken != "" && cfg.AllowedOrigin == "*" && !getbool("ALLOWED_ORIGIN_INSECURE", false) {
		log.Fatal("refusing to start: ALLOWED_ORIGIN=* with auth enabled is unsafe — set ALLOWED_ORIGIN to a specific origin (or ALLOWED_ORIGIN_INSECURE=true to override for dev)")
	}
}

func placeholder(v string) bool { return strings.Contains(v, "REPLACE_ME") }

// buildAuth enables Feishu OAuth login (and per-user plugin ownership) when the
// required config is present; otherwise the platform stays anonymous. Redirect
// URI is taken from OAUTH_REDIRECT_URI, or derived from PLATFORM_BASE_URL.
func buildAuth() *auth.Authenticator {
	redirect := os.Getenv("OAUTH_REDIRECT_URI")
	if redirect == "" {
		if base := os.Getenv("PLATFORM_BASE_URL"); base != "" {
			redirect = strings.TrimRight(base, "/") + "/auth/callback"
		}
	}
	a, ok := auth.New(auth.Config{
		AppID:         os.Getenv("FEISHU_APP_ID"),
		AppSecret:     os.Getenv("FEISHU_APP_SECRET"),
		BaseDomain:    getenv("FEISHU_BASE_DOMAIN", "feishu.cn"),
		RedirectURI:   redirect,
		SessionSecret: []byte(os.Getenv("SESSION_SECRET")),
	})
	if !ok {
		log.Printf("login disabled (to enable: FEISHU_APP_ID/SECRET + SESSION_SECRET + OAUTH_REDIRECT_URI|PLATFORM_BASE_URL)")
		return nil
	}
	log.Printf("login enabled (Feishu OAuth; redirect=%s)", redirect)
	return a
}

// buildStore selects the persistence backend via STORE (memory|bitable). The
// Bitable backend dogfoods the platform: definitions live in a 多维表格.
func buildStore() store.Store {
	switch os.Getenv("STORE") {
	case "bitable":
		log.Printf("store: bitable")
		return store.NewBitableStore(
			os.Getenv("FEISHU_APP_ID"),
			os.Getenv("FEISHU_APP_SECRET"),
			os.Getenv("FEISHU_BITABLE_APP_TOKEN"),
			os.Getenv("FEISHU_BITABLE_TABLE_ID"),
		)
	default:
		log.Printf("store: memory")
		return store.NewMemory()
	}
}

// buildPluginStore selects per-user plugin persistence: a Feishu Bitable table
// (ownership survives restarts) when FEISHU_BITABLE_APP_TOKEN + FEISHU_PLUGINS_TABLE_ID
// are set, otherwise an in-process store. Reuses the platform's Feishu app creds.
func buildPluginStore() store.PluginStore {
	appToken := os.Getenv("FEISHU_BITABLE_APP_TOKEN")
	tableID := os.Getenv("FEISHU_PLUGINS_TABLE_ID")
	if appToken != "" && tableID != "" && !placeholder(appToken) && !placeholder(tableID) {
		log.Printf("plugin store: bitable (persistent; table %s)", tableID)
		return store.NewBitablePluginStore(os.Getenv("FEISHU_APP_ID"), os.Getenv("FEISHU_APP_SECRET"), appToken, tableID)
	}
	log.Printf("plugin store: memory (set FEISHU_BITABLE_APP_TOKEN + FEISHU_PLUGINS_TABLE_ID to persist ownership)")
	return store.NewMemoryPluginStore()
}

// seed inserts one sample definition (dogfooding the sales_dashboard template) so
// the renderer has content on first load — but only when the store is empty, so
// a persistent Bitable backend is not re-seeded on every restart.
func seed(st store.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	existing, err := st.List(ctx)
	if err != nil {
		log.Printf("seed skipped (list failed): %v", err)
		return
	}
	if len(existing) > 0 {
		return
	}
	d, err := generator.Generate(generator.Request{Mode: "template", Template: "sales_dashboard"})
	if err != nil {
		log.Printf("seed skipped: %v", err)
		return
	}
	if _, err := st.Put(ctx, d); err != nil {
		log.Printf("seed put: %v", err)
	}
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func getbool(k string, fallback bool) bool {
	if v := os.Getenv(k); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return fallback
}

func atoiOr(k string, fallback int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// Command execute-runner is the self-hosted execute runtime: it interprets a
// field-shortcut DSL at request time (fetch external APIs + map the response)
// for the container-renderer track — connector / field-shortcut execution on your
// own server, no external function hosting. See docs/EXECUTE_RUNTIME.md.
//
// It is the FaaS-replacement: the container renderer (or the api BFF, call-chain
// B) POSTs a (DSL, inputs, auth) triple to /execute and gets back the mapped
// output. The runtime never writes host data, enforces the per-plugin domain
// allowlist on every request, and interprets the allowlisted expression grammar
// rather than eval'ing code.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/execrt"
	"github.com/dushibing/feishu-plugin-platform/internal/httpx"
	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

func main() {
	port := getenv("PORT", "8095")
	token := os.Getenv("PLATFORM_API_TOKEN") // optional bearer; required when set

	// Egress ledger: every outbound call is logged to stdout, and (when an audit
	// table is configured) appended to the same Bitable audit ledger as the catalog
	// audit — the per-call DLP evidence ("which plugin sent to which host").
	rec := newEgressRecorder(buildAuditSink())

	eng := execrt.New(execrt.Options{
		Timeout:      durEnv("EXECUTE_TIMEOUT_SECONDS", 10*time.Second),
		MaxBodyBytes: int64(intEnv("EXECUTE_MAX_BODY_BYTES", 1<<20)),
		AllowPrivate: boolEnv("EXECUTE_ALLOW_PRIVATE", false), // SSRF guard; only loosen for local dev
		Recorder:     rec,
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
	runErr := httpx.Run(srv) // blocks until SIGINT/SIGTERM, then drains in-flight HTTP
	// HTTP is fully drained now (no more /execute → no more egress events): flush the
	// buffered egress ledger before exit so a restart doesn't drop persisted records.
	rec.Drain(10 * time.Second)
	if runErr != nil {
		log.Fatal(runErr)
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
	// Attribute egress to the platform plugin id when the caller supplied one
	// (otherwise the Engine falls back to the shortcut's own id).
	ctx := r.Context()
	if req.PluginID != "" {
		ctx = execrt.WithPluginID(ctx, req.PluginID)
	}
	data, err := h.eng.Run(ctx, req.DSL, req.Inputs, req.Auth)
	if err != nil {
		// 422: the DSL/inputs/upstream produced a handled failure (not a bug).
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

// egressRecorder turns execrt egress events into audit records. It is ASYNC by
// design: execute is a hot path (one call per render, potentially per row), so a
// synchronous Bitable write per outbound hop would wreck latency and hammer the
// Feishu per-app QPS. RecordEgress only logs to stdout (cheap) + pushes onto a
// bounded buffer; a single background worker drains it to the ledger serially. The
// buffer drops (with a logged count) under extreme load — the audit is shed, never
// the execute, and the stdout log still has every event.
type egressRecorder struct {
	sink    store.AuditSink // nil = stdout-only
	ch      chan store.AuditEvent
	stop    chan struct{} // closed by Drain to flush + stop the worker
	done    chan struct{} // closed by the worker once it has drained and exited
	dropped atomic.Int64
}

func newEgressRecorder(sink store.AuditSink) *egressRecorder {
	r := &egressRecorder{sink: sink, ch: make(chan store.AuditEvent, 1024), stop: make(chan struct{}), done: make(chan struct{})}
	if sink != nil {
		go r.worker()
	} else {
		close(r.done) // no worker → already "drained"
	}
	return r
}

func (r *egressRecorder) RecordEgress(_ context.Context, ev execrt.EgressEvent) {
	id, step := orElse(ev.PluginID, "unknown"), orElse(ev.Step, "-")
	log.Printf("EGRESS plugin=%s host=%s method=%s outcome=%s step=%s detail=%q", id, ev.Host, ev.Method, ev.Outcome, step, ev.Detail)
	if r.sink == nil {
		return
	}
	ae := store.AuditEvent{
		Time:   time.Now().UTC(),
		Actor:  "plugin:" + id,
		Action: "execute.egress",
		Target: ev.Host,
		Detail: fmt.Sprintf("method=%s outcome=%s step=%s %s", ev.Method, ev.Outcome, step, ev.Detail),
	}
	// Never blocks the execute path; the channel is never closed, so a send can never
	// panic even if it races Drain (the event just stays buffered / drops).
	select {
	case r.ch <- ae:
	default:
		r.dropped.Add(1) // buffer full: shed the audit write, keep the execute fast
	}
}

func (r *egressRecorder) append(ae store.AuditEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.sink.Append(ctx, ae); err != nil {
		log.Printf("egress audit append failed (event is still in the stdout log): %v", err)
	}
	if d := r.dropped.Swap(0); d > 0 {
		log.Printf("egress audit: dropped %d events (buffer full under load; they remain in the stdout log)", d)
	}
}

func (r *egressRecorder) worker() {
	defer close(r.done)
	for {
		select {
		case ae := <-r.ch:
			r.append(ae)
		case <-r.stop:
			for { // flush whatever is buffered, then exit
				select {
				case ae := <-r.ch:
					r.append(ae)
				default:
					return
				}
			}
		}
	}
}

// Drain flushes the buffered egress events and stops the worker, bounded by timeout.
// Call it AFTER the HTTP server has shut down (httpx.Run returned), so no new events
// arrive concurrently — otherwise a pod restart would silently lose buffered records.
func (r *egressRecorder) Drain(timeout time.Duration) {
	if r.sink == nil {
		return
	}
	close(r.stop)
	select {
	case <-r.done:
	case <-time.After(timeout):
		log.Printf("egress audit: drain timed out after %s; remaining buffered events are only in the stdout log", timeout)
	}
}

// buildAuditSink wires the egress ledger to the same Bitable audit table as the api
// service when FEISHU_BITABLE_APP_TOKEN + FEISHU_AUDIT_TABLE_ID are set; otherwise
// egress is stdout-only.
func buildAuditSink() store.AuditSink {
	appToken := os.Getenv("FEISHU_BITABLE_APP_TOKEN")
	tableID := os.Getenv("FEISHU_AUDIT_TABLE_ID")
	if appToken == "" || tableID == "" {
		log.Printf("egress ledger: stdout only (set FEISHU_BITABLE_APP_TOKEN + FEISHU_AUDIT_TABLE_ID to persist egress to the audit table)")
		return nil
	}
	log.Printf("egress ledger: bitable (persistent; audit table %s)", tableID)
	return store.NewBitableAuditStore(os.Getenv("FEISHU_APP_ID"), os.Getenv("FEISHU_APP_SECRET"), appToken, tableID)
}

func orElse(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
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

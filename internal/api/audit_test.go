package api

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

// fakeAudit is an in-memory store.AuditSink for testing the wiring.
type fakeAudit struct {
	mu     sync.Mutex
	events []store.AuditEvent
}

func (f *fakeAudit) Append(_ context.Context, e store.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeAudit) List(_ context.Context, limit int) ([]store.AuditEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.AuditEvent, 0, len(f.events))
	for i := len(f.events) - 1; i >= 0; i-- { // newest-first
		out = append(out, f.events[i])
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func newAuditServer(cfg Config, a store.AuditSink) *httptest.Server {
	return httptest.NewServer(New(store.NewMemory(), cfg).WithAudit(a).Routes())
}

func TestAuditEndpointAdminOnly(t *testing.T) {
	ts := newAuditServer(Config{APIToken: "admin", ReadToken: "readonly"}, &fakeAudit{})
	defer ts.Close()

	if code := do(t, "GET", ts.URL+"/api/audit", "admin", ""); code != 200 {
		t.Errorf("admin GET /api/audit = %d, want 200", code)
	}
	// The read-only token (which ships in the client bundle) must NOT see the audit trail.
	if code := do(t, "GET", ts.URL+"/api/audit", "readonly", ""); code != 401 {
		t.Errorf("read-token GET /api/audit = %d, want 401 (audit is admin-only)", code)
	}
	if code := do(t, "GET", ts.URL+"/api/audit", "", ""); code != 401 {
		t.Errorf("no-token GET /api/audit = %d, want 401", code)
	}
}

func TestPutAppendsAuditEvent(t *testing.T) {
	fa := &fakeAudit{}
	ts := newAuditServer(Config{APIToken: "admin", ReadToken: "readonly"}, fa)
	defer ts.Close()

	if code := do(t, "POST", ts.URL+"/api/apps", "admin", validDefJSON()); code != 200 {
		t.Fatalf("POST /api/apps = %d, want 200", code)
	}
	fa.mu.Lock()
	defer fa.mu.Unlock()
	if len(fa.events) != 1 {
		t.Fatalf("audit events = %d, want 1 (put must append)", len(fa.events))
	}
	e := fa.events[0]
	if e.Action != "put.app" || e.Target != "app-x" || e.Version != 1 || e.Actor != "admin-token" {
		t.Errorf("audit event = %+v, want action=put.app target=app-x version=1 actor=admin-token", e)
	}
}

func TestAuditEndpointUnconfigured(t *testing.T) {
	ts := newTestServer(Config{APIToken: "admin"}) // no WithAudit → ledger absent
	defer ts.Close()
	if code := do(t, "GET", ts.URL+"/api/audit", "admin", ""); code != 503 {
		t.Errorf("unconfigured GET /api/audit = %d, want 503", code)
	}
}

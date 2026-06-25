package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/execrt"
	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

type fakeSink struct {
	mu   sync.Mutex
	got  []store.AuditEvent
	done chan struct{}
}

func (f *fakeSink) Append(_ context.Context, e store.AuditEvent) error {
	f.mu.Lock()
	f.got = append(f.got, e)
	f.mu.Unlock()
	if f.done != nil {
		f.done <- struct{}{}
	}
	return nil
}

func (f *fakeSink) List(_ context.Context, _ int) ([]store.AuditEvent, error) { return nil, nil }

// TestEgressRecorderMapsToAuditEvent proves an egress event is mapped onto the shared
// audit schema and appended asynchronously by the worker.
func TestEgressRecorderMapsToAuditEvent(t *testing.T) {
	fs := &fakeSink{done: make(chan struct{}, 1)}
	r := newEgressRecorder(fs)
	r.RecordEgress(context.Background(), execrt.EgressEvent{
		PluginID: "pl_1", Step: "geo", Host: "api.example.com", Method: "GET", Outcome: "allowed", Detail: "status=200",
	})
	select {
	case <-fs.done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not append the egress event within 2s")
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.got) != 1 {
		t.Fatalf("got %d audit events, want 1", len(fs.got))
	}
	e := fs.got[0]
	if e.Action != "execute.egress" || e.Actor != "plugin:pl_1" || e.Target != "api.example.com" {
		t.Errorf("event = %+v, want action=execute.egress actor=plugin:pl_1 target=api.example.com", e)
	}
	for _, want := range []string{"method=GET", "outcome=allowed", "step=geo", "status=200"} {
		if !strings.Contains(e.Detail, want) {
			t.Errorf("detail %q missing %q", e.Detail, want)
		}
	}
}

// TestEgressRecorderStdoutOnly proves a nil sink (no audit table) neither starts a
// worker nor panics — egress is logged to stdout only.
func TestEgressRecorderStdoutOnly(t *testing.T) {
	r := newEgressRecorder(nil)
	r.RecordEgress(context.Background(), execrt.EgressEvent{PluginID: "x", Host: "h", Method: "GET", Outcome: "allowed"})
	// The unknown/empty fields fall back to placeholders; this must not block or panic.
	r.RecordEgress(context.Background(), execrt.EgressEvent{Host: "h2", Method: "POST", Outcome: "error"})
}

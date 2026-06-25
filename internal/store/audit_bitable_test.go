package store

import (
	"context"
	"testing"
	"time"
)

func TestAuditAppendAndList(t *testing.T) {
	s := newBitableAuditStoreWith(newFakeBitable())
	ctx := context.Background()

	base := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{Time: base, Actor: "admin-token", Action: "put.app", Target: "a1", Version: 1, IP: "10.0.0.1"},
		{Time: base.Add(time.Minute), Actor: "user:ou_x", Action: "put.app", Target: "a1", Version: 2, IP: "10.0.0.2"},
		{Time: base.Add(2 * time.Minute), Actor: "admin-token", Action: "delete.app", Target: "a1", IP: "10.0.0.3"},
	}
	for _, e := range events {
		if err := s.Append(ctx, e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := s.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d events, want 3", len(got))
	}
	// Newest-first ordering.
	if got[0].Action != "delete.app" || got[2].Action != "put.app" {
		t.Errorf("not newest-first: %s … %s", got[0].Action, got[2].Action)
	}
	// Field round-trip on the most recent put (got[1]).
	if got[1].Actor != "user:ou_x" || got[1].Target != "a1" || got[1].Version != 2 || got[1].IP != "10.0.0.2" {
		t.Errorf("round-trip mismatch: %+v", got[1])
	}

	// Limit is honored.
	top, err := s.List(ctx, 1)
	if err != nil {
		t.Fatalf("List(limit=1): %v", err)
	}
	if len(top) != 1 || top[0].Action != "delete.app" {
		t.Errorf("limit=1 returned %+v, want only the newest (delete.app)", top)
	}
}

// TestAuditAppendDefaultsTimestamp proves a zero Time is stamped at append (so a
// caller that forgets to set it still gets an ordered, dated record).
func TestAuditAppendDefaultsTimestamp(t *testing.T) {
	s := newBitableAuditStoreWith(newFakeBitable())
	if err := s.Append(context.Background(), AuditEvent{Actor: "admin-token", Action: "put.app", Target: "z"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, _ := s.List(context.Background(), 0)
	if len(got) != 1 || got[0].Time.IsZero() {
		t.Errorf("append did not stamp a timestamp: %+v", got)
	}
}

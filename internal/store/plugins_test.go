package store

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPluginStorePerUserIsolation(t *testing.T) {
	ps := NewMemoryPluginStore()
	ctx := context.Background()

	a, err := ps.SaveForUser(ctx, "ou_alice", PluginRecord{Title: "汇率", Kind: "field", DSL: json.RawMessage(`{"id":"fx"}`)})
	if err != nil || a.ID == "" {
		t.Fatalf("save: %v id=%q", err, a.ID)
	}
	if a.Owner.OpenID != "ou_alice" || a.CreatedAt.IsZero() {
		t.Errorf("save must stamp owner+createdAt, got %+v", a)
	}
	if _, err := ps.SaveForUser(ctx, "ou_bob", PluginRecord{Title: "翻译", Kind: "field", DSL: json.RawMessage(`{"id":"tr"}`)}); err != nil {
		t.Fatal(err)
	}

	// Each user sees only their own.
	la, _ := ps.ListForUser(ctx, "ou_alice")
	lb, _ := ps.ListForUser(ctx, "ou_bob")
	if len(la) != 1 || len(lb) != 1 || la[0].Title != "汇率" || lb[0].Title != "翻译" {
		t.Fatalf("isolation broken: alice=%v bob=%v", la, lb)
	}

	// Bob cannot read or delete Alice's plugin via his scope.
	if _, ok, _ := ps.GetForUser(ctx, "ou_bob", a.ID); ok {
		t.Error("bob must not read alice's plugin")
	}
	if err := ps.DeleteForUser(ctx, "ou_bob", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := ps.GetForUser(ctx, "ou_alice", a.ID); !ok {
		t.Error("bob's delete must not affect alice's plugin")
	}

	// Owner field cannot be spoofed to another user.
	spoof, _ := ps.SaveForUser(ctx, "ou_alice", PluginRecord{Owner: Owner{OpenID: "ou_evil"}, Kind: "field", DSL: json.RawMessage(`{}`)})
	if spoof.Owner.OpenID != "ou_alice" {
		t.Errorf("owner must be forced to the scope user, got %q", spoof.Owner.OpenID)
	}

	if _, err := ps.SaveForUser(ctx, "", PluginRecord{}); err == nil {
		t.Error("save with empty openID must error")
	}
}

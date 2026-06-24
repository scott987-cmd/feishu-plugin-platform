package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestBitablePluginStoreRoundTripAndIsolation(t *testing.T) {
	ps := newBitablePluginStoreWith(newFakeBitable())
	ctx := context.Background()

	a, err := ps.SaveForUser(ctx, "ou_alice", PluginRecord{Title: "汇率", Kind: "field", DSL: json.RawMessage(`{"id":"fx","x":1}`)})
	if err != nil || a.ID == "" || a.Owner.OpenID != "ou_alice" || a.CreatedAt.IsZero() {
		t.Fatalf("save: %v rec=%+v", err, a)
	}
	if _, err := ps.SaveForUser(ctx, "ou_bob", PluginRecord{Title: "翻译", Kind: "field", DSL: json.RawMessage(`{"id":"tr"}`)}); err != nil {
		t.Fatal(err)
	}

	// round-trip preserves the DSL + scalars through Bitable fields.
	got, ok, _ := ps.GetForUser(ctx, "ou_alice", a.ID)
	if !ok || got.Title != "汇率" || got.Kind != "field" || string(got.DSL) != `{"id":"fx","x":1}` {
		t.Fatalf("round-trip lost data: %+v", got)
	}

	// isolation: each user sees only their own; cross-user get/delete denied.
	la, _ := ps.ListForUser(ctx, "ou_alice")
	lb, _ := ps.ListForUser(ctx, "ou_bob")
	if len(la) != 1 || len(lb) != 1 || la[0].Title != "汇率" || lb[0].Title != "翻译" {
		t.Fatalf("isolation broken: alice=%v bob=%v", la, lb)
	}
	if _, ok, _ := ps.GetForUser(ctx, "ou_bob", a.ID); ok {
		t.Error("bob must not read alice's plugin")
	}
	_ = ps.DeleteForUser(ctx, "ou_bob", a.ID) // bob can't delete alice's
	if _, ok, _ := ps.GetForUser(ctx, "ou_alice", a.ID); !ok {
		t.Error("bob's delete must not affect alice's plugin")
	}

	// update in place (same id) does not create a duplicate row.
	a2, _ := ps.SaveForUser(ctx, "ou_alice", PluginRecord{ID: a.ID, Title: "汇率v2", Kind: "field", DSL: json.RawMessage(`{"id":"fx"}`)})
	la, _ = ps.ListForUser(ctx, "ou_alice")
	if len(la) != 1 || la[0].Title != "汇率v2" || a2.ID != a.ID {
		t.Fatalf("update should replace, not duplicate: %v", la)
	}

	// owner cannot be spoofed.
	sp, _ := ps.SaveForUser(ctx, "ou_alice", PluginRecord{Owner: Owner{OpenID: "ou_evil"}, Kind: "field", DSL: json.RawMessage(`{}`)})
	if sp.Owner.OpenID != "ou_alice" {
		t.Errorf("owner must be forced to scope user, got %q", sp.Owner.OpenID)
	}
}

// TestBitablePluginListCache verifies the per-user store serves ListForUser /
// GetForUser from the TTL cache (so "my plugins" + execute-by-pluginId don't
// full-scan the whole org's plugin table each time) and that writes invalidate it.
func TestBitablePluginListCache(t *testing.T) {
	ctx := context.Background()
	fake := newFakeBitable()
	ps := &BitablePluginStore{api: fake, cacheTTL: time.Minute}

	if _, err := ps.SaveForUser(ctx, "ou_a", PluginRecord{ID: "p1", Kind: "field", DSL: json.RawMessage(`{"id":"p1"}`)}); err != nil {
		t.Fatal(err)
	}
	// Two reads after the save → exactly one underlying list (second is cached).
	n0 := fake.listN
	if _, err := ps.ListForUser(ctx, "ou_a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ps.GetForUser(ctx, "ou_a", "p1"); err != nil {
		t.Fatal(err)
	}
	if got := fake.listN - n0; got != 1 {
		t.Errorf("ListForUser+GetForUser hit the API %d times, want 1 (second cached)", got)
	}
	// A write invalidates: the next read is fresh.
	n1 := fake.listN
	if _, err := ps.SaveForUser(ctx, "ou_a", PluginRecord{ID: "p2", Kind: "field", DSL: json.RawMessage(`{"id":"p2"}`)}); err != nil {
		t.Fatal(err)
	}
	list, _ := ps.ListForUser(ctx, "ou_a")
	if len(list) != 2 {
		t.Errorf("after second save ListForUser = %d, want 2 (cache must have been invalidated)", len(list))
	}
	if fake.listN <= n1 {
		t.Error("read after Save served stale cache; Save must invalidate")
	}
}

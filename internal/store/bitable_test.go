package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// fakeBitable is an in-memory bitableAPI for testing BitableStore logic.
type fakeBitable struct {
	recs  map[string]map[string]any
	seq   int
	listN int // number of list() calls — lets cache tests assert hits/misses
}

func newFakeBitable() *fakeBitable { return &fakeBitable{recs: map[string]map[string]any{}} }

func (f *fakeBitable) list(_ context.Context) ([]rawRecord, error) {
	f.listN++
	out := make([]rawRecord, 0, len(f.recs))
	for id, fields := range f.recs {
		out = append(out, rawRecord{recordID: id, fields: fields})
	}
	return out, nil
}
func (f *fakeBitable) create(_ context.Context, fields map[string]any) (string, error) {
	f.seq++
	id := fmt.Sprintf("rec%d", f.seq)
	f.recs[id] = fields
	return id, nil
}
func (f *fakeBitable) update(_ context.Context, recordID string, fields map[string]any) error {
	f.recs[recordID] = fields
	return nil
}
func (f *fakeBitable) delete(_ context.Context, recordID string) error {
	delete(f.recs, recordID)
	return nil
}
func (f *fakeBitable) ping(_ context.Context) error { return nil }

func sampleDef(id, name string) dsl.AppDefinition {
	return dsl.AppDefinition{
		ID: id, Name: name, Type: "view_extension",
		UI: dsl.UI{Layout: "dashboard", Components: []dsl.Component{
			{Type: "stat", Title: "本月销售额", Agg: "sum", Field: "金额"},
		}},
		Actions: []dsl.Action{{ID: "export", Trigger: "button", Do: "exportXlsx"}},
	}
}

func TestBitablePutGet(t *testing.T) {
	ctx := context.Background()
	st := &BitableStore{api: newFakeBitable()}
	stored, err := st.Put(ctx, sampleDef("app-a", "看板A"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if stored.Version != 1 {
		t.Errorf("first put version = %d, want 1", stored.Version)
	}
	got, ok, err := st.Get(ctx, "app-a")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Name != "看板A" || len(got.UI.Components) != 1 || got.Actions[0].Do != "exportXlsx" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestBitableVersionBumpsAndUpdatesInPlace(t *testing.T) {
	ctx := context.Background()
	fake := newFakeBitable()
	st := &BitableStore{api: fake}
	if _, err := st.Put(ctx, sampleDef("app-a", "v1")); err != nil {
		t.Fatal(err)
	}
	stored, err := st.Put(ctx, sampleDef("app-a", "v2"))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != 2 {
		t.Errorf("second put version = %d, want 2", stored.Version)
	}
	if len(fake.recs) != 1 {
		t.Errorf("same id should update in place, got %d records", len(fake.recs))
	}
	got, _, _ := st.Get(ctx, "app-a")
	if got.Name != "v2" {
		t.Errorf("expected updated name v2, got %q", got.Name)
	}
}

func TestBitableListAndDelete(t *testing.T) {
	ctx := context.Background()
	st := &BitableStore{api: newFakeBitable()}
	_, _ = st.Put(ctx, sampleDef("app-a", "A"))
	_, _ = st.Put(ctx, sampleDef("app-b", "B"))
	list, err := st.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d (err %v), want 2", len(list), err)
	}
	if err := st.Delete(ctx, "app-a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Get(ctx, "app-a"); ok {
		t.Error("app-a should be gone after delete")
	}
}

func TestFieldStringRichTextSegments(t *testing.T) {
	v := []any{
		map[string]any{"text": "he"},
		map[string]any{"text": "llo"},
	}
	if got := fieldString(v); got != "hello" {
		t.Errorf("fieldString segments = %q, want %q", got, "hello")
	}
	if got := fieldString("plain"); got != "plain" {
		t.Errorf("fieldString string = %q, want plain", got)
	}
}

func TestDefFieldsRoundTrip(t *testing.T) {
	def := sampleDef("app-x", "RT")
	def.Version = 3
	fields, err := defToFields(def)
	if err != nil {
		t.Fatal(err)
	}
	if fields["id"] != "app-x" || fields["version"] != 3 {
		t.Errorf("scalar fields wrong: %+v", fields)
	}
	back, err := defFromFields(fields)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != def.ID || back.Version != 3 || back.Actions[0].Trigger != "button" {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}

// TestBitableListCache verifies List is served from the TTL cache (so the widget's
// per-open reads don't full-scan Bitable each time) and that writes invalidate it.
func TestBitableListCache(t *testing.T) {
	ctx := context.Background()
	fake := newFakeBitable()
	st := &BitableStore{api: fake, cacheTTL: time.Minute}

	if _, err := st.List(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.listN != 1 {
		t.Errorf("two List calls hit the API %d times, want 1 (second should be cached)", fake.listN)
	}
	// Mutating a returned (cached) result must NOT corrupt the cache for other readers.
	if _, err := st.Put(ctx, sampleDef("app-m", "orig")); err != nil {
		t.Fatal(err)
	}
	first, _ := st.List(ctx) // refreshes cache (Put invalidated)
	for i := range first {
		first[i].Name = "MUTATED"
	}
	second, _ := st.List(ctx) // cache hit
	for _, d := range second {
		if d.Name == "MUTATED" {
			t.Fatalf("mutating a List result corrupted the cache (id=%s)", d.ID)
		}
	}

	// A write must invalidate the cache so the next read is fresh.
	before := fake.listN
	if _, err := st.Put(ctx, sampleDef("app-z", "Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.listN <= before {
		t.Errorf("List after Put served stale cache (listN=%d, before=%d); Put must invalidate", fake.listN, before)
	}
	// Delete must also invalidate.
	before = fake.listN
	if err := st.Delete(ctx, "app-z"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.listN <= before {
		t.Errorf("List after Delete served stale cache; Delete must invalidate")
	}
}

func TestFilterByTable(t *testing.T) {
	defs := []dsl.AppDefinition{
		{ID: "a", Bind: dsl.Bind{TableID: "tbl1"}},
		{ID: "b", Bind: dsl.Bind{TableID: "tbl2"}},
		{ID: "c", Bind: dsl.Bind{TableID: "tbl1"}},
	}
	got := FilterByTable(defs, "tbl1")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("FilterByTable(tbl1) = %+v, want a,c", got)
	}
	if len(FilterByTable(defs, "")) != 3 {
		t.Error("empty tableID should return all")
	}
	if len(FilterByTable(defs, "nope")) != 0 {
		t.Error("unknown tableID should return none")
	}
}

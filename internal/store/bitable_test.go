package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// fakeBitable is an in-memory bitableAPI for testing BitableStore logic.
type fakeBitable struct {
	recs map[string]map[string]any
	seq  int
}

func newFakeBitable() *fakeBitable { return &fakeBitable{recs: map[string]map[string]any{}} }

func (f *fakeBitable) list(_ context.Context) ([]rawRecord, error) {
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

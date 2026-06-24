package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// BitablePluginStore persists per-user plugins into a Feishu 多维表格 so ownership
// survives restarts (the in-process MemoryPluginStore loses them). Each plugin is
// one record; the full DSL is stored as JSON in a "dsl" text field, with owner /
// title / kind / created_at mirrored into scalar fields for human visibility.
//
// Scale note: like BitableStore, reads list the table and filter in-process —
// fine for a single-org platform. find+write is serialized by mu (per process).
type BitablePluginStore struct {
	api bitableAPI
	mu  sync.Mutex

	// Read-through cache for the table-list (the read paths ListForUser/GetForUser
	// hit it), invalidated on every local write. Same rationale as BitableStore:
	// without it, every "my plugins" read and every execute-by-pluginId full-scans
	// the whole org's plugin table over the rate-limited Feishu OpenAPI.
	cacheMu  sync.Mutex
	cache    []rawRecord
	cacheAt  time.Time
	cacheOK  bool
	cacheTTL time.Duration
	cacheGen uint64 // bumped on every invalidate; a fill commits only if gen is unchanged
}

// NewBitablePluginStore wires the store to a dedicated plugin-records table.
func NewBitablePluginStore(appID, appSecret, appToken, tableID string) *BitablePluginStore {
	return &BitablePluginStore{cacheTTL: listCacheTTL, api: &feishuBitable{
		appID: appID, appSecret: appSecret, appToken: appToken, tableID: tableID,
		http: newFeishuHTTPClient(),
	}}
}

// newBitablePluginStoreWith injects a bitableAPI (tests use a fake).
func newBitablePluginStoreWith(api bitableAPI) *BitablePluginStore {
	return &BitablePluginStore{api: api}
}

// listRaw returns the raw records, served from a short-TTL cache (read paths only;
// writes use a fresh find for record-id correctness, then invalidate). Callers
// only READ the records (parse into fresh PluginRecords), so the cached slice is
// never mutated by them.
func (s *BitablePluginStore) listRaw(ctx context.Context) ([]rawRecord, error) {
	s.cacheMu.Lock()
	if s.cacheOK && s.cacheTTL > 0 && time.Since(s.cacheAt) < s.cacheTTL {
		recs := s.cache
		s.cacheMu.Unlock()
		return recs, nil
	}
	gen := s.cacheGen // snapshot: if a write invalidates while we fetch, don't cache stale data
	s.cacheMu.Unlock()
	recs, err := s.api.list(ctx)
	if err != nil {
		return nil, err
	}
	if s.cacheTTL > 0 {
		s.cacheMu.Lock()
		// Only commit if no invalidate landed mid-fetch (else a concurrent write's
		// invalidate is lost and this stale snapshot serves for a full TTL).
		if s.cacheGen == gen {
			s.cache, s.cacheAt, s.cacheOK = recs, time.Now(), true
		}
		s.cacheMu.Unlock()
	}
	return recs, nil
}

func (s *BitablePluginStore) invalidate() {
	s.cacheMu.Lock()
	s.cacheOK = false
	s.cacheGen++
	s.cacheMu.Unlock()
}

// scanFor finds the record matching BOTH owner and id (owner scoping enforced).
func scanFor(recs []rawRecord, openID, id string) (recordID string, rec PluginRecord, ok bool) {
	for _, r := range recs {
		p, perr := pluginFromFields(r.fields)
		if perr != nil {
			continue
		}
		if p.Owner.OpenID == openID && p.ID == id {
			return r.recordID, p, true
		}
	}
	return "", PluginRecord{}, false
}

func (s *BitablePluginStore) SaveForUser(ctx context.Context, openID string, rec PluginRecord) (PluginRecord, error) {
	if openID == "" {
		return PluginRecord{}, errors.New("openID required")
	}
	ctx, cancel := derive(ctx)
	defer cancel()
	if rec.ID == "" {
		rec.ID = newPluginID()
	}
	rec.Owner.OpenID = openID // ownership is authoritative
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	fields, err := pluginToFields(rec)
	if err != nil {
		return PluginRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recordID, _, ok, err := s.find(ctx, openID, rec.ID)
	if err != nil {
		return PluginRecord{}, err
	}
	if ok {
		if err := s.api.update(ctx, recordID, fields); err != nil {
			return PluginRecord{}, err
		}
	} else if _, err := s.api.create(ctx, fields); err != nil {
		return PluginRecord{}, err
	}
	s.invalidate()
	return rec, nil
}

func (s *BitablePluginStore) ListForUser(ctx context.Context, openID string) ([]PluginRecord, error) {
	ctx, cancel := derive(ctx)
	defer cancel()
	recs, err := s.listRaw(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PluginRecord, 0)
	for _, r := range recs {
		p, perr := pluginFromFields(r.fields)
		if perr != nil || p.Owner.OpenID != openID { // isolation: only this user's rows
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *BitablePluginStore) GetForUser(ctx context.Context, openID, id string) (PluginRecord, bool, error) {
	ctx, cancel := derive(ctx)
	defer cancel()
	recs, err := s.listRaw(ctx)
	if err != nil {
		return PluginRecord{}, false, err
	}
	if _, p, ok := scanFor(recs, openID, id); ok {
		return p, true, nil
	}
	// Cache miss: confirm against a fresh list so a just-created plugin (possibly on
	// another replica) isn't a false 404.
	fresh, err := s.api.list(ctx)
	if err != nil {
		return PluginRecord{}, false, err
	}
	_, p, ok := scanFor(fresh, openID, id)
	return p, ok, nil
}

func (s *BitablePluginStore) DeleteForUser(ctx context.Context, openID, id string) error {
	ctx, cancel := derive(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	recordID, _, ok, err := s.find(ctx, openID, id)
	if err != nil || !ok {
		return err
	}
	if err := s.api.delete(ctx, recordID); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// find returns the record matching BOTH owner and plugin id, reading a FRESH list
// (not the cache) so writes get the authoritative record id. Owner scoping is
// enforced so one user can never address another's record.
func (s *BitablePluginStore) find(ctx context.Context, openID, id string) (recordID string, rec PluginRecord, ok bool, err error) {
	recs, err := s.api.list(ctx)
	if err != nil {
		return "", PluginRecord{}, false, err
	}
	recordID, rec, ok = scanFor(recs, openID, id)
	return recordID, rec, ok, nil
}

func pluginToFields(r PluginRecord) (map[string]any, error) {
	if len(r.DSL) == 0 {
		return nil, errors.New("dsl required")
	}
	return map[string]any{
		"id":            r.ID,
		"owner_open_id": r.Owner.OpenID,
		"owner_name":    r.Owner.Name,
		"title":         r.Title,
		"kind":          r.Kind,
		"dsl":           string(r.DSL),
		"created_at":    r.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func pluginFromFields(f map[string]any) (PluginRecord, error) {
	id := fieldString(f["id"])
	raw := fieldString(f["dsl"])
	if id == "" || raw == "" {
		return PluginRecord{}, fmt.Errorf("record missing id/dsl")
	}
	rec := PluginRecord{
		ID:    id,
		Owner: Owner{OpenID: fieldString(f["owner_open_id"]), Name: fieldString(f["owner_name"])},
		Title: fieldString(f["title"]),
		Kind:  fieldString(f["kind"]),
		DSL:   json.RawMessage(raw),
	}
	if ca := fieldString(f["created_at"]); ca != "" {
		if t, err := time.Parse(time.RFC3339, ca); err == nil {
			rec.CreatedAt = t
		}
	}
	return rec, nil
}

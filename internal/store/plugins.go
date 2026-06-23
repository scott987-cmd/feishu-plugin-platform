package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// Per-user plugin ownership: a generated plugin (field shortcut or automation
// action) is attributed to and owned by the Feishu user who created it, so each
// person sees and manages only their own plugins. This is the persistence seam
// for that; MemoryPluginStore is the in-process implementation.

// Owner is the Feishu identity a plugin belongs to.
type Owner struct {
	OpenID string `json:"open_id"`
	Name   string `json:"name"`
}

// PluginRecord is one owned, generated plugin.
type PluginRecord struct {
	ID        string          `json:"id"`         // server-assigned if empty on save
	Owner     Owner           `json:"owner"`      // who created it
	Title     string          `json:"title"`      // human label (from the DSL)
	Kind      string          `json:"kind"`       // "field" | "action"
	DSL       json.RawMessage `json:"dsl"`        // the generated definition
	CreatedAt time.Time       `json:"created_at"`
}

// PluginStore persists plugins scoped to their owner. Implementations must be
// safe for concurrent use and must never leak one user's plugins to another.
type PluginStore interface {
	SaveForUser(ctx context.Context, openID string, rec PluginRecord) (PluginRecord, error)
	ListForUser(ctx context.Context, openID string) ([]PluginRecord, error)
	GetForUser(ctx context.Context, openID, id string) (PluginRecord, bool, error)
	DeleteForUser(ctx context.Context, openID, id string) error
}

// MemoryPluginStore is an in-process PluginStore for dev/tests (and the default
// single-node deployment). A Bitable-backed implementation can be added later.
type MemoryPluginStore struct {
	mu     sync.RWMutex
	byUser map[string]map[string]PluginRecord // openID → id → record
}

// NewMemoryPluginStore constructs an empty MemoryPluginStore.
func NewMemoryPluginStore() *MemoryPluginStore {
	return &MemoryPluginStore{byUser: map[string]map[string]PluginRecord{}}
}

func newPluginID() string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return "plg_" + hex.EncodeToString(b[:])
}

// SaveForUser inserts or replaces a record owned by openID. A blank ID gets a
// fresh server-assigned one; the owner is always forced to openID (never trust
// the caller's Owner field for the scope).
func (m *MemoryPluginStore) SaveForUser(_ context.Context, openID string, rec PluginRecord) (PluginRecord, error) {
	if openID == "" {
		return PluginRecord{}, errors.New("openID required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec.ID == "" {
		rec.ID = newPluginID()
	}
	rec.Owner.OpenID = openID // ownership is authoritative, not caller-supplied
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if m.byUser[openID] == nil {
		m.byUser[openID] = map[string]PluginRecord{}
	}
	m.byUser[openID][rec.ID] = rec
	return rec, nil
}

// ListForUser returns the user's plugins, newest first.
func (m *MemoryPluginStore) ListForUser(_ context.Context, openID string) ([]PluginRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PluginRecord, 0, len(m.byUser[openID]))
	for _, r := range m.byUser[openID] {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// GetForUser fetches one of the user's plugins by id.
func (m *MemoryPluginStore) GetForUser(_ context.Context, openID, id string) (PluginRecord, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.byUser[openID][id]
	return r, ok, nil
}

// DeleteForUser removes one of the user's plugins (no error if absent).
func (m *MemoryPluginStore) DeleteForUser(_ context.Context, openID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byUser[openID], id)
	return nil
}

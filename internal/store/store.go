// Package store persists application definitions. Phase 0 ships an in-memory
// implementation behind a Store interface; later phases swap in a Bitable-backed
// store (dogfooding: definitions live in a multi-dimensional table) or Postgres
// without touching callers.
package store

import (
	"context"
	"sort"
	"sync"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// Store is the persistence seam. Implementations must be safe for concurrent use.
// Every method takes a context so request cancellation / shutdown propagates to
// any underlying I/O (e.g. the Bitable backend's Feishu calls).
type Store interface {
	Get(ctx context.Context, id string) (dsl.AppDefinition, bool, error)
	List(ctx context.Context) ([]dsl.AppDefinition, error)
	// Put inserts or replaces and returns the stored definition (with the
	// server-assigned version), so callers never need a read-back.
	Put(ctx context.Context, def dsl.AppDefinition) (dsl.AppDefinition, error)
	Delete(ctx context.Context, id string) error
}

// FilterByTable returns only the definitions bound to tableID (empty tableID →
// all). Used to serve GET /api/apps?tableId= so a client (the in-Bitable widget)
// receives ONLY the apps for its current table instead of the whole org catalog —
// both a bandwidth fix and a confidentiality fix (no enumerating every plugin).
func FilterByTable(defs []dsl.AppDefinition, tableID string) []dsl.AppDefinition {
	if tableID == "" {
		return defs
	}
	out := make([]dsl.AppDefinition, 0, len(defs))
	for _, d := range defs {
		if d.Bind.TableID == tableID {
			out = append(out, d)
		}
	}
	return out
}

// Pinger is an optional cheap health check (no full scan) for readiness probes.
// Stores that talk to a remote backend should implement it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Memory is an in-process Store for local development and tests.
type Memory struct {
	mu sync.RWMutex
	m  map[string]dsl.AppDefinition
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{m: make(map[string]dsl.AppDefinition)}
}

// Get returns the definition for id and whether it existed.
func (s *Memory) Get(_ context.Context, id string) (dsl.AppDefinition, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.m[id]
	return d, ok, nil
}

// List returns all definitions sorted by ID for stable output.
func (s *Memory) List(_ context.Context) ([]dsl.AppDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]dsl.AppDefinition, 0, len(s.m))
	for _, d := range s.m {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Put inserts or replaces a definition. The version is server-authoritative: any
// client-supplied def.Version is ignored and replaced with prev+1 (or 1 for a
// new id), so version stays monotonic and trustworthy.
func (s *Memory) Put(_ context.Context, def dsl.AppDefinition) (dsl.AppDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.m[def.ID]; ok {
		def.Version = prev.Version + 1
	} else {
		def.Version = 1
	}
	s.m[def.ID] = def
	return def, nil
}

// Delete removes a definition; deleting a missing id is a no-op.
func (s *Memory) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

// Ping is trivially healthy for the in-memory store.
func (s *Memory) Ping(_ context.Context) error { return nil }

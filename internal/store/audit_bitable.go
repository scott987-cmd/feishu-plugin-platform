package store

import (
	"context"
	"sort"
	"strconv"
	"time"
)

// AuditEvent is one entry in the platform's audit trail (who did what, when, from
// where). Persisted append-only so the trail survives pod restarts and is
// queryable by a compliance owner.
type AuditEvent struct {
	Time    time.Time `json:"time"`
	Actor   string    `json:"actor"`             // "user:<open_id>" | "admin-token"
	Action  string    `json:"action"`            // e.g. "put.app" | "delete.app"
	Target  string    `json:"target"`            // the app/plugin id acted on
	Version int       `json:"version,omitempty"` // resulting version (0 if n/a)
	IP      string    `json:"ip"`                // best-effort caller IP
	Detail  string    `json:"detail,omitempty"`  // optional freeform note
}

// AuditSink is the append-only audit-ledger seam. A nil sink means audit is
// stdout-only (no Bitable table configured); the server still logs every event.
type AuditSink interface {
	Append(ctx context.Context, e AuditEvent) error
	List(ctx context.Context, limit int) ([]AuditEvent, error)
}

// BitableAuditStore persists audit events into a dedicated Feishu 多维表格 table —
// the same dogfooding pattern as BitableStore / BitablePluginStore (zero external
// DB). It is APPEND-ONLY (no update/delete path), so the trail is tamper-evident at
// the application layer; reads return newest-first.
type BitableAuditStore struct {
	api bitableAPI
}

// NewBitableAuditStore wires the ledger to a dedicated audit-records table. Reuses
// the platform's Feishu app credentials.
func NewBitableAuditStore(appID, appSecret, appToken, tableID string) *BitableAuditStore {
	return &BitableAuditStore{api: &feishuBitable{
		appID: appID, appSecret: appSecret, appToken: appToken, tableID: tableID,
		http: newFeishuHTTPClient(),
	}}
}

// newBitableAuditStoreWith injects a bitableAPI (tests use a fake).
func newBitableAuditStoreWith(api bitableAPI) *BitableAuditStore { return &BitableAuditStore{api: api} }

// Append writes one event. No find-before-write: the ledger is append-only, so
// every event is a new row.
func (s *BitableAuditStore) Append(ctx context.Context, e AuditEvent) error {
	ctx, cancel := derive(ctx)
	defer cancel()
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	_, err := s.api.create(ctx, auditToFields(e))
	return err
}

// List returns events newest-first, capped at limit (limit<=0 = all).
func (s *BitableAuditStore) List(ctx context.Context, limit int) ([]AuditEvent, error) {
	ctx, cancel := derive(ctx)
	defer cancel()
	recs, err := s.api.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(recs))
	for _, r := range recs {
		if e, ok := auditFromFields(r.fields); ok {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Ping is a cheap readiness check (auth + table access, no full scan).
func (s *BitableAuditStore) Ping(ctx context.Context) error {
	ctx, cancel := derive(ctx)
	defer cancel()
	return s.api.ping(ctx)
}

func auditToFields(e AuditEvent) map[string]any {
	return map[string]any{
		"time":    e.Time.UTC().Format(time.RFC3339),
		"actor":   e.Actor,
		"action":  e.Action,
		"target":  e.Target,
		"version": e.Version,
		"ip":      e.IP,
		"detail":  e.Detail,
	}
}

func auditFromFields(f map[string]any) (AuditEvent, bool) {
	action := fieldString(f["action"])
	if action == "" {
		return AuditEvent{}, false // not an audit record (or malformed) — skip
	}
	e := AuditEvent{
		Actor:   fieldString(f["actor"]),
		Action:  action,
		Target:  fieldString(f["target"]),
		Version: fieldInt(f["version"]),
		IP:      fieldString(f["ip"]),
		Detail:  fieldString(f["detail"]),
	}
	if ts := fieldString(f["time"]); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.Time = t
		}
	}
	return e, true
}

// fieldInt coerces a Bitable number cell (returned as float64 in JSON) to an int.
func fieldInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

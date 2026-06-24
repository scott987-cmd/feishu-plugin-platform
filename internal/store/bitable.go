package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// BitableStore persists app definitions into a Feishu 多维表格 (dogfooding: the
// platform's own data lives in a Bitable). Each definition is one record; the
// full DSL is stored as JSON in a "definition" text field, with id/name/version
// mirrored into scalar fields for human visibility in the table.
//
// Note: Feishu is a domestic endpoint — the underlying client deliberately
// bypasses any HTTPS_PROXY (see newFeishuHTTPClient), the server-side analog of
// LARK_CLI_NO_PROXY=1.
type BitableStore struct {
	api bitableAPI
	mu  sync.Mutex // serializes find+write so Put/Delete are atomic per process

	// Read-through cache for the hot List path. Definitions change rarely (only on
	// publish), but the widget reads them on EVERY open by EVERY user — without a
	// cache that is a full Bitable table scan per open, which exhausts the Feishu
	// per-app QPS at scale. The cache is invalidated on every local Put/Delete;
	// cross-replica writes become visible within cacheTTL.
	cacheMu  sync.Mutex
	cache    []dsl.AppDefinition
	cacheAt  time.Time
	cacheOK  bool
	cacheTTL time.Duration
	cacheGen uint64 // bumped on every invalidate; a fill commits only if gen is unchanged
}

// listCacheTTL bounds staleness of the List read cache (cross-replica writes show
// up within this window; same-replica writes are reflected immediately via invalidate).
const listCacheTTL = 15 * time.Second

// bitableAPI is the low-level record CRUD seam. Tests substitute a fake; the real
// implementation (feishuBitable) talks to the Bitable OpenAPI.
type bitableAPI interface {
	list(ctx context.Context) ([]rawRecord, error)
	create(ctx context.Context, fields map[string]any) (recordID string, err error)
	update(ctx context.Context, recordID string, fields map[string]any) error
	delete(ctx context.Context, recordID string) error
	ping(ctx context.Context) error // cheap reachability check (no full scan)
}

type rawRecord struct {
	recordID string
	fields   map[string]any
}

// NewBitableStore wires a BitableStore to a real Feishu Bitable table.
func NewBitableStore(appID, appSecret, appToken, tableID string) *BitableStore {
	return &BitableStore{cacheTTL: listCacheTTL, api: &feishuBitable{
		appID:     appID,
		appSecret: appSecret,
		appToken:  appToken,
		tableID:   tableID,
		http:      newFeishuHTTPClient(),
	}}
}

// derive bounds a store operation while inheriting the caller's deadline/cancel.
func derive(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 20*time.Second)
}

// Get finds the record whose definition id matches.
func (s *BitableStore) Get(ctx context.Context, id string) (dsl.AppDefinition, bool, error) {
	ctx, cancel := derive(ctx)
	defer cancel()
	_, def, ok, err := s.find(ctx, id)
	return def, ok, err
}

// List returns every parseable definition, served from a short-TTL read cache to
// avoid a full Bitable scan on every call (the widget hits this on every open).
func (s *BitableStore) List(ctx context.Context) ([]dsl.AppDefinition, error) {
	s.cacheMu.Lock()
	if s.cacheOK && s.cacheTTL > 0 && time.Since(s.cacheAt) < s.cacheTTL {
		// Return a copy: the cache slice is private, so a caller mutating the result
		// (sort/dedup/in-place edit) can never corrupt it for other readers — matching
		// Memory.List's fresh-slice contract.
		defs := append([]dsl.AppDefinition(nil), s.cache...)
		s.cacheMu.Unlock()
		return defs, nil
	}
	gen := s.cacheGen // snapshot: if a write invalidates while we fetch, don't cache stale data
	s.cacheMu.Unlock()

	ctx, cancel := derive(ctx)
	defer cancel()
	recs, err := s.api.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dsl.AppDefinition, 0, len(recs))
	for _, r := range recs {
		d, err := defFromFields(r.fields)
		if err != nil {
			log.Printf("bitable: skipping unparseable record %s: %v", r.recordID, err)
			continue
		}
		out = append(out, d)
	}
	if s.cacheTTL > 0 {
		s.cacheMu.Lock()
		// Only commit if no invalidate landed mid-fetch (TOCTOU: otherwise a concurrent
		// Put/Delete's invalidate is lost and this stale snapshot serves for a full TTL).
		// Store a private copy so `out` and the cache never share a backing array.
		if s.cacheGen == gen {
			s.cache = append([]dsl.AppDefinition(nil), out...)
			s.cacheAt, s.cacheOK = time.Now(), true
		}
		s.cacheMu.Unlock()
	}
	return out, nil
}

// invalidate drops the List cache after a local write so the next read is fresh.
func (s *BitableStore) invalidate() {
	s.cacheMu.Lock()
	s.cacheOK = false
	s.cacheGen++
	s.cacheMu.Unlock()
}

// Put creates or updates the record for def.ID with a server-authoritative
// version. find+write is serialized by mu so concurrent Puts for one id don't
// create duplicate rows or race the version (per process; cross-replica safety
// still needs a backend constraint — see docs/PRODUCTION.md).
func (s *BitableStore) Put(ctx context.Context, def dsl.AppDefinition) (dsl.AppDefinition, error) {
	ctx, cancel := derive(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	recordID, prev, ok, err := s.find(ctx, def.ID)
	if err != nil {
		return dsl.AppDefinition{}, err
	}
	if ok {
		def.Version = prev.Version + 1
	} else {
		def.Version = 1
	}
	fields, err := defToFields(def)
	if err != nil {
		return dsl.AppDefinition{}, err
	}
	if ok {
		if err := s.api.update(ctx, recordID, fields); err != nil {
			return dsl.AppDefinition{}, err
		}
	} else {
		if _, err := s.api.create(ctx, fields); err != nil {
			return dsl.AppDefinition{}, err
		}
	}
	s.invalidate()
	return def, nil
}

// Delete removes the record for id (no-op if absent).
func (s *BitableStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := derive(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	recordID, _, ok, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := s.api.delete(ctx, recordID); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Ping is a cheap readiness check: confirm Feishu auth + table access without
// scanning the whole table.
func (s *BitableStore) Ping(ctx context.Context) error {
	ctx, cancel := derive(ctx)
	defer cancel()
	return s.api.ping(ctx)
}

// find lists records and returns the one whose definition id matches.
func (s *BitableStore) find(ctx context.Context, id string) (recordID string, def dsl.AppDefinition, ok bool, err error) {
	recs, err := s.api.list(ctx)
	if err != nil {
		return "", dsl.AppDefinition{}, false, err
	}
	for _, r := range recs {
		d, derr := defFromFields(r.fields)
		if derr != nil {
			continue
		}
		if d.ID == id {
			return r.recordID, d, true, nil
		}
	}
	return "", dsl.AppDefinition{}, false, nil
}

// defToFields serializes a definition into Bitable fields.
func defToFields(d dsl.AppDefinition) (map[string]any, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":         d.ID,
		"name":       d.Name,
		"version":    d.Version,
		"definition": string(raw),
	}, nil
}

// defFromFields parses the "definition" field back into an AppDefinition.
func defFromFields(fields map[string]any) (dsl.AppDefinition, error) {
	raw := fieldString(fields["definition"])
	if raw == "" {
		return dsl.AppDefinition{}, fmt.Errorf("record has no definition field")
	}
	var d dsl.AppDefinition
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return dsl.AppDefinition{}, fmt.Errorf("definition not valid JSON: %w", err)
	}
	return d, nil
}

// fieldString coerces a Bitable cell value to a string. Text fields can come back
// as a plain string or as rich-text segments ([]{"text": ...}).
func fieldString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, seg := range t {
			if m, ok := seg.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	}
	return ""
}

// ─── Real Feishu Bitable client ───

type feishuBitable struct {
	appID, appSecret, appToken, tableID string
	http                                *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// newFeishuHTTPClient returns a client that does NOT use HTTPS_PROXY — Feishu is
// a domestic endpoint and must not be routed through an overseas VPN proxy.
func newFeishuHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
}

func (b *feishuBitable) recordsURL() string {
	return fmt.Sprintf("https://open.feishu.cn/open-apis/bitable/v1/apps/%s/tables/%s/records", b.appToken, b.tableID)
}

func (b *feishuBitable) accessToken(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.token != "" && time.Now().Before(b.tokenExp) {
		return b.token, nil
	}
	body, _ := json.Marshal(map[string]string{"app_id": b.appID, "app_secret": b.appSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	var out struct {
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
		Token  string `json:"tenant_access_token"`
		Expire int    `json:"expire"`
	}
	if err := b.do(req, &out); err != nil {
		return "", err
	}
	if out.Code != 0 {
		return "", fmt.Errorf("tenant_access_token: code %d: %s", out.Code, out.Msg)
	}
	b.token = out.Token
	b.tokenExp = time.Now().Add(time.Duration(out.Expire-60) * time.Second)
	return b.token, nil
}

func (b *feishuBitable) authReq(ctx context.Context, method, url string, body any) (*http.Request, error) {
	token, err := b.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

func (b *feishuBitable) list(ctx context.Context) ([]rawRecord, error) {
	var out []rawRecord
	pageToken := ""
	for {
		url := b.recordsURL() + "?page_size=500"
		if pageToken != "" {
			url += "&page_token=" + pageToken
		}
		req, err := b.authReq(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				HasMore   bool   `json:"has_more"`
				PageToken string `json:"page_token"`
				Items     []struct {
					RecordID string         `json:"record_id"`
					Fields   map[string]any `json:"fields"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := b.do(req, &resp); err != nil {
			return nil, err
		}
		if resp.Code != 0 {
			return nil, fmt.Errorf("list records: code %d: %s", resp.Code, resp.Msg)
		}
		for _, it := range resp.Data.Items {
			out = append(out, rawRecord{recordID: it.RecordID, fields: it.Fields})
		}
		if !resp.Data.HasMore || resp.Data.PageToken == "" {
			break
		}
		pageToken = resp.Data.PageToken
	}
	return out, nil
}

// ping confirms auth + table access with a single 1-record read (no full scan).
func (b *feishuBitable) ping(ctx context.Context) error {
	req, err := b.authReq(ctx, http.MethodGet, b.recordsURL()+"?page_size=1", nil)
	if err != nil {
		return err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := b.do(req, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("ping: code %d: %s", resp.Code, resp.Msg)
	}
	return nil
}

func (b *feishuBitable) create(ctx context.Context, fields map[string]any) (string, error) {
	req, err := b.authReq(ctx, http.MethodPost, b.recordsURL(), map[string]any{"fields": fields})
	if err != nil {
		return "", err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Record struct {
				RecordID string `json:"record_id"`
			} `json:"record"`
		} `json:"data"`
	}
	if err := b.do(req, &resp); err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("create record: code %d: %s", resp.Code, resp.Msg)
	}
	return resp.Data.Record.RecordID, nil
}

func (b *feishuBitable) update(ctx context.Context, recordID string, fields map[string]any) error {
	req, err := b.authReq(ctx, http.MethodPut, b.recordsURL()+"/"+recordID, map[string]any{"fields": fields})
	if err != nil {
		return err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := b.do(req, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("update record: code %d: %s", resp.Code, resp.Msg)
	}
	return nil
}

func (b *feishuBitable) delete(ctx context.Context, recordID string) error {
	req, err := b.authReq(ctx, http.MethodDelete, b.recordsURL()+"/"+recordID, nil)
	if err != nil {
		return err
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := b.do(req, &resp); err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("delete record: code %d: %s", resp.Code, resp.Msg)
	}
	return nil
}

func (b *feishuBitable) do(req *http.Request, out any) error {
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("feishu http %d: %s", resp.StatusCode, string(raw))
	}
	return json.Unmarshal(raw, out)
}

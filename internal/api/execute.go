package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// handleExecute is call-chain B: the BFF forwards an execute request to the
// self-hosted execute-runner (the FaaS replacement; see docs/EXECUTE_RUNTIME.md).
// The runner is internal-only — this is the single authenticated, audited entry
// the in-Feishu container plugin calls. Two input shapes:
//   - inline {dsl, inputs, auth}                  — caller already has the DSL
//   - {pluginId, inputs, auth} + session cookie   —收口模型：fetch the user's
//     stored field-shortcut DSL by id so the DSL never leaves the backend.
//
// The runner enforces the per-plugin domain allowlist + SSRF guard + read-only
// fetch; this handler only authenticates, resolves the DSL, and forwards.
func (s *Server) handleExecute(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ExecuteRunnerURL == "" {
		writeErr(w, http.StatusServiceUnavailable, "execute runtime not configured")
		return
	}
	var in struct {
		PluginID string            `json:"pluginId,omitempty"`
		DSL      json.RawMessage   `json:"dsl,omitempty"`
		Inputs   map[string]any    `json:"inputs"`
		Auth     map[string]string `json:"auth,omitempty"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	// Resolve the DSL: inline wins; otherwise look it up by pluginId from the
	// caller's owned plugins (requires login + a field-kind plugin).
	dslRaw := in.DSL
	if len(dslRaw) == 0 {
		if in.PluginID == "" {
			writeErr(w, http.StatusBadRequest, "provide either dsl or pluginId")
			return
		}
		if s.plugins == nil {
			writeErr(w, http.StatusServiceUnavailable, "plugin store disabled; send dsl inline")
			return
		}
		u, ok := s.currentUser(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "login required to execute by pluginId")
			return
		}
		rec, found, err := s.plugins.GetForUser(r.Context(), u.OpenID, in.PluginID)
		if err != nil {
			log.Printf("execute: plugin lookup: %v", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !found {
			writeErr(w, http.StatusNotFound, "plugin not found")
			return
		}
		if rec.Kind != "field" {
			writeErr(w, http.StatusUnprocessableEntity, "plugin is not an executable field shortcut")
			return
		}
		dslRaw = rec.DSL
	}

	payload, err := json.Marshal(map[string]any{"dsl": dslRaw, "inputs": in.Inputs, "auth": in.Auth})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.ExecuteRunnerURL+"/execute", bytes.NewReader(payload))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.ExecuteRunnerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.ExecuteRunnerToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("execute proxy: %v", err)
		writeErr(w, http.StatusBadGateway, "execute runtime unreachable")
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
}

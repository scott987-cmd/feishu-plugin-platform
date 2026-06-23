package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/auth"
	"github.com/dushibing/feishu-plugin-platform/internal/store"
)

const (
	sessionCookie = "fpp_session"
	stateCookie   = "fpp_oauth_state"
)

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// currentUser reads + verifies the session cookie. ok=false when anonymous.
func (s *Server) currentUser(r *http.Request) (auth.User, bool) {
	if s.authn == nil {
		return auth.User{}, false
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return auth.User{}, false
	}
	return s.authn.Verify(c.Value)
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

// handleAuthLogin starts the OAuth round-trip: set a CSRF state cookie, redirect
// to the Feishu authorization page.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	state := auth.NewState()
	s.setCookie(w, r, stateCookie, state, int((10 * time.Minute).Seconds()))
	http.Redirect(w, r, s.authn.AuthorizeURL(state), http.StatusFound)
}

// handleAuthCallback completes the round-trip: verify state, exchange the code,
// set the session cookie, and return to the app.
func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	sc, err := r.Cookie(stateCookie)
	if code == "" || state == "" || err != nil || sc.Value == "" || sc.Value != state {
		writeErr(w, http.StatusBadRequest, "invalid oauth state or code")
		return
	}
	user, err := s.authn.Exchange(r.Context(), code)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "login failed: "+err.Error())
		return
	}
	s.setCookie(w, r, stateCookie, "", -1) // clear state
	s.setCookie(w, r, sessionCookie, s.authn.Sign(user), s.authn.SessionMaxAge())
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleAuthLogout clears the session.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	s.setCookie(w, r, sessionCookie, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMe reports the current identity (200 with logged_in:false when anonymous,
// so the UI can branch without treating it as an error).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"logged_in": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logged_in": true, "open_id": u.OpenID, "name": u.Name})
}

func (s *Server) handleMyList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "login required")
		return
	}
	recs, err := s.plugins.ListForUser(r.Context(), u.OpenID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

func (s *Server) handleMySave(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "login required")
		return
	}
	var in struct {
		ID    string          `json:"id"`
		Title string          `json:"title"`
		Kind  string          `json:"kind"`
		DSL   json.RawMessage `json:"dsl"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Kind != "field" && in.Kind != "action" {
		writeErr(w, http.StatusBadRequest, "kind must be field|action")
		return
	}
	if len(in.DSL) == 0 {
		writeErr(w, http.StatusBadRequest, "dsl required")
		return
	}
	rec, err := s.plugins.SaveForUser(r.Context(), u.OpenID, store.PluginRecord{
		ID: in.ID, Owner: store.Owner{OpenID: u.OpenID, Name: u.Name}, Title: in.Title, Kind: in.Kind, DSL: in.DSL,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleMyDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "login required")
		return
	}
	if err := s.plugins.DeleteForUser(r.Context(), u.OpenID, r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

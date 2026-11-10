// Package api is Stim's HTTP API and static file server.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/manager"
	"github.com/ungerdas/stim/internal/store"
)

// Server wires the store and manager to HTTP.
type Server struct {
	store   *store.Store
	manager *manager.Manager
	ui      fs.FS
	version string
}

// New builds the HTTP handler. ui is the built frontend (may be empty).
func New(s *store.Store, m *manager.Manager, ui fs.FS, version string) http.Handler {
	srv := &Server{store: s, manager: m, ui: ui, version: version}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", srv.health)
	mux.HandleFunc("GET /api/kinds", srv.kinds)
	mux.HandleFunc("GET /api/instances", srv.listInstances)
	mux.HandleFunc("POST /api/instances", srv.createInstance)
	mux.HandleFunc("POST /api/instances/test", srv.testInstance)
	mux.HandleFunc("PUT /api/instances/{id}", srv.updateInstance)
	mux.HandleFunc("DELETE /api/instances/{id}", srv.deleteInstance)
	mux.HandleFunc("POST /api/instances/{id}/retry", srv.retryInstance)
	mux.HandleFunc("GET /api/torrents", srv.torrents)
	mux.HandleFunc("POST /api/torrents/action", srv.action)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", srv.static())

	return guard(mux)
}

// guard rejects cross-site writes. Stim has no login of its own yet, so a
// page on another site must not be able to make the browser call the API:
// writes must be JSON (which forces a CORS preflight we never approve) and,
// when the browser sends an Origin, it must be Stim's own.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				// Behind a reverse proxy that rewrites Host, the public host
				// arrives in X-Forwarded-Host.
				u, err := url.Parse(origin)
				if err != nil || (u.Host != r.Host && u.Host != r.Header.Get("X-Forwarded-Host")) {
					writeError(w, http.StatusForbidden, "cross-site request refused")
					return
				}
			}
			if r.Method != http.MethodDelete {
				mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if mt != "application/json" {
					writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) kinds(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, client.Kinds())
}

// instanceView is an instance as the UI sees it: no secrets, only whether
// they are set.
type instanceView struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Kind          client.Kind `json:"kind"`
	URL           string      `json:"url"`
	Username      string      `json:"username"`
	HasPassword   bool        `json:"hasPassword"`
	BasicUser     string      `json:"basicUser"`
	HasBasicPass  bool        `json:"hasBasicPass"`
	TLSSkipVerify bool        `json:"tlsSkipVerify"`
}

func view(in store.Instance) instanceView {
	return instanceView{
		ID:            in.ID,
		Name:          in.Name,
		Kind:          in.Kind,
		URL:           in.URL,
		Username:      in.Username,
		HasPassword:   in.Password != "",
		BasicUser:     in.BasicUser,
		HasBasicPass:  in.BasicPass != "",
		TLSSkipVerify: in.TLSSkipVerify,
	}
}

// instanceInput is what the UI sends. A nil password means "keep the stored
// one"; an empty string clears it.
type instanceInput struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Kind          client.Kind `json:"kind"`
	URL           string      `json:"url"`
	Username      string      `json:"username"`
	Password      *string     `json:"password"`
	BasicUser     string      `json:"basicUser"`
	BasicPass     *string     `json:"basicPass"`
	TLSSkipVerify bool        `json:"tlsSkipVerify"`
}

// merge applies the input on top of an existing instance (zero for new ones).
func (in instanceInput) merge(base store.Instance) store.Instance {
	out := store.Instance{
		ID:            base.ID,
		Name:          strings.TrimSpace(in.Name),
		Kind:          in.Kind,
		URL:           strings.TrimSpace(in.URL),
		Username:      strings.TrimSpace(in.Username),
		Password:      base.Password,
		BasicUser:     strings.TrimSpace(in.BasicUser),
		BasicPass:     base.BasicPass,
		TLSSkipVerify: in.TLSSkipVerify,
	}
	if in.Password != nil {
		out.Password = *in.Password
	}
	if in.BasicPass != nil {
		out.BasicPass = *in.BasicPass
	}
	if out.BasicUser == "" {
		out.BasicPass = ""
	}
	return out
}

func (s *Server) listInstances(w http.ResponseWriter, _ *http.Request) {
	list := s.store.List()
	out := make([]instanceView, 0, len(list))
	for _, in := range list {
		out = append(out, view(in))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	var in instanceInput
	if !readJSON(w, r, &in) {
		return
	}
	created, err := s.store.Create(in.merge(store.Instance{}))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.manager.Reload()
	writeJSON(w, http.StatusCreated, view(created))
}

func (s *Server) updateInstance(w http.ResponseWriter, r *http.Request) {
	var in instanceInput
	if !readJSON(w, r, &in) {
		return
	}
	base, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	updated := in.merge(base)
	if err := s.store.Update(updated); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.manager.Reload()
	s.manager.Retry(updated.ID) // saving is also how the user says "try again"
	writeJSON(w, http.StatusOK, view(updated))
}

func (s *Server) deleteInstance(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	s.manager.Reload()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) retryInstance(w http.ResponseWriter, r *http.Request) {
	s.manager.Retry(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// testInstance checks connection settings before they are saved. When the
// input carries the ID of a stored instance, blank passwords fall back to
// the stored ones so "edit, then test" works without retyping them.
func (s *Server) testInstance(w http.ResponseWriter, r *http.Request) {
	var in instanceInput
	if !readJSON(w, r, &in) {
		return
	}
	var base store.Instance
	if in.ID != "" {
		if stored, err := s.store.Get(in.ID); err == nil {
			base = stored
		}
	}
	candidate := in.merge(base)
	if candidate.Name == "" {
		candidate.Name = "test"
	}
	if err := candidate.Validate(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	version, err := manager.Test(r.Context(), candidate)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "errorCode": client.ErrorCode(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

func (s *Server) torrents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.manager.Snapshot(r.Context()))
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string         `json:"action"`
		Items  []manager.Item `json:"items"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	failed, err := s.manager.Act(r.Context(), req.Action, req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"failed": failed})
}

// static serves the built frontend, falling back to index.html so the
// single-page app can own its routes.
func (s *Server) static() http.Handler {
	fileServer := http.FileServer(http.FS(s.ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(s.ui, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Stim is running, but the web UI has not been built.\nRun `npm install && npm run build` in web/ and rebuild, or use the Vite dev server.\n"))
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(s.ui, path); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

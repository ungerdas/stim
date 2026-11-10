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
	"github.com/ungerdas/stim/internal/remotefiles"
	"github.com/ungerdas/stim/internal/store"
	"github.com/ungerdas/stim/internal/transfer"
)

// Server wires the store and manager to HTTP.
type Server struct {
	store     *store.Store
	manager   *manager.Manager
	transfers *transfer.Engine
	ui        fs.FS
	version   string
}

// New builds the HTTP handler. ui is the built frontend (may be empty).
func New(s *store.Store, m *manager.Manager, t *transfer.Engine, ui fs.FS, version string) http.Handler {
	srv := &Server{store: s, manager: m, transfers: t, ui: ui, version: version}
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
	mux.HandleFunc("POST /api/torrents/remove", srv.remove)
	mux.HandleFunc("GET /api/transfers", srv.listTransfers)
	mux.HandleFunc("POST /api/transfers", srv.startTransfer)
	mux.HandleFunc("POST /api/transfers/{id}/cancel", srv.cancelTransfer)
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

	SSHHost            string `json:"sshHost"`
	SSHPort            int    `json:"sshPort"`
	SSHUser            string `json:"sshUser"`
	HasSSHPassword     bool   `json:"hasSshPassword"`
	HasSSHKey          bool   `json:"hasSshKey"`
	SSHHostFingerprint string `json:"sshHostFingerprint"`
	TorrentDir         string `json:"torrentDir"`
}

func view(in store.Instance) instanceView {
	return instanceView{
		SSHHost:            in.SSHHost,
		SSHPort:            in.SSHPort,
		SSHUser:            in.SSHUser,
		HasSSHPassword:     in.SSHPassword != "",
		HasSSHKey:          in.SSHKey != "",
		SSHHostFingerprint: remotefiles.Fingerprint(in.SSHHostKey),
		TorrentDir:         in.TorrentDir,

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

	SSHHost     string  `json:"sshHost"`
	SSHPort     int     `json:"sshPort"`
	SSHUser     string  `json:"sshUser"`
	SSHPassword *string `json:"sshPassword"`
	SSHKey      *string `json:"sshKey"`
	TorrentDir  string  `json:"torrentDir"`
	// SSHHostKey is the key the user saw when testing the connection; it is
	// stored so later connections must present the same one.
	SSHHostKey    string `json:"sshHostKey"`
	ForgetHostKey bool   `json:"forgetHostKey"`
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

	out.SSHHost = strings.TrimSpace(in.SSHHost)
	out.SSHPort = in.SSHPort
	out.SSHUser = strings.TrimSpace(in.SSHUser)
	out.TorrentDir = strings.TrimSpace(in.TorrentDir)
	out.SSHPassword, out.SSHKey, out.SSHHostKey = base.SSHPassword, base.SSHKey, base.SSHHostKey
	if in.SSHPassword != nil {
		out.SSHPassword = *in.SSHPassword
	}
	if in.SSHKey != nil {
		out.SSHKey = strings.TrimSpace(*in.SSHKey)
		if out.SSHKey != "" {
			out.SSHKey += "\n" // PEM parsers want the trailing newline
		}
	}
	// A stored host key belongs to one server. Forget it when the address
	// changes or the user asks; otherwise keep it and never overwrite it.
	if in.ForgetHostKey || out.SSHHost != base.SSHHost || out.SSHPort != base.SSHPort {
		out.SSHHostKey = ""
	}
	if out.SSHHostKey == "" && remotefiles.Fingerprint(in.SSHHostKey) != "" {
		out.SSHHostKey = strings.TrimSpace(in.SSHHostKey)
	}
	if out.SSHHost == "" {
		out.SSHUser, out.SSHPassword, out.SSHKey, out.SSHHostKey, out.SSHPort = "", "", "", "", 0
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
	// The client and SSH are tested independently, so a typo in one does
	// not hide the result of the other.
	res := map[string]any{"ok": true}
	if version, err := manager.Test(r.Context(), candidate); err != nil {
		res["ok"], res["error"], res["errorCode"] = false, err.Error(), client.ErrorCode(err)
	} else {
		res["version"] = version
	}
	if candidate.SSHHost != "" {
		res["ssh"] = testSSH(r, candidate)
	}
	writeJSON(w, http.StatusOK, res)
}

// testSSH tries the SSH settings and lists the torrent folder.
func testSSH(r *http.Request, in store.Instance) map[string]any {
	cfg := in.SSHConfig()
	if !cfg.Configured() {
		return map[string]any{"ok": false, "error": "SSH needs a user name and a password or key"}
	}
	// Clients that say where each .torrent file is need no folder; for them
	// the test only checks that logging in and listing the home folder work.
	dir := in.TorrentDir
	if info, ok := client.InfoFor(in.Kind); ok && info.TorrentDirHint == "" {
		dir = "."
	} else if dir == "" {
		return map[string]any{"ok": false, "error": "the torrent folder is not filled in"}
	}
	count, hostKey, err := remotefiles.Probe(r.Context(), cfg, dir)
	res := map[string]any{"ok": err == nil, "torrents": count}
	if hostKey != "" {
		res["hostKey"] = hostKey
		res["fingerprint"] = remotefiles.Fingerprint(hostKey)
		res["newHostKey"] = cfg.HostKey == ""
	}
	if err != nil {
		res["error"] = err.Error()
		res["hostKeyChanged"] = errors.Is(err, remotefiles.ErrHostKeyChanged)
	}
	return res
}

// remove removes torrents, and deletes their files only when the request
// says so explicitly.
func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items      []manager.Item `json:"items"`
		DeleteData bool           `json:"deleteData"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	// Leave torrents alone that a move is still working on.
	busy := s.transfers.Busy()
	var items []manager.Item
	failed := map[string]string{}
	for _, it := range req.Items {
		if busy[it.InstanceID+":"+strings.ToLower(it.Hash)] {
			failed[it.InstanceID] = "the torrent is being moved; cancel the move first"
			continue
		}
		items = append(items, it)
	}
	for id, msg := range s.manager.Remove(r.Context(), items, req.DeleteData) {
		failed[id] = msg
	}
	log.Printf("remove: %d torrents, delete data %v, failed on %d clients", len(items), req.DeleteData, len(failed))
	writeJSON(w, http.StatusOK, map[string]any{"failed": failed})
}

func (s *Server) listTransfers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.transfers.Jobs())
}

func (s *Server) startTransfer(w http.ResponseWriter, r *http.Request) {
	var req transfer.Request
	if !readJSON(w, r, &req) {
		return
	}
	job, err := s.transfers.Start(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) cancelTransfer(w http.ResponseWriter, r *http.Request) {
	if !s.transfers.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "no running move with that id")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

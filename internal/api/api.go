// Package api is Stim's HTTP API and static file server.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/manager"
	"github.com/ungerdas/stim/internal/orphans"
	"github.com/ungerdas/stim/internal/remotefiles"
	"github.com/ungerdas/stim/internal/search"
	"github.com/ungerdas/stim/internal/store"
	"github.com/ungerdas/stim/internal/transfer"
)

// Server wires the store and manager to HTTP.
type Server struct {
	store     *store.Store
	manager   *manager.Manager
	transfers *transfer.Engine
	search    *search.Service
	ui        fs.FS
	version   string
}

// New builds the HTTP handler. ui is the built frontend (may be empty).
func New(s *store.Store, m *manager.Manager, t *transfer.Engine, sr *search.Service, ui fs.FS, version string) http.Handler {
	srv := &Server{store: s, manager: m, transfers: t, search: sr, ui: ui, version: version}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", srv.health)
	mux.HandleFunc("GET /api/kinds", srv.kinds)
	mux.HandleFunc("GET /api/instances", srv.listInstances)
	mux.HandleFunc("POST /api/instances", srv.createInstance)
	mux.HandleFunc("POST /api/instances/test", srv.testInstance)
	mux.HandleFunc("PUT /api/instances/{id}", srv.updateInstance)
	mux.HandleFunc("DELETE /api/instances/{id}", srv.deleteInstance)
	mux.HandleFunc("POST /api/instances/{id}/retry", srv.retryInstance)
	mux.HandleFunc("GET /api/instances/{id}/savepath", srv.savePath)
	mux.HandleFunc("GET /api/torrents", srv.torrents)
	mux.HandleFunc("POST /api/torrents/action", srv.action)
	mux.HandleFunc("POST /api/torrents/remove", srv.remove)
	mux.HandleFunc("GET /api/details", srv.details)
	mux.HandleFunc("POST /api/orphans/scan", srv.scanOrphans)
	mux.HandleFunc("POST /api/orphans/delete", srv.deleteOrphans)
	mux.HandleFunc("GET /api/search/settings", srv.searchSettings)
	mux.HandleFunc("PUT /api/search/settings", srv.saveSearchSettings)
	mux.HandleFunc("POST /api/search/test", srv.testSearch)
	mux.HandleFunc("GET /api/search/indexers", srv.searchIndexers)
	mux.HandleFunc("POST /api/search", srv.runSearch)
	mux.HandleFunc("POST /api/search/grab", srv.grab)
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
	// Server identifies the machine the client runs on (see
	// store.Instance.Server), so the UI can tell same-server moves apart.
	Server string `json:"server"`
	// SSHReady means files can be copied to this client's server: SSH is
	// set up and its host key is stored.
	SSHReady bool `json:"sshReady"`
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
		Server:             in.Server(),
		SSHReady:           in.SSHConfig().Configured() && in.SSHHostKey != "",

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

// savePath suggests a folder on the client's server for torrents moved
// there from another server: the client's default download folder.
func (s *Server) savePath(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.manager.Lookup(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown client")
		return
	}
	sp, ok := c.(client.SavePather)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]string{"path": ""})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	dir, err := sp.DefaultSavePath(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": dir})
}

// orphanTimeout bounds a scan: listing a few folders over SSH and adding up
// the leftovers, plus a fresh torrent list from every client on the server.
const orphanTimeout = 3 * time.Minute

// scanOrphans lists files in a server's download folders that no torrent uses.
func (s *Server) scanOrphans(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Server string `json:"server"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), orphanTimeout)
	defer cancel()
	res, err := orphans.Scan(ctx, s.manager, in.Server)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// deleteOrphans removes leftovers. Each path is checked again first, so only
// what is still a leftover in a fresh scan is removed.
func (s *Server) deleteOrphans(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Server string   `json:"server"`
		Paths  []string `json:"paths"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to delete")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), orphanTimeout)
	defer cancel()
	removed, failed, err := orphans.Delete(ctx, s.manager, in.Server, in.Paths)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if removed == nil {
		removed = []string{}
	}
	// Free space changed: refresh the clients on that server.
	var ids []string
	for _, inst := range s.manager.Instances() {
		if inst.Server() == in.Server {
			ids = append(ids, inst.ID)
		}
	}
	s.manager.Invalidate(ids...)
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed, "failed": failed})
}

// details describes one torrent for the detail panel: files, trackers with
// their passkeys masked, and peers.
func (s *Server) details(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.manager.Lookup(r.URL.Query().Get("instance"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown client")
		return
	}
	d, ok := c.(client.Detailer)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this client type cannot show details")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := d.Details(ctx, strings.ToLower(r.URL.Query().Get("hash")))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	sort.SliceStable(res.Trackers, func(i, j int) bool { return res.Trackers[i].Tier < res.Trackers[j].Tier })
	sort.Slice(res.Peers, func(i, j int) bool {
		a, b := res.Peers[i], res.Peers[j]
		if a.DownRate+a.UpRate != b.DownRate+b.UpRate {
			return a.DownRate+a.UpRate > b.DownRate+b.UpRate
		}
		return a.Address < b.Address
	})
	if res.Files == nil {
		res.Files = []client.File{}
	}
	if res.Trackers == nil {
		res.Trackers = []client.TrackerInfo{}
	}
	if res.Peers == nil {
		res.Peers = []client.Peer{}
	}
	writeJSON(w, http.StatusOK, res)
}

// searchInput is what the settings form sends. A nil API key means "keep
// the stored one".
type searchInput struct {
	ProwlarrURL string  `json:"prowlarrUrl"`
	APIKey      *string `json:"apiKey"`
}

func (in searchInput) merge(base store.Search) store.Search {
	out := store.Search{ProwlarrURL: strings.TrimRight(strings.TrimSpace(in.ProwlarrURL), "/"), ProwlarrAPIKey: base.ProwlarrAPIKey}
	if in.APIKey != nil {
		out.ProwlarrAPIKey = strings.TrimSpace(*in.APIKey)
	}
	return out
}

// searchSettings shows the Prowlarr address; the key never leaves the server.
func (s *Server) searchSettings(w http.ResponseWriter, _ *http.Request) {
	cfg := s.store.SearchSettings()
	writeJSON(w, http.StatusOK, map[string]any{"prowlarrUrl": cfg.ProwlarrURL, "hasApiKey": cfg.ProwlarrAPIKey != ""})
}

func (s *Server) saveSearchSettings(w http.ResponseWriter, r *http.Request) {
	var in searchInput
	if !readJSON(w, r, &in) {
		return
	}
	cfg := in.merge(s.store.SearchSettings())
	if cfg.ProwlarrURL == "" {
		cfg.ProwlarrAPIKey = "" // clearing the address turns search off
	}
	if err := s.store.SetSearchSettings(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prowlarrUrl": cfg.ProwlarrURL, "hasApiKey": cfg.ProwlarrAPIKey != ""})
}

func (s *Server) testSearch(w http.ResponseWriter, r *http.Request) {
	var in searchInput
	if !readJSON(w, r, &in) {
		return
	}
	cfg := in.merge(s.store.SearchSettings())
	if !cfg.Configured() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "fill in both the address and the API key"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	version, indexers, err := search.Test(ctx, cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version, "indexers": indexers})
}

func (s *Server) searchIndexers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	list, err := s.search.Indexers(ctx)
	if errors.Is(err, search.ErrNotConfigured) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) runSearch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Query      string `json:"query"`
		IndexerIDs []int  `json:"indexerIds"`
		Categories []int  `json:"categories"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	start := time.Now()
	hits, err := s.search.Search(ctx, in.Query, in.IndexerIDs, in.Categories)
	switch {
	case errors.Is(err, search.ErrNotConfigured):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, search.ErrBadRequest):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "bad request: "))
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "tookMs": time.Since(start).Milliseconds()})
	}
}

func (s *Server) grab(w http.ResponseWriter, r *http.Request) {
	var in search.Grab
	if !readJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	name, err := s.search.Grab(ctx, in)
	switch {
	case errors.Is(err, search.ErrExpired):
		writeError(w, http.StatusGone, err.Error())
	case errors.Is(err, search.ErrDuplicate):
		writeError(w, http.StatusConflict, strings.TrimPrefix(err.Error(), "duplicate: "))
	case errors.Is(err, search.ErrBadRequest):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "bad request: "))
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]string{"client": name})
	}
}

// Package deluge talks to Deluge through the JSON-RPC API that the Deluge web
// UI (deluge-web) exposes at <url>/json. The web UI in turn talks to a
// daemon (deluged); Stim connects the web UI to its first configured daemon
// if it is not connected already.
package deluge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ungerdas/stim/internal/client"
)

const Kind client.Kind = "deluge"

func init() {
	client.Register(client.Info{
		Kind:          Kind,
		Label:         "Deluge",
		NeedsUsername: false, // the web UI only has a password
		URLHint:       "http://192.168.1.10:8112",
		Capabilities:  caps,

		// Deluge has no API call that returns a .torrent file; they live in
		// the daemon's state folder.
		ExportsTorrentFiles: false,
		TorrentDirHint:      ".config/deluge/state",
	}, New)
}

// Labels come from Deluge's Label plugin. When the plugin is off the label
// field is simply empty.
var caps = client.Capabilities{Labels: true, Tags: false}

// Client is a Deluge web JSON-RPC client for one instance.
type Client struct {
	base string
	cfg  client.Config
	http *http.Client
	id   atomic.Int64

	mu sync.Mutex
	// Deluge 1.x names the pause/resume calls in the singular.
	legacyPause *bool

	totalsMu   sync.Mutex
	totals     client.Totals
	haveTotals bool
	// Whether the Stats plugin answered last time, and when that was
	// checked, so a missing plugin is not asked for on every poll.
	statsPlugin        bool
	statsPluginChecked time.Time
}

// New builds a Deluge client.
func New(cfg client.Config) (client.Client, error) {
	base, err := client.BaseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	// Accept the URL with or without the /json suffix.
	base = strings.TrimSuffix(base, "/json")
	return &Client{base: base, cfg: cfg, http: client.NewHTTPClient(cfg)}, nil
}

func (c *Client) Kind() client.Kind                 { return Kind }
func (c *Client) Capabilities() client.Capabilities { return caps }

// rpcError is an error returned by Deluge itself (as opposed to a transport
// error). Code 1 means the session is not authenticated.
type rpcError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *rpcError) Error() string { return "deluge: " + e.Message }

const (
	codeNotAuthenticated = 1
	codeUnknownMethod    = 2
)

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

var errNotDeluge = fmt.Errorf("%w: the URL did not answer like a Deluge web UI", client.ErrNotClient)

// rawCall performs one JSON-RPC call without any session handling.
func (c *Client) rawCall(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	payload, err := json.Marshal(map[string]any{
		"method": method,
		"params": params,
		"id":     c.id.Add(1),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.cfg.BasicUser != "" {
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: HTTP basic auth was rejected", client.ErrAuth)
	}
	var r rpcResponse
	if err := json.Unmarshal(body, &r); err != nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w (HTTP %d)", errNotDeluge, resp.StatusCode)
	}
	if r.Error != nil {
		return nil, r.Error
	}
	return r.Result, nil
}

func (c *Client) login(ctx context.Context) error {
	res, err := c.rawCall(ctx, "auth.login", c.cfg.Password)
	if err != nil {
		return err
	}
	var ok bool
	if err := json.Unmarshal(res, &ok); err != nil {
		return errNotDeluge
	}
	if !ok {
		return fmt.Errorf("%w: wrong password", client.ErrAuth)
	}
	return c.connectDaemon(ctx)
}

// connectDaemon makes sure the web UI is attached to a daemon. A freshly
// started deluge-web is not connected to anything until someone picks a host
// in its connection manager.
func (c *Client) connectDaemon(ctx context.Context) error {
	res, err := c.rawCall(ctx, "web.connected")
	if err != nil {
		return err
	}
	var connected bool
	_ = json.Unmarshal(res, &connected)
	if connected {
		return nil
	}
	res, err = c.rawCall(ctx, "web.get_hosts")
	if err != nil {
		return err
	}
	var hosts [][]any
	if err := json.Unmarshal(res, &hosts); err != nil || len(hosts) == 0 || len(hosts[0]) == 0 {
		return errors.New("the Deluge web UI has no daemon configured in its connection manager")
	}
	hostID, _ := hosts[0][0].(string)
	if hostID == "" {
		return errors.New("the Deluge web UI has no daemon configured in its connection manager")
	}
	if _, err := c.rawCall(ctx, "web.connect", hostID); err != nil {
		return fmt.Errorf("could not connect the Deluge web UI to its daemon: %w", err)
	}
	return nil
}

// call performs a JSON-RPC call and logs in (once) when the session is
// missing or has expired.
func (c *Client) call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	res, err := c.rawCall(ctx, method, params...)
	var re *rpcError
	if errors.As(err, &re) && re.Code == codeNotAuthenticated {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		return c.rawCall(ctx, method, params...)
	}
	// Daemon methods (core.*, daemon.*) are unknown to the web UI while it is
	// not attached to a daemon, e.g. after deluged was restarted.
	if isUnknownMethod(err) && !strings.HasPrefix(method, "web.") && !strings.HasPrefix(method, "auth.") {
		if cerr := c.connectDaemon(ctx); cerr != nil {
			return nil, cerr
		}
		return c.rawCall(ctx, method, params...)
	}
	return res, err
}

func isUnknownMethod(err error) bool {
	var re *rpcError
	if !errors.As(err, &re) {
		return false
	}
	return re.Code == codeUnknownMethod || strings.Contains(strings.ToLower(re.Message), "unknown method")
}

func (c *Client) Version(ctx context.Context) (string, error) {
	// Always log in here so a wrong password is reported even when an old
	// session cookie would still be accepted.
	if err := c.login(ctx); err != nil {
		return "", err
	}
	res, err := c.call(ctx, "daemon.get_version")
	if isUnknownMethod(err) {
		res, err = c.call(ctx, "daemon.info") // Deluge 1.x
	}
	if err != nil {
		return "", err
	}
	var v string
	if err := json.Unmarshal(res, &v); err != nil {
		return "", errNotDeluge
	}
	return v, nil
}

// statusKeys are the per-torrent fields we ask the daemon for. Unknown keys
// (e.g. "label" without the Label plugin) are left out of the answer.
var statusKeys = []string{
	"name", "state", "progress", "total_wanted", "total_done", "total_uploaded",
	"all_time_download", "download_payload_rate", "upload_payload_rate",
	"ratio", "eta", "num_seeds", "num_peers", "label", "tracker_host",
	"download_location", "save_path", "time_added", "completed_time", "message",
}

// apiTorrent uses float64 throughout because Deluge reports several integer
// fields as floats depending on version.
type apiTorrent struct {
	Name             string  `json:"name"`
	State            string  `json:"state"`
	Progress         float64 `json:"progress"` // 0..100
	TotalWanted      float64 `json:"total_wanted"`
	TotalDone        float64 `json:"total_done"`
	TotalUploaded    float64 `json:"total_uploaded"`
	AllTimeDownload  float64 `json:"all_time_download"`
	DownloadRate     float64 `json:"download_payload_rate"`
	UploadRate       float64 `json:"upload_payload_rate"`
	Ratio            float64 `json:"ratio"`
	ETA              float64 `json:"eta"`
	NumSeeds         float64 `json:"num_seeds"`
	NumPeers         float64 `json:"num_peers"`
	Label            string  `json:"label"`
	TrackerHost      string  `json:"tracker_host"`
	DownloadLocation string  `json:"download_location"`
	SavePath         string  `json:"save_path"`
	TimeAdded        float64 `json:"time_added"`
	CompletedTime    float64 `json:"completed_time"`
	Message          string  `json:"message"`
}

func (c *Client) Torrents(ctx context.Context) ([]client.Torrent, error) {
	res, err := c.call(ctx, "core.get_torrents_status", map[string]any{}, statusKeys)
	if err != nil {
		return nil, err
	}
	var raw map[string]apiTorrent
	if err := json.Unmarshal(res, &raw); err != nil {
		return nil, errNotDeluge
	}
	c.updateTotals(ctx)
	out := make([]client.Torrent, 0, len(raw))
	for hash, t := range raw {
		out = append(out, t.toTorrent(hash))
	}
	return out, nil
}

func (t apiTorrent) toTorrent(hash string) client.Torrent {
	progress := t.Progress / 100
	done := progress >= 1

	downloaded := int64(t.AllTimeDownload)
	if downloaded <= 0 {
		downloaded = int64(t.TotalDone)
	}
	eta := int64(t.ETA)
	if eta <= 0 || done {
		eta = -1
	}
	completed := int64(t.CompletedTime)
	if completed < 0 || !done {
		completed = 0
	}
	path := t.DownloadLocation
	if path == "" {
		path = t.SavePath
	}
	state := mapState(t.State)
	msg := ""
	if state == client.StateError {
		msg = t.Message
	}
	return client.Torrent{
		Hash:         strings.ToLower(hash),
		Name:         t.Name,
		State:        state,
		RawState:     t.State,
		Progress:     progress,
		Size:         int64(t.TotalWanted),
		Downloaded:   downloaded,
		Uploaded:     int64(t.TotalUploaded),
		DownloadRate: int64(t.DownloadRate),
		UploadRate:   int64(t.UploadRate),
		Ratio:        t.Ratio,
		ETA:          eta,
		Seeds:        int(t.NumSeeds),
		Peers:        int(t.NumPeers),
		Label:        t.Label,
		Tags:         []string{},
		Tracker:      client.TrackerDomain(t.TrackerHost),
		SavePath:     path,
		AddedAt:      int64(t.TimeAdded),
		CompletedAt:  completed,
		Message:      msg,
	}
}

func mapState(s string) client.State {
	switch s {
	case "Downloading":
		return client.StateDownloading
	case "Seeding":
		return client.StateSeeding
	case "Paused":
		return client.StatePaused
	case "Queued":
		return client.StateQueued
	case "Checking", "Allocating":
		return client.StateChecking
	case "Moving":
		return client.StateMoving
	case "Error":
		return client.StateError
	default:
		return client.StateUnknown
	}
}

func (c *Client) Pause(ctx context.Context, hashes []string) error {
	return c.setRunning(ctx, hashes, "pause")
}

func (c *Client) Resume(ctx context.Context, hashes []string) error {
	return c.setRunning(ctx, hashes, "resume")
}

// setRunning tries the Deluge 2 method (core.pause_torrents) first and falls
// back to the Deluge 1 name (core.pause_torrent) when it does not exist.
func (c *Client) setRunning(ctx context.Context, hashes []string, verb string) error {
	if len(hashes) == 0 {
		return nil
	}
	c.mu.Lock()
	known := c.legacyPause
	c.mu.Unlock()
	remember := func(isLegacy bool) {
		c.mu.Lock()
		c.legacyPause = &isLegacy
		c.mu.Unlock()
	}

	if known == nil || !*known {
		_, err := c.call(ctx, "core."+verb+"_torrents", hashes)
		if err == nil {
			remember(false)
			return nil
		}
		if !isUnknownMethod(err) {
			return err
		}
	}
	if _, err := c.call(ctx, "core."+verb+"_torrent", hashes); err != nil {
		return err
	}
	remember(true)
	return nil
}

// statsRecheck is how often a missing Stats plugin is looked for again.
const statsRecheck = 5 * time.Minute

// updateTotals refreshes the client-wide totals. It runs right after a
// successful call, so the session is valid and the plain rawCall is enough;
// failures only leave the numbers unknown.
func (c *Client) updateTotals(ctx context.Context) {
	totals := client.UnknownTotals

	var session struct {
		Upload   float64 `json:"total_upload"`
		Download float64 `json:"total_download"`
	}
	if res, err := c.rawCall(ctx, "core.get_session_status", []string{"total_upload", "total_download"}); err == nil &&
		json.Unmarshal(res, &session) == nil {
		totals.SessionUpload, totals.SessionDownload = int64(session.Upload), int64(session.Download)
	}

	c.totalsMu.Lock()
	tryPlugin := c.statsPlugin || time.Since(c.statsPluginChecked) > statsRecheck
	c.totalsMu.Unlock()
	if tryPlugin {
		// The Stats plugin keeps totals across restarts, counted from when
		// it was enabled.
		res, err := c.rawCall(ctx, "stats.get_totals")
		var all struct {
			Upload   *float64 `json:"total_upload"`
			Download *float64 `json:"total_download"`
		}
		ok := err == nil && json.Unmarshal(res, &all) == nil && all.Upload != nil && all.Download != nil
		if ok {
			totals.AllTimeUpload, totals.AllTimeDownload = int64(*all.Upload), int64(*all.Download)
		}
		c.totalsMu.Lock()
		c.statsPlugin, c.statsPluginChecked = ok, time.Now()
		c.totalsMu.Unlock()
	}

	c.totalsMu.Lock()
	c.totals, c.haveTotals = totals, true
	c.totalsMu.Unlock()
}

// Totals returns the totals seen during the last Torrents call.
func (c *Client) Totals() (client.Totals, bool) {
	c.totalsMu.Lock()
	defer c.totalsMu.Unlock()
	return c.totals, c.haveTotals
}

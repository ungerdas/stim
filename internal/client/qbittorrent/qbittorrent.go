// Package qbittorrent talks to qBittorrent's WebUI API (v2) directly.
package qbittorrent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/ungerdas/stim/internal/client"
)

const Kind client.Kind = "qbittorrent"

func init() {
	client.Register(client.Info{
		Kind:          Kind,
		Label:         "qBittorrent",
		NeedsUsername: true,
		URLHint:       "http://192.168.1.10:8080",
		Capabilities:  caps,

		ExportsTorrentFiles: true,
		CanMoveFiles:        true,
	}, New)
}

var caps = client.Capabilities{Labels: true, Tags: true}

// Client is a qBittorrent WebUI API client for one instance.
type Client struct {
	base string
	cfg  client.Config
	http *http.Client

	// syncMu serialises Torrents calls, which share the sync state.
	syncMu sync.Mutex
	sync   syncState

	mu sync.Mutex
	// qBittorrent 5 renamed pause/resume to stop/start. Once we know which
	// pair this instance answers to, remember it.
	legacyPause *bool
}

// New builds a qBittorrent client.
func New(cfg client.Config) (client.Client, error) {
	base, err := client.BaseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	return &Client{base: base, cfg: cfg, http: client.NewHTTPClient(cfg)}, nil
}

func (c *Client) Kind() client.Kind                 { return Kind }
func (c *Client) Capabilities() client.Capabilities { return caps }

var errAuth = fmt.Errorf("%w: wrong username or password", client.ErrAuth)

var errNotQBittorrent = fmt.Errorf("%w: the URL did not answer like a qBittorrent WebUI", client.ErrNotClient)

// payload is a request body with its content type.
type payload struct {
	body        []byte
	contentType string
}

func formPayload(form url.Values) *payload {
	if form == nil {
		return nil
	}
	return &payload{body: []byte(form.Encode()), contentType: "application/x-www-form-urlencoded"}
}

func (c *Client) newRequest(ctx context.Context, method, path string, p *payload) (*http.Request, error) {
	var body io.Reader
	if p != nil {
		body = bytes.NewReader(p.body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if p != nil {
		req.Header.Set("Content-Type", p.contentType)
	}
	// qBittorrent's CSRF protection requires Referer/Origin to match the host.
	req.Header.Set("Referer", c.base)
	if c.cfg.BasicUser != "" {
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}
	return req, nil
}

func (c *Client) login(ctx context.Context) error {
	form := url.Values{"username": {c.cfg.Username}, "password": {c.cfg.Password}}
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v2/auth/login", formPayload(form))
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(body))

	switch {
	case resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: qBittorrent has temporarily banned this address after too many failed logins", client.ErrAuth)
	case resp.StatusCode == http.StatusUnauthorized && c.cfg.BasicUser == "":
		return errAuth
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: HTTP basic auth was rejected", client.ErrAuth)
	case resp.StatusCode == http.StatusOK && text == "Fails.":
		return errAuth
	case resp.StatusCode == http.StatusOK, resp.StatusCode == http.StatusNoContent:
		return nil
	default:
		return fmt.Errorf("login failed: HTTP %d", resp.StatusCode)
	}
}

// do performs a request and logs in (once) if the session is missing or has
// expired. qBittorrent answers 403 to API calls without a valid session.
func (c *Client) do(ctx context.Context, method, path string, form url.Values) ([]byte, int, error) {
	return c.doPayload(ctx, method, path, formPayload(form))
}

func (c *Client) doPayload(ctx context.Context, method, path string, p *payload) ([]byte, int, error) {
	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, method, path, p)
		if err != nil {
			return nil, 0, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, 0, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, 0, err
		}
		if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized) && attempt == 0 {
			if err := c.login(ctx); err != nil {
				return nil, 0, err
			}
			continue
		}
		return body, resp.StatusCode, nil
	}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	body, status, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", path, status)
	}
	return body, nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/api/v2/app/version", nil)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(body))
	// A reverse proxy or the wrong URL typically answers with an HTML page.
	if status != http.StatusOK || v == "" || len(v) > 40 || strings.ContainsAny(v, "<{") {
		return "", fmt.Errorf("%w (HTTP %d)", errNotQBittorrent, status)
	}
	return strings.TrimPrefix(v, "v"), nil
}

type apiTorrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	State        string  `json:"state"`
	Progress     float64 `json:"progress"`
	Size         int64   `json:"size"`
	Downloaded   int64   `json:"downloaded"`
	Uploaded     int64   `json:"uploaded"`
	DlSpeed      int64   `json:"dlspeed"`
	UpSpeed      int64   `json:"upspeed"`
	Ratio        float64 `json:"ratio"`
	ETA          int64   `json:"eta"`
	NumSeeds     int     `json:"num_seeds"`
	NumLeechs    int     `json:"num_leechs"`
	Category     string  `json:"category"`
	Tags         string  `json:"tags"`
	Tracker      string  `json:"tracker"`
	SavePath     string  `json:"save_path"`
	AddedOn      int64   `json:"added_on"`
	CompletionOn int64   `json:"completion_on"`
	InfohashV1   string  `json:"infohash_v1"` // qBittorrent 4.4 and later
	InfohashV2   string  `json:"infohash_v2"`
}

// altHash returns the torrent's other ID form, if it has one.
func (t apiTorrent) altHash() string {
	hash := strings.ToLower(t.Hash)
	v1 := strings.ToLower(t.InfohashV1)
	v2 := strings.ToLower(t.InfohashV2)
	if len(v2) > 40 {
		v2 = v2[:40]
	}
	switch {
	case v1 != "" && v1 != hash:
		return v1
	case v2 != "" && v2 != hash:
		return v2
	}
	return ""
}

// etaInfinity is the value qBittorrent reports when it has no estimate.
const etaInfinity = 8640000

func (t apiTorrent) toTorrent() client.Torrent {
	eta := t.ETA
	if eta >= etaInfinity || eta < 0 {
		eta = -1
	}
	completed := t.CompletionOn
	if completed < 0 || t.Progress < 1 {
		completed = 0
	}
	return client.Torrent{
		Hash:         strings.ToLower(t.Hash),
		AltHash:      t.altHash(),
		Name:         t.Name,
		State:        mapState(t.State),
		RawState:     t.State,
		Progress:     t.Progress,
		Size:         t.Size,
		Downloaded:   t.Downloaded,
		Uploaded:     t.Uploaded,
		DownloadRate: t.DlSpeed,
		UploadRate:   t.UpSpeed,
		Ratio:        t.Ratio,
		ETA:          eta,
		Seeds:        t.NumSeeds,
		Peers:        t.NumLeechs,
		Label:        t.Category,
		Tags:         splitTags(t.Tags),
		Tracker:      client.TrackerDomain(t.Tracker),
		SavePath:     t.SavePath,
		AddedAt:      t.AddedOn,
		CompletedAt:  completed,
	}
}

func splitTags(s string) []string {
	tags := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func mapState(s string) client.State {
	switch s {
	case "downloading", "forcedDL", "metaDL", "forcedMetaDL", "stalledDL", "allocating":
		return client.StateDownloading
	case "uploading", "forcedUP", "stalledUP":
		return client.StateSeeding
	case "pausedDL", "pausedUP", "stoppedDL", "stoppedUP":
		return client.StatePaused
	case "queuedDL", "queuedUP":
		return client.StateQueued
	case "checkingDL", "checkingUP", "checkingResumeData":
		return client.StateChecking
	case "moving":
		return client.StateMoving
	case "error", "missingFiles":
		return client.StateError
	default:
		return client.StateUnknown
	}
}

func (c *Client) Pause(ctx context.Context, hashes []string) error {
	return c.setRunning(ctx, hashes, false)
}

func (c *Client) Resume(ctx context.Context, hashes []string) error {
	return c.setRunning(ctx, hashes, true)
}

// setRunning tries the qBittorrent 5 endpoint names first (stop/start) and
// falls back to the v4 names (pause/resume) when the instance answers 404.
func (c *Client) setRunning(ctx context.Context, hashes []string, run bool) error {
	if len(hashes) == 0 {
		return nil
	}
	modern, legacy := "stop", "pause"
	if run {
		modern, legacy = "start", "resume"
	}
	form := url.Values{"hashes": {strings.Join(hashes, "|")}}

	c.mu.Lock()
	known := c.legacyPause
	c.mu.Unlock()

	try := func(name string) (int, error) {
		_, status, err := c.do(ctx, http.MethodPost, "/api/v2/torrents/"+name, form)
		return status, err
	}
	remember := func(isLegacy bool) {
		c.mu.Lock()
		c.legacyPause = &isLegacy
		c.mu.Unlock()
	}

	if known == nil || !*known {
		status, err := try(modern)
		if err != nil {
			return err
		}
		if status == http.StatusOK {
			remember(false)
			return nil
		}
		if status != http.StatusNotFound {
			return fmt.Errorf("%s: HTTP %d", modern, status)
		}
	}
	status, err := try(legacy)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", legacy, status)
	}
	remember(true)
	return nil
}

// Remove deletes torrents, and their files when deleteData is set.
// qBittorrent answers 200 also for hashes it does not have.
func (c *Client) Remove(ctx context.Context, hashes []string, deleteData bool) error {
	if len(hashes) == 0 {
		return nil
	}
	form := url.Values{"hashes": {strings.Join(hashes, "|")}, "deleteFiles": {strconv.FormatBool(deleteData)}}
	_, status, err := c.do(ctx, http.MethodPost, "/api/v2/torrents/delete", form)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("delete: HTTP %d", status)
	}
	return nil
}

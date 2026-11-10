// Package transmission talks to Transmission's RPC interface
// (<url>/transmission/rpc).
package transmission

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ungerdas/stim/internal/client"
)

const Kind client.Kind = "transmission"

func init() {
	client.Register(client.Info{
		Kind:          Kind,
		Label:         "Transmission",
		NeedsUsername: true,
		URLHint:       "http://192.168.1.10:9091",
		Capabilities:  caps,
		// The RPC interface does not hand out .torrent files, but tells
		// where each one is, so it can be read over SSH.
		ExportsTorrentFiles: false,
		SSH:                 "required",
		CanMoveFiles:        true,
	}, New)
}

// Transmission 3.0 and later has labels: several per torrent, so they map to
// tags rather than to a single label.
var caps = client.Capabilities{Labels: false, Tags: true}

// Compile-time checks.
var (
	_ client.Client            = (*Client)(nil)
	_ client.Transferer        = (*Client)(nil)
	_ client.Mover             = (*Client)(nil)
	_ client.TorrentFilePather = (*Client)(nil)
	_ client.TotalsReporter    = (*Client)(nil)
	_ client.TrackerLister     = (*Client)(nil)
)

// Client is a Transmission RPC client for one instance.
type Client struct {
	endpoint string
	cfg      client.Config
	http     *http.Client
	tag      atomic.Int64

	mu        sync.Mutex
	sessionID string // CSRF token, see do

	totalsMu   sync.Mutex
	totals     client.Totals
	haveTotals bool
}

// New builds a Transmission client. The URL may be the server
// (http://host:9091), the web interface (…/transmission/web/) or the RPC
// endpoint itself.
func New(cfg client.Config) (client.Client, error) {
	base, err := client.BaseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(base)
	p := strings.TrimRight(u.Path, "/")
	switch {
	case p == "":
		u.Path = "/transmission/rpc"
	case strings.HasSuffix(p, "/transmission/web"):
		u.Path = strings.TrimSuffix(p, "/web") + "/rpc"
	case strings.HasSuffix(p, "/transmission"):
		u.Path = p + "/rpc"
	}
	return &Client{endpoint: u.String(), cfg: cfg, http: client.NewHTTPClient(cfg)}, nil
}

func (c *Client) Kind() client.Kind                 { return Kind }
func (c *Client) Capabilities() client.Capabilities { return caps }

var (
	errAuth         = fmt.Errorf("%w: wrong username or password", client.ErrAuth)
	errNotTransmiss = fmt.Errorf("%w: the URL did not answer like Transmission", client.ErrNotClient)
)

type response struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

// call performs one RPC call. Transmission protects its RPC against
// cross-site requests with a session ID that it hands out in a 409 answer;
// the request is repeated once with it.
func (c *Client) call(ctx context.Context, method string, args any, out any) error {
	payload, err := json.Marshal(map[string]any{"method": method, "arguments": args, "tag": c.tag.Add(1)})
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		c.mu.Lock()
		if c.sessionID != "" {
			req.Header.Set("X-Transmission-Session-Id", c.sessionID)
		}
		c.mu.Unlock()
		// Transmission's own login is HTTP basic auth. A separate HTTP login
		// in front (a reverse proxy) is used only when no username is set.
		switch {
		case c.cfg.Username != "":
			req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
		case c.cfg.BasicUser != "":
			req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		switch resp.StatusCode {
		case http.StatusConflict:
			id := resp.Header.Get("X-Transmission-Session-Id")
			if id == "" {
				return errNotTransmiss
			}
			c.mu.Lock()
			c.sessionID = id
			c.mu.Unlock()
			continue
		case http.StatusUnauthorized:
			return errAuth
		case http.StatusForbidden:
			return errors.New("Transmission refused the request; check rpc-whitelist in its settings")
		case http.StatusOK:
		default:
			return fmt.Errorf("%s: HTTP %d", method, resp.StatusCode)
		}
		var r response
		if err := json.Unmarshal(body, &r); err != nil || r.Result == "" {
			return errNotTransmiss
		}
		if r.Result != "success" {
			return fmt.Errorf("Transmission: %s", r.Result)
		}
		if out != nil {
			if err := json.Unmarshal(r.Arguments, out); err != nil {
				return errNotTransmiss
			}
		}
		return nil
	}
	return errNotTransmiss
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var res struct {
		Version string `json:"version"`
	}
	if err := c.call(ctx, "session-get", map[string]any{"fields": []string{"version", "rpc-version"}}, &res); err != nil {
		return "", err
	}
	if res.Version == "" {
		return "", errNotTransmiss
	}
	// "4.0.5 (a6fe2a64aa)" → "4.0.5"
	return strings.Fields(res.Version)[0], nil
}

var fields = []string{
	"hashString", "name", "status", "percentDone", "sizeWhenDone", "downloadedEver", "uploadedEver",
	"rateDownload", "rateUpload", "uploadRatio", "eta", "peersSendingToUs", "peersGettingFromUs",
	"labels", "trackers", "downloadDir", "addedDate", "doneDate", "error", "errorString",
}

type apiTorrent struct {
	Hash         string   `json:"hashString"`
	Name         string   `json:"name"`
	Status       int      `json:"status"`
	PercentDone  float64  `json:"percentDone"`
	SizeWhenDone int64    `json:"sizeWhenDone"`
	Downloaded   int64    `json:"downloadedEver"`
	Uploaded     int64    `json:"uploadedEver"`
	RateDown     int64    `json:"rateDownload"`
	RateUp       int64    `json:"rateUpload"`
	Ratio        float64  `json:"uploadRatio"`
	ETA          int64    `json:"eta"`
	Seeds        int      `json:"peersSendingToUs"`
	Peers        int      `json:"peersGettingFromUs"`
	Labels       []string `json:"labels"`
	Trackers     []struct {
		Announce string `json:"announce"`
		Tier     int    `json:"tier"`
	} `json:"trackers"`
	DownloadDir string `json:"downloadDir"`
	AddedDate   int64  `json:"addedDate"`
	DoneDate    int64  `json:"doneDate"`
	Error       int    `json:"error"`
	ErrorString string `json:"errorString"`
}

// Transmission's status numbers.
const (
	statusStopped = iota
	statusCheckWait
	statusCheck
	statusDownloadWait
	statusDownload
	statusSeedWait
	statusSeed
)

// errLocal is Transmission's error number for a problem with local data, as
// opposed to tracker warnings and errors (1 and 2).
const errLocal = 3

func (t apiTorrent) toTorrent() client.Torrent {
	state, raw := client.StateUnknown, "unknown"
	switch t.Status {
	case statusStopped:
		state, raw = client.StatePaused, "stopped"
	case statusCheckWait:
		state, raw = client.StateChecking, "check pending"
	case statusCheck:
		state, raw = client.StateChecking, "checking"
	case statusDownloadWait:
		state, raw = client.StateQueued, "download queued"
	case statusDownload:
		state, raw = client.StateDownloading, "downloading"
	case statusSeedWait:
		state, raw = client.StateQueued, "seed queued"
	case statusSeed:
		state, raw = client.StateSeeding, "seeding"
	}
	msg := ""
	if t.Error != 0 {
		msg = t.ErrorString
	}
	if t.Error == errLocal {
		state = client.StateError
	}
	eta := t.ETA
	if eta < 0 || t.PercentDone >= 1 {
		eta = -1
	}
	ratio := t.Ratio
	if ratio < 0 {
		ratio = -1
	}
	completed := t.DoneDate
	if t.PercentDone < 1 {
		completed = 0
	}
	tracker, bestTier := "", -1
	for _, tr := range t.Trackers {
		if bestTier == -1 || tr.Tier < bestTier {
			tracker, bestTier = client.TrackerDomain(tr.Announce), tr.Tier
		}
	}
	tags := t.Labels
	if tags == nil {
		tags = []string{}
	}
	return client.Torrent{
		Hash:         strings.ToLower(t.Hash),
		Name:         t.Name,
		State:        state,
		RawState:     raw,
		Progress:     t.PercentDone,
		Size:         t.SizeWhenDone,
		Downloaded:   t.Downloaded,
		Uploaded:     t.Uploaded,
		DownloadRate: t.RateDown,
		UploadRate:   t.RateUp,
		Ratio:        ratio,
		ETA:          eta,
		Seeds:        t.Seeds,
		Peers:        t.Peers,
		Tags:         tags,
		Tracker:      tracker,
		SavePath:     t.DownloadDir,
		AddedAt:      t.AddedDate,
		CompletedAt:  completed,
		Message:      msg,
	}
}

func (c *Client) get(ctx context.Context, ids []string) ([]apiTorrent, error) {
	args := map[string]any{"fields": fields}
	if ids != nil {
		args["ids"] = ids
	}
	var res struct {
		Torrents []apiTorrent `json:"torrents"`
	}
	if err := c.call(ctx, "torrent-get", args, &res); err != nil {
		return nil, err
	}
	return res.Torrents, nil
}

func (c *Client) Torrents(ctx context.Context) ([]client.Torrent, error) {
	raw, err := c.get(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]client.Torrent, 0, len(raw))
	for _, t := range raw {
		out = append(out, t.toTorrent())
	}
	c.updateTotals(ctx)
	return out, nil
}

func (c *Client) updateTotals(ctx context.Context) {
	var res struct {
		Cumulative struct {
			Up   int64 `json:"uploadedBytes"`
			Down int64 `json:"downloadedBytes"`
		} `json:"cumulative-stats"`
		Current struct {
			Up   int64 `json:"uploadedBytes"`
			Down int64 `json:"downloadedBytes"`
		} `json:"current-stats"`
	}
	totals := client.UnknownTotals
	if err := c.call(ctx, "session-stats", map[string]any{}, &res); err == nil {
		totals = client.Totals{
			AllTimeUpload:   res.Cumulative.Up,
			AllTimeDownload: res.Cumulative.Down,
			SessionUpload:   res.Current.Up,
			SessionDownload: res.Current.Down,
		}
	}
	c.totalsMu.Lock()
	c.totals, c.haveTotals = totals, true
	c.totalsMu.Unlock()
}

// Totals returns what session-stats said during the last Torrents call.
func (c *Client) Totals() (client.Totals, bool) {
	c.totalsMu.Lock()
	defer c.totalsMu.Unlock()
	return c.totals, c.haveTotals
}

func (c *Client) Pause(ctx context.Context, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}
	return c.call(ctx, "torrent-stop", map[string]any{"ids": hashes}, nil)
}

func (c *Client) Resume(ctx context.Context, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}
	return c.call(ctx, "torrent-start", map[string]any{"ids": hashes}, nil)
}

// Remove removes torrents, and their files when deleteData is set. Unknown
// hashes are ignored by Transmission.
func (c *Client) Remove(ctx context.Context, hashes []string, deleteData bool) error {
	if len(hashes) == 0 {
		return nil
	}
	return c.call(ctx, "torrent-remove", map[string]any{"ids": hashes, "delete-local-data": deleteData}, nil)
}

func (c *Client) Torrent(ctx context.Context, hash string) (client.Torrent, bool, error) {
	raw, err := c.get(ctx, []string{hash})
	if err != nil {
		return client.Torrent{}, false, err
	}
	for _, t := range raw {
		if strings.EqualFold(t.Hash, hash) {
			return t.toTorrent(), true, nil
		}
	}
	return client.Torrent{}, false, nil
}

// AddStopped adds the torrent paused, at opts.SavePath. The label and tags
// become Transmission labels.
func (c *Client) AddStopped(ctx context.Context, torrentFile []byte, opts client.AddOptions) error {
	var res struct {
		Added *struct {
			Hash string `json:"hashString"`
		} `json:"torrent-added"`
		Duplicate *struct {
			Hash string `json:"hashString"`
		} `json:"torrent-duplicate"`
	}
	args := map[string]any{
		"metainfo":     base64.StdEncoding.EncodeToString(torrentFile),
		"download-dir": opts.SavePath,
		"paused":       true,
	}
	if err := c.call(ctx, "torrent-add", args, &res); err != nil {
		return err
	}
	if res.Duplicate != nil {
		return errors.New("Transmission already has this torrent")
	}
	if res.Added == nil {
		return errors.New("Transmission refused the torrent")
	}
	labels := append([]string{}, opts.Tags...)
	if opts.Label != "" {
		labels = append([]string{opts.Label}, labels...)
	}
	if len(labels) > 0 {
		// Labels need Transmission 3.0; a missing one is cosmetic.
		_ = c.call(ctx, "torrent-set", map[string]any{"ids": []string{res.Added.Hash}, "labels": labels}, nil)
	}
	return nil
}

func (c *Client) Recheck(ctx context.Context, hash string) error {
	return c.call(ctx, "torrent-verify", map[string]any{"ids": []string{hash}}, nil)
}

// RemoveKeepData removes the torrent and leaves its files.
func (c *Client) RemoveKeepData(ctx context.Context, hash string) error {
	return c.Remove(ctx, []string{hash}, false)
}

func (c *Client) Move(ctx context.Context, hash, path string) error {
	return c.call(ctx, "torrent-set-location", map[string]any{"ids": []string{hash}, "location": path, "move": true}, nil)
}

// TorrentFilePath returns where Transmission keeps the torrent's .torrent
// file on its server.
func (c *Client) TorrentFilePath(ctx context.Context, hash string) (string, error) {
	var res struct {
		Torrents []struct {
			TorrentFile string `json:"torrentFile"`
		} `json:"torrents"`
	}
	if err := c.call(ctx, "torrent-get", map[string]any{"ids": []string{hash}, "fields": []string{"torrentFile"}}, &res); err != nil {
		return "", err
	}
	if len(res.Torrents) == 0 || res.Torrents[0].TorrentFile == "" {
		return "", errors.New("Transmission did not say where the torrent file is")
	}
	return res.Torrents[0].TorrentFile, nil
}

// Trackers lists the torrent's announce URLs, best tier first.
func (c *Client) Trackers(ctx context.Context, hash string) ([]string, error) {
	raw, err := c.get(ctx, []string{hash})
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	urls := []string{}
	for tier := 0; len(urls) < len(raw[0].Trackers) && tier < 64; tier++ {
		for _, tr := range raw[0].Trackers {
			if tr.Tier == tier {
				urls = append(urls, tr.Announce)
			}
		}
	}
	return urls, nil
}

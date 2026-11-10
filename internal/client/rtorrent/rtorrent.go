// Package rtorrent talks to rTorrent over XML-RPC, either directly (an
// /RPC2 endpoint in front of rTorrent's SCGI port) or through ruTorrent's
// httprpc plugin, which passes XML-RPC on.
package rtorrent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/remotefiles"
)

const Kind client.Kind = "rtorrent"

func init() {
	client.Register(client.Info{
		Kind:          Kind,
		Label:         "rTorrent",
		NeedsUsername: true,
		URLHint:       "https://server/brukernavn/rutorrent eller …/RPC2",
		Capabilities:  caps,
		// Exporting works over a plain /RPC2 endpoint but is refused by
		// ruTorrent, so SSH is the fallback, also for deleting files.
		ExportsTorrentFiles: false,
		SSH:                 "optional",
		// rTorrent cannot move files itself.
		CanMoveFiles: false,
	}, New)
}

// The label is ruTorrent's: d.custom1, URL-encoded.
var caps = client.Capabilities{Labels: true, Tags: false}

// Compile-time checks.
var (
	_ client.Client            = (*Client)(nil)
	_ client.Transferer        = (*Client)(nil)
	_ client.Exporter          = (*Client)(nil)
	_ client.TorrentFilePather = (*Client)(nil)
	_ client.TotalsReporter    = (*Client)(nil)
	_ client.TrackerLister     = (*Client)(nil)
)

// Client is an rTorrent XML-RPC client for one instance.
type Client struct {
	cfg  client.Config
	http *http.Client
	// candidates are the endpoints to try, in order, until one answers.
	candidates []string

	mu       sync.Mutex
	endpoint string // the one that answered

	totalsMu   sync.Mutex
	totals     client.Totals
	haveTotals bool
}

// New builds an rTorrent client. The URL may be an XML-RPC endpoint (…/RPC2
// or a .php file) or the address of ruTorrent, in which case its httprpc
// plugin is used.
func New(cfg client.Config) (client.Client, error) {
	base, err := client.BaseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(base)
	p := strings.ToLower(u.Path)
	var candidates []string
	if strings.HasSuffix(p, ".php") || strings.Contains(p, "/rpc") {
		candidates = []string{base}
	} else {
		candidates = []string{base + "/plugins/httprpc/action.php", base + "/RPC2", base}
	}
	return &Client{cfg: cfg, http: client.NewHTTPClient(cfg), candidates: candidates}, nil
}

func (c *Client) Kind() client.Kind                 { return Kind }
func (c *Client) Capabilities() client.Capabilities { return caps }

var (
	errAuth     = fmt.Errorf("%w: wrong username or password", client.ErrAuth)
	errNotRT    = fmt.Errorf("%w: the URL did not answer like rTorrent or ruTorrent", client.ErrNotClient)
	errNotFound = errors.New("rTorrent does not have this torrent")
	errRefused  = errors.New("refused by the server")
)

// post sends one XML-RPC call to one endpoint.
func (c *Client) post(ctx context.Context, endpoint, method string, params []any) (any, error) {
	body, err := encodeCall(method, params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	// The login in front of rTorrent is HTTP basic auth, from the web server
	// or ruTorrent's host. The separate HTTP fields are used when the main
	// ones are empty.
	switch {
	case c.cfg.Username != "":
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	case c.cfg.BasicUser != "":
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errAuth
	}
	v, decErr := decodeResponse(data)
	var f *Fault
	if errors.As(decErr, &f) {
		// ruTorrent answers refused commands with 403 and a fault.
		if resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: %s", errRefused, f.Message)
		}
		return nil, f
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w (HTTP %d)", errNotRT, resp.StatusCode)
	}
	if decErr != nil {
		return nil, errNotRT
	}
	return v, nil
}

// call sends a call to the endpoint that works, finding it first if needed.
func (c *Client) call(ctx context.Context, method string, params ...any) (any, error) {
	c.mu.Lock()
	endpoint := c.endpoint
	c.mu.Unlock()
	if endpoint != "" {
		return c.post(ctx, endpoint, method, params)
	}
	var firstErr error
	for _, e := range c.candidates {
		v, err := c.post(ctx, e, "system.client_version", nil)
		if err == nil && asString(v) != "" {
			c.mu.Lock()
			c.endpoint = e
			c.mu.Unlock()
			return c.post(ctx, e, method, params)
		}
		if errors.Is(err, client.ErrAuth) {
			return nil, err
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil || errors.Is(firstErr, errNotXMLRPC) {
		firstErr = errNotRT
	}
	return nil, firstErr
}

func isNotFound(err error) bool {
	var f *Fault
	return errors.As(err, &f) && strings.Contains(strings.ToLower(f.Message), "info-hash")
}

func (c *Client) Version(ctx context.Context) (string, error) {
	v, err := c.call(ctx, "system.client_version")
	if err != nil {
		return "", err
	}
	return asString(v), nil
}

// fields are read for every torrent with d.multicall2. The last one runs a
// nested t.multicall that joins the torrent's announce URLs with "#", the
// way ruTorrent itself reads them.
var fields = []string{
	"d.hash=", "d.name=", "d.state=", "d.is_active=", "d.complete=", "d.hashing=",
	"d.size_bytes=", "d.completed_bytes=", "d.up.total=", "d.down.total=", "d.up.rate=", "d.down.rate=",
	"d.ratio=", "d.peers_complete=", "d.peers_accounted=", "d.custom1=", "d.directory=", "d.is_multi_file=",
	"d.load_date=", "d.timestamp.finished=", "d.message=", "d.custom=addtime",
	`cat="$t.multicall=d.hash=,t.url=,cat=#"`,
}

const (
	fHash = iota
	fName
	fState
	fActive
	fComplete
	fHashing
	fSize
	fCompleted
	fUp
	fDown
	fUpRate
	fDownRate
	fRatio
	fSeeds
	fPeers
	fLabel
	fDirectory
	fMulti
	fLoadDate
	fFinished
	fMessage
	fAddTime
	fTrackers
)

func toTorrent(row []any) client.Torrent {
	get := func(i int) any {
		if i < len(row) {
			return row[i]
		}
		return nil
	}
	size, done := asInt(get(fSize)), asInt(get(fCompleted))
	progress := 0.0
	if size > 0 {
		progress = float64(done) / float64(size)
	}
	if progress > 1 {
		progress = 1
	}
	complete := asInt(get(fComplete)) == 1

	state, raw := client.StateUnknown, ""
	switch {
	case asInt(get(fHashing)) != 0:
		state, raw = client.StateChecking, "hashing"
	case asInt(get(fState)) == 0:
		state, raw = client.StatePaused, "stopped"
	case asInt(get(fActive)) == 0:
		state, raw = client.StatePaused, "paused"
	case complete:
		state, raw = client.StateSeeding, "seeding"
	default:
		state, raw = client.StateDownloading, "downloading"
	}

	downRate := asInt(get(fDownRate))
	eta := int64(-1)
	if !complete && downRate > 0 {
		eta = (size - done) / downRate
	}

	// The save folder, as other clients report it: for a multi-file
	// torrent rTorrent's directory is the torrent's own folder.
	dir := asString(get(fDirectory))
	name := asString(get(fName))
	savePath := dir
	if asInt(get(fMulti)) == 1 && path.Base(dir) == name {
		savePath = path.Dir(dir)
	}

	label := asString(get(fLabel))
	if l, err := url.PathUnescape(label); err == nil {
		label = l
	}

	added := asInt(get(fAddTime)) // set by ruTorrent when it adds a torrent
	if added == 0 {
		added = asInt(get(fLoadDate))
	}
	finished := asInt(get(fFinished))
	if !complete {
		finished = 0
	}

	tracker := ""
	for _, u := range strings.Split(asString(get(fTrackers)), "#") {
		if d := client.TrackerDomain(u); d != "" && !strings.HasPrefix(u, "dht://") {
			tracker = d
			break
		}
	}

	return client.Torrent{
		Hash:         strings.ToLower(asString(get(fHash))),
		Name:         name,
		State:        state,
		RawState:     raw,
		Progress:     progress,
		Size:         size,
		Downloaded:   asInt(get(fDown)),
		Uploaded:     asInt(get(fUp)),
		DownloadRate: downRate,
		UploadRate:   asInt(get(fUpRate)),
		Ratio:        float64(asInt(get(fRatio))) / 1000,
		ETA:          eta,
		Seeds:        int(asInt(get(fSeeds))),
		Peers:        int(asInt(get(fPeers))),
		Label:        label,
		Tags:         []string{},
		Tracker:      tracker,
		SavePath:     savePath,
		AddedAt:      added,
		CompletedAt:  finished,
		Message:      asString(get(fMessage)),
	}
}

func (c *Client) Torrents(ctx context.Context) ([]client.Torrent, error) {
	params := make([]any, 0, len(fields)+2)
	params = append(params, "", "main")
	for _, f := range fields {
		params = append(params, f)
	}
	v, err := c.call(ctx, "d.multicall2", params...)
	if err != nil {
		return nil, err
	}
	rows, ok := v.([]any)
	if !ok {
		return nil, errNotRT
	}
	out := make([]client.Torrent, 0, len(rows))
	for _, r := range rows {
		if row, ok := r.([]any); ok {
			out = append(out, toTorrent(row))
		}
	}
	c.updateTotals(ctx)
	return out, nil
}

func (c *Client) updateTotals(ctx context.Context) {
	totals := client.UnknownTotals
	if up, err := c.call(ctx, "throttle.global_up.total"); err == nil {
		totals.SessionUpload = asInt(up)
	}
	if down, err := c.call(ctx, "throttle.global_down.total"); err == nil {
		totals.SessionDownload = asInt(down)
	}
	c.totalsMu.Lock()
	c.totals, c.haveTotals = totals, true
	c.totalsMu.Unlock()
}

// Totals returns rTorrent's totals since it started; it keeps none for
// all time.
func (c *Client) Totals() (client.Totals, bool) {
	c.totalsMu.Lock()
	defer c.totalsMu.Unlock()
	return c.totals, c.haveTotals
}

// multicall runs the given commands for every hash in one request. Each
// command takes the hash as its only argument.
func (c *Client) multicall(ctx context.Context, hashes []string, commands ...string) error {
	if len(hashes) == 0 {
		return nil
	}
	calls := make([]any, 0, len(hashes)*len(commands))
	for _, h := range hashes {
		for _, cmd := range commands {
			calls = append(calls, map[string]any{"methodName": cmd, "params": []any{strings.ToUpper(h)}})
		}
	}
	v, err := c.call(ctx, "system.multicall", calls)
	if err != nil {
		return err
	}
	// Each result is either [value] or a fault struct.
	results, _ := v.([]any)
	for _, r := range results {
		if m, ok := r.(map[string]any); ok {
			msg := asString(m["faultString"])
			if !strings.Contains(strings.ToLower(msg), "info-hash") { // already gone is fine
				return fmt.Errorf("rTorrent: %s", msg)
			}
		}
	}
	return nil
}

// Pause stops torrents and closes their files, as ruTorrent's "Stop" does.
func (c *Client) Pause(ctx context.Context, hashes []string) error {
	return c.multicall(ctx, hashes, "d.stop", "d.close")
}

func (c *Client) Resume(ctx context.Context, hashes []string) error {
	return c.multicall(ctx, hashes, "d.open", "d.start")
}

func (c *Client) Torrent(ctx context.Context, hash string) (client.Torrent, bool, error) {
	h := strings.ToUpper(hash)
	calls := make([]any, 0, len(fields))
	for _, f := range fields[:len(fields)-1] {
		cmd, arg, _ := strings.Cut(f, "=")
		params := []any{h}
		if arg != "" {
			params = append(params, arg)
		}
		calls = append(calls, map[string]any{"methodName": cmd, "params": params})
	}
	v, err := c.call(ctx, "system.multicall", calls)
	if err != nil {
		return client.Torrent{}, false, err
	}
	results, _ := v.([]any)
	row := make([]any, 0, len(fields))
	for _, r := range results {
		switch x := r.(type) {
		case []any:
			if len(x) > 0 {
				row = append(row, x[0])
			} else {
				row = append(row, nil)
			}
		case map[string]any:
			if strings.Contains(strings.ToLower(asString(x["faultString"])), "info-hash") {
				return client.Torrent{}, false, nil
			}
			row = append(row, nil)
		}
	}
	urls, err := c.Trackers(ctx, hash)
	if err == nil {
		row = append(row, strings.Join(urls, "#"))
	}
	return toTorrent(row), true, nil
}

// Trackers lists the torrent's announce URLs.
func (c *Client) Trackers(ctx context.Context, hash string) ([]string, error) {
	v, err := c.call(ctx, "t.multicall", strings.ToUpper(hash), "", "t.url=")
	if err != nil {
		return nil, err
	}
	rows, _ := v.([]any)
	urls := []string{}
	for _, r := range rows {
		if row, ok := r.([]any); ok && len(row) > 0 {
			if u := asString(row[0]); u != "" && !strings.HasPrefix(u, "dht://") {
				urls = append(urls, u)
			}
		}
	}
	return urls, nil
}

// AddStopped loads the torrent without starting it. For a multi-file torrent
// rTorrent puts the files in a folder named after the torrent inside the
// directory, so the save folder can be passed as it is.
func (c *Client) AddStopped(ctx context.Context, torrentFile []byte, opts client.AddOptions) error {
	params := []any{"", Base64(torrentFile), "d.directory.set=" + opts.SavePath,
		"d.custom.set=addtime," + strconv.FormatInt(time.Now().Unix(), 10)}
	if opts.Label != "" {
		params = append(params, "d.custom1.set="+url.PathEscape(opts.Label))
	}
	if _, err := c.call(ctx, "load.raw", params...); err != nil {
		return fmt.Errorf("rTorrent did not accept the torrent: %w", err)
	}
	return nil
}

func (c *Client) Recheck(ctx context.Context, hash string) error {
	_, err := c.call(ctx, "d.check_hash", strings.ToUpper(hash))
	return err
}

// RemoveKeepData removes the torrent from rTorrent; d.erase never touches
// the files.
func (c *Client) RemoveKeepData(ctx context.Context, hash string) error {
	_, err := c.call(ctx, "d.erase", strings.ToUpper(hash))
	if isNotFound(err) {
		return nil
	}
	return err
}

// Remove removes torrents. rTorrent has no command for deleting a
// download's files, so with deleteData Stim deletes them itself: through
// rTorrent's execute command where the endpoint allows it, otherwise over
// SSH. If neither is possible nothing is removed, so files are never left
// behind silently.
func (c *Client) Remove(ctx context.Context, hashes []string, deleteData bool) error {
	if !deleteData {
		return c.multicall(ctx, hashes, "d.erase")
	}
	var failed []string
	for _, h := range hashes {
		if err := c.removeWithData(ctx, h); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not delete %d of the torrents: %s", len(failed), failed[0])
	}
	return nil
}

// contentPath returns where a torrent's files are, as rTorrent sees them.
func (c *Client) contentPath(ctx context.Context, hash string) (string, error) {
	h := strings.ToUpper(hash)
	v, err := c.call(ctx, "system.multicall", []any{
		map[string]any{"methodName": "d.directory", "params": []any{h}},
		map[string]any{"methodName": "d.name", "params": []any{h}},
		map[string]any{"methodName": "d.is_multi_file", "params": []any{h}},
	})
	if err != nil {
		return "", err
	}
	results, _ := v.([]any)
	if len(results) != 3 {
		return "", errNotRT
	}
	val := func(i int) any {
		if r, ok := results[i].([]any); ok && len(r) > 0 {
			return r[0]
		}
		return nil
	}
	if m, ok := results[0].(map[string]any); ok {
		if strings.Contains(strings.ToLower(asString(m["faultString"])), "info-hash") {
			return "", errNotFound
		}
		return "", fmt.Errorf("rTorrent: %s", asString(m["faultString"]))
	}
	dir, name := asString(val(0)), asString(val(1))
	p := dir
	if asInt(val(2)) != 1 {
		p = path.Join(dir, name)
	}
	// Only ever delete the torrent's own file or folder.
	if name == "" || path.Base(p) != name || !path.IsAbs(p) || strings.Count(p, "/") < 2 {
		return "", fmt.Errorf("refusing to delete %q", p)
	}
	return p, nil
}

func (c *Client) removeWithData(ctx context.Context, hash string) error {
	p, err := c.contentPath(ctx, hash)
	if errors.Is(err, errNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// Stop first so rTorrent lets go of the files.
	if err := c.multicall(ctx, []string{hash}, "d.stop", "d.close"); err != nil {
		return err
	}
	_, execErr := c.call(ctx, "execute.throw", "", "rm", "-rf", "--", p)
	if execErr != nil {
		ssh := c.cfg.SSH
		if !ssh.Configured() {
			return fmt.Errorf("rTorrent cannot delete files through this address (%v), and no SSH access is set up for it; the torrent is stopped but kept", execErr)
		}
		if err := remotefiles.RemoveAll(ctx, ssh, p); err != nil {
			return fmt.Errorf("deleting %s over SSH failed: %w; the torrent is stopped but kept", p, err)
		}
	}
	return c.RemoveKeepData(ctx, hash)
}

// ExportTorrent reads the torrent's file from rTorrent's session folder
// through rTorrent itself. ruTorrent refuses the command used for that, and
// then the file is read over SSH instead (see TorrentFilePath).
func (c *Client) ExportTorrent(ctx context.Context, hash string) ([]byte, error) {
	p, err := c.TorrentFilePath(ctx, hash)
	if err != nil {
		return nil, err
	}
	v, err := c.call(ctx, "execute.capture", "", "base64", "-w0", "--", p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", client.ErrCannotExport, err)
	}
	data, err := decodeBase64(asString(v))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", client.ErrCannotExport, err)
	}
	return data, nil
}

// TorrentFilePath is where rTorrent keeps the torrent's file: its session
// folder, named by the upper-case hash.
func (c *Client) TorrentFilePath(ctx context.Context, hash string) (string, error) {
	v, err := c.call(ctx, "session.path")
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(asString(v))
	if dir == "" {
		return "", errors.New("rTorrent has no session folder, so it keeps no torrent files")
	}
	return path.Join(dir, strings.ToUpper(hash)+".torrent"), nil
}

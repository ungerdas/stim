// Package client defines the client-agnostic model that every torrent client
// adapter maps onto. The rest of Stim only ever talks to this interface, so
// adding a new client type means adding one adapter package and nothing else.
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
)

// Kind identifies a torrent client type, e.g. "qbittorrent" or "deluge".
type Kind string

// State is Stim's normalised torrent state. Each adapter maps the client's
// own states onto these; the original value is kept in Torrent.RawState.
type State string

const (
	StateDownloading State = "downloading"
	StateSeeding     State = "seeding"
	StatePaused      State = "paused"
	StateQueued      State = "queued"
	StateChecking    State = "checking"
	StateMoving      State = "moving"
	StateError       State = "error"
	StateUnknown     State = "unknown"
)

// Torrent is one torrent as reported by a client, in client-neutral form.
// Sizes are bytes, rates are bytes per second, times are Unix seconds.
type Torrent struct {
	Hash         string   `json:"hash"`
	Name         string   `json:"name"`
	State        State    `json:"state"`
	RawState     string   `json:"rawState"`
	Progress     float64  `json:"progress"` // 0..1
	Size         int64    `json:"size"`
	Downloaded   int64    `json:"downloaded"`
	Uploaded     int64    `json:"uploaded"`
	DownloadRate int64    `json:"downloadRate"`
	UploadRate   int64    `json:"uploadRate"`
	Ratio        float64  `json:"ratio"` // -1 when undefined
	ETA          int64    `json:"eta"`   // seconds, -1 when unknown
	Seeds        int      `json:"seeds"` // connected
	Peers        int      `json:"peers"` // connected
	Label        string   `json:"label"` // qBittorrent category / Deluge label
	Tags         []string `json:"tags"`
	Tracker      string   `json:"tracker"` // tracker site, see TrackerDomain
	SavePath     string   `json:"savePath"`
	AddedAt      int64    `json:"addedAt"`
	CompletedAt  int64    `json:"completedAt"` // 0 when not completed
	Message      string   `json:"message,omitempty"`
}

// Capabilities tells the UI what a client type supports, so features can be
// shown or hidden per instance instead of assuming qBittorrent semantics.
type Capabilities struct {
	Labels bool `json:"labels"` // a single label/category per torrent
	Tags   bool `json:"tags"`   // multiple free-form tags per torrent
}

// Config is what an adapter needs to reach one client instance.
type Config struct {
	URL           string
	Username      string
	Password      string
	BasicUser     string // optional HTTP basic auth in front of the client
	BasicPass     string
	TLSSkipVerify bool
}

// Client is the contract every adapter implements.
type Client interface {
	Kind() Kind
	Capabilities() Capabilities
	// Version returns the client's version string. It doubles as the
	// connection test: it must fail if the client cannot be reached or the
	// credentials are wrong.
	Version(ctx context.Context) (string, error)
	Torrents(ctx context.Context) ([]Torrent, error)
	Pause(ctx context.Context, hashes []string) error
	Resume(ctx context.Context, hashes []string) error
}

// Totals are client-wide transfer totals in bytes, including protocol
// overhead. -1 means the client does not report that number.
type Totals struct {
	// AllTimeUpload covers every torrent the client has had, also removed
	// ones. qBittorrent keeps it; Deluge only with its Stats plugin enabled.
	AllTimeUpload   int64 `json:"allTimeUpload"`
	AllTimeDownload int64 `json:"allTimeDownload"`
	// Session totals reset when the client restarts.
	SessionUpload   int64 `json:"sessionUpload"`
	SessionDownload int64 `json:"sessionDownload"`
}

// UnknownTotals is a Totals with nothing known.
var UnknownTotals = Totals{AllTimeUpload: -1, AllTimeDownload: -1, SessionUpload: -1, SessionDownload: -1}

// TotalsReporter is implemented by adapters that know client-wide totals.
// Totals returns what was seen during the last successful Torrents call;
// ok is false before the first one.
type TotalsReporter interface {
	Totals() (t Totals, ok bool)
}

// AddOptions describes how Stim hands a torrent to a client.
type AddOptions struct {
	SavePath string   // where the data already is, as the client sees the path
	Label    string   // category/label; empty for none
	Tags     []string // ignored by clients without tags
}

// Transferer is implemented by adapters whose torrents can be moved to and
// from other clients. Every method must be safe to call on a torrent whose
// data another client is still seeding.
type Transferer interface {
	// Torrent returns one torrent; ok is false when the client does not
	// have it.
	Torrent(ctx context.Context, hash string) (t Torrent, ok bool, err error)
	// AddStopped adds a .torrent without starting it. The client must use
	// opts.SavePath as is, keep the original file layout, and must not move
	// the data anywhere on its own afterwards.
	AddStopped(ctx context.Context, torrentFile []byte, opts AddOptions) error
	// Recheck makes the client hash the files on disk.
	Recheck(ctx context.Context, hash string) error
	// RemoveKeepData removes the torrent from the client and leaves every
	// file on disk. There is deliberately no flag for deleting data here.
	RemoveKeepData(ctx context.Context, hash string) error
	// Move asks the client to move the torrent's data to path.
	Move(ctx context.Context, hash, path string) error
}

// Exporter is implemented by adapters that can hand out the original
// .torrent file through the client's API.
type Exporter interface {
	ExportTorrent(ctx context.Context, hash string) ([]byte, error)
}

// TrackerLister returns a torrent's announce URLs. It is used to restore
// them when a stored .torrent file has none.
type TrackerLister interface {
	Trackers(ctx context.Context, hash string) ([]string, error)
}

// Info describes a client type to the UI, so the "add instance" form is
// driven by the backend and needs no changes when a new adapter is added.
type Info struct {
	Kind          Kind         `json:"kind"`
	Label         string       `json:"label"`
	NeedsUsername bool         `json:"needsUsername"`
	URLHint       string       `json:"urlHint"`
	Capabilities  Capabilities `json:"capabilities"`
	// ExportsTorrentFiles is true when the client's API can hand out
	// .torrent files. Otherwise moving torrents away from it needs file
	// access to TorrentDirHint on the server.
	ExportsTorrentFiles bool   `json:"exportsTorrentFiles"`
	TorrentDirHint      string `json:"torrentDirHint,omitempty"`
}

// Factory builds a Client for one instance.
type Factory func(Config) (Client, error)

type registration struct {
	info    Info
	factory Factory
}

var registry = map[Kind]registration{}

// Register makes a client type available. Adapters call it from init.
func Register(info Info, f Factory) {
	if _, dup := registry[info.Kind]; dup {
		panic(fmt.Sprintf("client: kind %q registered twice", info.Kind))
	}
	registry[info.Kind] = registration{info: info, factory: f}
}

// ErrUnknownKind is returned by New for a kind no adapter has registered.
var ErrUnknownKind = errors.New("unknown client kind")

// Adapters wrap these so the rest of Stim can tell failure kinds apart.
var (
	// ErrAuth means the client rejected the credentials. Stim stops retrying
	// on its own after this: clients such as qBittorrent ban an address
	// after a handful of failed logins.
	ErrAuth = errors.New("authentication failed")
	// ErrNotClient means something answered at the URL, but not the expected
	// client type.
	ErrNotClient = errors.New("unexpected answer")
)

// ErrorCode classifies an error for the UI: "auth", "wrong-type",
// "unreachable" or "other". It returns "" for nil.
func ErrorCode(err error) string {
	var netErr net.Error
	var urlErr *url.Error
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrAuth):
		return "auth"
	case errors.Is(err, ErrNotClient):
		return "wrong-type"
	case errors.As(err, &urlErr), errors.As(err, &netErr), errors.Is(err, context.DeadlineExceeded):
		return "unreachable"
	default:
		return "other"
	}
}

// New builds a client of the given kind.
func New(kind Kind, cfg Config) (Client, error) {
	r, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	return r.factory(cfg)
}

// Known reports whether an adapter is registered for kind.
func Known(kind Kind) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds lists all registered client types, sorted by label.
func Kinds() []Info {
	out := make([]Info, 0, len(registry))
	for _, r := range registry {
		out = append(out, r.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

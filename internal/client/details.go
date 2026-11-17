package client

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// Details is what a client knows about one torrent beyond its row in the
// list, for the detail panel.
type Details struct {
	Files     []File        `json:"files"`
	Trackers  []TrackerInfo `json:"trackers"`
	Peers     []Peer        `json:"peers"`
	Comment   string        `json:"comment,omitempty"`
	CreatedBy string        `json:"createdBy,omitempty"`
	CreatedAt int64         `json:"createdAt,omitempty"` // Unix seconds
	PieceSize int64         `json:"pieceSize,omitempty"`
	Pieces    int           `json:"pieces,omitempty"`
	Private   *bool         `json:"private,omitempty"`
}

// File is one file of a torrent.
type File struct {
	Path     string  `json:"path"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"` // 0..1
	Wanted   bool    `json:"wanted"`
}

// Tracker statuses in TrackerInfo.Status.
const (
	TrackerWorking  = "working"
	TrackerUpdating = "updating"
	TrackerFailing  = "error"
	TrackerWaiting  = "waiting" // not contacted yet, or not the one in use
	TrackerDisabled = "disabled"
)

// TrackerInfo is one tracker of a torrent. Counts are -1 when unknown.
type TrackerInfo struct {
	URL        string `json:"url"` // passkeys masked, see MaskURL
	Tier       int    `json:"tier"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	Seeds      int    `json:"seeds"`
	Leeches    int    `json:"leeches"`
	Downloaded int    `json:"downloaded"`
}

// Peer is one connected peer.
type Peer struct {
	Address   string  `json:"address"`
	Client    string  `json:"client"`
	Progress  float64 `json:"progress"` // 0..1
	DownRate  int64   `json:"downRate"`
	UpRate    int64   `json:"upRate"`
	Flags     string  `json:"flags,omitempty"`
	Country   string  `json:"country,omitempty"`
	Encrypted bool    `json:"encrypted"`
}

// Detailer is implemented by adapters that can describe one torrent.
type Detailer interface {
	Details(ctx context.Context, hash string) (Details, error)
}

// secretKeys are query parameters that carry a tracker login.
var secretKeys = map[string]bool{
	"passkey": true, "pk": true, "authkey": true, "auth": true, "torrent_pass": true,
	"key": true, "apikey": true, "token": true,
}

// secretSegment is a path segment that looks like a passkey: a long run of
// letters and digits.
var secretSegment = regexp.MustCompile(`^[A-Za-z0-9]{16,}$`)

// MaskURL hides the passkey in a tracker address, so the address can be
// shown (and end up in a screenshot) without it.
func MaskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.User != nil {
		u.User = url.User("•••")
	}
	parts := strings.Split(u.Path, "/")
	for i, p := range parts {
		if secretSegment.MatchString(p) {
			parts[i] = "•••"
		}
	}
	u.Path = strings.Join(parts, "/")
	u.RawPath = ""
	q := u.Query()
	for k, vs := range q {
		if secretKeys[strings.ToLower(k)] || (len(vs) == 1 && secretSegment.MatchString(vs[0])) {
			q[k] = []string{"•••"}
		}
	}
	u.RawQuery = q.Encode()
	out, err := url.PathUnescape(u.String())
	if err != nil {
		return u.String()
	}
	return out
}

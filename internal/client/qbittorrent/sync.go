package qbittorrent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

// qBittorrent's /api/v2/sync/maindata sends everything once and afterwards
// only what changed since the response ID ("rid") the caller passes back.
// That keeps polling cheap for large collections, and unlike
// /torrents/info it lists every torrent's trackers: the "tracker" field of
// a torrent is empty in qBittorrent 4 whenever its tracker is not working.

type syncTorrent struct {
	fields map[string]json.RawMessage // everything seen so far for the torrent
	parsed apiTorrent
}

type syncState struct {
	rid      int64
	torrents map[string]*syncTorrent
	trackers map[string][]string // announce URL → hashes of the torrents using it
}

func (s *syncState) reset() {
	s.rid = 0
	s.torrents = map[string]*syncTorrent{}
	s.trackers = map[string][]string{}
}

type mainData struct {
	Rid             int64                                 `json:"rid"`
	FullUpdate      bool                                  `json:"full_update"`
	Torrents        map[string]map[string]json.RawMessage `json:"torrents"`
	TorrentsRemoved []string                              `json:"torrents_removed"`
	Trackers        map[string][]string                   `json:"trackers"`
	TrackersRemoved []string                              `json:"trackers_removed"`
}

// Torrents returns every torrent, using incremental sync.
func (c *Client) Torrents(ctx context.Context) ([]client.Torrent, error) {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()

	if c.sync.torrents == nil {
		c.sync.reset()
	}
	body, err := c.get(ctx, "/api/v2/sync/maindata?rid="+strconv.FormatInt(c.sync.rid, 10))
	if err != nil {
		return nil, err
	}
	var md mainData
	if err := json.Unmarshal(body, &md); err != nil || md.Rid == 0 {
		c.sync.reset()
		return nil, errNotQBittorrent
	}
	if err := c.sync.apply(md); err != nil {
		// Start over with a full update next time rather than carry on
		// from a state that may be half applied.
		c.sync.reset()
		return nil, err
	}
	return c.sync.list(), nil
}

func (s *syncState) apply(md mainData) error {
	// A full update replaces everything: the first call, a new login
	// session, or qBittorrent deciding the caller is too far behind.
	if md.FullUpdate {
		s.reset()
	}
	for _, h := range md.TorrentsRemoved {
		delete(s.torrents, strings.ToLower(h))
	}
	for h, fields := range md.Torrents {
		h = strings.ToLower(h)
		st, ok := s.torrents[h]
		if !ok {
			st = &syncTorrent{fields: make(map[string]json.RawMessage, len(fields))}
			s.torrents[h] = st
		}
		for k, v := range fields {
			st.fields[k] = v
		}
		merged, err := json.Marshal(st.fields)
		if err != nil {
			return err
		}
		var t apiTorrent
		if err := json.Unmarshal(merged, &t); err != nil {
			return errNotQBittorrent
		}
		t.Hash = h // the hash is the key, not a field, in sync data
		st.parsed = t
	}
	for _, u := range md.TrackersRemoved {
		delete(s.trackers, u)
	}
	for u, hashes := range md.Trackers {
		s.trackers[u] = hashes
	}
	s.rid = md.Rid
	return nil
}

func (s *syncState) list() []client.Torrent {
	// Each torrent's tracker site from the tracker map, used when the
	// torrent's own field is empty. With several trackers, pick the
	// alphabetically first site so the choice is stable between polls.
	fallback := map[string]string{}
	for u, hashes := range s.trackers {
		domain := client.TrackerDomain(u)
		if domain == "" || strings.HasPrefix(u, "**") { // "** [DHT] **" and friends
			continue
		}
		for _, h := range hashes {
			h = strings.ToLower(h)
			if cur, ok := fallback[h]; !ok || domain < cur {
				fallback[h] = domain
			}
		}
	}
	out := make([]client.Torrent, 0, len(s.torrents))
	for h, st := range s.torrents {
		t := st.parsed.toTorrent()
		if t.Tracker == "" {
			t.Tracker = fallback[h]
		}
		out = append(out, t)
	}
	return out
}

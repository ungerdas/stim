package qbittorrent

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/ungerdas/stim/internal/client"
)

// Tracker problems per torrent. The torrent list does not carry tracker
// messages, so they are fetched for the torrents that have a problem only,
// and kept for a while: with thousands of torrents, asking for every one on
// every poll would be far too much.
//
// qBittorrent 5.1 and later flag the torrents with tracker errors in the
// list, and can include their trackers in /torrents/info. Older versions
// have neither; there a torrent with trackers but no working one (its
// "tracker" field is empty) is looked up on its own, a few per poll.

const (
	trackerRecheck   = 2 * time.Minute // how long a looked-up status is kept
	trackerBatch     = 100             // hashes per /torrents/info request
	trackerOldPerRun = 25              // single lookups per poll on old versions
)

type trackerEntry struct {
	status, message string
	at              time.Time
}

type apiTracker struct {
	URL string `json:"url"`
	// 0 disabled, 1 not contacted, 2 working, 3 updating, 4 not working;
	// qBittorrent 5.1 splits the last into 4, 5 (the tracker said no) and
	// 6 (unreachable).
	Status    int    `json:"status"`
	Msg       string `json:"msg"`
	Endpoints []struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	} `json:"endpoints"`
}

func trackerVerdict(trackers []apiTracker) (string, string) {
	working := false
	var errs []string
	for _, tr := range trackers {
		if strings.HasPrefix(tr.URL, "** [") { // DHT, PeX, LSD
			continue
		}
		if tr.Status == 2 {
			working = true
		}
		if tr.Status >= 4 {
			// The tracker's own message first, then each endpoint's (one per
			// network interface): one of them may say "unregistered" while
			// another only timed out.
			n := len(errs)
			if tr.Msg != "" {
				errs = append(errs, tr.Msg)
			}
			for _, ep := range tr.Endpoints {
				if ep.Status >= 4 && ep.Msg != "" {
					errs = append(errs, ep.Msg)
				}
			}
			if len(errs) == n {
				errs = append(errs, "not working")
			}
		}
	}
	return client.TrackerVerdict(working, errs)
}

// annotateTrackers fills in TrackerStatus and TrackerMessage. It is called
// with syncMu held.
func (c *Client) annotateTrackers(ctx context.Context, list []client.Torrent) {
	if c.trackerCache == nil {
		c.trackerCache = map[string]trackerEntry{}
	}
	var candidates, stale []string
	isCandidate := map[string]bool{}
	for _, t := range list {
		p := c.sync.torrents[t.Hash].parsed
		var problem bool
		switch {
		case p.HasTrackerError != nil || p.HasOtherAnnounceError != nil:
			problem = (p.HasTrackerError != nil && *p.HasTrackerError) || (p.HasOtherAnnounceError != nil && *p.HasOtherAnnounceError)
		default:
			// No flags: stopped and queued torrents do not announce, so an
			// empty tracker field says nothing about them.
			problem = p.Tracker == "" && p.TrackersCount != 0 &&
				t.State != client.StatePaused && t.State != client.StateQueued
		}
		if !problem {
			continue
		}
		candidates = append(candidates, t.Hash)
		isCandidate[t.Hash] = true
		if e, ok := c.trackerCache[t.Hash]; !ok || time.Since(e.at) > trackerRecheck {
			stale = append(stale, t.Hash)
		}
	}
	for h := range c.trackerCache {
		if !isCandidate[h] {
			delete(c.trackerCache, h)
		}
	}
	if len(stale) > 0 {
		c.lookUpTrackers(ctx, stale)
	}
	for i := range list {
		if e, ok := c.trackerCache[list[i].Hash]; ok && isCandidate[list[i].Hash] {
			list[i].TrackerStatus, list[i].TrackerMessage = e.status, e.message
		}
	}
}

func (c *Client) lookUpTrackers(ctx context.Context, hashes []string) {
	now := time.Now()
	if !c.noIncludeTrackers {
		for start := 0; start < len(hashes); start += trackerBatch {
			end := min(start+trackerBatch, len(hashes))
			body, err := c.get(ctx, "/api/v2/torrents/info?includeTrackers=true&hashes="+url.QueryEscape(strings.Join(hashes[start:end], "|")))
			if err != nil {
				return
			}
			var items []struct {
				Hash     string        `json:"hash"`
				Trackers *[]apiTracker `json:"trackers"`
			}
			if json.Unmarshal(body, &items) != nil {
				return
			}
			if len(items) > 0 && items[0].Trackers == nil {
				c.noIncludeTrackers = true // older qBittorrent ignores the parameter
				break
			}
			for _, it := range items {
				status, msg := trackerVerdict(*it.Trackers)
				c.trackerCache[strings.ToLower(it.Hash)] = trackerEntry{status, msg, now}
			}
		}
		if !c.noIncludeTrackers {
			return
		}
	}
	for i, h := range hashes {
		if i >= trackerOldPerRun {
			return
		}
		body, err := c.get(ctx, "/api/v2/torrents/trackers?hash="+url.QueryEscape(h))
		if err != nil {
			return
		}
		var trs []apiTracker
		if json.Unmarshal(body, &trs) != nil {
			return
		}
		status, msg := trackerVerdict(trs)
		c.trackerCache[h] = trackerEntry{status, msg, now}
	}
}

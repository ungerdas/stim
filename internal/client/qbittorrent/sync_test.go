package qbittorrent

import (
	"encoding/json"
	"testing"

	"github.com/ungerdas/stim/internal/client"
)

func md(t *testing.T, s string) mainData {
	t.Helper()
	var m mainData
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func byHash(s *syncState) map[string]string {
	out := map[string]string{}
	for _, t := range s.list() {
		out[t.Hash] = t.RawState + "|" + t.Tracker + "|" + t.Name
	}
	return out
}

func TestSyncMergesPartialUpdates(t *testing.T) {
	var s syncState
	s.reset()
	steps := []struct {
		data string
		want map[string]string
	}{
		{ // full update; qBittorrent 4 leaves "tracker" empty for a tracker that is down
			`{"rid":1,"full_update":true,
			  "torrents":{"AAA":{"name":"a","state":"stalledUP","tracker":""},"bbb":{"name":"b","state":"uploading","tracker":"https://t.norbits.net/announce"}},
			  "trackers":{"https://norbits.net/announce.php?passkey=x":["aaa"],"https://t.norbits.net/announce":["bbb"]},
			  "server_state":{"alltime_ul":5000000000000,"alltime_dl":100,"up_info_data":1024,"dl_info_data":0}}`,
			map[string]string{"aaa": "stalledUP|norbits.net|a", "bbb": "uploading|norbits.net|b"},
		},
		{ // only the changed field arrives
			`{"rid":2,"torrents":{"aaa":{"state":"pausedUP"}},"server_state":{"up_info_data":2048}}`,
			map[string]string{"aaa": "pausedUP|norbits.net|a", "bbb": "uploading|norbits.net|b"},
		},
		{ // a new torrent and a full hash list for its tracker
			`{"rid":3,"torrents":{"ccc":{"name":"c","state":"downloading"}},"trackers":{"udp://tracker.opentrackr.org:1337/announce":["ccc"]}}`,
			map[string]string{"aaa": "pausedUP|norbits.net|a", "bbb": "uploading|norbits.net|b", "ccc": "downloading|opentrackr.org|c"},
		},
		{ // removals
			`{"rid":4,"torrents_removed":["bbb"],"trackers_removed":["https://t.norbits.net/announce"]}`,
			map[string]string{"aaa": "pausedUP|norbits.net|a", "ccc": "downloading|opentrackr.org|c"},
		},
		{ // a new session starts over
			`{"rid":1,"full_update":true,"torrents":{"ddd":{"name":"d","state":"stalledUP"}},"trackers":{}}`,
			map[string]string{"ddd": "stalledUP||d"},
		},
	}
	for i, step := range steps {
		if err := s.apply(md(t, step.data)); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		got := byHash(&s)
		if len(got) != len(step.want) {
			t.Fatalf("step %d: got %v, want %v", i, got, step.want)
		}
		for h, w := range step.want {
			if got[h] != w {
				t.Errorf("step %d: %s = %q, want %q", i, h, got[h], w)
			}
		}
	}
	if s.rid != 1 {
		t.Errorf("rid = %d, want 1", s.rid)
	}
	// The last step was a full update without server_state.
	if s.totals.AllTimeUpload != -1 {
		t.Errorf("totals should be reset by a full update, got %+v", s.totals)
	}
}

func TestSyncTotalsMergeChanges(t *testing.T) {
	var s syncState
	s.reset()
	_ = s.apply(md(t, `{"rid":1,"full_update":true,"server_state":{"alltime_ul":5000000000000,"alltime_dl":100,"up_info_data":1024,"dl_info_data":0}}`))
	_ = s.apply(md(t, `{"rid":2,"server_state":{"up_info_data":2048}}`))
	want := client.Totals{AllTimeUpload: 5000000000000, AllTimeDownload: 100, SessionUpload: 2048, SessionDownload: 0}
	if s.totals != want || !s.haveTotals {
		t.Errorf("totals = %+v, want %+v", s.totals, want)
	}
}

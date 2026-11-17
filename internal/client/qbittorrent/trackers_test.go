package qbittorrent

import (
	"encoding/json"
	"testing"

	"github.com/ungerdas/stim/internal/client"
)

func TestTrackerVerdict(t *testing.T) {
	for raw, want := range map[string]string{
		// qBittorrent 5.1+: 5 is "the tracker said no".
		`[{"url":"** [DHT] **","status":0,"msg":""},{"url":"https://t/a","status":5,"msg":"Unregistered torrent"}]`:                          client.TrackerUnregistered,
		`[{"url":"https://t/a","status":4,"msg":"Unregistered torrent"}]`:                                                                    client.TrackerUnregistered,
		`[{"url":"https://t/a","status":6,"msg":"timed out"}]`:                                                                               client.TrackerError,
		`[{"url":"https://t/a","status":4,"msg":"","endpoints":[{"status":4,"msg":"Host not found"}]}]`:                                      client.TrackerError,
		`[{"url":"https://t/a","status":2,"msg":""},{"url":"https://u/a","status":5,"msg":"Unregistered torrent"}]`:                          "",
		`[{"url":"https://t/a","status":1,"msg":""}]`:                                                                                        "",
		`[{"url":"https://t/a","status":5,"msg":"","endpoints":[{"status":4,"msg":"timed out"},{"status":5,"msg":"Unregistered torrent"}]}]`: client.TrackerUnregistered,
	} {
		var trs []apiTracker
		if err := json.Unmarshal([]byte(raw), &trs); err != nil {
			t.Fatal(err)
		}
		if got, _ := trackerVerdict(trs); got != want {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}

package client

import "testing"

func TestClassifyTrackerMessage(t *testing.T) {
	for msg, want := range map[string]string{
		"Unregistered torrent":                          TrackerUnregistered,
		`Tracker gave an error: "Unregistered torrent"`: TrackerUnregistered,
		"torrent not registered with this tracker":      TrackerUnregistered,
		"Torrent has been trumped":                      TrackerUnregistered,
		"Torrenten finnes ikke":                         TrackerUnregistered,
		"Host not found (authoritative)":                TrackerError,
		"Timeout was reached":                           TrackerError,
		"Connection refused":                            TrackerError,
		"Passkey not found":                             TrackerError,
		"tracker is down for maintenance":               TrackerError,
	} {
		if got := ClassifyTrackerMessage(msg); got != want {
			t.Errorf("%q: got %s, want %s", msg, got, want)
		}
	}
}

func TestTrackerVerdict(t *testing.T) {
	if s, _ := TrackerVerdict(true, []string{"Unregistered torrent"}); s != "" {
		t.Errorf("a working tracker means no problem, got %q", s)
	}
	if s, m := TrackerVerdict(false, []string{"Timeout", "Unregistered torrent"}); s != TrackerUnregistered || m != "Unregistered torrent" {
		t.Errorf("got %q %q", s, m)
	}
	if s, m := TrackerVerdict(false, []string{"", "Timeout"}); s != TrackerError || m != "Timeout" {
		t.Errorf("got %q %q", s, m)
	}
	if s, _ := TrackerVerdict(false, nil); s != "" {
		t.Errorf("no errors yet means unknown, got %q", s)
	}
}

func TestMaskURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://norbits.net/announce.php?passkey=0123456789abcdef0123456789abcdef": "https://norbits.net/announce.php?passkey=•••",
		"https://tracker.example/0123456789abcdef0123/announce":                     "https://tracker.example/•••/announce",
		"http://t.example:2710/a1b2c3d4e5f6a7b8c9d0/announce?info=x":                "http://t.example:2710/•••/announce?info=x",
		"udp://tracker.opentrackr.org:1337/announce":                                "udp://tracker.opentrackr.org:1337/announce",
		"** [DHT] **": "** [DHT] **",
	} {
		if got := MaskURL(in); got != want {
			t.Errorf("%s\n got %s\nwant %s", in, got, want)
		}
	}
}

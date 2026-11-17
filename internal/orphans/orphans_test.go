package orphans

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/sshtest"
	"github.com/ungerdas/stim/internal/store"
	"golang.org/x/crypto/ssh"
)

type fakeClient struct {
	client.Client // only Torrents is used
	list          []client.Torrent
	err           error
}

func (f *fakeClient) Torrents(context.Context) ([]client.Torrent, error) { return f.list, f.err }

type fakeSource struct {
	insts   []store.Instance
	clients map[string]*fakeClient
}

func (s fakeSource) Instances() []store.Instance { return s.insts }
func (s fakeSource) Lookup(id string) (client.Client, store.Instance, bool) {
	for _, in := range s.insts {
		if in.ID == id {
			return s.clients[id], in, true
		}
	}
	return nil, store.Instance{}, false
}

func write(t *testing.T, p string, size int, old bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if old {
		age := time.Now().Add(-48 * time.Hour)
		_ = os.Chtimes(p, age, age)
		_ = os.Chtimes(filepath.Dir(p), age, age)
	}
}

func setup(t *testing.T) (fakeSource, string) {
	t.Helper()
	host, port, key := sshtest.Serve(t, nil)
	dir := filepath.Join(t.TempDir(), "srv")
	dl := filepath.Join(dir, "downloads")
	write(t, filepath.Join(dl, "Keep.Torrent", "a.mkv"), 10, true)
	write(t, filepath.Join(dl, "single.mkv"), 10, true)
	write(t, filepath.Join(dl, "Leftover.Old", "a.mkv"), 100, true)
	write(t, filepath.Join(dl, "Leftover.Old", "Subs", "a.srt"), 5, true)
	write(t, filepath.Join(dl, "orphan.nfo"), 7, true)
	write(t, filepath.Join(dl, ".hidden"), 7, true)
	write(t, filepath.Join(dl, "Recent.Thing"), 7, false)
	write(t, filepath.Join(dl, "Looks.Like.Name.mkv"), 7, true)
	write(t, filepath.Join(dl, "tv", "Show.S01", "e1.mkv"), 3, true)
	write(t, filepath.Join(dl, "tv", "old.mkv"), 9, true)
	write(t, filepath.Join(dl, "Protected.Pack", "a.mkv"), 9, true)
	age := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(dl, "Leftover.Old"), age, age)

	sshInst := store.Instance{SSHHost: host, SSHPort: port, SSHUser: "u", SSHPassword: sshtest.Password,
		SSHHostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))}
	a, b, other := sshInst, sshInst, store.Instance{ID: "c", Name: "Elsewhere", URL: "http://elsewhere:8080"}
	a.ID, a.Name = "a", "qBit"
	b.ID, b.Name, b.SSHHostKey = "b", "Deluge", ""
	src := fakeSource{
		insts: []store.Instance{a, b, other},
		clients: map[string]*fakeClient{
			"a": {list: []client.Torrent{
				{Hash: "1", Name: "Keep.Torrent", SavePath: dl},
				{Hash: "2", Name: "single.mkv", SavePath: dl},
				// An episode cross-seeded from inside the pack: its folder is not scanned.
				{Hash: "3", Name: "a.mkv", SavePath: filepath.Join(dl, "Keep.Torrent")},
			}},
			"b": {list: []client.Torrent{
				{Hash: "4", Name: "Show.S01", SavePath: filepath.Join(dl, "tv")},
				{Hash: "5", Name: "Looks Like Name", SavePath: "/somewhere/else"},
			}},
			// Another server by name, but its torrent protects Protected.Pack:
			// the same machine may have been added under two names.
			"c": {list: []client.Torrent{{Hash: "6", Name: "Protected.Pack", SavePath: dl}}},
		},
	}
	return src, dl
}

func paths(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, filepath.Base(filepath.Dir(it.Path))+"/"+filepath.Base(it.Path))
	}
	sort.Strings(out)
	return out
}

func TestScan(t *testing.T) {
	src, dl := setup(t)
	res, err := Scan(context.Background(), src, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paths(res.Items), " ")
	if got != "downloads/Leftover.Old downloads/orphan.nfo tv/old.mkv" {
		t.Errorf("leftovers = %s", got)
	}
	if res.Recent != 1 || res.Total != 105+7+9 {
		t.Errorf("recent %d, total %d", res.Recent, res.Total)
	}
	for _, d := range res.Dirs {
		if strings.HasPrefix(d, filepath.Join(dl, "Keep.Torrent")) {
			t.Errorf("scanned inside a torrent's folder: %s", d)
		}
	}
	if len(res.Clients) != 2 {
		t.Errorf("clients = %v", res.Clients)
	}
}

func TestScanNeedsEveryClient(t *testing.T) {
	src, _ := setup(t)
	src.clients["b"].err = errors.New("offline")
	if _, err := Scan(context.Background(), src, "127.0.0.1"); err == nil || !strings.Contains(err.Error(), "Deluge") {
		t.Errorf("want an error naming the client, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	src, dl := setup(t)
	removed, failed, err := Delete(context.Background(), src, "127.0.0.1", []string{
		filepath.Join(dl, "Leftover.Old"),
		filepath.Join(dl, "Keep.Torrent"), // in use: refused
		filepath.Join(dl, "Recent.Thing"), // too new: refused
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || len(failed) != 2 {
		t.Fatalf("removed %v, failed %v", removed, failed)
	}
	if _, err := os.Stat(filepath.Join(dl, "Leftover.Old")); !os.IsNotExist(err) {
		t.Error("leftover still there")
	}
	for _, keep := range []string{"Keep.Torrent/a.mkv", "Recent.Thing", "orphan.nfo"} {
		if _, err := os.Stat(filepath.Join(dl, keep)); err != nil {
			t.Errorf("%s should still be there: %v", keep, err)
		}
	}
}

func TestOtherServersMayBeDown(t *testing.T) {
	src, _ := setup(t)
	src.clients["c"].err = errors.New("offline")
	res, err := Scan(context.Background(), src, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	// Without c's list its folder looks abandoned; that is the price of c
	// being down, and c is on another server by its settings.
	if !strings.Contains(strings.Join(paths(res.Items), " "), "Protected.Pack") || len(res.Unanswered) != 1 {
		t.Errorf("got %v, unanswered %v", paths(res.Items), res.Unanswered)
	}
}

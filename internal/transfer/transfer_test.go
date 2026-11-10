package transfer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/store"
	"github.com/ungerdas/stim/internal/torrentfile"
)

const fileData = "d8:announce18:http://t.example/a4:infod6:lengthi5e4:name5:a.mkv12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaa7:privatei1eee"

func testHash(t *testing.T) string {
	t.Helper()
	f, err := torrentfile.Parse([]byte(fileData))
	if err != nil {
		t.Fatal(err)
	}
	return f.InfoHashes()[0]
}

// fake is an in-memory client. dataFound is what a recheck finds.
type fake struct {
	idIndex   int // which of the file's info hashes this client uses as ID
	mu        sync.Mutex
	torrents  map[string]client.Torrent
	dataFound float64
	checkFor  int // polls a recheck stays in "checking"
	checking  int
	removeErr error
	removed   []string // hashes removed, always without data
}

func newFake() *fake { return &fake{torrents: map[string]client.Torrent{}, dataFound: 1, checkFor: 2} }

func (f *fake) Kind() client.Kind                                  { return "fake" }
func (f *fake) Capabilities() client.Capabilities                  { return client.Capabilities{} }
func (f *fake) Version(context.Context) (string, error)            { return "1", nil }
func (f *fake) Torrents(context.Context) ([]client.Torrent, error) { return nil, nil }
func (f *fake) Pause(context.Context, []string) error              { return nil }
func (f *fake) Remove(context.Context, []string, bool) error       { return nil }

func (f *fake) Resume(_ context.Context, hashes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range hashes {
		t := f.torrents[h]
		t.State = client.StateSeeding
		f.torrents[h] = t
	}
	return nil
}

func (f *fake) Torrent(_ context.Context, hash string) (client.Torrent, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[hash]
	if ok && t.State == client.StateChecking {
		f.checking--
		if f.checking <= 0 {
			t.State, t.Progress = client.StatePaused, f.dataFound
			f.torrents[hash] = t
		}
	}
	return t, ok, nil
}

func (f *fake) ExportTorrent(context.Context, string) ([]byte, error) { return []byte(fileData), nil }

func (f *fake) AddStopped(_ context.Context, data []byte, opts client.AddOptions) error {
	tf, err := torrentfile.Parse(data)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	h := tf.InfoHashes()[f.idIndex]
	f.torrents[h] = client.Torrent{Hash: h, Name: "a.mkv", State: client.StatePaused, SavePath: opts.SavePath, Label: opts.Label}
	return nil
}

func (f *fake) Recheck(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.torrents[hash]
	t.State, t.Progress = client.StateChecking, 0
	f.torrents[hash] = t
	f.checking = f.checkFor
	return nil
}

func (f *fake) RemoveKeepData(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.torrents, hash)
	f.removed = append(f.removed, hash)
	return nil
}

func (f *fake) Move(_ context.Context, hash, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.torrents[hash]
	t.SavePath = p
	f.torrents[hash] = t
	return nil
}

func (f *fake) has(hash string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.torrents[hash]
	return ok
}

type fakeClients map[string]*fake

func (c fakeClients) Lookup(id string) (client.Client, store.Instance, bool) {
	f, ok := c[id]
	return f, store.Instance{ID: id}, ok
}
func (fakeClients) Invalidate(...string) {}

type noKeys struct{}

func (noKeys) SetHostKey(string, string) error { return nil }

var fast = Timing{Poll: time.Millisecond, CallTimeout: time.Second, AppearWithin: 50 * time.Millisecond, SettleFor: 3 * time.Millisecond, Overall: 5 * time.Second}

func setup(t *testing.T) (*Engine, *fake, *fake, string) {
	t.Helper()
	hash := testHash(t)
	src, dst := newFake(), newFake()
	src.torrents[hash] = client.Torrent{Hash: hash, Name: "a.mkv", State: client.StateSeeding, Progress: 1, SavePath: "/data", Label: "tv"}
	return New(fakeClients{"src": src, "dst": dst}, noKeys{}, fast), src, dst, hash
}

func request(hash string, remove bool, newPath string) Request {
	r := Request{TargetID: "dst", RemoveFromSource: remove, NewPath: newPath}
	r.Items = append(r.Items, struct {
		InstanceID string `json:"instanceId"`
		Hash       string `json:"hash"`
		Name       string `json:"name"`
	}{"src", hash, "a.mkv"})
	return r
}

func wait(t *testing.T, e *Engine, id string) Item {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range e.Jobs() {
			if j.ID == id && j.FinishedAt != 0 {
				return j.Items[0]
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Item{}
}

func TestMoveRemovesFromSourceAndKeepsData(t *testing.T) {
	e, src, dst, hash := setup(t)
	job, err := e.Start(request(hash, true, "/ssd"))
	if err != nil {
		t.Fatal(err)
	}
	it := wait(t, e, job.ID)
	if it.State != StateDone {
		t.Fatalf("state %s: %s", it.State, it.Error)
	}
	if src.has(hash) || !dst.has(hash) {
		t.Error("torrent should now be only in the target")
	}
	got, _, _ := dst.Torrent(context.Background(), hash)
	if got.SavePath != "/ssd" || got.State != client.StateSeeding || got.Label != "tv" {
		t.Errorf("target torrent = %+v", got)
	}
}

func TestMissingDataRollsBack(t *testing.T) {
	e, src, dst, hash := setup(t)
	dst.dataFound = 0.4
	job, _ := e.Start(request(hash, true, ""))
	it := wait(t, e, job.ID)
	if it.State != StateFailed || it.Code != CodeDataNotFound || it.InTarget {
		t.Fatalf("got %+v", it)
	}
	if !src.has(hash) || dst.has(hash) {
		t.Error("source must keep the torrent and the target copy must be removed")
	}
	if len(src.removed) != 0 {
		t.Error("nothing may be removed from the source")
	}
}

func TestCancelDuringCheckRollsBack(t *testing.T) {
	e, src, dst, hash := setup(t)
	dst.checkFor = 1 << 30 // never finishes
	job, _ := e.Start(request(hash, true, ""))
	for {
		j := e.Jobs()[0]
		if j.Items[0].State == StateChecking {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !e.Cancel(job.ID) {
		t.Fatal("cancel refused")
	}
	it := wait(t, e, job.ID)
	if it.State != StateCancelled || !src.has(hash) || dst.has(hash) {
		t.Fatalf("got %+v, src=%v dst=%v", it, src.has(hash), dst.has(hash))
	}
}

func TestSourceRemoveFailureIsReported(t *testing.T) {
	e, src, dst, hash := setup(t)
	src.removeErr = errors.New("boom")
	job, _ := e.Start(request(hash, true, ""))
	it := wait(t, e, job.ID)
	if it.State != StateFailed || it.Code != CodeSourceKept || !it.InTarget {
		t.Fatalf("got %+v", it)
	}
	if !dst.has(hash) {
		t.Error("the target keeps the torrent once it has taken over")
	}
}

func TestCopyKeepsSource(t *testing.T) {
	e, src, dst, hash := setup(t)
	job, _ := e.Start(request(hash, false, ""))
	if it := wait(t, e, job.ID); it.State != StateDone {
		t.Fatalf("got %+v", it)
	}
	if !src.has(hash) || !dst.has(hash) {
		t.Error("both clients should have the torrent")
	}
	job, _ = e.Start(request(hash, false, ""))
	if it := wait(t, e, job.ID); it.Code != CodeDuplicate {
		t.Errorf("second copy should be refused as a duplicate, got %+v", it)
	}
}

func TestStartValidation(t *testing.T) {
	e, _, _, hash := setup(t)
	if _, err := e.Start(request(hash, false, "/x")); err == nil {
		t.Error("a new folder without removing from the source must be refused")
	}
	if _, err := e.Start(request(hash, true, "relative")); err == nil {
		t.Error("a relative folder must be refused")
	}
	r := request(hash, true, "")
	r.TargetID = "src"
	if _, err := e.Start(r); err == nil {
		t.Error("moving to the same client must be refused")
	}
}

func TestBusyCoversRunningItems(t *testing.T) {
	e, _, dst, hash := setup(t)
	dst.checkFor = 1 << 30 // never finishes
	job, _ := e.Start(request(hash, true, ""))
	for e.Jobs()[0].Items[0].State != StateChecking {
		time.Sleep(time.Millisecond)
	}
	busy := e.Busy()
	if !busy["src:"+hash] || !busy["dst:"+hash] {
		t.Fatalf("busy = %v, want source and target", busy)
	}
	e.Cancel(job.ID)
	wait(t, e, job.ID)
	if len(e.Busy()) != 0 {
		t.Errorf("nothing should be busy after the job ended, got %v", e.Busy())
	}
}

// A hybrid torrent is known by its v1 hash in one client and by its
// truncated v2 hash in another; the move must still find and verify it.
func TestTargetUsesOtherID(t *testing.T) {
	e, src, dst, hash := setup(t)
	dst.idIndex = 1
	job, _ := e.Start(request(hash, true, ""))
	it := wait(t, e, job.ID)
	if it.State != StateDone {
		t.Fatalf("got %+v", it)
	}
	tf, _ := torrentfile.Parse([]byte(fileData))
	other := tf.InfoHashes()[1]
	if it.TargetHash != other || !dst.has(other) || src.has(hash) {
		t.Errorf("target hash %q, dst has %v, src has %v", it.TargetHash, dst.has(other), src.has(hash))
	}

	// A copy under the other ID counts as a duplicate.
	e2, src2, dst2, _ := setup(t)
	dst2.idIndex = 1
	_ = dst2.AddStopped(context.Background(), []byte(fileData), client.AddOptions{SavePath: "/data"})
	job, _ = e2.Start(request(hash, false, ""))
	if it := wait(t, e2, job.ID); it.Code != CodeDuplicate {
		t.Errorf("want a duplicate, got %+v", it)
	}
	_ = src2
}

// When the data is missing, the rollback must remove the copy under the ID
// the target actually uses.
func TestRollbackUsesTargetID(t *testing.T) {
	e, src, dst, hash := setup(t)
	dst.idIndex = 1
	dst.dataFound = 0.2
	job, _ := e.Start(request(hash, true, ""))
	it := wait(t, e, job.ID)
	tf, _ := torrentfile.Parse([]byte(fileData))
	if it.Code != CodeDataNotFound || dst.has(tf.InfoHashes()[1]) || !src.has(hash) {
		t.Errorf("got %+v; target still has copy: %v", it, dst.has(tf.InfoHashes()[1]))
	}
}

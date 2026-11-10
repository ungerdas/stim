// Package transfer moves torrents from one client to another on the same
// server. The data files stay where they are: the target client is pointed
// at them, hashes them, and only once it has found every piece is the
// torrent removed from the source (without deleting anything).
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/remotefiles"
	"github.com/ungerdas/stim/internal/store"
	"github.com/ungerdas/stim/internal/torrentfile"
)

// State is where one torrent is in its move.
type State string

const (
	StateQueued    State = "queued"
	StateFetching  State = "fetching" // getting the .torrent file from the source
	StateAdding    State = "adding"   // handing it to the target, stopped
	StateChecking  State = "checking" // target is hashing the files
	StateRemoving  State = "removing" // removing from the source, data kept
	StateMoving    State = "moving"   // target is moving the data to the new folder
	StateDone      State = "done"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

func (s State) finished() bool {
	return s == StateDone || s == StateFailed || s == StateCancelled
}

// Item is one torrent in a job.
type Item struct {
	SourceID string  `json:"sourceId"`
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	State    State   `json:"state"`
	Progress float64 `json:"progress"` // 0..1 while checking
	// Code classifies a failure for the UI; Error is the detail.
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
	// InTarget is true once the target client has taken the torrent over,
	// even if a later step failed.
	InTarget bool `json:"inTarget"`
	// TargetHash is the torrent's ID in the target client, which can differ
	// from Hash for hybrid torrents (see client.Torrent.AltHash).
	TargetHash string `json:"targetHash,omitempty"`
}

// Job is one "move these torrents to that client" request.
type Job struct {
	ID               string `json:"id"`
	TargetID         string `json:"targetId"`
	RemoveFromSource bool   `json:"removeFromSource"`
	NewPath          string `json:"newPath,omitempty"`
	Items            []Item `json:"items"`
	CreatedAt        int64  `json:"createdAt"`
	FinishedAt       int64  `json:"finishedAt"`
	Cancelling       bool   `json:"cancelling"`
}

// Request is what the API accepts.
type Request struct {
	TargetID         string `json:"targetId"`
	RemoveFromSource bool   `json:"removeFromSource"`
	NewPath          string `json:"newPath"`
	Items            []struct {
		InstanceID string `json:"instanceId"`
		Hash       string `json:"hash"`
		Name       string `json:"name"`
	} `json:"items"`
}

// Failure codes the UI translates.
const (
	CodeNeedsFiles   = "needs-file-access" // source cannot export and has no SSH access set up
	CodeIncomplete   = "incomplete"
	CodeBusy         = "busy"
	CodeGone         = "gone"
	CodeDuplicate    = "duplicate"
	CodeFileMissing  = "torrent-file-missing"
	CodeWrongFile    = "torrent-file-mismatch"
	CodeNoTrackers   = "no-trackers"
	CodeDataNotFound = "data-not-found" // target did not find all pieces
	CodeSourceKept   = "source-not-removed"
	CodeMoveFailed   = "move-failed"
	CodeUnsupported  = "unsupported"
	CodeHostKey      = "host-key-changed"
	CodeSSH          = "ssh-failed"
	CodeOther        = "other"
)

type failure struct {
	code string
	err  error
}

func (f *failure) Error() string { return f.err.Error() }

func fail(code string, format string, args ...any) error {
	return &failure{code: code, err: fmt.Errorf(format, args...)}
}

// Clients is how the engine reaches live clients and their settings.
type Clients interface {
	Lookup(id string) (client.Client, store.Instance, bool)
	Invalidate(ids ...string)
}

// HostKeys stores an SSH host key the first time a server is contacted.
type HostKeys interface {
	SetHostKey(id, hostKey string) error
}

// Timing is adjustable so tests do not wait for real poll intervals.
type Timing struct {
	Poll         time.Duration // between status polls
	CallTimeout  time.Duration // per client call
	AppearWithin time.Duration // for an added torrent to show up
	SettleFor    time.Duration // a check counts as finished after this long without "checking"
	Overall      time.Duration // cap for one check or one move
}

// DefaultTiming suits real clients.
var DefaultTiming = Timing{
	Poll:         1500 * time.Millisecond,
	CallTimeout:  30 * time.Second,
	AppearWithin: 30 * time.Second,
	SettleFor:    4 * time.Second,
	Overall:      24 * time.Hour,
}

const keepFinishedJobs = 20

// Engine runs jobs, one torrent at a time per job.
type Engine struct {
	clients  Clients
	hostKeys HostKeys
	timing   Timing

	mu      sync.Mutex
	jobs    []*Job // newest first
	cancels map[string]context.CancelFunc
}

// New builds an engine.
func New(c Clients, h HostKeys, t Timing) *Engine {
	return &Engine{clients: c, hostKeys: h, timing: t, cancels: map[string]context.CancelFunc{}}
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// samePath compares two folders the way a client would.
func samePath(a, b string) bool {
	return path.Clean(strings.TrimSpace(a)) == path.Clean(strings.TrimSpace(b))
}

// Start validates a request and begins working on it in the background.
func (e *Engine) Start(req Request) (Job, error) {
	if _, _, ok := e.clients.Lookup(req.TargetID); !ok {
		return Job{}, errors.New("unknown target client")
	}
	req.NewPath = strings.TrimSpace(req.NewPath)
	if req.NewPath != "" {
		if !strings.HasPrefix(req.NewPath, "/") {
			return Job{}, errors.New("the new folder must be an absolute path, as the target client sees it")
		}
		// Moving the files out from under a source that still has the
		// torrent would leave the source pointing at nothing.
		if !req.RemoveFromSource {
			return Job{}, errors.New("moving the files to a new folder requires removing the torrent from the source client")
		}
	}
	job := &Job{
		ID:               newID(),
		TargetID:         req.TargetID,
		RemoveFromSource: req.RemoveFromSource,
		NewPath:          req.NewPath,
		CreatedAt:        time.Now().Unix(),
	}
	seen := map[string]bool{}
	for _, it := range req.Items {
		hash := strings.ToLower(strings.TrimSpace(it.Hash))
		if hash == "" || it.InstanceID == req.TargetID || seen[it.InstanceID+":"+hash] {
			continue
		}
		seen[it.InstanceID+":"+hash] = true
		job.Items = append(job.Items, Item{SourceID: it.InstanceID, Hash: hash, Name: it.Name, State: StateQueued})
	}
	if len(job.Items) == 0 {
		return Job{}, errors.New("nothing to move: pick torrents that are on another client than the target")
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.jobs = append([]*Job{job}, e.jobs...)
	e.cancels[job.ID] = cancel
	e.trim()
	snapshot := copyJob(job)
	e.mu.Unlock()

	go e.run(ctx, job)
	return snapshot, nil
}

// trim drops the oldest finished jobs. Callers hold e.mu.
func (e *Engine) trim() {
	finished := 0
	kept := e.jobs[:0]
	for _, j := range e.jobs {
		if j.FinishedAt != 0 {
			finished++
			if finished > keepFinishedJobs {
				continue
			}
		}
		kept = append(kept, j)
	}
	e.jobs = kept
}

func copyJob(j *Job) Job {
	c := *j
	c.Items = append([]Item(nil), j.Items...)
	return c
}

// Jobs returns all known jobs, newest first.
func (e *Engine) Jobs() []Job {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Job, 0, len(e.jobs))
	for _, j := range e.jobs {
		out = append(out, copyJob(j))
	}
	return out
}

// Busy reports which torrents a running job is still working on, as
// "instanceID:hash" for both the source and the target client. They must
// not be removed underneath it.
func (e *Engine) Busy() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	busy := map[string]bool{}
	for _, j := range e.jobs {
		if j.FinishedAt != 0 {
			continue
		}
		for _, it := range j.Items {
			if !it.State.finished() {
				busy[it.SourceID+":"+it.Hash] = true
				busy[j.TargetID+":"+it.Hash] = true
				if it.TargetHash != "" {
					busy[j.TargetID+":"+it.TargetHash] = true
				}
			}
		}
	}
	return busy
}

// Cancel stops a job. Torrents not yet handed over stay in their source; a
// torrent whose source copy has already been removed is completed.
func (e *Engine) Cancel(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	cancel, ok := e.cancels[id]
	if !ok {
		return false
	}
	for _, j := range e.jobs {
		if j.ID == id && j.FinishedAt == 0 {
			j.Cancelling = true
		}
	}
	cancel()
	return true
}

func (e *Engine) update(job *Job, i int, fn func(*Item)) {
	e.mu.Lock()
	fn(&job.Items[i])
	e.mu.Unlock()
}

func (e *Engine) setState(job *Job, i int, s State) {
	e.update(job, i, func(it *Item) { it.State = s })
}

func (e *Engine) run(ctx context.Context, job *Job) {
	for i := range job.Items {
		if ctx.Err() != nil {
			e.setState(job, i, StateCancelled)
			continue
		}
		err := e.moveOne(ctx, job, i)
		e.update(job, i, func(it *Item) {
			switch {
			case err == nil:
				it.State = StateDone
			case errors.Is(err, context.Canceled):
				it.State = StateCancelled
			default:
				it.State = StateFailed
				it.Error = err.Error()
				it.Code = CodeOther
				var f *failure
				if errors.As(err, &f) {
					it.Code = f.code
				}
			}
		})
		e.clients.Invalidate(job.TargetID, job.Items[i].SourceID)
	}
	e.mu.Lock()
	job.FinishedAt = time.Now().Unix()
	job.Cancelling = false
	delete(e.cancels, job.ID)
	e.mu.Unlock()
}

// call runs one client call with its own timeout. Calls that must not be
// abandoned half-way (see moveOne) pass a context that ignores cancellation.
func (e *Engine) call(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, e.timing.CallTimeout)
	defer cancel()
	return fn(ctx)
}

func (e *Engine) sleep(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(e.timing.Poll):
		return nil
	}
}

// lookupAny finds a torrent under any of the IDs it may have and returns
// the one the client uses.
func (e *Engine) lookupAny(ctx context.Context, c client.Transferer, hashes []string) (client.Torrent, string, bool, error) {
	for _, h := range hashes {
		t, found, err := e.lookupTorrent(ctx, c, h)
		if err != nil {
			return client.Torrent{}, "", false, err
		}
		if found {
			return t, h, true, nil
		}
	}
	return client.Torrent{}, "", false, nil
}

// candidates lists the IDs a torrent can have: the source's ID first, then
// the v1 and truncated v2 hashes of its info dictionary.
func candidates(hash string, tf *torrentfile.File) []string {
	out := []string{hash}
	for _, h := range tf.InfoHashes() {
		if h != hash {
			out = append(out, h)
		}
	}
	return out
}

func (e *Engine) lookupTorrent(ctx context.Context, c client.Transferer, hash string) (client.Torrent, bool, error) {
	var (
		t  client.Torrent
		ok bool
	)
	err := e.call(ctx, func(ctx context.Context) error {
		var err error
		t, ok, err = c.Torrent(ctx, hash)
		return err
	})
	return t, ok, err
}

// moveOne moves a single torrent. Until the source copy is removed, any
// failure or cancellation rolls the target back, so the torrent is left
// exactly as it was. After that point the move is always completed.
func (e *Engine) moveOne(ctx context.Context, job *Job, i int) error {
	item := job.Items[i]
	hash := item.Hash

	srcClient, srcInst, ok := e.clients.Lookup(item.SourceID)
	if !ok {
		return fail(CodeGone, "the source client no longer exists")
	}
	dstClient, _, ok := e.clients.Lookup(job.TargetID)
	if !ok {
		return fail(CodeGone, "the target client no longer exists")
	}
	src, ok1 := srcClient.(client.Transferer)
	dst, ok2 := dstClient.(client.Transferer)
	if !ok1 || !ok2 {
		return fail(CodeUnsupported, "moving is not supported for this client type")
	}

	// 1. The source must have the torrent, complete and idle.
	t, found, err := e.lookupTorrent(ctx, src, hash)
	if err != nil {
		return err
	}
	if !found {
		return fail(CodeGone, "the torrent is no longer in the source client")
	}
	e.update(job, i, func(it *Item) { it.Name = t.Name })
	if t.Progress < 1 {
		return fail(CodeIncomplete, "the torrent is not fully downloaded")
	}
	if t.State == client.StateChecking || t.State == client.StateMoving || t.State == client.StateError {
		return fail(CodeBusy, "the source client is busy with this torrent (%s)", t.RawState)
	}
	if t.SavePath == "" {
		return fail(CodeOther, "the source client did not report where the files are")
	}

	// 2. Get the .torrent file and make sure it is the right one.
	e.setState(job, i, StateFetching)
	data, err := e.torrentFile(ctx, srcClient, srcInst, hash)
	if err != nil {
		return err
	}
	tf, err := torrentfile.Parse(data)
	if err != nil {
		return fail(CodeWrongFile, "the torrent file could not be read: %v", err)
	}
	ids := candidates(hash, tf)
	if _, _, exists, err := e.lookupAny(ctx, dst, ids); err != nil {
		return err
	} else if exists {
		return fail(CodeDuplicate, "the target client already has this torrent")
	}

	// 3. Hand it to the target, stopped, pointing at the existing files.
	e.setState(job, i, StateAdding)
	opts := client.AddOptions{SavePath: t.SavePath, Label: t.Label, Tags: t.Tags}
	if err := e.call(ctx, func(ctx context.Context) error { return dst.AddStopped(ctx, data, opts) }); err != nil {
		return fmt.Errorf("the target client did not accept the torrent: %w", err)
	}

	// From here on the target has the torrent. Roll it back on any failure
	// or cancellation, using a context that outlives the cancelled one.
	// dstHash is the ID the target uses, once it is known.
	dstHash := ""
	rollback := func(cause error) error {
		clean := context.WithoutCancel(ctx)
		if dstHash == "" {
			// It never showed up under any ID; look once more before giving up.
			if _, h, found, err := e.lookupAny(clean, dst, ids); err == nil && found {
				dstHash = h
			}
		}
		if dstHash == "" {
			return cause
		}
		if err := e.call(clean, func(ctx context.Context) error { return dst.RemoveKeepData(ctx, dstHash) }); err != nil {
			e.update(job, i, func(it *Item) { it.InTarget = true })
			return fmt.Errorf("%w (and the stopped copy could not be removed from the target client: %v)", cause, err)
		}
		return cause
	}

	// 4. Let the target hash the files, and require every piece.
	e.setState(job, i, StateChecking)
	dstHash, err = e.appear(ctx, dst, ids)
	if err != nil {
		return rollback(err)
	}
	e.update(job, i, func(it *Item) { it.TargetHash = dstHash })
	checked, err := e.check(ctx, job, i, dst, dstHash)
	if err != nil {
		return rollback(err)
	}
	if checked.Progress < 1 {
		return rollback(fail(CodeDataNotFound, "the target client found only %.1f %% of the data in %s", checked.Progress*100, t.SavePath))
	}
	if ctx.Err() != nil {
		return rollback(ctx.Err())
	}

	// Point of no return: nothing below may be interrupted by a cancel.
	steady := context.WithoutCancel(ctx)
	e.update(job, i, func(it *Item) { it.InTarget = true; it.Progress = 1 })

	// 5. Remove from the source, keeping the data.
	var sourceErr error
	if job.RemoveFromSource {
		e.setState(job, i, StateRemoving)
		if err := e.call(steady, func(ctx context.Context) error { return src.RemoveKeepData(ctx, hash) }); err != nil {
			sourceErr = fail(CodeSourceKept, "the target client has taken over, but the torrent could not be removed from the source client: %v", err)
		}
	}

	// 6. Start seeding from the target.
	if err := e.call(steady, func(ctx context.Context) error { return dstClient.Resume(ctx, []string{dstHash}) }); err != nil && sourceErr == nil {
		sourceErr = fmt.Errorf("the target client has taken over but could not be started: %w", err)
	}
	if sourceErr != nil {
		return sourceErr
	}

	// 7. Optionally let the target move the data to a new folder.
	if job.NewPath != "" && !samePath(job.NewPath, t.SavePath) {
		e.setState(job, i, StateMoving)
		if err := e.relocate(steady, dst, dstHash, job.NewPath); err != nil {
			return fail(CodeMoveFailed, "the target client has taken over, but moving the files to %s failed: %v", job.NewPath, err)
		}
	}
	return nil
}

// torrentFile fetches and verifies the .torrent for hash.
func (e *Engine) torrentFile(ctx context.Context, srcClient client.Client, srcInst store.Instance, hash string) ([]byte, error) {
	var data []byte
	if exp, ok := srcClient.(client.Exporter); ok {
		err := e.call(ctx, func(ctx context.Context) error {
			var err error
			data, err = exp.ExportTorrent(ctx, hash)
			return err
		})
		if err != nil {
			return nil, err
		}
	} else {
		cfg := srcInst.SSHConfig()
		if !cfg.Configured() || strings.TrimSpace(srcInst.TorrentDir) == "" {
			return nil, fail(CodeNeedsFiles, "this client cannot export torrent files; set up SSH access to its torrent folder")
		}
		var hostKey string
		err := e.call(ctx, func(ctx context.Context) error {
			var err error
			data, hostKey, err = remotefiles.ReadFile(ctx, cfg, srcInst.TorrentDir, hash+".torrent")
			return err
		})
		if hostKey != "" && cfg.HostKey == "" {
			_ = e.hostKeys.SetHostKey(srcInst.ID, hostKey)
		}
		switch {
		case errors.Is(err, remotefiles.ErrNotFound):
			return nil, fail(CodeFileMissing, "%v", err)
		case errors.Is(err, remotefiles.ErrHostKeyChanged):
			return nil, fail(CodeHostKey, "%v", err)
		case err != nil:
			return nil, fail(CodeSSH, "%v", err)
		}
	}

	tf, err := torrentfile.Parse(data)
	if err != nil {
		return nil, fail(CodeWrongFile, "the torrent file from the source could not be read: %v", err)
	}
	if !tf.Matches(hash) {
		return nil, fail(CodeWrongFile, "the torrent file from the source belongs to a different torrent")
	}
	if !tf.HasTrackers() {
		// Some clients keep announce URLs outside the stored file.
		lister, ok := srcClient.(client.TrackerLister)
		if !ok {
			return nil, fail(CodeNoTrackers, "the torrent file has no trackers")
		}
		var urls []string
		err := e.call(ctx, func(ctx context.Context) error {
			var err error
			urls, err = lister.Trackers(ctx, hash)
			return err
		})
		if err != nil {
			return nil, err
		}
		if len(urls) == 0 {
			return nil, fail(CodeNoTrackers, "the torrent has no trackers in the source client")
		}
		data = tf.WithTrackers(urls)
	}
	return data, nil
}

// appear waits for a freshly added torrent to show up in the target and
// returns the ID the target gave it.
func (e *Engine) appear(ctx context.Context, dst client.Transferer, ids []string) (string, error) {
	deadline := time.Now().Add(e.timing.AppearWithin)
	for {
		_, h, found, err := e.lookupAny(ctx, dst, ids)
		if err != nil {
			return "", err
		}
		if found {
			return h, nil
		}
		if time.Now().After(deadline) {
			return "", errors.New("the torrent never appeared in the target client")
		}
		if err := e.sleep(ctx); err != nil {
			return "", err
		}
	}
}

// check starts a recheck in the target and waits for it to finish.
func (e *Engine) check(ctx context.Context, job *Job, i int, dst client.Transferer, hash string) (client.Torrent, error) {
	if err := e.call(ctx, func(ctx context.Context) error { return dst.Recheck(ctx, hash) }); err != nil {
		return client.Torrent{}, fmt.Errorf("the target client would not check the files: %w", err)
	}

	// The client reports "checking" shortly after the call and for as long
	// as the check runs or waits its turn. It is finished once the state
	// has been something else for SettleFor.
	//
	// A client with a queue of checks may not have started ours yet. An
	// incomplete result is therefore only trusted once the check has been
	// seen running, or after AppearWithin without it ever starting.
	start := time.Now()
	var idleSince time.Time
	sawChecking := false
	for {
		if err := e.sleep(ctx); err != nil {
			return client.Torrent{}, err
		}
		t, found, err := e.lookupTorrent(ctx, dst, hash)
		if err != nil {
			return client.Torrent{}, err
		}
		if !found {
			return client.Torrent{}, errors.New("the torrent disappeared from the target client during the check")
		}
		e.update(job, i, func(it *Item) { it.Progress = t.Progress })
		settled := !idleSince.IsZero() && time.Since(idleSince) >= e.timing.SettleFor
		switch {
		case t.State == client.StateChecking:
			sawChecking = true
			idleSince = time.Time{}
		case t.State == client.StateError:
			return t, fmt.Errorf("the target client reported an error: %s", strings.TrimSpace(t.RawState+" "+t.Message))
		case idleSince.IsZero():
			idleSince = time.Now()
		case settled && (t.Progress >= 1 || sawChecking || time.Since(start) >= e.timing.AppearWithin):
			return t, nil
		}
		if time.Since(start) > e.timing.Overall {
			return t, errors.New("the check did not finish in time")
		}
	}
}

// relocate asks the target to move the data and waits until it has.
func (e *Engine) relocate(ctx context.Context, dst client.Transferer, hash, newPath string) error {
	if err := e.call(ctx, func(ctx context.Context) error { return dst.Move(ctx, hash, newPath) }); err != nil {
		return err
	}
	start := time.Now()
	for {
		if err := e.sleep(ctx); err != nil {
			return err
		}
		t, found, err := e.lookupTorrent(ctx, dst, hash)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("the torrent disappeared from the target client")
		}
		if t.State == client.StateError {
			return fmt.Errorf("the target client reported an error: %s", strings.TrimSpace(t.RawState+" "+t.Message))
		}
		if t.State != client.StateMoving && samePath(t.SavePath, newPath) {
			return nil
		}
		// A client that refuses the move goes straight back to its old
		// state without changing the path.
		if t.State != client.StateMoving && time.Since(start) > 4*e.timing.SettleFor {
			return fmt.Errorf("the files are still in %s", t.SavePath)
		}
		if time.Since(start) > e.timing.Overall {
			return errors.New("the move did not finish in time")
		}
	}
}

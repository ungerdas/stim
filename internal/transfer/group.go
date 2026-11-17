package transfer

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/store"
)

// Cross-seeds: the same files seeded as several torrents, typically the same
// release on several trackers. Moved in one job, they form a group: the
// files are moved to the new folder, or deleted from the old server, once,
// by the last of them, and the others are pointed at the moved files.

// sharer is another torrent using some of the moved torrent's files.
type sharer struct {
	instanceID, hash, savePath, name string
}

// identical reports whether the sharer uses exactly the same files, as
// opposed to overlapping ones (an episode seeded from inside a pack).
func (s sharer) identical(t client.Torrent) bool {
	return samePath(s.savePath, t.SavePath) && s.name == t.Name
}

// groups tracks the items of one job that wait for the rest of their group.
type groups struct {
	waiting map[string][]int // group key → item indexes
}

func newGroups() *groups { return &groups{waiting: map[string][]int{}} }

func groupKey(inst store.Instance, t client.Torrent) string {
	return inst.Server() + "\x00" + path.Join(path.Clean(t.SavePath), t.Name)
}

// classify splits the sharers of item i into torrents later in this job with
// the same files (the group still to come) and everything else (outsiders,
// whose files must stay). Group members already handled and waiting in the
// target client are neither.
func (g *groups) classify(job *Job, i int, sh []sharer, t client.Torrent, key string) (later, outsiders int) {
	for _, s := range sh {
		if s.identical(t) {
			if laterItem(job, i, s) {
				later++
				continue
			}
			if g.isWaiting(job, key, s) {
				continue
			}
		}
		outsiders++
	}
	return later, outsiders
}

func laterItem(job *Job, i int, s sharer) bool {
	for j := i + 1; j < len(job.Items); j++ {
		it := job.Items[j]
		if it.State == StateQueued && it.SourceID == s.instanceID && strings.EqualFold(it.Hash, s.hash) {
			return true
		}
	}
	return false
}

func (g *groups) isWaiting(job *Job, key string, s sharer) bool {
	if s.instanceID != job.TargetID {
		return false
	}
	for _, k := range g.waiting[key] {
		if strings.EqualFold(job.Items[k].TargetHash, s.hash) {
			return true
		}
	}
	return false
}

func (g *groups) wait(key string, i int) { g.waiting[key] = append(g.waiting[key], i) }

// take removes and returns the items waiting in a group.
func (g *groups) take(key string) []int {
	w := g.waiting[key]
	delete(g.waiting, key)
	return w
}

// finish gives every item waiting in a group its final note: "" when the
// group's files were moved or deleted, otherwise why they were not.
func (e *Engine) finish(job *Job, g *groups, key, note string, shared int, detail string) {
	for _, k := range g.take(key) {
		e.update(job, k, func(it *Item) { it.Note, it.Shared, it.NoteDetail = note, shared, detail })
	}
}

// finishAll settles groups whose last member never got that far (it failed,
// or the job was cancelled): their files stay where they are.
func (e *Engine) finishAll(job *Job, g *groups) {
	note := NoteNotMovedGroup
	if job.CopyFiles {
		note = NoteKeptGroup
	}
	for key := range g.waiting {
		e.finish(job, g, key, note, 0, "")
	}
}

// repoint makes a waiting group member in the target client use the files
// another member has just moved to newPath: the client is pointed there,
// checks the files, and starts again once it finds all of them.
func (e *Engine) repoint(ctx context.Context, job *Job, k int, dst client.Transferer, dstClient client.Client, newPath string) {
	hash := job.Items[k].TargetHash
	e.update(job, k, func(it *Item) { it.State, it.Note = StateMoving, "" })
	err := e.relocate(ctx, dst, hash, newPath)
	if err == nil {
		e.update(job, k, func(it *Item) { it.State = StateChecking })
		var t client.Torrent
		t, err = e.check(ctx, job, k, dst, hash)
		if err == nil && t.Progress < 1 {
			err = fmt.Errorf("it found only %.1f %% of the data there", t.Progress*100)
		}
	}
	if err == nil {
		err = e.call(ctx, func(ctx context.Context) error { return dstClient.Resume(ctx, []string{hash}) })
	}
	e.update(job, k, func(it *Item) {
		if err != nil {
			it.State, it.Code = StateFailed, CodeMoveFailed
			it.Error = fmt.Sprintf("the files were moved to %s together with the other torrents that use them, but this one could not be pointed at them: %v", newPath, err)
			return
		}
		it.State, it.Progress = StateDone, 1
	})
}

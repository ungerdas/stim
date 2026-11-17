package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/remotefiles"
	"github.com/ungerdas/stim/internal/store"
	"github.com/ungerdas/stim/internal/torrentfile"
)

// Moving between servers: Stim copies the torrent's files from the source
// server to the target server over SFTP (the data passes through Stim), and
// the target client then takes over the copy exactly like a move on one
// server.

const (
	copyChunk = 4 << 20 // read/write size; large enough for many SFTP requests in flight
	// spaceMargin is left free on the target on top of the files.
	spaceMargin = 256 << 20
	partSuffix  = ".stimpart"
)

// copied records what a copy created on the target server, so it can be
// removed again.
type copied struct {
	files []string
	dirs  []string // in creation order
	// sameFiles is set when source and target are the very same files (the
	// "other" server turned out to be this one): the source files must stay.
	sameFiles bool
}

// sshReady checks an instance's SSH settings for a copy.
func sshReady(inst store.Instance, write bool) error {
	cfg := inst.SSHConfig()
	if !cfg.Configured() {
		return fail(CodeSSHNotReady, "SSH access to %s's server is not set up", inst.Name)
	}
	if write && cfg.HostKey == "" {
		return fail(CodeSSHNotReady, "the SSH connection to %s's server has not been tested and saved yet", inst.Name)
	}
	return nil
}

// copyAcross copies the files of a torrent from srcRoot on the source
// server to dstRoot on the target server. Files already on the target with
// the right size are kept, so a copy that was interrupted resumes. A copy
// error leaves the files that were completed (to resume from); the caller
// removes them in every other case.
func (e *Engine) copyAcross(ctx context.Context, job *Job, i int, srcInst, dstInst store.Instance, layout torrentfile.Layout, srcRoot, dstRoot string) (copied, error) {
	var done copied
	if err := sshReady(srcInst, false); err != nil {
		return done, err
	}
	if err := sshReady(dstInst, true); err != nil {
		return done, err
	}

	srcCfg, dstCfg := srcInst.SSHConfig(), dstInst.SSHConfig()
	src, err := remotefiles.Open(ctx, srcCfg, false)
	if err != nil {
		return done, fail(sshCode(err), "source server: %v", err)
	}
	defer src.Close()
	if srcCfg.HostKey == "" && src.HostKey != "" {
		_ = e.hostKeys.SetHostKey(srcInst.ID, src.HostKey)
	}
	dst, err := remotefiles.Open(ctx, dstCfg, true)
	if err != nil {
		return done, fail(sshCode(err), "target server: %v", err)
	}
	defer dst.Close()
	sameServer := dstCfg.Host == srcCfg.Host && dstCfg.Port == srcCfg.Port

	// Plan: every file must be on the source with its full size.
	type plan struct {
		src, dst string
		size     int64
		skip     bool
	}
	rels, sizes := layout.RelPaths(), layout.Files
	plans := make([]plan, 0, len(rels))
	var total, toCopy int64
	for k, rel := range rels {
		p := plan{src: path.Join(srcRoot, rel), dst: path.Join(dstRoot, rel), size: sizes[k].Length}
		have, err := src.Size(p.src)
		switch {
		case remotefiles.IsNotExist(err):
			return done, fail(CodeSourceFileMissing, "%s is missing on the source server", p.src)
		case err != nil:
			return done, fail(CodeCopyFailed, "source server: %v", err)
		case have != p.size:
			return done, fail(CodeSourceFileMissing, "%s on the source server is %d bytes, the torrent says %d", p.src, have, p.size)
		}
		if sameServer && p.src == p.dst {
			p.skip = true // the very same file
			done.sameFiles = true
		} else {
			n, err := dst.Size(p.dst)
			switch {
			case err == nil && n == p.size:
				// Already there: from an interrupted copy, or another torrent
				// of the same files (a cross-seed moved earlier). The target
				// client's check decides whether it really is the same.
				p.skip = true
			case err == nil:
				// Never overwrite: another torrent may be seeding it.
				return done, fail(CodeTargetExists, "%s already exists on the target server with another size (%d bytes, the torrent says %d)", p.dst, n, p.size)
			case !remotefiles.IsNotExist(err):
				return done, fail(CodeTargetExists, "%s cannot be written on the target server: %v", p.dst, err)
			}
		}
		total += p.size
		if !p.skip {
			toCopy += p.size
		}
		plans = append(plans, p)
	}
	if free := dst.Free(dstRoot); free >= 0 && free < toCopy+spaceMargin {
		return done, fail(CodeNoSpace, "the target server has %s free in %s; the files need %s, and Stim leaves %s to spare", human(free), dstRoot, human(toCopy), human(spaceMargin))
	}

	started := time.Now()
	e.update(job, i, func(it *Item) {
		it.CopyTotal, it.CopyDone, it.CopyStartedAt = total, total-toCopy, started.UnixMilli()
	})
	copiedBytes := total - toCopy
	rateAt, rateBytes := started, copiedBytes

	buf := make([]byte, copyChunk)
	for _, p := range plans {
		if p.skip {
			continue
		}
		dirs, err := dst.MkdirAll(path.Dir(p.dst))
		done.dirs = append(done.dirs, dirs...)
		if err != nil {
			return done, fail(CodeCopyFailed, "target server: %v", err)
		}
		part := p.dst + partSuffix
		if err := copyOne(ctx, src, dst, p.src, part, p.size, buf, func(n int64) {
			copiedBytes += n
			now := time.Now()
			var rate int64 = -1
			if el := now.Sub(rateAt); el >= time.Second {
				rate = int64(float64(copiedBytes-rateBytes) / el.Seconds())
				rateAt, rateBytes = now, copiedBytes
			}
			e.update(job, i, func(it *Item) {
				it.CopyDone = copiedBytes
				if rate >= 0 {
					it.CopyRate = rate
				}
			})
		}); err != nil {
			_ = dst.Remove(part)
			if errors.Is(err, context.Canceled) {
				return done, err
			}
			return done, fail(CodeCopyFailed, "copying %s: %v", path.Base(p.src), err)
		}
		if err := dst.Rename(part, p.dst); err != nil {
			_ = dst.Remove(part)
			return done, fail(CodeCopyFailed, "target server: %v", err)
		}
		done.files = append(done.files, p.dst)
	}
	return done, nil
}

// copyOne streams one file and checks that all of it arrived.
func copyOne(ctx context.Context, src, dst *remotefiles.Session, from, to string, size int64, buf []byte, progress func(int64)) error {
	r, err := src.OpenRead(from)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := dst.Create(to)
	if err != nil {
		return err
	}
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			w.Close()
			return err
		}
		n, readErr := io.ReadFull(r, buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				w.Close()
				return err
			}
			written += int64(n)
			progress(int64(n))
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			w.Close()
			return readErr
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	if written != size {
		return fmt.Errorf("copied %d of %d bytes", written, size)
	}
	return nil
}

// removeCopied deletes what a copy created on the target server: its files,
// then the folders it made, deepest first, as far as they are empty.
func (e *Engine) removeCopied(ctx context.Context, dstInst store.Instance, c copied) {
	if len(c.files) == 0 && len(c.dirs) == 0 {
		return
	}
	dst, err := remotefiles.Open(ctx, dstInst.SSHConfig(), true)
	if err != nil {
		return
	}
	defer dst.Close()
	for _, f := range c.files {
		_ = dst.Remove(f)
	}
	for k := len(c.dirs) - 1; k >= 0; k-- {
		_ = dst.RemoveDir(c.dirs[k]) // fails, as it should, if something else is inside
	}
}

func sshCode(err error) string {
	if errors.Is(err, remotefiles.ErrHostKeyChanged) {
		return CodeHostKey
	}
	return CodeSSH
}

func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// lister is implemented by the manager: every configured instance, so a
// deletion can check all clients on the source server, not just the source.
type lister interface {
	Instances() []store.Instance
}

// overlaps reports whether two files or folders are the same or one holds
// the other.
func overlaps(a, b string) bool {
	a, b = path.Clean(a), path.Clean(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// sharers lists the other torrents, in any client on the source server,
// whose files overlap the moved torrent's: cross-seeds of the same files on
// other trackers, or a single episode seeded from inside a season pack.
// Their files must not move or disappear under them. skip leaves out the
// moved torrent's own entry in the target client, when the target is on the
// same server.
func (e *Engine) sharers(ctx context.Context, srcInst store.Instance, t client.Torrent, skip func(instanceID, hash string) bool) ([]sharer, error) {
	ids := []string{srcInst.ID}
	if l, ok := e.clients.(lister); ok {
		server := srcInst.Server()
		for _, in := range l.Instances() {
			if in.ID != srcInst.ID && server != "" && in.Server() == server {
				ids = append(ids, in.ID)
			}
		}
	}
	ours := path.Join(t.SavePath, t.Name)
	var out []sharer
	for _, id := range ids {
		c, inst, ok := e.clients.Lookup(id)
		if !ok {
			continue
		}
		var list []client.Torrent
		err := e.call(ctx, func(ctx context.Context) error {
			var err error
			list, err = c.Torrents(ctx)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inst.Name, err)
		}
		for _, o := range list {
			if id == srcInst.ID && (strings.EqualFold(o.Hash, t.Hash) || (t.AltHash != "" && strings.EqualFold(o.Hash, t.AltHash))) {
				continue
			}
			if skip != nil && skip(id, o.Hash) {
				continue
			}
			if o.SavePath != "" && overlaps(path.Join(o.SavePath, o.Name), ours) {
				out = append(out, sharer{instanceID: id, hash: o.Hash, savePath: o.SavePath, name: o.Name})
			}
		}
	}
	return out, nil
}

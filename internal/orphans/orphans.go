// Package orphans finds files and folders in a server's download folders
// that no torrent uses any more: leftovers of deleted torrents, failed
// cross-seeds and interrupted copies. They are found over SSH, by listing
// every folder torrents are saved in and comparing with the torrents of
// every client on that server.
//
// Only the top level of each folder is compared: a folder that belongs to
// a torrent is left alone as a whole, even if it holds extra files.
package orphans

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/remotefiles"
	"github.com/ungerdas/stim/internal/store"
)

// RecentWindow keeps anything changed this recently out of the results, so
// a torrent being added right now is not mistaken for a leftover.
const RecentWindow = time.Hour

// walkLimit caps how many entries are counted under one leftover folder.
const walkLimit = 200_000

// Source is how the scan reaches clients and their settings (the manager).
type Source interface {
	Instances() []store.Instance
	Lookup(id string) (client.Client, store.Instance, bool)
}

// Item is one leftover in a download folder.
type Item struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	ModTime int64  `json:"modTime"`
	// SizeIncomplete is set when the folder was too big to count fully.
	SizeIncomplete bool `json:"sizeIncomplete,omitempty"`
}

// Result is one scan of one server.
type Result struct {
	Server string `json:"server"`
	// Clients are the clients on the server, whose save folders were looked
	// through. Torrents in every client count, wherever it runs.
	Clients []string `json:"clients"`
	// Unanswered are clients on other servers that did not answer: their
	// torrents could not be counted.
	Unanswered []string `json:"unanswered"`
	Dirs       []string `json:"dirs"`    // the folders looked through
	Skipped    []string `json:"skipped"` // folders not looked through, and why
	Items      []Item   `json:"items"`
	Total      int64    `json:"total"`
	Recent     int      `json:"recent"` // leftovers left out for being too new
	ScannedAt  int64    `json:"scannedAt"`
}

// Scan looks for leftovers on the server (see store.Instance.Server).
func Scan(ctx context.Context, src Source, server string) (Result, error) {
	res := Result{Server: server, Dirs: []string{}, Skipped: []string{}, Items: []Item{}, Clients: []string{}, Unanswered: []string{}}
	known, err := collect(ctx, src, server, &res)
	if err != nil {
		return res, err
	}
	sess, err := remotefiles.Open(ctx, known.ssh.SSHConfig(), true)
	if err != nil {
		return res, fmt.Errorf("SSH to %s: %w", server, err)
	}
	defer sess.Close()
	home, err := sess.Home()
	if err != nil {
		return res, fmt.Errorf("SSH to %s: %w", server, err)
	}

	for _, dir := range known.dirs() {
		if why := known.unsafeDir(dir, home); why != "" {
			res.Skipped = append(res.Skipped, dir+": "+why)
			continue
		}
		entries, err := sess.ReadDir(dir)
		if err != nil {
			if remotefiles.IsNotExist(err) {
				continue
			}
			res.Skipped = append(res.Skipped, dir+": "+err.Error())
			continue
		}
		res.Dirs = append(res.Dirs, dir)
		for _, fi := range entries {
			p := path.Join(dir, fi.Name())
			if !known.leftover(p, fi.Name()) {
				continue
			}
			it := Item{Path: p, IsDir: fi.IsDir(), Size: fi.Size(), ModTime: fi.ModTime().Unix()}
			newest := fi.ModTime()
			if fi.IsDir() {
				size, n, complete, err := sess.DiskUsage(p, walkLimit)
				if err != nil {
					res.Skipped = append(res.Skipped, p+": "+err.Error())
					continue
				}
				it.Size, it.SizeIncomplete = size, !complete
				if n.After(newest) {
					newest = n
				}
				it.ModTime = newest.Unix()
			} else if !fi.Mode().IsRegular() {
				it.Size = 0 // a link: removing it frees nothing
			}
			if time.Since(newest) < RecentWindow {
				res.Recent++
				continue
			}
			res.Items = append(res.Items, it)
			res.Total += it.Size
		}
	}
	sort.Slice(res.Items, func(i, j int) bool { return res.Items[i].Size > res.Items[j].Size })
	res.ScannedAt = time.Now().Unix()
	return res, nil
}

// Delete removes leftovers found by a scan. It scans again first and only
// removes paths that are still leftovers, so a torrent added since the scan
// is safe. It returns what was removed and why the rest was not.
func Delete(ctx context.Context, src Source, server string, paths []string) (removed []string, failed map[string]string, err error) {
	fresh, err := Scan(ctx, src, server)
	if err != nil {
		return nil, nil, err
	}
	still := map[string]bool{}
	for _, it := range fresh.Items {
		still[it.Path] = true
	}
	known, err := collect(ctx, src, server, &Result{})
	if err != nil {
		return nil, nil, err
	}
	cfg := known.ssh.SSHConfig()
	failed = map[string]string{}
	for _, p := range paths {
		if !still[p] {
			failed[p] = "no longer a leftover, or too recently changed"
			continue
		}
		if err := remotefiles.RemoveAll(ctx, cfg, p); err != nil {
			failed[p] = err.Error()
			continue
		}
		removed = append(removed, p)
	}
	return removed, failed, nil
}

// known is everything the torrents on a server tell about its folders.
type known struct {
	ssh       store.Instance
	saveDirs  map[string]bool // folders torrents are saved in
	roots     map[string]bool // each torrent's file or top folder
	ancestors map[string]bool // every folder above a root or a save folder
	names     map[string]bool // loose torrent names, as a second check
}

func collect(ctx context.Context, src Source, server string, res *Result) (*known, error) {
	k := &known{saveDirs: map[string]bool{}, roots: map[string]bool{}, ancestors: map[string]bool{}, names: map[string]bool{}}
	found := false
	for _, inst := range src.Instances() {
		same := server != "" && inst.Server() == server
		found = found || same
		c, _, ok := src.Lookup(inst.ID)
		if !ok {
			continue
		}
		list, err := c.Torrents(ctx)
		if err != nil {
			if same {
				// Without every list from the server a folder in use could
				// look abandoned.
				return nil, fmt.Errorf("%s did not answer, so its torrents cannot be counted: %w", inst.Name, err)
			}
			res.Unanswered = append(res.Unanswered, inst.Name)
			continue
		}
		// Torrents in clients on other servers count too: the same machine
		// may have been added under two names, and a folder in use must
		// never be offered. Only the save folders of this server's clients
		// are looked through.
		for _, t := range list {
			k.add(t, same)
		}
		if !same {
			continue
		}
		res.Clients = append(res.Clients, inst.Name)
		if sp, ok := c.(client.SavePather); ok {
			if dir, err := sp.DefaultSavePath(ctx); err == nil && strings.HasPrefix(dir, "/") {
				k.saveDirs[path.Clean(dir)] = true
				k.addAncestors(path.Clean(dir))
			}
		}
		cfg := inst.SSHConfig()
		if k.ssh.ID == "" && cfg.Configured() && cfg.HostKey != "" {
			k.ssh = inst
		}
	}
	if !found {
		return nil, errors.New("no client runs on that server")
	}
	if k.ssh.ID == "" {
		return nil, errors.New("none of the clients on that server has SSH access set up and tested")
	}
	return k, nil
}

// add counts a torrent. Only torrents of the server's own clients make their
// save folder one to look through; every torrent protects its files.
func (k *known) add(t client.Torrent, scanFolder bool) {
	k.names[loose(t.Name)] = true
	if t.SavePath == "" || !strings.HasPrefix(t.SavePath, "/") {
		return
	}
	save := path.Clean(t.SavePath)
	root := t.ContentPath
	if root == "" || !strings.HasPrefix(root, "/") {
		root = path.Join(save, t.Name)
	}
	root = path.Clean(root)
	if scanFolder {
		k.saveDirs[save] = true
	}
	k.roots[root] = true
	// The name under the save folder too, in case the content path and the
	// name disagree (a renamed top folder).
	k.roots[path.Join(save, t.Name)] = true
	k.addAncestors(root)
	k.addAncestors(save)
}

func (k *known) addAncestors(p string) {
	for d := path.Dir(p); d != "/" && d != "."; d = path.Dir(d) {
		if k.ancestors[d] {
			return
		}
		k.ancestors[d] = true
	}
}

// dirs lists the save folders to look through, shortest first. A folder
// inside a torrent's own folder (an episode saved into its pack) is not a
// place for leftovers.
func (k *known) dirs() []string {
	var out []string
	for d := range k.saveDirs {
		if k.insideRoot(d) {
			continue
		}
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func (k *known) insideRoot(p string) bool {
	for d := p; d != "/" && d != "."; d = path.Dir(d) {
		if k.roots[d] {
			return true
		}
	}
	return false
}

// unsafeDir says why a folder must not be looked through, or "".
func (k *known) unsafeDir(dir, home string) string {
	switch {
	case strings.Count(dir, "/") < 2:
		return "too close to the root of the file system"
	case dir == home || strings.HasPrefix(home, dir+"/"):
		return "the home folder itself; save torrents in a folder below it"
	}
	return ""
}

// leftover reports whether an entry in a save folder belongs to nothing.
func (k *known) leftover(p, name string) bool {
	switch {
	case strings.HasPrefix(name, "."):
		return false // hidden: settings and client state, never offered
	case k.roots[p], k.saveDirs[p], k.ancestors[p]:
		return false
	case k.names[loose(name)]:
		return false // looks like a torrent's name: rather keep it
	}
	return true
}

var videoExt = []string{".mkv", ".mp4", ".m4v", ".avi", ".ts", ".m2ts", ".wmv"}

// loose compares names without case, separators or a video extension, like
// the release grouping in the UI.
func loose(name string) string {
	n := strings.ToLower(name)
	for _, ext := range videoExt {
		n = strings.TrimSuffix(n, ext)
	}
	var b strings.Builder
	sep := false
	for _, r := range n {
		if r == ' ' || r == '_' || r == '.' || r == '\t' {
			sep = true
			continue
		}
		if sep && b.Len() > 0 {
			b.WriteByte('.')
		}
		sep = false
		b.WriteRune(r)
	}
	return b.String()
}

// Package manager owns one live client per configured instance and serves a
// combined, briefly cached view of all their torrents.
package manager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/store"
)

const (
	// cacheTTL is how long a fetched torrent list is reused. It decouples how
	// often browsers poll Stim from how often Stim polls the clients.
	cacheTTL = 2 * time.Second
	// errorBackoff is how long to wait before retrying an unreachable client.
	errorBackoff = 10 * time.Second
	fetchTimeout = 20 * time.Second
)

// InstanceStatus is the live state of one instance, safe to send to the UI.
type InstanceStatus struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Kind         client.Kind         `json:"kind"`
	Capabilities client.Capabilities `json:"capabilities"`
	Online       bool                `json:"online"`
	Error        string              `json:"error,omitempty"`
	ErrorCode    string              `json:"errorCode,omitempty"`
	Version      string              `json:"version,omitempty"`
	UpdatedAt    int64               `json:"updatedAt"` // last successful fetch
	Count        int                 `json:"count"`
	DownloadRate int64               `json:"downloadRate"`
	UploadRate   int64               `json:"uploadRate"`
}

// Torrent is a client torrent plus the instance it lives on.
type Torrent struct {
	client.Torrent
	InstanceID string `json:"instanceId"`
}

// Snapshot is the combined view across all instances.
type Snapshot struct {
	Instances []InstanceStatus `json:"instances"`
	Torrents  []Torrent        `json:"torrents"`
}

type entry struct {
	inst store.Instance
	cli  client.Client

	mu        sync.Mutex // serialises fetches for this instance
	torrents  []client.Torrent
	version   string
	err       error
	checkedAt time.Time // last attempt
	okAt      time.Time // last success
}

func (e *entry) refresh(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// After rejected credentials, wait for the user to fix the instance
	// (see Manager.Retry) instead of hammering the client into a ban.
	if errors.Is(e.err, client.ErrAuth) {
		return
	}
	wait := cacheTTL
	if e.err != nil {
		wait = errorBackoff
	}
	if !e.checkedAt.IsZero() && time.Since(e.checkedAt) < wait {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	if e.version == "" {
		v, err := e.cli.Version(ctx)
		if err != nil {
			e.err, e.checkedAt = err, time.Now()
			return
		}
		e.version = v
	}
	ts, err := e.cli.Torrents(ctx)
	e.checkedAt = time.Now()
	if err != nil {
		e.err = err // keep the last known torrents; the UI marks them stale
		return
	}
	e.err, e.torrents, e.okAt = nil, ts, e.checkedAt
}

func (e *entry) invalidate() {
	e.mu.Lock()
	e.checkedAt = time.Time{}
	e.mu.Unlock()
}

// retry clears a failure so the next refresh tries again immediately.
func (e *entry) retry() {
	e.mu.Lock()
	if _, broken := e.cli.(brokenClient); !broken {
		e.err = nil
	}
	e.checkedAt = time.Time{}
	e.mu.Unlock()
}

// Manager keeps clients in sync with the store.
type Manager struct {
	store *store.Store

	mu      sync.RWMutex
	entries map[string]*entry
	order   []string
}

// New builds a manager and its clients from the store.
func New(s *store.Store) *Manager {
	m := &Manager{store: s, entries: map[string]*entry{}}
	m.Reload()
	return m
}

// Reload re-reads the store. Clients whose configuration is unchanged are
// kept, so they do not lose their session or cached torrents.
func (m *Manager) Reload() {
	list := m.store.List()
	m.mu.Lock()
	defer m.mu.Unlock()

	next := make(map[string]*entry, len(list))
	order := make([]string, 0, len(list))
	for _, in := range list {
		order = append(order, in.ID)
		if old, ok := m.entries[in.ID]; ok && old.inst == in {
			next[in.ID] = old
			continue
		}
		e := &entry{inst: in}
		cli, err := client.New(in.Kind, in.ClientConfig())
		if err != nil {
			e.cli, e.err = brokenClient{kind: in.Kind, err: err}, err
		} else {
			e.cli = cli
		}
		next[in.ID] = e
	}
	m.entries, m.order = next, order
}

func (m *Manager) snapshotEntries() []*entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*entry, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.entries[id])
	}
	return out
}

// Snapshot refreshes every instance in parallel (subject to the cache) and
// returns the combined result.
func (m *Manager) Snapshot(ctx context.Context) Snapshot {
	entries := m.snapshotEntries()
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(e *entry) {
			defer wg.Done()
			e.refresh(ctx)
		}(e)
	}
	wg.Wait()

	snap := Snapshot{Instances: []InstanceStatus{}, Torrents: []Torrent{}}
	for _, e := range entries {
		e.mu.Lock()
		st := InstanceStatus{
			ID:           e.inst.ID,
			Name:         e.inst.Name,
			Kind:         e.inst.Kind,
			Capabilities: e.cli.Capabilities(),
			Online:       e.err == nil,
			Version:      e.version,
			Count:        len(e.torrents),
		}
		if e.err != nil {
			st.Error = e.err.Error()
			st.ErrorCode = client.ErrorCode(e.err)
		}
		if !e.okAt.IsZero() {
			st.UpdatedAt = e.okAt.Unix()
		}
		for _, t := range e.torrents {
			st.DownloadRate += t.DownloadRate
			st.UploadRate += t.UploadRate
			snap.Torrents = append(snap.Torrents, Torrent{Torrent: t, InstanceID: e.inst.ID})
		}
		e.mu.Unlock()
		snap.Instances = append(snap.Instances, st)
	}
	// A stable order keeps the UI calm between polls; the UI re-sorts anyway.
	sort.SliceStable(snap.Torrents, func(i, j int) bool {
		a, b := snap.Torrents[i], snap.Torrents[j]
		if a.AddedAt != b.AddedAt {
			return a.AddedAt > b.AddedAt
		}
		return a.Hash < b.Hash
	})
	return snap
}

// Retry makes the next snapshot contact the instance again right away, even
// after an authentication failure.
func (m *Manager) Retry(id string) {
	m.mu.RLock()
	e, ok := m.entries[id]
	m.mu.RUnlock()
	if ok {
		e.retry()
	}
}

// Item points at one torrent on one instance.
type Item struct {
	InstanceID string `json:"instanceId"`
	Hash       string `json:"hash"`
}

// Act runs "pause" or "resume" on the given torrents, grouped per instance.
// It returns one error message per instance that failed, keyed by ID.
func (m *Manager) Act(ctx context.Context, action string, items []Item) (map[string]string, error) {
	if action != "pause" && action != "resume" {
		return nil, fmt.Errorf("unknown action %q", action)
	}
	byInstance := map[string][]string{}
	for _, it := range items {
		byInstance[it.InstanceID] = append(byInstance[it.InstanceID], it.Hash)
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed = map[string]string{}
	)
	for id, hashes := range byInstance {
		m.mu.RLock()
		e, ok := m.entries[id]
		m.mu.RUnlock()
		if !ok {
			mu.Lock()
			failed[id] = store.ErrNotFound.Error()
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(e *entry, hashes []string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
			defer cancel()
			var err error
			if action == "pause" {
				err = e.cli.Pause(ctx, hashes)
			} else {
				err = e.cli.Resume(ctx, hashes)
			}
			e.invalidate()
			if err != nil {
				mu.Lock()
				failed[e.inst.ID] = err.Error()
				mu.Unlock()
			}
		}(e, hashes)
	}
	wg.Wait()
	return failed, nil
}

// Test tries to reach a client with the given settings without saving them.
func Test(ctx context.Context, in store.Instance) (string, error) {
	cli, err := client.New(in.Kind, in.ClientConfig())
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	return cli.Version(ctx)
}

// brokenClient stands in for an instance whose configuration could not be
// turned into a client (e.g. a hand-edited config file with a bad URL).
type brokenClient struct {
	kind client.Kind
	err  error
}

func (b brokenClient) Kind() client.Kind                 { return b.kind }
func (b brokenClient) Capabilities() client.Capabilities { return client.Capabilities{} }
func (b brokenClient) Version(context.Context) (string, error) {
	return "", b.err
}
func (b brokenClient) Torrents(context.Context) ([]client.Torrent, error) { return nil, b.err }
func (b brokenClient) Pause(context.Context, []string) error              { return b.err }
func (b brokenClient) Resume(context.Context, []string) error             { return b.err }

// Package search lets the UI search trackers through Prowlarr and send a
// result straight to one of the clients. Prowlarr's download links carry its
// API key, so they stay here: the UI gets an opaque ID per result.
package search

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/prowlarr"
	"github.com/ungerdas/stim/internal/store"
)

const (
	keepFor  = 2 * time.Hour // how long a result can be downloaded after the search
	keepMost = 5000          // results remembered at most
)

// Clients is how a result reaches a client (the manager).
type Clients interface {
	Lookup(id string) (client.Client, store.Instance, bool)
	Invalidate(ids ...string)
}

// Service searches and grabs.
type Service struct {
	store   *store.Store
	clients Clients

	mu      sync.Mutex
	results map[string]kept
}

type kept struct {
	r  prowlarr.Result
	at time.Time
}

// New returns a Service using the search settings in st.
func New(st *store.Store, c Clients) *Service {
	return &Service{store: st, clients: c, results: map[string]kept{}}
}

// ErrNotConfigured means no Prowlarr is set up yet.
var ErrNotConfigured = errors.New("search is not set up: add Prowlarr's address and API key under Settings")

func (s *Service) prowlarr() (*prowlarr.Client, error) {
	cfg := s.store.SearchSettings()
	if !cfg.Configured() {
		return nil, ErrNotConfigured
	}
	return prowlarr.New(cfg.ProwlarrURL, cfg.ProwlarrAPIKey)
}

// Test tries a Prowlarr address and key without saving them.
func Test(ctx context.Context, cfg store.Search) (version string, indexers []prowlarr.Indexer, err error) {
	p, err := prowlarr.New(cfg.ProwlarrURL, cfg.ProwlarrAPIKey)
	if err != nil {
		return "", nil, err
	}
	if version, err = p.Version(ctx); err != nil {
		return "", nil, err
	}
	indexers, err = p.Indexers(ctx)
	return version, indexers, err
}

// Indexers lists the trackers Prowlarr can search.
func (s *Service) Indexers(ctx context.Context) ([]prowlarr.Indexer, error) {
	p, err := s.prowlarr()
	if err != nil {
		return nil, err
	}
	return p.Indexers(ctx)
}

// Hit is a search result as the UI sees it.
type Hit struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Indexer     string   `json:"indexer"`
	IndexerID   int      `json:"indexerId"`
	Size        int64    `json:"size"`
	Seeders     *int     `json:"seeders,omitempty"`
	Leechers    *int     `json:"leechers,omitempty"`
	Grabs       *int     `json:"grabs,omitempty"`
	PublishDate string   `json:"publishDate,omitempty"`
	InfoURL     string   `json:"infoUrl,omitempty"`
	InfoHash    string   `json:"infoHash,omitempty"`
	Freeleech   bool     `json:"freeleech"`
	Flags       []string `json:"flags"`
	Categories  []string `json:"categories"`
}

// Search asks Prowlarr and remembers each result's download link.
func (s *Service) Search(ctx context.Context, query string, indexerIDs, categories []int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: type something to search for", ErrBadRequest)
	}
	p, err := s.prowlarr()
	if err != nil {
		return nil, err
	}
	res, err := p.Search(ctx, query, indexerIDs, categories)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	hits := make([]Hit, 0, len(res))
	for _, r := range res {
		id := newID()
		s.results[id] = kept{r, now}
		h := Hit{
			ID: id, Title: r.Title, Indexer: r.Indexer, IndexerID: r.IndexerID, Size: r.Size,
			Seeders: r.Seeders, Leechers: r.Leechers, Grabs: r.Grabs, PublishDate: r.PublishDate,
			InfoURL: safeURL(r.InfoURL), InfoHash: strings.ToLower(r.InfoHash), Flags: []string{}, Categories: []string{},
		}
		for _, f := range r.Flags {
			h.Flags = append(h.Flags, f)
			if strings.EqualFold(f, "freeleech") {
				h.Freeleech = true
			}
		}
		for _, c := range r.Categories {
			if c.Name != "" && c.ID < 100000 { // above are the tracker's own numbers
				h.Categories = append(h.Categories, c.Name)
			}
		}
		hits = append(hits, h)
	}
	return hits, nil
}

// safeURL drops a link to the tracker's page if it carries a key.
func safeURL(u string) string {
	l := strings.ToLower(u)
	if strings.Contains(l, "apikey") || strings.Contains(l, "passkey") || strings.Contains(l, "authkey") {
		return ""
	}
	return u
}

func (s *Service) prune(now time.Time) {
	for id, k := range s.results {
		if now.Sub(k.at) > keepFor {
			delete(s.results, id)
		}
	}
	for len(s.results) > keepMost {
		var oldest string
		var at time.Time
		for id, k := range s.results {
			if oldest == "" || k.at.Before(at) {
				oldest, at = id, k.at
			}
		}
		delete(s.results, oldest)
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Grab is where and how to download a result.
type Grab struct {
	ID         string `json:"id"`
	InstanceID string `json:"instanceId"`
	SavePath   string `json:"savePath"`
	Label      string `json:"label"`
	Paused     bool   `json:"paused"`
}

// ErrBadRequest wraps problems with what was asked, as opposed to failures
// on the way to Prowlarr or the client.
var ErrBadRequest = errors.New("bad request")

// ErrDuplicate means the client already has the torrent.
var ErrDuplicate = errors.New("duplicate")

// ErrExpired means the result is too old to download; search again.
var ErrExpired = errors.New("the search result has expired; search again")

// Grab fetches a result's .torrent through Prowlarr and adds it to a client.
func (s *Service) Grab(ctx context.Context, g Grab) (string, error) {
	s.mu.Lock()
	k, ok := s.results[g.ID]
	s.mu.Unlock()
	if !ok {
		return "", ErrExpired
	}
	c, inst, ok := s.clients.Lookup(g.InstanceID)
	if !ok {
		return "", fmt.Errorf("%w: unknown client", ErrBadRequest)
	}
	adder, ok := c.(client.Adder)
	if !ok {
		return "", errors.New("this client type cannot take new torrents")
	}
	if p := strings.TrimSpace(g.SavePath); p != "" && !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: the folder must be an absolute path, as the client sees it", ErrBadRequest)
	}
	p, err := s.prowlarr()
	if err != nil {
		return "", err
	}
	f, err := p.Fetch(ctx, k.r)
	if err != nil {
		return "", err
	}
	err = adder.Add(ctx, client.NewTorrent{
		File: f.Torrent, Magnet: f.Magnet, SavePath: strings.TrimSpace(g.SavePath),
		Label: strings.TrimSpace(g.Label), Paused: g.Paused,
	})
	if err != nil {
		if errors.Is(err, client.ErrDuplicate) {
			return "", fmt.Errorf("%w: %s already has this torrent", ErrDuplicate, inst.Name)
		}
		return "", err
	}
	s.clients.Invalidate(g.InstanceID)
	return inst.Name, nil
}

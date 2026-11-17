// Package prowlarr searches trackers through Prowlarr, which keeps the
// logins and knows how to talk to each tracker, and fetches the .torrent
// files it finds.
package prowlarr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one Prowlarr instance.
type Client struct {
	base   string
	apiKey string
	http   *http.Client
}

// ErrAuth means Prowlarr refused the API key.
var ErrAuth = errors.New("Prowlarr refused the API key")

// New returns a client for Prowlarr at baseURL (e.g. http://192.168.50.5:9696).
func New(baseURL, apiKey string) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("the Prowlarr address must start with http:// or https://")
	}
	return &Client{
		base:   strings.TrimRight(u.String(), "/"),
		apiKey: strings.TrimSpace(apiKey),
		// Searches wait for the slowest tracker; Prowlarr gives up on its own.
		http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // a magnet link comes back as a redirect
		}},
	}, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + "/api/v1" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Prowlarr: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrAuth
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("Prowlarr answered HTTP %d: %s", resp.StatusCode, firstLine(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("the address does not answer like Prowlarr; check it, including any path in front")
	}
	return nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Version returns Prowlarr's version, as a connection test.
func (c *Client) Version(ctx context.Context) (string, error) {
	var st struct {
		Version string `json:"version"`
		AppName string `json:"appName"`
	}
	if err := c.get(ctx, "/system/status", nil, &st); err != nil {
		return "", err
	}
	if st.Version == "" {
		return "", errors.New("the address does not answer like Prowlarr")
	}
	return st.Version, nil
}

// Indexer is one tracker set up in Prowlarr.
type Indexer struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Private bool   `json:"private"`
}

// Indexers lists the torrent indexers.
func (c *Client) Indexers(ctx context.Context) ([]Indexer, error) {
	var list []struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Enable   bool   `json:"enable"`
		Protocol string `json:"protocol"`
		Privacy  string `json:"privacy"`
	}
	if err := c.get(ctx, "/indexer", nil, &list); err != nil {
		return nil, err
	}
	out := []Indexer{}
	for _, i := range list {
		if i.Protocol != "" && i.Protocol != "torrent" {
			continue
		}
		out = append(out, Indexer{ID: i.ID, Name: i.Name, Enabled: i.Enable, Private: i.Privacy == "private"})
	}
	return out, nil
}

// Result is one search hit, as Prowlarr reports it. DownloadURL carries the
// Prowlarr API key and must not leave the server.
type Result struct {
	GUID        string   `json:"guid"`
	Title       string   `json:"title"`
	IndexerID   int      `json:"indexerId"`
	Indexer     string   `json:"indexer"`
	Size        int64    `json:"size"`
	Seeders     *int     `json:"seeders"`
	Leechers    *int     `json:"leechers"`
	Grabs       *int     `json:"grabs"`
	PublishDate string   `json:"publishDate"`
	InfoURL     string   `json:"infoUrl"`
	InfoHash    string   `json:"infoHash"`
	DownloadURL string   `json:"downloadUrl"`
	MagnetURL   string   `json:"magnetUrl"`
	Flags       []string `json:"indexerFlags"`
	Categories  []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"categories"`
	Protocol string `json:"protocol"`
}

// Search asks the given indexers (all enabled ones when none are given)
// for query, in the given Newznab categories (all when none).
func (c *Client) Search(ctx context.Context, query string, indexerIDs, categories []int) ([]Result, error) {
	q := url.Values{"query": {query}, "type": {"search"}, "limit": {"100"}, "offset": {"0"}}
	for _, id := range indexerIDs {
		q.Add("indexerIds", strconv.Itoa(id))
	}
	for _, cat := range categories {
		q.Add("categories", strconv.Itoa(cat))
	}
	var res []Result
	if err := c.get(ctx, "/search", q, &res); err != nil {
		return nil, err
	}
	out := res[:0]
	for _, r := range res {
		if r.Protocol == "" || r.Protocol == "torrent" {
			out = append(out, r)
		}
	}
	return out, nil
}

// Fetched is what a download link gave: a .torrent file or a magnet link.
type Fetched struct {
	Torrent []byte
	Magnet  string
}

// Fetch downloads a result through Prowlarr, which logs in to the tracker.
func (c *Client) Fetch(ctx context.Context, r Result) (Fetched, error) {
	link := r.DownloadURL
	if link == "" {
		if strings.HasPrefix(r.MagnetURL, "magnet:") {
			return Fetched{Magnet: r.MagnetURL}, nil
		}
		return Fetched{}, errors.New("the result has no download link")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return Fetched{}, err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("could not reach Prowlarr: %w", err)
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode/100 == 3 && strings.HasPrefix(loc, "magnet:") {
		return Fetched{Magnet: loc}, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return Fetched{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Fetched{}, fmt.Errorf("Prowlarr could not fetch the torrent (HTTP %d): %s", resp.StatusCode, firstLine(body))
	}
	if len(body) == 0 || body[0] != 'd' {
		return Fetched{}, fmt.Errorf("the tracker did not send a torrent file: %s", firstLine(body))
	}
	return Fetched{Torrent: body}, nil
}

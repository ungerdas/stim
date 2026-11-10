package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

// Compile-time checks that the adapter supports moving torrents.
var (
	_ client.Transferer = (*Client)(nil)
	_ client.Exporter   = (*Client)(nil)
)

// post sends a form and requires HTTP 200.
func (c *Client) post(ctx context.Context, name string, form url.Values) error {
	_, status, err := c.do(ctx, http.MethodPost, "/api/v2/torrents/"+name, form)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", name, status)
	}
	return nil
}

func (c *Client) Torrent(ctx context.Context, hash string) (client.Torrent, bool, error) {
	body, err := c.get(ctx, "/api/v2/torrents/info?hashes="+url.QueryEscape(hash))
	if err != nil {
		return client.Torrent{}, false, err
	}
	var raw []apiTorrent
	if err := json.Unmarshal(body, &raw); err != nil {
		return client.Torrent{}, false, errNotQBittorrent
	}
	for _, t := range raw {
		if strings.EqualFold(t.Hash, hash) {
			return t.toTorrent(), true, nil
		}
	}
	return client.Torrent{}, false, nil
}

// ExportTorrent returns the .torrent file as qBittorrent stores it
// (qBittorrent 4.5 and later).
func (c *Client) ExportTorrent(ctx context.Context, hash string) ([]byte, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/api/v2/torrents/export?hash="+url.QueryEscape(hash), nil)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, errors.New("qBittorrent could not export the torrent file (this needs qBittorrent 4.5 or later)")
	default:
		return nil, fmt.Errorf("export: HTTP %d", status)
	}
}

// AddStopped adds the torrent stopped, at opts.SavePath, with automatic
// torrent management and the separate incomplete-downloads folder switched
// off so qBittorrent neither looks for nor puts the data anywhere else.
func (c *Client) AddStopped(ctx context.Context, torrentFile []byte, opts client.AddOptions) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fields := map[string]string{
		"savepath":        opts.SavePath,
		"stopped":         "true", // qBittorrent 5
		"paused":          "true", // qBittorrent 4
		"skip_checking":   "false",
		"autoTMM":         "false",
		"useDownloadPath": "false",
		"contentLayout":   "Original",
		"root_folder":     "true", // pre-4.3.2 name for the same thing
	}
	if opts.Label != "" {
		fields["category"] = opts.Label
	}
	if len(opts.Tags) > 0 {
		fields["tags"] = strings.Join(opts.Tags, ",")
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return err
		}
	}
	part, err := w.CreateFormFile("torrents", "stim.torrent")
	if err != nil {
		return err
	}
	if _, err := part.Write(torrentFile); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	body, status, err := c.doPayload(ctx, http.MethodPost, "/api/v2/torrents/add",
		&payload{body: buf.Bytes(), contentType: w.FormDataContentType()})
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(body))
	if status != http.StatusOK {
		return fmt.Errorf("add: HTTP %d %s", status, text)
	}
	// qBittorrent 4 answers "Ok." or "Fails."; qBittorrent 5 answers JSON.
	if text == "Fails." {
		return errors.New("qBittorrent refused the torrent")
	}
	var res struct {
		Success *int `json:"success_count"`
		Failure int  `json:"failure_count"`
	}
	if json.Unmarshal(body, &res) == nil && res.Success != nil && (*res.Success == 0 || res.Failure > 0) {
		return errors.New("qBittorrent refused the torrent")
	}
	return nil
}

func (c *Client) Recheck(ctx context.Context, hash string) error {
	return c.post(ctx, "recheck", url.Values{"hashes": {hash}})
}

// RemoveKeepData removes the torrent and leaves its files.
func (c *Client) RemoveKeepData(ctx context.Context, hash string) error {
	return c.post(ctx, "delete", url.Values{"hashes": {hash}, "deleteFiles": {"false"}})
}

func (c *Client) Move(ctx context.Context, hash, path string) error {
	return c.post(ctx, "setLocation", url.Values{"hashes": {hash}, "location": {path}})
}

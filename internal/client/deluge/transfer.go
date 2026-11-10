package deluge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

// Compile-time checks that the adapter supports moving torrents.
var (
	_ client.Transferer    = (*Client)(nil)
	_ client.TrackerLister = (*Client)(nil)
	_ client.Mover         = (*Client)(nil)
)

func (c *Client) Torrent(ctx context.Context, hash string) (client.Torrent, bool, error) {
	res, err := c.call(ctx, "core.get_torrent_status", hash, statusKeys)
	if err != nil {
		return client.Torrent{}, false, err
	}
	var raw apiTorrent
	if err := json.Unmarshal(res, &raw); err != nil {
		return client.Torrent{}, false, errNotDeluge
	}
	// Deluge answers an empty object for a torrent it does not have.
	if raw.Name == "" && raw.State == "" {
		return client.Torrent{}, false, nil
	}
	return raw.toTorrent(hash), true, nil
}

// Trackers lists the torrent's announce URLs, best tier first.
func (c *Client) Trackers(ctx context.Context, hash string) ([]string, error) {
	res, err := c.call(ctx, "core.get_torrent_status", hash, []string{"trackers"})
	if err != nil {
		return nil, err
	}
	var raw struct {
		Trackers []struct {
			URL string `json:"url"`
		} `json:"trackers"`
	}
	if err := json.Unmarshal(res, &raw); err != nil {
		return nil, errNotDeluge
	}
	urls := make([]string, 0, len(raw.Trackers))
	for _, t := range raw.Trackers {
		if t.URL != "" {
			urls = append(urls, t.URL)
		}
	}
	return urls, nil
}

// AddStopped adds the torrent paused at opts.SavePath. "Move completed" is
// switched off for it, so a Deluge that normally moves finished downloads
// leaves this data where it is.
func (c *Client) AddStopped(ctx context.Context, torrentFile []byte, opts client.AddOptions) error {
	options := map[string]any{
		"download_location": opts.SavePath,
		"add_paused":        true,
		"move_completed":    false,
	}
	res, err := c.call(ctx, "core.add_torrent_file", "stim.torrent",
		base64.StdEncoding.EncodeToString(torrentFile), options)
	if err != nil {
		return err
	}
	var id string
	if json.Unmarshal(res, &id) != nil || id == "" {
		return errors.New("Deluge refused the torrent")
	}
	if opts.Label != "" {
		// A missing label is cosmetic; never fail the move because of it.
		_ = c.setLabel(ctx, id, opts.Label)
	}
	return nil
}

var invalidLabelChars = regexp.MustCompile(`[^a-z0-9_-]+`)

// setLabel applies a label through the Label plugin, creating it first when
// needed. Deluge only accepts lowercase letters, digits, "_" and "-".
func (c *Client) setLabel(ctx context.Context, id, label string) error {
	label = strings.Trim(invalidLabelChars.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if label == "" {
		return nil
	}
	res, err := c.call(ctx, "label.get_labels")
	if err != nil {
		return err // plugin not enabled
	}
	var labels []string
	_ = json.Unmarshal(res, &labels)
	found := false
	for _, l := range labels {
		if l == label {
			found = true
			break
		}
	}
	if !found {
		if _, err := c.call(ctx, "label.add", label); err != nil {
			return err
		}
	}
	_, err = c.call(ctx, "label.set_torrent", id, label)
	return err
}

func (c *Client) Recheck(ctx context.Context, hash string) error {
	_, err := c.call(ctx, "core.force_recheck", []string{hash})
	return err
}

// RemoveKeepData removes the torrent with remove_data=false.
func (c *Client) RemoveKeepData(ctx context.Context, hash string) error {
	const removeData = false
	res, err := c.call(ctx, "core.remove_torrent", hash, removeData)
	if err != nil {
		return err
	}
	// Deluge 2 answers true; older versions answer nothing at all.
	if strings.TrimSpace(string(res)) == "false" {
		return fmt.Errorf("Deluge did not remove the torrent")
	}
	return nil
}

func (c *Client) Move(ctx context.Context, hash, path string) error {
	_, err := c.call(ctx, "core.move_storage", []string{hash}, path)
	return err
}

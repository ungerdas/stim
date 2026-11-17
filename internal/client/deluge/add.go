package deluge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

// Add starts downloading a new torrent.
func (c *Client) Add(ctx context.Context, t client.NewTorrent) error {
	options := map[string]any{"add_paused": t.Paused}
	if t.SavePath != "" {
		options["download_location"] = t.SavePath
	}
	var (
		res json.RawMessage
		err error
	)
	if t.Magnet != "" {
		res, err = c.call(ctx, "core.add_torrent_magnet", t.Magnet, options)
	} else {
		res, err = c.call(ctx, "core.add_torrent_file", "stim.torrent", base64.StdEncoding.EncodeToString(t.File), options)
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already in session") {
			return client.ErrDuplicate
		}
		return err
	}
	var id string
	if json.Unmarshal(res, &id) != nil || id == "" {
		return errors.New("Deluge refused the torrent; it may already have it")
	}
	if t.Label != "" {
		_ = c.setLabel(ctx, id, t.Label) // a missing label is cosmetic
	}
	return nil
}

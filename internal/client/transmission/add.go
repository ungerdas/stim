package transmission

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/ungerdas/stim/internal/client"
)

// Add starts downloading a new torrent.
func (c *Client) Add(ctx context.Context, t client.NewTorrent) error {
	var res struct {
		Added *struct {
			Hash string `json:"hashString"`
		} `json:"torrent-added"`
		Duplicate *struct {
			Hash string `json:"hashString"`
		} `json:"torrent-duplicate"`
	}
	args := map[string]any{"paused": t.Paused}
	if t.Magnet != "" {
		args["filename"] = t.Magnet
	} else {
		args["metainfo"] = base64.StdEncoding.EncodeToString(t.File)
	}
	if t.SavePath != "" {
		args["download-dir"] = t.SavePath
	}
	if err := c.call(ctx, "torrent-add", args, &res); err != nil {
		return err
	}
	if res.Duplicate != nil {
		return client.ErrDuplicate
	}
	if res.Added == nil {
		return errors.New("Transmission refused the torrent")
	}
	if t.Label != "" {
		_ = c.call(ctx, "torrent-set", map[string]any{"ids": []string{res.Added.Hash}, "labels": []string{t.Label}}, nil)
	}
	return nil
}

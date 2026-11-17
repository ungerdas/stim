package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

// Add starts downloading a new torrent. Without a save path qBittorrent
// decides, from the category (with automatic management) or its default.
func (c *Client) Add(ctx context.Context, t client.NewTorrent) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fields := map[string]string{}
	if t.SavePath != "" {
		fields["savepath"] = t.SavePath
		fields["autoTMM"] = "false"
	}
	if t.Label != "" {
		fields["category"] = t.Label
	}
	if t.Paused {
		fields["stopped"], fields["paused"] = "true", "true"
	}
	if t.Magnet != "" {
		fields["urls"] = t.Magnet
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return err
		}
	}
	if len(t.File) > 0 {
		part, err := w.CreateFormFile("torrents", "stim.torrent")
		if err != nil {
			return err
		}
		if _, err := part.Write(t.File); err != nil {
			return err
		}
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
	switch {
	case status == http.StatusConflict:
		return client.ErrDuplicate // qBittorrent 5.1+
	case status != http.StatusOK:
		return fmt.Errorf("add: HTTP %d %s", status, text)
	case text == "Fails.":
		return errors.New("qBittorrent refused the torrent; it may already have it")
	}
	var res struct {
		Success *int `json:"success_count"`
		Failure int  `json:"failure_count"`
	}
	if json.Unmarshal(body, &res) == nil && res.Success != nil && *res.Success == 0 {
		if res.Failure > 0 {
			return errors.New("qBittorrent refused the torrent; it may already have it")
		}
		return client.ErrDuplicate
	}
	return nil
}

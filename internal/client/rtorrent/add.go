package rtorrent

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/remotefiles"
)

// Add starts downloading a new torrent. Without a save path rTorrent uses
// its default folder.
func (c *Client) Add(ctx context.Context, t client.NewTorrent) error {
	method := "load.raw_start"
	if t.Paused {
		method = "load.raw"
	}
	var data any = Base64(t.File)
	if t.Magnet != "" {
		method, data = "load.start", t.Magnet
		if t.Paused {
			method = "load.normal"
		}
	}
	params := []any{"", data, "d.custom.set=addtime," + strconv.FormatInt(time.Now().Unix(), 10)}
	if t.SavePath != "" {
		// rTorrent makes only the torrent's own folder; without the one
		// above it, the download sits still with nothing but a line in
		// rTorrent's own log.
		if err := c.makeDir(ctx, t.SavePath); err != nil {
			return err
		}
		params = append(params, "d.directory.set="+t.SavePath)
	}
	if t.Label != "" {
		params = append(params, "d.custom1.set="+url.PathEscape(t.Label))
	}
	if _, err := c.call(ctx, method, params...); err != nil {
		return fmt.Errorf("rTorrent did not accept the torrent: %w", err)
	}
	return nil
}

// makeDir creates a folder through rTorrent, or over SSH where ruTorrent
// refuses the command.
func (c *Client) makeDir(ctx context.Context, dir string) error {
	if _, err := c.call(ctx, "execute.throw", "", "mkdir", "-p", "--", dir); err == nil {
		return nil
	}
	ssh := c.cfg.SSH
	if !ssh.Configured() || ssh.HostKey == "" {
		return fmt.Errorf("rTorrent cannot create %s through this address; pick a folder that exists, or set up and test SSH access for this client", dir)
	}
	s, err := remotefiles.Open(ctx, ssh, true)
	if err != nil {
		return fmt.Errorf("creating %s over SSH: %w", dir, err)
	}
	defer s.Close()
	if _, err := s.MkdirAll(dir); err != nil {
		return fmt.Errorf("creating %s over SSH: %w", dir, err)
	}
	return nil
}

var _ client.Adder = (*Client)(nil)

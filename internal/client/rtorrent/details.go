package rtorrent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

// Details reads one torrent's files, trackers, peers and properties.
func (c *Client) Details(ctx context.Context, hash string) (client.Details, error) {
	var d client.Details
	id := strings.ToUpper(hash)

	v, err := c.call(ctx, "f.multicall", id, "", "f.path=", "f.size_bytes=", "f.completed_chunks=", "f.size_chunks=", "f.priority=")
	if err != nil {
		return d, err
	}
	for _, r := range asRows(v) {
		f := client.File{Path: asString(at(r, 0)), Size: asInt(at(r, 1)), Wanted: asInt(at(r, 4)) != 0}
		if chunks := asInt(at(r, 3)); chunks > 0 {
			f.Progress = float64(asInt(at(r, 2))) / float64(chunks)
		}
		d.Files = append(d.Files, f)
	}

	message := ""
	if m, err := c.call(ctx, "d.message", id); err == nil {
		_, message = trackerProblem(asString(m))
	}
	if v, err := c.call(ctx, "t.multicall", id, "", "t.url=", "t.group=", "t.is_enabled=", "t.scrape_complete=",
		"t.scrape_incomplete=", "t.scrape_downloaded=", "t.success_counter=", "t.failed_counter="); err == nil {
		for _, r := range asRows(v) {
			u := asString(at(r, 0))
			if strings.HasPrefix(u, "dht://") {
				continue
			}
			info := client.TrackerInfo{URL: client.MaskURL(u), Tier: int(asInt(at(r, 1))),
				Seeds: int(asInt(at(r, 3))), Leeches: int(asInt(at(r, 4))), Downloaded: int(asInt(at(r, 5)))}
			switch {
			case asInt(at(r, 2)) == 0:
				info.Status = client.TrackerDisabled
			case asInt(at(r, 7)) > 0:
				info.Status, info.Message = client.TrackerFailing, message
			case asInt(at(r, 6)) > 0:
				info.Status = client.TrackerWorking
			default:
				info.Status = client.TrackerWaiting
			}
			d.Trackers = append(d.Trackers, info)
		}
	}
	if v, err := c.call(ctx, "p.multicall", id, "", "p.address=", "p.port=", "p.client_version=", "p.completed_percent=",
		"p.down_rate=", "p.up_rate=", "p.is_encrypted="); err == nil {
		for _, r := range asRows(v) {
			d.Peers = append(d.Peers, client.Peer{
				Address: fmt.Sprintf("%s:%d", asString(at(r, 0)), asInt(at(r, 1))), Client: asString(at(r, 2)),
				Progress: float64(asInt(at(r, 3))) / 100, DownRate: asInt(at(r, 4)), UpRate: asInt(at(r, 5)),
				Encrypted: asInt(at(r, 6)) != 0,
			})
		}
	}
	if v, err := c.call(ctx, "d.chunk_size", id); err == nil {
		d.PieceSize = asInt(v)
	}
	if v, err := c.call(ctx, "d.size_chunks", id); err == nil {
		d.Pieces = int(asInt(v))
	}
	if v, err := c.call(ctx, "d.is_private", id); err == nil {
		p := asInt(v) != 0
		d.Private = &p
	}
	return d, nil
}

func asRows(v any) [][]any {
	list, _ := v.([]any)
	out := make([][]any, 0, len(list))
	for _, r := range list {
		if row, ok := r.([]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func at(row []any, i int) any {
	if i < len(row) {
		return row[i]
	}
	return nil
}

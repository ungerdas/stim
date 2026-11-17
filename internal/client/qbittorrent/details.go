package qbittorrent

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

// Details reads one torrent's files, trackers, peers and properties.
func (c *Client) Details(ctx context.Context, hash string) (client.Details, error) {
	q := "?hash=" + url.QueryEscape(hash)
	var d client.Details

	body, err := c.get(ctx, "/api/v2/torrents/files"+q)
	if err != nil {
		return d, err
	}
	var files []struct {
		Name     string  `json:"name"`
		Size     int64   `json:"size"`
		Progress float64 `json:"progress"`
		Priority int     `json:"priority"`
	}
	if err := json.Unmarshal(body, &files); err != nil {
		return d, errNotQBittorrent
	}
	for _, f := range files {
		d.Files = append(d.Files, client.File{Path: f.Name, Size: f.Size, Progress: f.Progress, Wanted: f.Priority != 0})
	}

	if body, err = c.get(ctx, "/api/v2/torrents/trackers"+q); err != nil {
		return d, err
	}
	var trackers []struct {
		apiTracker
		Tier       json.Number `json:"tier"` // "" for DHT and friends in some versions
		Seeds      int         `json:"num_seeds"`
		Leeches    int         `json:"num_leeches"`
		Downloaded int         `json:"num_downloaded"`
	}
	if err := json.Unmarshal(body, &trackers); err != nil {
		return d, errNotQBittorrent
	}
	for _, tr := range trackers {
		if strings.HasPrefix(tr.URL, "** [") {
			continue
		}
		tier, _ := tr.Tier.Int64()
		info := client.TrackerInfo{URL: client.MaskURL(tr.URL), Tier: int(tier), Seeds: tr.Seeds, Leeches: tr.Leeches, Downloaded: tr.Downloaded, Message: tr.Msg}
		switch tr.Status {
		case 0:
			info.Status = client.TrackerDisabled
		case 1:
			info.Status = client.TrackerWaiting
		case 2:
			info.Status = client.TrackerWorking
		case 3:
			info.Status = client.TrackerUpdating
		default:
			info.Status = client.TrackerFailing
			if _, m := trackerVerdict([]apiTracker{tr.apiTracker}); m != "" {
				info.Message = m
			}
		}
		d.Trackers = append(d.Trackers, info)
	}

	if body, err = c.get(ctx, "/api/v2/sync/torrentPeers"+q+"&rid=0"); err == nil {
		var peers struct {
			Peers map[string]struct {
				IP         string  `json:"ip"`
				Port       int     `json:"port"`
				Client     string  `json:"client"`
				Progress   float64 `json:"progress"`
				DlSpeed    int64   `json:"dl_speed"`
				UpSpeed    int64   `json:"up_speed"`
				Flags      string  `json:"flags"`
				Country    string  `json:"country_code"`
				Connection string  `json:"connection"`
			} `json:"peers"`
		}
		if json.Unmarshal(body, &peers) == nil {
			for addr, p := range peers.Peers {
				d.Peers = append(d.Peers, client.Peer{
					Address: addr, Client: p.Client, Progress: p.Progress, DownRate: p.DlSpeed, UpRate: p.UpSpeed,
					Flags: p.Flags, Country: strings.ToUpper(p.Country), Encrypted: strings.Contains(p.Flags, "E"),
				})
			}
		}
	}

	if body, err = c.get(ctx, "/api/v2/torrents/properties"+q); err == nil {
		var props struct {
			Comment   string `json:"comment"`
			CreatedBy string `json:"created_by"`
			Created   int64  `json:"creation_date"`
			PieceSize int64  `json:"piece_size"`
			Pieces    int    `json:"pieces_num"`
			Private   *bool  `json:"is_private"`
		}
		if json.Unmarshal(body, &props) == nil {
			d.Comment, d.CreatedBy, d.PieceSize, d.Pieces, d.Private = props.Comment, props.CreatedBy, props.PieceSize, props.Pieces, props.Private
			if props.Created > 0 {
				d.CreatedAt = props.Created
			}
		}
	}
	return d, nil
}

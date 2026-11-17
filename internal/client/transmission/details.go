package transmission

import (
	"context"
	"fmt"

	"github.com/ungerdas/stim/internal/client"
)

// Details reads one torrent's files, trackers, peers and properties.
func (c *Client) Details(ctx context.Context, hash string) (client.Details, error) {
	var d client.Details
	var res struct {
		Torrents []struct {
			Files []struct {
				Name      string `json:"name"`
				Length    int64  `json:"length"`
				Completed int64  `json:"bytesCompleted"`
			} `json:"files"`
			FileStats []struct {
				Wanted bool `json:"wanted"`
			} `json:"fileStats"`
			TrackerStats []struct {
				Announce      string `json:"announce"`
				Tier          int    `json:"tier"`
				HasAnnounced  bool   `json:"hasAnnounced"`
				Succeeded     bool   `json:"lastAnnounceSucceeded"`
				Result        string `json:"lastAnnounceResult"`
				AnnounceState int    `json:"announceState"` // 0 inactive, 1 waiting, 2 queued, 3 active
				Seeders       int    `json:"seederCount"`
				Leechers      int    `json:"leecherCount"`
				Downloads     int    `json:"downloadCount"`
			} `json:"trackerStats"`
			Peers []struct {
				Address     string  `json:"address"`
				Port        int     `json:"port"`
				ClientName  string  `json:"clientName"`
				Progress    float64 `json:"progress"`
				RateToUs    int64   `json:"rateToClient"`
				RateToPeer  int64   `json:"rateToPeer"`
				Flags       string  `json:"flagStr"`
				IsEncrypted bool    `json:"isEncrypted"`
			} `json:"peers"`
			Comment   string `json:"comment"`
			Creator   string `json:"creator"`
			Created   int64  `json:"dateCreated"`
			PieceSize int64  `json:"pieceSize"`
			Pieces    int    `json:"pieceCount"`
			Private   *bool  `json:"isPrivate"`
		} `json:"torrents"`
	}
	fields := []string{"files", "fileStats", "trackerStats", "peers", "comment", "creator", "dateCreated", "pieceSize", "pieceCount", "isPrivate"}
	if err := c.call(ctx, "torrent-get", map[string]any{"ids": []string{hash}, "fields": fields}, &res); err != nil {
		return d, err
	}
	if len(res.Torrents) == 0 {
		return d, fmt.Errorf("transmission: no torrent %s", hash)
	}
	t := res.Torrents[0]
	for i, f := range t.Files {
		file := client.File{Path: f.Name, Size: f.Length, Wanted: true}
		if f.Length > 0 {
			file.Progress = float64(f.Completed) / float64(f.Length)
		}
		if i < len(t.FileStats) {
			file.Wanted = t.FileStats[i].Wanted
		}
		d.Files = append(d.Files, file)
	}
	for _, tr := range t.TrackerStats {
		info := client.TrackerInfo{URL: client.MaskURL(tr.Announce), Tier: tr.Tier, Seeds: tr.Seeders, Leeches: tr.Leechers, Downloaded: tr.Downloads}
		switch {
		case tr.AnnounceState == 3:
			info.Status = client.TrackerUpdating
		case !tr.HasAnnounced:
			info.Status = client.TrackerWaiting
		case tr.Succeeded:
			info.Status = client.TrackerWorking
		default:
			info.Status = client.TrackerFailing
			info.Message = tr.Result
		}
		d.Trackers = append(d.Trackers, info)
	}
	for _, p := range t.Peers {
		d.Peers = append(d.Peers, client.Peer{
			Address: fmt.Sprintf("%s:%d", p.Address, p.Port), Client: p.ClientName, Progress: p.Progress,
			DownRate: p.RateToUs, UpRate: p.RateToPeer, Flags: p.Flags, Encrypted: p.IsEncrypted,
		})
	}
	d.Comment, d.CreatedBy, d.CreatedAt, d.PieceSize, d.Pieces, d.Private = t.Comment, t.Creator, t.Created, t.PieceSize, t.Pieces, t.Private
	return d, nil
}

package deluge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ungerdas/stim/internal/client"
)

var detailKeys = []string{
	"files", "file_progress", "file_priorities", "trackers", "tracker", "tracker_status",
	"peers", "comment", "creator", "piece_length", "num_pieces", "private",
	"total_seeds", "total_peers",
}

// Details reads one torrent's files, trackers, peers and properties.
func (c *Client) Details(ctx context.Context, hash string) (client.Details, error) {
	var d client.Details
	res, err := c.call(ctx, "core.get_torrent_status", hash, detailKeys)
	if err != nil {
		return d, err
	}
	var st struct {
		Files []struct {
			Path string  `json:"path"`
			Size float64 `json:"size"`
		} `json:"files"`
		FileProgress   []float64 `json:"file_progress"`
		FilePriorities []float64 `json:"file_priorities"`
		Trackers       []struct {
			URL  string  `json:"url"`
			Tier float64 `json:"tier"`
		} `json:"trackers"`
		Tracker       string `json:"tracker"` // the one in use
		TrackerStatus string `json:"tracker_status"`
		Peers         []struct {
			Client   string  `json:"client"`
			Country  string  `json:"country"`
			Down     float64 `json:"down_speed"`
			Up       float64 `json:"up_speed"`
			IP       string  `json:"ip"`
			Progress float64 `json:"progress"`
			Seed     any     `json:"seed"`
		} `json:"peers"`
		Comment    string   `json:"comment"`
		Creator    string   `json:"creator"`
		PieceSize  float64  `json:"piece_length"`
		Pieces     float64  `json:"num_pieces"`
		Private    *bool    `json:"private"`
		TotalSeeds *float64 `json:"total_seeds"`
		TotalPeers *float64 `json:"total_peers"`
	}
	if err := json.Unmarshal(res, &st); err != nil {
		return d, errNotDeluge
	}
	if len(st.Files) == 0 && st.Trackers == nil {
		return d, fmt.Errorf("deluge: no torrent %s", hash)
	}
	for i, f := range st.Files {
		file := client.File{Path: f.Path, Size: int64(f.Size), Wanted: true}
		if i < len(st.FileProgress) {
			file.Progress = st.FileProgress[i]
		}
		if i < len(st.FilePriorities) {
			file.Wanted = st.FilePriorities[i] != 0
		}
		d.Files = append(d.Files, file)
	}
	// Deluge reports the status of the tracker in use only.
	for _, tr := range st.Trackers {
		info := client.TrackerInfo{URL: client.MaskURL(tr.URL), Tier: int(tr.Tier), Status: client.TrackerWaiting, Seeds: -1, Leeches: -1, Downloaded: -1}
		if tr.URL == st.Tracker || len(st.Trackers) == 1 {
			switch s := st.TrackerStatus; {
			case strings.HasPrefix(s, "Announce OK"):
				info.Status = client.TrackerWorking
			case strings.HasPrefix(s, "Announce Sent"):
				info.Status = client.TrackerUpdating
			case strings.HasPrefix(s, "Error:"):
				info.Status = client.TrackerFailing
				info.Message = strings.TrimSpace(strings.TrimPrefix(s, "Error:"))
			case strings.HasPrefix(s, "Warning:"):
				info.Status = client.TrackerWorking
				info.Message = strings.TrimSpace(strings.TrimPrefix(s, "Warning:"))
			}
			if st.TotalSeeds != nil && *st.TotalSeeds >= 0 {
				info.Seeds = int(*st.TotalSeeds)
			}
			if st.TotalPeers != nil && *st.TotalPeers >= 0 {
				info.Leeches = int(*st.TotalPeers)
			}
		}
		d.Trackers = append(d.Trackers, info)
	}
	for _, p := range st.Peers {
		d.Peers = append(d.Peers, client.Peer{
			Address: p.IP, Client: p.Client, Country: strings.ToUpper(strings.TrimSpace(p.Country)),
			DownRate: int64(p.Down), UpRate: int64(p.Up), Progress: p.Progress,
		})
	}
	d.Comment, d.CreatedBy, d.PieceSize, d.Pieces, d.Private = st.Comment, st.Creator, int64(st.PieceSize), int(st.Pieces), st.Private
	return d, nil
}

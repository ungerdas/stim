package client

import "strings"

// Tracker trouble, as shown in Torrent.TrackerStatus. No value means no
// known problem: working, not contacted yet, or the client does not say.
const (
	// TrackerError is a tracker problem that may pass: the tracker is
	// down, times out, or cannot be resolved.
	TrackerError = "error"
	// TrackerUnregistered means the tracker no longer accepts the torrent:
	// deleted, trumped, replaced, or never uploaded there. It will not
	// come back on its own.
	TrackerUnregistered = "unregistered"
)

// unregisteredPhrases are pieces of the messages trackers send for torrents
// they no longer know, in lower case. They are specific on purpose: a
// generic "not found" also covers "Host not found", a DNS failure that
// passes.
var unregisteredPhrases = []string{
	"unregistered",
	"not registered",
	"torrent not found",
	"torrent is not found",
	"torrent does not exist",
	"torrent doesn't exist",
	"torrent not exist",
	"unknown torrent",
	"infohash not found",
	"info hash not found",
	"info_hash not found",
	"torrent has been deleted",
	"torrent was deleted",
	"torrent has been removed",
	"torrent was removed",
	"torrent has been nuked",
	"has been trumped",
	"trumped",
	"retitled",
	"truncated",
	"torrent is not authorized",
	"not authorized for use on this tracker",
	"been replaced by",
	"packs are available",
	"complete season uploaded",
	"problem with description",
	"specifically banned",
	// Norwegian trackers.
	"ikke registrert",
	"torrenten finnes ikke",
	"torrent finnes ikke",
	"torrenten er slettet",
	"torrent er slettet",
}

// ClassifyTrackerMessage tells an error message from a tracker apart:
// TrackerUnregistered for a torrent the tracker no longer knows, otherwise
// TrackerError.
func ClassifyTrackerMessage(msg string) string {
	m := strings.ToLower(msg)
	for _, p := range unregisteredPhrases {
		if strings.Contains(m, p) {
			return TrackerUnregistered
		}
	}
	return TrackerError
}

// TrackerVerdict sums up a torrent's trackers: no problem when any of them
// works, otherwise the worst of the error messages (unregistered beats a
// passing error), or nothing when there are no errors yet.
func TrackerVerdict(anyWorking bool, errors []string) (status, message string) {
	if anyWorking {
		return "", ""
	}
	for _, m := range errors {
		if m = strings.TrimSpace(m); m != "" && ClassifyTrackerMessage(m) == TrackerUnregistered {
			return TrackerUnregistered, m
		}
	}
	for _, m := range errors {
		if m = strings.TrimSpace(m); m != "" {
			return TrackerError, m
		}
	}
	return "", ""
}

// SpaceReporter is implemented by adapters that know how much disk space the
// client has left where it saves new torrents. FreeSpace returns what was
// seen during the last successful Torrents call; ok is false when unknown.
type SpaceReporter interface {
	FreeSpace() (bytes int64, ok bool)
}

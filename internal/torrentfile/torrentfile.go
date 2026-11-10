// Package torrentfile reads just enough of a .torrent file to check that it
// is the torrent we expect and to add announce URLs when it has none. The
// info dictionary is never re-encoded, so the info hash cannot change.
package torrentfile

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrInvalid is returned for data that is not a usable .torrent file.
var ErrInvalid = errors.New("not a valid .torrent file")

const maxDepth = 64

// skip returns the index just past the bencoded value starting at i.
func skip(b []byte, i, depth int) (int, error) {
	if i >= len(b) || depth > maxDepth {
		return 0, ErrInvalid
	}
	switch c := b[i]; {
	case c == 'i':
		end := bytes.IndexByte(b[i:], 'e')
		if end < 2 {
			return 0, ErrInvalid
		}
		return i + end + 1, nil
	case c == 'l' || c == 'd':
		i++
		for i < len(b) && b[i] != 'e' {
			var err error
			if i, err = skip(b, i, depth+1); err != nil {
				return 0, err
			}
		}
		if i >= len(b) {
			return 0, ErrInvalid
		}
		return i + 1, nil
	case c >= '0' && c <= '9':
		_, end, err := readString(b, i)
		return end, err
	default:
		return 0, ErrInvalid
	}
}

// readString decodes the byte string at i and returns it with the index
// just past it.
func readString(b []byte, i int) ([]byte, int, error) {
	colon := bytes.IndexByte(b[i:], ':')
	if colon < 1 || colon > 12 {
		return nil, 0, ErrInvalid
	}
	n, err := strconv.Atoi(string(b[i : i+colon]))
	start := i + colon + 1
	if err != nil || n < 0 || start+n > len(b) {
		return nil, 0, ErrInvalid
	}
	return b[start : start+n], start + n, nil
}

// File is a parsed .torrent: its top-level keys with their raw, still
// encoded values.
type File struct {
	keys []string
	raw  map[string][]byte
}

// Parse reads a .torrent file.
func Parse(data []byte) (*File, error) {
	if len(data) < 2 || data[0] != 'd' {
		return nil, ErrInvalid
	}
	f := &File{raw: map[string][]byte{}}
	i := 1
	for i < len(data) && data[i] != 'e' {
		key, next, err := readString(data, i)
		if err != nil {
			return nil, err
		}
		end, err := skip(data, next, 1)
		if err != nil {
			return nil, err
		}
		k := string(key)
		if _, dup := f.raw[k]; !dup {
			f.keys = append(f.keys, k)
		}
		f.raw[k] = data[next:end]
		i = end
	}
	if i >= len(data) {
		return nil, ErrInvalid
	}
	info, ok := f.raw["info"]
	if !ok || len(info) == 0 || info[0] != 'd' {
		return nil, fmt.Errorf("%w: no info dictionary", ErrInvalid)
	}
	return f, nil
}

// InfoHashes returns the hashes a client may identify this torrent by: the
// v1 info hash, and the truncated v2 hash that some clients use for hybrid
// and v2 torrents.
func (f *File) InfoHashes() []string {
	info := f.raw["info"]
	v1 := sha1.Sum(info)
	v2 := sha256.Sum256(info)
	return []string{hex.EncodeToString(v1[:]), hex.EncodeToString(v2[:20])}
}

// Matches reports whether hash identifies this torrent.
func (f *File) Matches(hash string) bool {
	for _, h := range f.InfoHashes() {
		if strings.EqualFold(h, hash) {
			return true
		}
	}
	return false
}

// HasTrackers reports whether the file carries any announce URL.
func (f *File) HasTrackers() bool {
	if a, ok := f.raw["announce"]; ok {
		if s, _, err := readString(a, 0); err == nil && len(s) > 0 {
			return true
		}
	}
	// "lle" is an announce-list holding one empty tier.
	if l, ok := f.raw["announce-list"]; ok && len(l) > 2 && string(l) != "lle" {
		return true
	}
	return false
}

func encodeString(s string) []byte {
	return []byte(strconv.Itoa(len(s)) + ":" + s)
}

// WithTrackers returns the file with announce and announce-list replaced by
// urls (one tier per URL). Every other key, the info dictionary included,
// is carried over byte for byte.
func (f *File) WithTrackers(urls []string) []byte {
	raw := make(map[string][]byte, len(f.raw)+2)
	for k, v := range f.raw {
		raw[k] = v
	}
	if len(urls) > 0 {
		raw["announce"] = encodeString(urls[0])
		var list bytes.Buffer
		list.WriteByte('l')
		for _, u := range urls {
			list.WriteByte('l')
			list.Write(encodeString(u))
			list.WriteByte('e')
		}
		list.WriteByte('e')
		raw["announce-list"] = list.Bytes()
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys) // bencode dictionaries are sorted by key

	var out bytes.Buffer
	out.WriteByte('d')
	for _, k := range keys {
		out.Write(encodeString(k))
		out.Write(raw[k])
	}
	out.WriteByte('e')
	return out.Bytes()
}

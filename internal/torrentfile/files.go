package torrentfile

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Entry is one file in a torrent: its path inside the torrent (below the
// torrent's name for multi-file torrents) and its size.
type Entry struct {
	Path   string // "" for a single-file torrent; "Season 1/E01.mkv" otherwise
	Length int64
}

// Layout is what a torrent puts on disk: the top-level name (a file for a
// single-file torrent, a folder otherwise) and the files below it.
type Layout struct {
	Name  string
	Multi bool
	Files []Entry
}

// Total is the sum of the file sizes.
func (l Layout) Total() int64 {
	var n int64
	for _, f := range l.Files {
		n += f.Length
	}
	return n
}

// RelPaths lists every file relative to the save folder.
func (l Layout) RelPaths() []string {
	out := make([]string, 0, len(l.Files))
	for _, f := range l.Files {
		if l.Multi {
			out = append(out, path.Join(l.Name, f.Path))
		} else {
			out = append(out, l.Name)
		}
	}
	return out
}

// Layout decodes the info dictionary. It understands v1 torrents, hybrid
// torrents (v1 file list with BEP 47 padding files, which clients never
// write to disk) and v2-only torrents (the "file tree"). Names that could
// escape the save folder are rejected.
func (f *File) Layout() (Layout, error) {
	v, _, err := decode(f.raw["info"], 0, 0)
	if err != nil {
		return Layout{}, err
	}
	info, ok := v.(map[string]any)
	if !ok {
		return Layout{}, fmt.Errorf("%w: info is not a dictionary", ErrInvalid)
	}
	name := str(info["name.utf-8"])
	if name == "" {
		name = str(info["name"])
	}
	if !safeComponent(name) {
		return Layout{}, fmt.Errorf("%w: unsafe name %q", ErrInvalid, name)
	}
	l := Layout{Name: name}

	switch {
	case info["files"] != nil:
		l.Multi = true
		list, _ := info["files"].([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			if strings.Contains(str(m["attr"]), "p") {
				continue // padding file
			}
			parts := list2strings(m["path.utf-8"])
			if len(parts) == 0 {
				parts = list2strings(m["path"])
			}
			if len(parts) == 0 {
				return Layout{}, fmt.Errorf("%w: file without a path", ErrInvalid)
			}
			for _, p := range parts {
				if !safeComponent(p) {
					return Layout{}, fmt.Errorf("%w: unsafe path %q", ErrInvalid, strings.Join(parts, "/"))
				}
			}
			l.Files = append(l.Files, Entry{Path: strings.Join(parts, "/"), Length: num(m["length"])})
		}
	case info["length"] != nil:
		l.Files = []Entry{{Length: num(info["length"])}}
	case info["file tree"] != nil:
		tree, _ := info["file tree"].(map[string]any)
		// A single-file v2 torrent has a tree with just its own name as a file.
		if leaf, ok := tree[name].(map[string]any); ok && len(tree) == 1 && leaf[""] != nil {
			l.Files = []Entry{{Length: num(leaf[""].(map[string]any)["length"])}}
			break
		}
		l.Multi = true
		if err := walkTree(tree, nil, &l.Files); err != nil {
			return Layout{}, err
		}
	default:
		return Layout{}, fmt.Errorf("%w: no file list", ErrInvalid)
	}
	if len(l.Files) == 0 {
		return Layout{}, fmt.Errorf("%w: no files", ErrInvalid)
	}
	return l, nil
}

func walkTree(node map[string]any, prefix []string, out *[]Entry) error {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		child, _ := node[k].(map[string]any)
		if k == "" {
			if len(prefix) == 0 {
				return fmt.Errorf("%w: file without a name", ErrInvalid)
			}
			*out = append(*out, Entry{Path: strings.Join(prefix, "/"), Length: num(child["length"])})
			continue
		}
		if !safeComponent(k) {
			return fmt.Errorf("%w: unsafe path %q", ErrInvalid, k)
		}
		if err := walkTree(child, append(append([]string{}, prefix...), k), out); err != nil {
			return err
		}
	}
	return nil
}

// safeComponent accepts a single path element that stays where it is put.
func safeComponent(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}

func str(v any) string {
	b, _ := v.([]byte)
	return string(b)
}

func num(v any) int64 {
	n, _ := v.(int64)
	return n
}

func list2strings(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, e := range l {
		out = append(out, str(e))
	}
	return out
}

// decode is a small bencode decoder for the info dictionary. Strings come
// back as []byte, integers as int64.
func decode(b []byte, i, depth int) (any, int, error) {
	if i >= len(b) || depth > maxDepth {
		return nil, 0, ErrInvalid
	}
	switch c := b[i]; {
	case c == 'i':
		end := bytes.IndexByte(b[i:], 'e')
		if end < 2 {
			return nil, 0, ErrInvalid
		}
		n, err := strconv.ParseInt(string(b[i+1:i+end]), 10, 64)
		if err != nil {
			return nil, 0, ErrInvalid
		}
		return n, i + end + 1, nil
	case c == 'l':
		var out []any
		i++
		for i < len(b) && b[i] != 'e' {
			v, next, err := decode(b, i, depth+1)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, v)
			i = next
		}
		if i >= len(b) {
			return nil, 0, ErrInvalid
		}
		return out, i + 1, nil
	case c == 'd':
		out := map[string]any{}
		i++
		for i < len(b) && b[i] != 'e' {
			k, next, err := readString(b, i)
			if err != nil {
				return nil, 0, err
			}
			v, after, err := decode(b, next, depth+1)
			if err != nil {
				return nil, 0, err
			}
			out[string(k)] = v
			i = after
		}
		if i >= len(b) {
			return nil, 0, ErrInvalid
		}
		return out, i + 1, nil
	case c >= '0' && c <= '9':
		s, next, err := readString(b, i)
		return s, next, err
	default:
		return nil, 0, ErrInvalid
	}
}

package torrentfile

import (
	"crypto/sha1"
	"encoding/hex"
	"testing"
)

const info = "d6:lengthi5e4:name5:a.txt12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaa7:privatei1ee"

func hashOf(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestParseAndHash(t *testing.T) {
	data := []byte("d8:announce20:http://t.example/ann7:comment2:hi4:info" + info + "e")
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Matches(hashOf(info)) {
		t.Errorf("info hash mismatch: %v, want %s", f.InfoHashes(), hashOf(info))
	}
	if f.Matches("0000000000000000000000000000000000000000") {
		t.Error("matched a foreign hash")
	}
	if !f.HasTrackers() {
		t.Error("announce not detected")
	}
}

func TestWithTrackersKeepsInfoHash(t *testing.T) {
	f, err := Parse([]byte("d4:info" + info + "e"))
	if err != nil {
		t.Fatal(err)
	}
	if f.HasTrackers() {
		t.Fatal("should have no trackers")
	}
	out := f.WithTrackers([]string{"http://a.example/x", "http://b.example/y"})
	want := "d8:announce18:http://a.example/x13:announce-listll18:http://a.example/xel18:http://b.example/yee4:info" + info + "e"
	if string(out) != want {
		t.Errorf("got  %s\nwant %s", out, want)
	}
	g, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Matches(hashOf(info)) || !g.HasTrackers() {
		t.Error("rewritten file lost its hash or trackers")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "x", "d", "de", "d4:infoi1ee", "d4:info" + info, "<html>", "d3:fooli1e", "d99999999999999:a"} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("Parse(%q) should fail", s)
		}
	}
}

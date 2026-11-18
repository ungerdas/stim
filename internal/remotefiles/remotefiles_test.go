package remotefiles

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ungerdas/stim/internal/sshtest"

	"golang.org/x/crypto/ssh"
)

func serve(t *testing.T, allowed ssh.PublicKey) (addr string, hostKey ssh.PublicKey) {
	t.Helper()
	host, port, key := sshtest.Serve(t, allowed)
	return net.JoinHostPort(host, strconv.Itoa(port)), key
}

func split(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	return host, p
}

func TestReadFileAndHostKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "abc.torrent"), []byte("d4:infodee"), 0o600); err != nil {
		t.Fatal(err)
	}
	addr, hostKey := serve(t, nil)
	host, port := split(t, addr)
	cfg := Config{Host: host, Port: port, User: "u", Password: "pw"}
	ctx := context.Background()

	data, seen, err := ReadFile(ctx, cfg, dir, "abc.torrent")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "d4:infodee" {
		t.Errorf("got %q", data)
	}
	if Fingerprint(seen) != ssh.FingerprintSHA256(hostKey) {
		t.Errorf("reported host key %q does not match the server's", seen)
	}

	// The stored key is accepted on the next connection …
	cfg.HostKey = seen
	if n, _, err := Probe(ctx, cfg, dir); err != nil || n != 1 {
		t.Errorf("Probe = %d, %v; want 1, nil", n, err)
	}
	// … and a different server on the same address is refused.
	addr2, _ := serve(t, nil)
	cfg.Host, cfg.Port = split(t, addr2)
	_, presented, err := ReadFile(ctx, cfg, dir, "abc.torrent")
	if !errors.Is(err, ErrHostKeyChanged) {
		t.Errorf("want ErrHostKeyChanged, got %v", err)
	}
	if presented == "" || presented == seen {
		t.Error("the new server's key should be reported so the user can accept it")
	}
}

func TestErrors(t *testing.T) {
	dir := t.TempDir()
	addr, _ := serve(t, nil)
	host, port := split(t, addr)
	ctx := context.Background()

	if _, _, err := ReadFile(ctx, Config{Host: host, Port: port, User: "u", Password: "pw"}, dir, "missing.torrent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	_, _, err := ReadFile(ctx, Config{Host: host, Port: port, User: "u", Password: "nope"}, dir, "x.torrent")
	if !errors.Is(err, ErrAuth) {
		t.Errorf("want a rejected-credentials error, got %v", err)
	}
	if _, _, err := ReadFile(ctx, Config{Host: host, Port: port, User: "u", Password: "pw"}, dir, "../etc/passwd"); err == nil {
		t.Error("path separators in the file name must be refused")
	}
	if _, _, err := ReadFile(ctx, Config{}, dir, "x.torrent"); err == nil {
		t.Error("unconfigured access must fail")
	}
}

func TestPrivateKeyAuth(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "k.torrent"), []byte("x"), 0o600)
	addr, _ := serve(t, sshPub)
	host, port := split(t, addr)
	data, _, err := ReadFile(context.Background(), Config{Host: host, Port: port, User: "u", PrivateKey: string(pem.EncodeToMemory(block))}, dir, "k.torrent")
	if err != nil || string(data) != "x" {
		t.Errorf("key auth: %q, %v", data, err)
	}
}

func TestRemoveAll(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Some.Release")
	if err := os.MkdirAll(filepath.Join(target, "Subs"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(target, "a.mkv"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(target, "Subs", "a.srt"), []byte("x"), 0o600)
	keep := filepath.Join(dir, "Other.Release.mkv")
	_ = os.WriteFile(keep, []byte("x"), 0o600)

	addr, _ := serve(t, nil)
	host, port := split(t, addr)
	cfg := Config{Host: host, Port: port, User: "u", Password: "pw"}
	ctx := context.Background()

	if err := RemoveAll(ctx, cfg, target); err == nil {
		t.Fatal("deleting without a stored host key must be refused")
	}
	_, seen, err := Probe(ctx, cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HostKey = seen
	for _, bad := range []string{"relative/path", "/", "/tmp", target + "/", target + "/../x"} {
		if err := RemoveAll(ctx, cfg, bad); err == nil {
			t.Errorf("RemoveAll(%q) should be refused", bad)
		}
	}
	if err := RemoveAll(ctx, cfg, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("the folder should be gone")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("a neighbouring file must stay")
	}
	if err := RemoveAll(ctx, cfg, target); err != nil {
		t.Errorf("deleting something already gone should succeed, got %v", err)
	}
}

func TestProbeDirs(t *testing.T) {
	home := t.TempDir()
	mk := func(rel string, files ...string) string {
		d := filepath.Join(home, rel)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			_ = os.WriteFile(filepath.Join(d, f), []byte("x"), 0o600)
		}
		return d
	}
	first := mk(".config/deluge/state", "aaa.torrent", "bbb.torrent", "torrents.state")
	second := mk(".config/deluge2/state", "ccc.torrent", "ddd.torrent", "eee.torrent")
	mk(".config/deluge-old") // no state folder: not a candidate
	mk(".config/qBittorrent/state", "ccc.torrent")

	addr, _ := serve(t, nil)
	host, port := split(t, addr)
	cfg := Config{Host: host, Port: port, User: "u", Password: "pw"}
	ctx := context.Background()
	glob := filepath.Join(home, ".config/deluge*/state")
	names := []string{"ccc.torrent", "ddd.torrent", "fff.torrent"}

	// The folder given is another daemon's: none of the names are there,
	// and the other candidate is found.
	given, found, _, err := ProbeDirs(ctx, cfg, first, glob, names)
	if err != nil {
		t.Fatal(err)
	}
	if given.Files != 2 || given.Matched != 0 {
		t.Errorf("given = %+v, want 2 files, 0 matched", *given)
	}
	if len(found) != 1 || found[0].Dir != second || found[0].Files != 3 || found[0].Matched != 2 {
		t.Errorf("found = %+v, want only %s with 3 files, 2 matched", found, second)
	}

	// No folder given: every candidate is reported.
	given, found, _, err = ProbeDirs(ctx, cfg, "", glob, names)
	if err != nil || given != nil || len(found) != 2 {
		t.Fatalf("given %v, found %+v, err %v; want both candidates", given, found, err)
	}

	// A folder that isn't there is a DirError, not a login failure.
	// The candidates are still reported, to suggest instead.
	_, found, _, err = ProbeDirs(ctx, cfg, filepath.Join(home, "nope"), glob, names)
	var dirErr *DirError
	if !errors.As(err, &dirErr) {
		t.Errorf("err = %v, want a *DirError", err)
	}
	if len(found) != 2 {
		t.Errorf("found %+v beside a missing folder, want both candidates", found)
	}

	// Logging in failing is not a DirError.
	_, _, _, err = ProbeDirs(ctx, Config{Host: host, Port: port, User: "u", Password: "nope"}, first, glob, names)
	if err == nil || errors.As(err, &dirErr) {
		t.Errorf("err = %v, want a login error", err)
	}
}

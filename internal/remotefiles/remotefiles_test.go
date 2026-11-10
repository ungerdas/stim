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
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// serve starts a throwaway SSH server with SFTP rooted at the real file
// system. It accepts password "pw" and the given public key.
func serve(t *testing.T, allowed ssh.PublicKey) (addr string, hostKey ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if string(pw) == "pw" {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if allowed != nil && string(key.Marshal()) == string(allowed.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() != "session" {
						_ = ch.Reject(ssh.UnknownChannelType, "")
						continue
					}
					channel, requests, _ := ch.Accept()
					go func() {
						for req := range requests {
							_ = req.Reply(req.Type == "subsystem" && string(req.Payload[4:]) == "sftp", nil)
						}
					}()
					go func() {
						server, err := sftp.NewServer(channel)
						if err == nil {
							_ = server.Serve()
						}
						channel.Close()
					}()
				}
			}()
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
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

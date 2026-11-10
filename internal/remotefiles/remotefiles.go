// Package remotefiles reads files from a server over SFTP. Stim uses it to
// fetch .torrent files from a client's state folder when the client's API
// cannot hand them out.
package remotefiles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Config is how to reach one server.
type Config struct {
	Host       string
	Port       int // 0 means 22
	User       string
	Password   string
	PrivateKey string // PEM, without passphrase
	// HostKey is the server's public key in authorized_keys format, as
	// stored after the first connection. Empty means "not seen yet": the
	// key presented is accepted and reported back so it can be stored.
	HostKey string
}

// Configured reports whether enough is filled in to try a connection.
func (c Config) Configured() bool {
	return c.Host != "" && c.User != "" && (c.Password != "" || c.PrivateKey != "")
}

const (
	dialTimeout = 15 * time.Second
	// maxFileSize guards against reading something that is clearly not a
	// .torrent file.
	maxFileSize = 64 << 20
)

// ErrHostKeyChanged means the server presented a different key than the one
// stored. That is either a reinstalled server or someone in the middle.
var ErrHostKeyChanged = errors.New("the server's SSH host key has changed")

// ErrAuth means the server rejected the user name, password or key.
var ErrAuth = errors.New("the server rejected the SSH user name, password or key")

// ErrNotFound means the file does not exist on the server.
var ErrNotFound = errors.New("file not found on the server")

// Fingerprint renders an authorized_keys line as a SHA256 fingerprint.
func Fingerprint(hostKey string) string {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(hostKey))
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(key)
}

type session struct {
	ssh     *ssh.Client
	sftp    *sftp.Client
	hostKey string
}

func (s *session) Close() {
	s.sftp.Close()
	s.ssh.Close()
}

// open connects. The returned host key is the one the server presented,
// also when the connection was refused because it changed.
func open(ctx context.Context, cfg Config) (*session, string, error) {
	if !cfg.Configured() {
		return nil, "", errors.New("SSH access is not configured for this client")
	}
	var auth []ssh.AuthMethod
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				return nil, "", errors.New("the SSH key is protected by a passphrase; use a key without one")
			}
			return nil, "", fmt.Errorf("the SSH key could not be read: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		auth = append(auth, ssh.Password(cfg.Password))
		// Many servers offer passwords only as keyboard-interactive.
		auth = append(auth, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = cfg.Password
			}
			return answers, nil
		}))
	}

	var seen string
	clientCfg := &ssh.ClientConfig{
		User:    cfg.User,
		Auth:    auth,
		Timeout: dialTimeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
			if cfg.HostKey == "" {
				return nil // first contact: trust and report back
			}
			known, _, _, _, err := ssh.ParseAuthorizedKey([]byte(cfg.HostKey))
			if err != nil || !bytes.Equal(known.Marshal(), key.Marshal()) {
				return ErrHostKeyChanged
			}
			return nil
		},
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	conn, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("could not reach %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, clientCfg)
	if err != nil {
		conn.Close()
		if errors.Is(err, ErrHostKeyChanged) {
			return nil, seen, fmt.Errorf("%w: it now presents %s", ErrHostKeyChanged, Fingerprint(seen))
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, seen, ErrAuth
		}
		return nil, seen, fmt.Errorf("SSH connection failed: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	sshClient := ssh.NewClient(sshConn, chans, reqs)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, seen, fmt.Errorf("the server did not start SFTP: %w", err)
	}
	return &session{ssh: sshClient, sftp: sftpClient, hostKey: seen}, seen, nil
}

// clean turns a user-typed folder into an SFTP path. "~/x" and "x" are both
// relative to the login folder, which is where an SFTP session starts.
func clean(dir string) string {
	dir = strings.TrimSpace(dir)
	dir = strings.TrimPrefix(dir, "~/")
	if dir == "~" || dir == "" {
		return "."
	}
	return path.Clean(dir)
}

// ReadFile returns the contents of dir/name. It also returns the server's
// host key so the caller can store it after the first connection.
func ReadFile(ctx context.Context, cfg Config, dir, name string) (data []byte, hostKey string, err error) {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return nil, "", errors.New("invalid file name")
	}
	s, seen, err := open(ctx, cfg)
	if err != nil {
		return nil, seen, err
	}
	defer s.Close()

	full := path.Join(clean(dir), name)
	f, err := s.sftp.Open(full)
	if err != nil {
		if errors.Is(err, sftp.ErrSSHFxNoSuchFile) || strings.Contains(err.Error(), "not exist") {
			return nil, s.hostKey, fmt.Errorf("%w: %s", ErrNotFound, full)
		}
		return nil, s.hostKey, fmt.Errorf("could not open %s: %w", full, err)
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, s.hostKey, fmt.Errorf("could not read %s: %w", full, err)
	}
	if len(data) > maxFileSize {
		return nil, s.hostKey, fmt.Errorf("%s is too large to be a .torrent file", full)
	}
	return data, s.hostKey, nil
}

// Probe checks that dir can be listed and counts the .torrent files in it.
func Probe(ctx context.Context, cfg Config, dir string) (torrents int, hostKey string, err error) {
	s, seen, err := open(ctx, cfg)
	if err != nil {
		return 0, seen, err
	}
	defer s.Close()
	entries, err := s.sftp.ReadDir(clean(dir))
	if err != nil {
		return 0, s.hostKey, fmt.Errorf("could not list %s: %w", clean(dir), err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".torrent") {
			torrents++
		}
	}
	return torrents, s.hostKey, nil
}

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
	"os"
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
	// Many requests in flight per file keep throughput up over high-latency
	// links, which matters when copying between servers.
	sftpClient, err := sftp.NewClient(sshClient,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	)
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

// RemoveAll deletes a file or folder and everything in it. Because it
// destroys data it never trusts a server on first contact: the host key
// must already be stored. It also refuses paths that are relative, not
// clean, or too close to the root to be a single download.
func RemoveAll(ctx context.Context, cfg Config, p string) error {
	if cfg.HostKey == "" {
		return errors.New("the server's SSH key is not stored yet; test the SSH connection under the client's settings and save")
	}
	if !path.IsAbs(p) || path.Clean(p) != p || strings.Count(p, "/") < 2 {
		return fmt.Errorf("refusing to delete %q", p)
	}
	s, _, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()
	if _, err := s.sftp.Lstat(p); err != nil {
		if errors.Is(err, sftp.ErrSSHFxNoSuchFile) || strings.Contains(err.Error(), "not exist") {
			return nil // already gone
		}
		return fmt.Errorf("could not look at %s: %w", p, err)
	}
	if err := s.sftp.RemoveAll(p); err != nil {
		return fmt.Errorf("could not delete %s: %w", p, err)
	}
	return nil
}

// Session is an open SFTP connection for working with several files, as
// when copying a torrent's files between servers. Close it when done.
type Session struct {
	s       *session
	HostKey string // the key the server presented
}

// Open connects. Like ReadFile, it accepts an unknown server on first
// contact and reports its key; requireKnownHost refuses that, for sessions
// that will write or delete.
func Open(ctx context.Context, cfg Config, requireKnownHost bool) (*Session, error) {
	if requireKnownHost && cfg.HostKey == "" {
		return nil, errors.New("the server's SSH key is not stored yet; test the SSH connection under the client's settings and save")
	}
	s, seen, err := open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Session{s: s, HostKey: seen}, nil
}

func (s *Session) Close() { s.s.Close() }

// IsNotExist reports whether err means a file is missing.
func IsNotExist(err error) bool {
	return err != nil && (errors.Is(err, sftp.ErrSSHFxNoSuchFile) || errors.Is(err, os.ErrNotExist) ||
		strings.Contains(err.Error(), "not exist"))
}

// Size returns a file's size, or an error matching IsNotExist.
func (s *Session) Size(p string) (int64, error) {
	fi, err := s.s.sftp.Stat(p)
	if err != nil {
		return 0, err
	}
	if !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", p)
	}
	return fi.Size(), nil
}

// OpenRead opens a file for reading.
func (s *Session) OpenRead(p string) (io.ReadCloser, error) { return s.s.sftp.Open(p) }

// Create creates or truncates a file for writing.
func (s *Session) Create(p string) (io.WriteCloser, error) { return s.s.sftp.Create(p) }

// MkdirAll creates a folder and its parents. It returns the folders it
// created, deepest last, so they can be removed again.
func (s *Session) MkdirAll(p string) ([]string, error) {
	var missing []string
	for d := p; d != "/" && d != "."; d = path.Dir(d) {
		fi, err := s.s.sftp.Stat(d)
		if err == nil {
			if !fi.IsDir() {
				return nil, fmt.Errorf("%s exists and is not a folder", d)
			}
			break
		}
		if !IsNotExist(err) {
			return nil, err
		}
		missing = append(missing, d)
	}
	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		if err := s.s.sftp.Mkdir(missing[i]); err != nil {
			return created, fmt.Errorf("could not create %s: %w", missing[i], err)
		}
		created = append(created, missing[i])
	}
	return created, nil
}

// Rename replaces newPath with oldPath. It prefers the POSIX rename
// extension, which overwrites atomically.
func (s *Session) Rename(oldPath, newPath string) error {
	if err := s.s.sftp.PosixRename(oldPath, newPath); err == nil {
		return nil
	}
	return s.s.sftp.Rename(oldPath, newPath)
}

// Remove deletes one file.
func (s *Session) Remove(p string) error { return s.s.sftp.Remove(p) }

// RemoveDir deletes an empty folder.
func (s *Session) RemoveDir(p string) error { return s.s.sftp.RemoveDirectory(p) }

// Free returns the space available to the user at p, or -1 if the server
// does not say (it needs the statvfs extension, which OpenSSH has).
func (s *Session) Free(p string) int64 {
	for d := p; ; d = path.Dir(d) {
		if st, err := s.s.sftp.StatVFS(d); err == nil {
			return int64(st.FreeSpace())
		} else if !IsNotExist(err) {
			return -1
		}
		if d == "/" || d == "." {
			return -1
		}
	}
}

// Home is the folder the SSH user starts in.
func (s *Session) Home() (string, error) { return s.s.sftp.Getwd() }

// ReadDir lists a folder without following symbolic links.
func (s *Session) ReadDir(p string) ([]os.FileInfo, error) { return s.s.sftp.ReadDir(p) }

// DiskUsage adds up the sizes of the files under p, without following
// symbolic links, and reports the newest modification time seen. It stops
// after limit entries; complete is then false.
func (s *Session) DiskUsage(p string, limit int) (size int64, newest time.Time, complete bool, err error) {
	w := s.s.sftp.Walk(p)
	n := 0
	for w.Step() {
		if err := w.Err(); err != nil {
			if w.Path() == p {
				return 0, time.Time{}, false, err
			}
			continue // an unreadable corner; count what can be read
		}
		n++
		if n > limit {
			return size, newest, false, nil
		}
		fi := w.Stat()
		if fi.Mode().IsRegular() {
			size += fi.Size()
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return size, newest, true, nil
}

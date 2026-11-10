// Package store persists the configured client instances as a JSON file.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/remotefiles"
)

// Instance is one configured torrent client. Passwords are stored as-is in
// the config file (mode 0600); the API never returns them.
type Instance struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Kind          client.Kind `json:"kind"`
	URL           string      `json:"url"`
	Username      string      `json:"username,omitempty"`
	Password      string      `json:"password,omitempty"`
	BasicUser     string      `json:"basicUser,omitempty"`
	BasicPass     string      `json:"basicPass,omitempty"`
	TLSSkipVerify bool        `json:"tlsSkipVerify,omitempty"`

	// SSH access to the server the client runs on. Only used to read
	// .torrent files from TorrentDir when moving torrents away from a client
	// whose API cannot export them.
	SSHHost     string `json:"sshHost,omitempty"`
	SSHPort     int    `json:"sshPort,omitempty"`
	SSHUser     string `json:"sshUser,omitempty"`
	SSHPassword string `json:"sshPassword,omitempty"`
	SSHKey      string `json:"sshKey,omitempty"`
	SSHHostKey  string `json:"sshHostKey,omitempty"` // stored on first contact
	TorrentDir  string `json:"torrentDir,omitempty"`
}

// SSHConfig converts the instance's SSH settings for the remotefiles package.
func (i Instance) SSHConfig() remotefiles.Config {
	return remotefiles.Config{
		Host:       i.SSHHost,
		Port:       i.SSHPort,
		User:       i.SSHUser,
		Password:   i.SSHPassword,
		PrivateKey: i.SSHKey,
		HostKey:    i.SSHHostKey,
	}
}

// ClientConfig converts the instance to what an adapter needs.
func (i Instance) ClientConfig() client.Config {
	return client.Config{
		URL:           i.URL,
		Username:      i.Username,
		Password:      i.Password,
		BasicUser:     i.BasicUser,
		BasicPass:     i.BasicPass,
		TLSSkipVerify: i.TLSSkipVerify,
	}
}

// Validate checks the fields a user can get wrong.
func (i Instance) Validate() error {
	if strings.TrimSpace(i.Name) == "" {
		return errors.New("name is required")
	}
	if !client.Known(i.Kind) {
		return fmt.Errorf("unknown client type %q", i.Kind)
	}
	if _, err := client.BaseURL(i.URL); err != nil {
		return err
	}
	if i.SSHPort < 0 || i.SSHPort > 65535 {
		return errors.New("SSH port must be between 1 and 65535")
	}
	return nil
}

// SetHostKey stores the SSH host key seen on first contact. It does nothing
// when a key is already stored, so a changed key can never overwrite it.
func (s *Store) SetHostKey(id, hostKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx, old := range s.data.Instances {
		if old.ID == id {
			if old.SSHHostKey != "" || hostKey == "" {
				return nil
			}
			s.data.Instances[idx].SSHHostKey = hostKey
			if err := s.save(); err != nil {
				s.data.Instances[idx] = old
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

type file struct {
	Instances []Instance `json:"instances"`
}

// Store is a small file-backed list of instances, safe for concurrent use.
type Store struct {
	path string
	mu   sync.Mutex
	data file
}

// ErrNotFound is returned when an instance ID does not exist.
var ErrNotFound = errors.New("instance not found")

// Open loads (or creates) instances.json inside dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "instances.json")}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", s.path, err)
	}
	return s, nil
}

// save writes the file atomically. Callers hold s.mu.
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// List returns a copy of all instances in the order they were added.
func (s *Store) List() []Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Instance(nil), s.data.Instances...)
}

// Get returns one instance.
func (s *Store) Get(id string) (Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, in := range s.data.Instances {
		if in.ID == id {
			return in, nil
		}
	}
	return Instance{}, ErrNotFound
}

// Create assigns an ID and stores the instance.
func (s *Store) Create(in Instance) (Instance, error) {
	if err := in.Validate(); err != nil {
		return Instance{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = newID()
	s.data.Instances = append(s.data.Instances, in)
	if err := s.save(); err != nil {
		s.data.Instances = s.data.Instances[:len(s.data.Instances)-1]
		return Instance{}, err
	}
	return in, nil
}

// Update replaces the instance with the same ID.
func (s *Store) Update(in Instance) error {
	if err := in.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx, old := range s.data.Instances {
		if old.ID == in.ID {
			s.data.Instances[idx] = in
			if err := s.save(); err != nil {
				s.data.Instances[idx] = old
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

// Delete removes an instance.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx, old := range s.data.Instances {
		if old.ID == id {
			prev := append([]Instance(nil), s.data.Instances...)
			s.data.Instances = append(s.data.Instances[:idx], s.data.Instances[idx+1:]...)
			if err := s.save(); err != nil {
				s.data.Instances = prev
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

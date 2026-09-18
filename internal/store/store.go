// Package store implements local, file-based persistence for the app's
// configuration and last-known-good snapshot. It deliberately avoids any
// external database: a single JSON file, written with 0600 permissions by
// the application itself.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nicolasb114/pd-status-wall/internal/model"
)

// Store guards the on-disk config file and keeps an in-memory copy for fast
// reads from the poller and HTTP handlers.
type Store struct {
	mu       sync.RWMutex
	path     string
	uploads  string
	cfg      model.Config
	snapshot model.Snapshot
}

// Open loads the config file at dataDir/config.json, creating it with
// defaults (and restrictive permissions) if it does not exist yet.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	uploadsDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create uploads dir: %w", err)
	}

	s := &Store{
		path:    filepath.Join(dataDir, "config.json"),
		uploads: uploadsDir,
	}

	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.cfg = model.DefaultConfig()
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		return s, nil
	} else if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg model.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	s.cfg = cfg
	return s, nil
}

// UploadsDir returns the local folder used for logo/banner uploads.
func (s *Store) UploadsDir() string { return s.uploads }

// Get returns a copy of the current configuration.
func (s *Store) Get() model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update applies fn to a copy of the current config and persists the result
// atomically. fn should mutate the passed-in config in place.
func (s *Store) Update(fn func(cfg *model.Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg
	fn(&next)
	s.cfg = next
	return s.persistLocked()
}

// persistLocked writes s.cfg to disk. Caller must hold s.mu.
func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmp := s.path + ".tmp"
	// O_CREATE with explicit 0600 mode: the file is created with
	// restrictive permissions by the application itself, before any data
	// (including the API key and password hash) is ever written to it.
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temp config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// Snapshot returns the last-known-good poller snapshot.
func (s *Store) Snapshot() model.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

// SetSnapshot replaces the in-memory snapshot. Snapshots are not persisted
// to disk on every poll (that would mean a disk write every 30-60s for no
// real benefit) - on restart the display simply shows "unconfigured" until
// the first poll succeeds, which happens within one poll interval.
func (s *Store) SetSnapshot(snap model.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = snap
}

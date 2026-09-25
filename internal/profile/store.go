package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const storeVersion = 1

type storeFile struct {
	Version  int        `json:"version"`
	Profiles []*Profile `json:"profiles"`
}

// Store persists profiles as JSON in <root>/profiles.json.
type Store struct {
	mu       sync.Mutex
	root     string
	profiles []*Profile
}

// ErrNotFound is returned when no profile matches.
var ErrNotFound = errors.New("profile not found")

// Open loads (or initialises) the store under root.
func Open(root string) (*Store, error) {
	s := &Store{root: root}
	data, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	for _, p := range f.Profiles {
		p.root = root
	}
	s.profiles = f.Profiles
	s.sortLocked()
	return s, nil
}

// Root is the manager data directory.
func (s *Store) Root() string { return s.root }

func (s *Store) path() string { return filepath.Join(s.root, "profiles.json") }

// List returns a snapshot of all profiles, sorted by name.
func (s *Store) List() []*Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Profile, len(s.profiles))
	copy(out, s.profiles)
	return out
}

// Get finds a profile by ID, exact name or slug (case-insensitive).
func (s *Store) Get(key string) (*Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.ID == key {
			return p, nil
		}
	}
	for _, p := range s.profiles {
		if strings.EqualFold(p.Name, key) || p.Slug() == Slugify(key) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrNotFound, key)
}

// New returns an unsaved profile bound to this store.
func (s *Store) New(name string) *Profile {
	s.mu.Lock()
	n := len(s.profiles)
	s.mu.Unlock()
	return &Profile{
		ID:        NewID(),
		Name:      name,
		Color:     Palette[n%len(Palette)],
		CreatedAt: time.Now(),
		root:      s.root,
	}
}

// Save inserts or updates p and writes the store to disk.
func (s *Store) Save(p *Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.profiles {
		if o.ID != p.ID && strings.EqualFold(o.Name, p.Name) {
			return fmt.Errorf("a profile named %q already exists", p.Name)
		}
	}
	p.root = s.root
	replaced := false
	for i, o := range s.profiles {
		if o.ID == p.ID {
			s.profiles[i] = p
			replaced = true
		}
	}
	if !replaced {
		s.profiles = append(s.profiles, p)
	}
	s.sortLocked()
	return s.writeLocked()
}

// Touch records that p was just launched.
func (s *Store) Touch(p *Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.LastUsedAt = time.Now()
	return s.writeLocked()
}

// Delete removes the profile. When purge is true the app-managed data
// directory (logins, history, desktop data) is deleted too. Custom
// directories chosen by the user are never deleted.
func (s *Store) Delete(id string, purge bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, p := range s.profiles {
		if p.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	p := s.profiles[idx]
	s.profiles = append(s.profiles[:idx], s.profiles[idx+1:]...)
	if err := s.writeLocked(); err != nil {
		return err
	}
	if purge && p.ID != "" {
		dir := p.Dir()
		// Guard against ever deleting something outside our root.
		if rel, err := filepath.Rel(filepath.Join(s.root, "profiles"), dir); err == nil &&
			rel != "." && !strings.HasPrefix(rel, "..") {
			return os.RemoveAll(dir)
		}
	}
	return nil
}

func (s *Store) sortLocked() {
	sort.SliceStable(s.profiles, func(i, j int) bool {
		return strings.ToLower(s.profiles[i].Name) < strings.ToLower(s.profiles[j].Name)
	})
}

func (s *Store) writeLocked() error {
	data, err := json.MarshalIndent(storeFile{Version: storeVersion, Profiles: s.profiles}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

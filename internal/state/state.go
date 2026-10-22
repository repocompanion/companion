// Package state stores the local changeset -> branch -> PR mapping.
//
// state.json is deliberately a cache. It can always be rebuilt by inspecting
// worktrees and the remote; it is never the source of truth for document
// content.
package state

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const Version = 1

type Change struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Area      string    `json:"area"`
	Branch    string    `json:"branch"`
	Path      string    `json:"path"`
	PR        int       `json:"pr,omitempty"`
	PRState   string    `json:"pr_state,omitempty"` // draft | ready | merged | closed | none
	Frozen    bool      `json:"frozen,omitempty"`
	NeedsSync bool      `json:"needs_sync,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsOpen reports whether the changeset can still receive commits.
func (c Change) IsOpen() bool {
	return c.PRState != "merged" && c.PRState != "closed"
}

// IsReady reports whether the PR is ready for review (and therefore frozen).
func (c Change) IsReady() bool { return c.Frozen || c.PRState == "ready" }

type File struct {
	Version int               `json:"version"`
	Active  map[string]string `json:"active,omitempty"` // area -> changeset ID
	Changes []Change          `json:"changes"`
}

func New() *File {
	return &File{Version: Version, Active: map[string]string{}}
}

type Store struct {
	Path string
	lock *flock.Flock
}

func NewStore(path string) *Store {
	return &Store{Path: path, lock: flock.New(path + ".lock")}
}

// With loads state under a cross-process lock and saves it after fn.
func (s *Store) With(fn func(*File) error) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, err := s.lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("timed out waiting for state lock")
	}
	defer s.lock.Unlock()
	f, err := s.Load()
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		return err
	}
	f.Version = Version
	return s.Save(f)
}

// Load reads state. A missing or corrupt file yields an empty state.
func (s *Store) Load() (*File, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		// A corrupt cache must never block work. Keep a copy for diagnosis.
		_ = os.WriteFile(s.Path+".corrupt", b, 0o600)
		return New(), nil
	}
	if f.Active == nil {
		f.Active = map[string]string{}
	}
	if f.Version == 0 {
		f.Version = Version
	}
	return &f, nil
}

func (s *Store) Save(f *File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".state-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.Path)
}

// Find returns a changeset by ID, or by area when the argument is an area.
func (f *File) Find(idOrArea string) (*Change, error) {
	if idOrArea == "" {
		return nil, errors.New("changeset is required")
	}
	if c := f.ByID(idOrArea); c != nil {
		return c, nil
	}
	if id := f.Active[idOrArea]; id != "" {
		if c := f.ByID(id); c != nil {
			return c, nil
		}
	}
	var matches []*Change
	for i := range f.Changes {
		if strings.EqualFold(f.Changes[i].Area, idOrArea) {
			matches = append(matches, &f.Changes[i])
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple changesets match area %q; use an ID", idOrArea)
	}
	return nil, fmt.Errorf("changeset %q not found", idOrArea)
}

func (f *File) ByID(id string) *Change {
	for i := range f.Changes {
		if f.Changes[i].ID == id {
			return &f.Changes[i]
		}
	}
	return nil
}

func (f *File) ActiveFor(area string) *Change {
	id := f.Active[area]
	if id == "" {
		return nil
	}
	return f.ByID(id)
}

func (f *File) SetActive(c *Change) {
	if f.Active == nil {
		f.Active = map[string]string{}
	}
	f.Active[c.Area] = c.ID
}

func (f *File) Add(c Change) {
	if f.Active == nil {
		f.Active = map[string]string{}
	}
	f.Changes = append(f.Changes, c)
}

func (f *File) Remove(id string) {
	out := f.Changes[:0]
	for _, c := range f.Changes {
		if c.ID != id {
			out = append(out, c)
		}
	}
	f.Changes = out
	for area, active := range f.Active {
		if active == id {
			delete(f.Active, area)
		}
	}
}

// Sort newest first, primarily for status output.
func (f *File) Sort() {
	sort.SliceStable(f.Changes, func(i, j int) bool {
		return f.Changes[i].UpdatedAt.After(f.Changes[j].UpdatedAt)
	})
}

// NewID returns a short, sortable-ish, branch-safe local ID.
func NewID() string {
	var b [7]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("cs-%d", time.Now().UnixNano())
	}
	return "cs-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

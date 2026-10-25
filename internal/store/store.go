// Package store owns the local clone of the central docs-store repository and
// the read-only mirror of main.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/repocompanion/companion/internal/gitx"
)

const DefaultBranch = "main"

// ErrEmptyStore means the remote exists but has no commits. `companion init`
// uses this to seed the initial main branch instead of failing with a raw git
// revision error.
var ErrEmptyStore = errors.New("docs store has no commits")

type Store struct {
	Home      string
	Bare      string
	Mirror    string
	Changes   string
	StatePath string
	Remote    string
	Branch    string
}

func New(home, remote string) *Store {
	if remote == "" {
		remote = os.Getenv("COMPANION_REMOTE")
	}
	if remote == "" {
		remote = "git@github.com:your-org/docs-store.git"
	}
	return &Store{
		Home:      home,
		Bare:      filepath.Join(home, "repo.git"),
		Mirror:    filepath.Join(home, "main"),
		Changes:   filepath.Join(home, "changes"),
		StatePath: filepath.Join(home, "state.json"),
		Remote:    remote,
		Branch:    DefaultBranch,
	}
}

func (s *Store) Exists() bool {
	_, err := os.Stat(filepath.Join(s.Mirror, ".git"))
	if err == nil {
		return true
	}
	_, err = os.Stat(s.Bare)
	return err == nil
}

func (s *Store) git(ctx context.Context, args ...string) (string, error) {
	all := append([]string{"--git-dir", s.Bare}, args...)
	return gitx.Run(ctx, "", all...)
}

// Init creates the bare clone, the main mirror and the changes directory. It
// is safe to run more than once.
func (s *Store) Init(ctx context.Context) error {
	if err := os.MkdirAll(s.Home, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Changes, 0o700); err != nil {
		return err
	}
	// MkdirAll does not tighten a directory that already exists. Older installs
	// (or a pre-created COMPANION_HOME) could leave doc checkouts readable by
	// other local users.
	if err := os.Chmod(s.Home, 0o700); err != nil {
		return fmt.Errorf("secure companion home: %w", err)
	}
	if err := os.Chmod(s.Changes, 0o700); err != nil {
		return fmt.Errorf("secure changes directory: %w", err)
	}
	if s.Remote == "" {
		return errors.New("store remote is not configured; run companion init --remote <url>")
	}
	if _, err := os.Stat(s.Bare); errors.Is(err, os.ErrNotExist) {
		if _, err := gitx.Run(ctx, s.Home, "clone", "--bare", s.Remote, s.Bare); err != nil {
			return fmt.Errorf("clone store: %w", err)
		}
	} else if err != nil {
		return err
	} else {
		// A re-init may point at a new remote. Update origin before fetching.
		_, _ = s.git(ctx, "remote", "set-url", "origin", s.Remote)
	}

	// Bare clones do not get a fetch refspec by default. Without this,
	// `git fetch origin` only writes FETCH_HEAD and origin/main never exists.
	_, _ = s.git(ctx, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")

	// Fetch even for an existing clone; init is the natural setup/repair path.
	_, _ = s.git(ctx, "fetch", "--prune", "origin")

	if _, err := s.git(ctx, "rev-parse", "--verify", "refs/heads/"+s.Branch); err != nil {
		// Distinguish a genuinely empty remote from one that uses a different
		// default branch. An empty repository has no refs at all.
		if refs, refErr := s.git(ctx, "for-each-ref", "--count=1", "--format=%(refname)", "refs/heads", "refs/remotes"); refErr == nil && strings.TrimSpace(refs) == "" {
			return ErrEmptyStore
		}
		// Some bare clones only have remote-tracking refs after an odd fetch.
		if _, err2 := s.git(ctx, "branch", s.Branch, "origin/"+s.Branch); err2 != nil {
			return fmt.Errorf("store has no %q branch: %w", s.Branch, err)
		}
	}

	if _, err := os.Stat(s.Mirror); errors.Is(err, os.ErrNotExist) {
		if _, err := s.git(ctx, "worktree", "add", s.Mirror, s.Branch); err != nil {
			return fmt.Errorf("create main mirror: %w", err)
		}
	}
	return s.MakeReadOnly()
}

// Bootstrap creates the first commit on an empty docs store. The commit only
// contains the org registry; project docs are written later through changesets.
// It is intentionally explicit: callers use ErrEmptyStore to decide when a
// remote is safe to seed.
func (s *Store) Bootstrap(ctx context.Context, name, email string) error {
	if strings.TrimSpace(s.Remote) == "" {
		return errors.New("store remote is not configured")
	}
	if strings.TrimSpace(name) == "" {
		name = "companion"
	}
	if strings.TrimSpace(email) == "" {
		email = "companion@localhost"
	}
	dir, err := os.MkdirTemp("", "companion-bootstrap-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	if _, err := gitx.Run(ctx, dir, "init", "-q", "-b", s.Branch); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".companion"), 0o700); err != nil {
		return err
	}
	registry := "# Org registry maintained through docs pull requests.\nteams: {}\nrepos: {}\n"
	if err := os.WriteFile(filepath.Join(dir, ".companion", "registry.yaml"), []byte(registry), 0o644); err != nil {
		return err
	}
	if _, err := gitx.Run(ctx, dir, "add", "."); err != nil {
		return err
	}
	if _, err := gitx.Run(ctx, dir, "-c", "user.name="+name, "-c", "user.email="+email,
		"commit", "-qm", "chore: initialize docs store"); err != nil {
		return err
	}
	if _, err := gitx.Run(ctx, dir, "remote", "add", "origin", s.Remote); err != nil {
		return err
	}
	if _, err := gitx.Run(ctx, dir, "push", "-u", "origin", s.Branch); err != nil {
		return fmt.Errorf("push initial commit: %w", err)
	}
	return nil
}

// Pull fetches origin and fast-forwards the mirror.
func (s *Store) Pull(ctx context.Context) (rev string, commitTime time.Time, err error) {
	_, _ = s.git(ctx, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	if _, err := s.git(ctx, "fetch", "--prune", "origin", "+refs/heads/"+s.Branch+":refs/remotes/origin/"+s.Branch); err != nil {
		return "", time.Time{}, fmt.Errorf("fetch store: %w", err)
	}
	if err := s.MakeWritable(); err != nil {
		return "", time.Time{}, err
	}
	defer func() {
		if rerr := s.MakeReadOnly(); rerr != nil && err == nil {
			err = fmt.Errorf("restore read-only mirror: %w", rerr)
		}
	}()

	if _, err := gitx.Run(ctx, s.Mirror, "merge", "--ff-only", "origin/"+s.Branch); err != nil {
		return "", time.Time{}, fmt.Errorf("fast-forward mirror (local mirror edits may need `companion change adopt`): %w", err)
	}
	rev, commitTime, err = s.MirrorRev(ctx)
	return rev, commitTime, err
}

func (s *Store) MirrorRev(ctx context.Context) (string, time.Time, error) {
	rev, err := gitx.Run(ctx, s.Mirror, "rev-parse", "HEAD")
	if err != nil {
		return "", time.Time{}, err
	}
	ts, err := gitx.Run(ctx, s.Mirror, "show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return rev, time.Time{}, nil
	}
	t, _ := time.Parse(time.RFC3339, ts)
	return rev, t, nil
}

func (s *Store) MirrorAge(ctx context.Context) time.Duration {
	_, t, err := s.MirrorRev(ctx)
	if err != nil || t.IsZero() {
		return 0
	}
	return time.Since(t)
}

// CleanRel validates a store-relative path.
func CleanRel(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path is required")
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == "" {
		return "", errors.New("path is empty")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes the store: %s", rel)
	}
	if strings.HasPrefix(clean, ".git/") || clean == ".git" {
		return "", errors.New("the .git directory is not a document area")
	}
	return clean, nil
}

func (s *Store) Abs(rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.Mirror, filepath.FromSlash(clean))
	// Resolve the deepest existing ancestor to prevent symlink escapes.
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		root, rerr := filepath.EvalSymlinks(s.Mirror)
		if rerr != nil {
			root = s.Mirror
		}
		if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			return "", fmt.Errorf("path escapes the store through a symlink: %s", rel)
		}
	}
	parent := filepath.Dir(full)
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			root, err := filepath.EvalSymlinks(s.Mirror)
			if err != nil {
				root = s.Mirror
			}
			if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
				return "", fmt.Errorf("path escapes the store through a symlink: %s", rel)
			}
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return full, nil
}

func (s *Store) Read(rel string) (string, error) {
	full, err := s.Abs(rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ReadFileUnder reads rel beneath root with the same containment rules as a
// store read. Config files, CODEOWNERS and registry files are content from a
// cloned repository, so following a tracked symlink outside root would turn a
// read into an arbitrary local-file disclosure.
func ReadFileUnder(root, rel string) ([]byte, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("root is required")
	}
	st := &Store{Mirror: root}
	full, err := st.Abs(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (s *Store) ExistsPath(rel string) bool {
	full, err := s.Abs(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(full)
	return err == nil
}

// EnsureArea validates that rel lives inside one of the allowed areas.
func EnsureArea(area, rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	area = strings.Trim(area, "/")
	if area == "" {
		return "", errors.New("changeset area is required")
	}
	if clean != area && !strings.HasPrefix(clean, area+"/") {
		return "", fmt.Errorf("path %q is outside area %q", rel, area)
	}
	return clean, nil
}

// JoinArea joins an area and a possibly area-relative path.
func JoinArea(area, p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return area
	}
	if p == area || strings.HasPrefix(p, area+"/") {
		return p
	}
	return path.Join(area, p)
}

func (s *Store) MakeWritable() error {
	return chmodTree(s.Mirror, true)
}

func (s *Store) MakeReadOnly() error {
	return chmodTree(s.Mirror, false)
}

// chmodTree toggles the owner write bit while preserving the rest of each
// entry's mode. The previous implementation forced every file to 0644 and every
// directory to 0755, which stripped executable bits and made tracked files
// appear modified. Symlinks are skipped: os.Chmod follows them and could change
// permissions outside the store.
func chmodTree(root string, writable bool) error {
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() | 0o400
		if d.IsDir() {
			mode |= 0o100
		}
		if writable {
			mode |= 0o200
		} else {
			mode &^= 0o222
		}
		if err := os.Chmod(p, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", p, err)
		}
		return nil
	})
}

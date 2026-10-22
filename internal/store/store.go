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

	"github.com/bolacha/docsync/internal/gitx"
)

const DefaultBranch = "main"

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
		remote = os.Getenv("DOCSYNC_REMOTE")
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
	if s.Remote == "" {
		return errors.New("store remote is not configured; run docsync init --remote <url>")
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

// Pull fetches origin and fast-forwards the mirror.
func (s *Store) Pull(ctx context.Context) (rev string, commitTime time.Time, err error) {
	_, _ = s.git(ctx, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	if _, err := s.git(ctx, "fetch", "--prune", "origin", "+refs/heads/"+s.Branch+":refs/remotes/origin/"+s.Branch); err != nil {
		return "", time.Time{}, fmt.Errorf("fetch store: %w", err)
	}
	if err := s.MakeWritable(); err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = s.MakeReadOnly() }()

	if _, err := gitx.Run(ctx, s.Mirror, "merge", "--ff-only", "origin/"+s.Branch); err != nil {
		return "", time.Time{}, fmt.Errorf("fast-forward mirror (local mirror edits may need `docsync change adopt`): %w", err)
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
	return chmodTree(s.Mirror, 0o644, 0o755)
}

func (s *Store) MakeReadOnly() error {
	return chmodTree(s.Mirror, 0o444, 0o555)
}

func chmodTree(root string, fileMode, dirMode os.FileMode) error {
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			_ = os.Chmod(p, dirMode)
			return nil
		}
		_ = os.Chmod(p, fileMode)
		return nil
	})
}

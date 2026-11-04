package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/gitx"
	"github.com/repocompanion/companion/internal/store"
)

// explicitStore is a checked-out docs store selected with --store. It is a
// read-only view: the local mirror, changesets and user config are not touched,
// so CI jobs and reviewers can read a pinned store checkout without a
// configured companion home.
type explicitStore struct {
	Root string
	Ctx  *config.Context
}

// explicitStore resolves an explicit store checkout. It deliberately ignores the
// user config and the environment so the same checkout and flags resolve the
// same context on any machine.
func (a *App) explicitStore(path string) (*explicitStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("--store requires a directory")
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve store path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("read store %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("store %s is not a directory", root)
	}
	detected := gitx.Detect(context.Background(), a.Cwd)
	repo, err := config.LoadRepo(detected.Root)
	if err != nil {
		return nil, fmt.Errorf("load repository config: %w", err)
	}
	registry, err := config.LoadRegistry(root)
	if err != nil {
		return nil, fmt.Errorf("load store registry: %w", err)
	}
	ctx := config.Resolve(context.Background(), config.ResolveInput{
		Cwd:      a.Cwd,
		Flags:    config.Flags{Project: a.Project, Team: a.Team, Domain: a.Domain, Languages: a.Languages},
		Repo:     repo,
		Registry: registry,
		Store:    root,
		Env:      map[string]string{},
	})
	return &explicitStore{Root: root, Ctx: ctx}, nil
}

// Docs walks the checkout. Symlinks are skipped by store.WalkDocsAt.
func (e *explicitStore) Docs() ([]*store.Doc, error) {
	docs, err := store.WalkDocsAt(e.Root, "")
	if err != nil {
		return nil, fmt.Errorf("read store %s: %w", e.Root, err)
	}
	return docs, nil
}

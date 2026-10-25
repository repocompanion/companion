package changeset

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/forge"
	"github.com/repocompanion/companion/internal/state"
	"github.com/repocompanion/companion/internal/store"
)

// newChangesetFixture creates a local bare store, a mirrored main branch and an
// active changeset with representative Markdown and Mermaid documents.
func newChangesetFixture(t *testing.T) (*Manager, state.Change) {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "remote.git")
	home := filepath.Join(root, "home")
	writeFile(t, seed, "projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Acme API\n")
	writeFile(t, seed, "projects/acme/api/flows/legacy.md", "---\nid: legacy\nkind: flow\ntitle: Legacy\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Legacy\n")
	writeFile(t, seed, "projects/acme/api/flows/main.mmd", "graph TD\n")
	git(t, seed, "init", "-q", "-b", "main")
	git(t, seed, "add", ".")
	git(t, seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed")
	git(t, root, "init", "-q", "--bare", remote)
	git(t, seed, "remote", "add", "origin", remote)
	git(t, seed, "push", "-q", "origin", "main")

	t.Setenv("GIT_AUTHOR_NAME", "Tester")
	t.Setenv("GIT_AUTHOR_EMAIL", "tester@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Tester")
	t.Setenv("GIT_COMMITTER_EMAIL", "tester@example.com")

	st := store.New(home, remote)
	if err := st.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.MakeWritable() })
	cc := &config.Context{Project: "acme/api", Team: "payments", UserName: "Ana", Machine: "test"}
	m := New(st, state.NewStore(st.StatePath), config.UserConfig{}, cc, &forge.Client{})
	c, err := m.Start(context.Background(), "p0", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	return m, c
}

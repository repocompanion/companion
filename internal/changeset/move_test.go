package changeset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/forge"
	"github.com/repocompanion/companion/internal/state"
	"github.com/repocompanion/companion/internal/store"
)

func TestMoveFile(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "remote.git")
	home := filepath.Join(root, "home")
	writeFile(t, seed, "projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Acme API\n")
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
	ctx := context.Background()
	cfg := config.UserConfig{}
	cfg.Changesets.BranchPrefix = "docs"
	cc := &config.Context{Project: "acme/api", Team: "payments", UserName: "Ana", Machine: "test"}
	m := New(st, state.NewStore(st.StatePath), cfg, cc, &forge.Client{})

	c, err := m.Start(ctx, "refund flow", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	const refund = "---\nid: refund\nkind: flow\ntitle: Refund\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Refund\n"
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/refund.md", refund, "", "test"); err != nil {
		t.Fatal(err)
	}
	hash, err := m.HashFile(ctx, c, "flows/refund.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.MoveFile(ctx, c.Area, "flows/refund.md", "flows/refunds.md", hash, "test"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Path, "projects/acme/api/flows/refund.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old path still exists: %v", err)
	}
	moved, err := os.ReadFile(filepath.Join(c.Path, "projects/acme/api/flows/refunds.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moved), "# Refund") {
		t.Fatal("moved content is missing")
	}

	const other = "---\nid: other\nkind: flow\ntitle: Other\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Other\n"
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/other.md", other, "", "test"); err != nil {
		t.Fatal(err)
	}
	otherHash, err := m.HashFile(ctx, c, "flows/other.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.MoveFile(ctx, c.Area, "flows/other.md", "flows/refunds.md", otherHash, "test"); err == nil || !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("collision error = %v", err)
	}
	if _, err := m.MoveFile(ctx, c.Area, "flows/other.md", "other.md", otherHash, "test"); err == nil || !strings.Contains(err.Error(), "flow documents must live under flows/") {
		t.Fatalf("layout error = %v", err)
	}
	var bre *BaseRevError
	if _, err := m.MoveFile(ctx, c.Area, "flows/other.md", "flows/other2.md", "deadbeef", "test"); !errors.As(err, &bre) {
		t.Fatalf("expected BaseRevError, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Path, "projects/acme/api/flows/other2.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed move created destination: %v", err)
	}
}

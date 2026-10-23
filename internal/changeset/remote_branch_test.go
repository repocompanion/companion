package changeset

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
)

func TestEnsureNamedReusesSingleRemoteBranch(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	if _, err := m.Cleanup(ctx, c.ID, false, true); err != nil {
		t.Fatal(err)
	}
	branch := "docs/ana/projects-acme-api/remote-topic"
	mainSHA, err := m.git(ctx, "rev-parse", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, "", "--git-dir", m.Store.Remote, "update-ref", "refs/heads/"+branch, mainSHA); err != nil {
		t.Fatal(err)
	}

	got, err := m.EnsureNamed(ctx, "projects/acme/api", "remote work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != branch {
		t.Fatalf("branch = %q, want %q", got.Branch, branch)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatalf("reused worktree missing: %v", err)
	}
	active, err := m.Active("projects/acme/api")
	if err != nil || active == nil || active.ID != got.ID {
		t.Fatalf("active = %+v, err=%v", active, err)
	}
}

func TestStartAvoidsRemoteBranchCollision(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	if _, err := m.Cleanup(ctx, c.ID, false, true); err != nil {
		t.Fatal(err)
	}
	branch := "docs/ana/projects-acme-api/collision"
	mainSHA, err := m.git(ctx, "rev-parse", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, "", "--git-dir", m.Store.Remote, "update-ref", "refs/heads/"+branch, mainSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := m.git(ctx, "fetch", "--prune", "origin"); err != nil {
		t.Fatal(err)
	}

	got, err := m.Start(ctx, "collision", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch == branch || !strings.HasSuffix(got.Branch, "/collision-2") {
		t.Fatalf("branch = %q, want collision suffix", got.Branch)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatalf("new worktree missing: %v", err)
	}
}

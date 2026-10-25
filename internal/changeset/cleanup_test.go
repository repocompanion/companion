package changeset

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/gitx"
	"github.com/repocompanion/companion/internal/state"
)

const cleanupDoc = "---\nid: cleanup-work\nkind: flow\ntitle: Cleanup work\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Cleanup work\n"

func TestCleanupComparesAgainstPRHeadWhenBranchIsGone(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	c, _, err := m.WriteFile(ctx, c.Area, "flows/cleanup.md", cleanupDoc, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	head, err := m.wt(ctx, c, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, c.Path, "push", "-q", "origin", head+":refs/pull/7/head"); err != nil {
		t.Fatal(err)
	}
	c.PR = 7
	c.PRState = "merged"
	if err := m.State.With(func(f *state.File) error {
		ch := f.ByID(c.ID)
		ch.PR = c.PR
		ch.PRState = c.PRState
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	unpublished, err := m.unpublished(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if unpublished {
		t.Fatal("cleanup treated merged PR commits as unpushed")
	}
	if _, err := m.Cleanup(ctx, c.ID, false, false); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(c.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}

func TestCleanupStillRejectsCommitsAfterPRHead(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	c, _, err := m.WriteFile(ctx, c.Area, "flows/cleanup.md", cleanupDoc, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	head, err := m.wt(ctx, c, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, c.Path, "push", "-q", "origin", head+":refs/pull/8/head"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/after-pr.md", strings.ReplaceAll(cleanupDoc, "cleanup-work", "after-pr"), "", "test"); err != nil {
		t.Fatal(err)
	}
	c.PR = 8
	c.PRState = "merged"
	if err := m.State.With(func(f *state.File) error {
		ch := f.ByID(c.ID)
		ch.PR = c.PR
		ch.PRState = c.PRState
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	unpublished, err := m.unpublished(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if !unpublished {
		t.Fatal("cleanup missed a commit after the PR head")
	}
	if _, err := m.Cleanup(ctx, c.ID, false, false); err == nil || !strings.Contains(err.Error(), "unpushed local work") {
		t.Fatalf("cleanup error = %v", err)
	}
}

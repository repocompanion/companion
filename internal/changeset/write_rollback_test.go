package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/state"
)

const legacyDoc = "---\nid: legacy\nkind: flow\ntitle: Legacy\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Legacy\n"

const otherDoc = "---\nid: other\nkind: flow\ntitle: Other\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Other\n"

func assertCleanWorktree(t *testing.T, m *Manager, c state.Change) {
	t.Helper()
	dirty, err := m.Dirty(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) > 0 {
		t.Fatalf("worktree left dirty after rejected write: %v", dirty)
	}
}

func TestPatchFileSecretRejectionLeavesChangesetUsable(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	hash, err := m.HashFile(ctx, c, "flows/legacy.md")
	if err != nil {
		t.Fatal(err)
	}
	secret := "ghp_" + strings.Repeat("a", 30)
	_, err = m.PatchFile(ctx, c.Area, "flows/legacy.md", hash, []Edit{{Old: "# Legacy", New: "# Legacy\n" + secret}}, "test")
	if err == nil || !strings.Contains(err.Error(), "possible secret") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	b, err := os.ReadFile(filepath.Join(c.Path, "projects/acme/api/flows/legacy.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != legacyDoc {
		t.Fatalf("content changed after rejected patch: %q", string(b))
	}
	assertCleanWorktree(t, m, c)
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/other.md", otherDoc, "", "test"); err != nil {
		t.Fatalf("unrelated write after rejected patch: %v", err)
	}
}

func TestApplyPatchFileSecretRejectionLeavesChangesetUsable(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	hash, err := m.HashFile(ctx, c, "flows/legacy.md")
	if err != nil {
		t.Fatal(err)
	}
	diff := `--- a/projects/acme/api/flows/legacy.md
+++ b/projects/acme/api/flows/legacy.md
@@ -9,1 +9,1 @@
-# Legacy
+ghp_` + strings.Repeat("a", 30) + "\n"
	_, err = m.ApplyPatchFile(ctx, c.Area, "flows/legacy.md", hash, diff, "test")
	if err == nil || !strings.Contains(err.Error(), "possible secret") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	assertCleanWorktree(t, m, c)
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/other.md", otherDoc, "", "test"); err != nil {
		t.Fatalf("unrelated write after rejected patch: %v", err)
	}
}

func TestWriteFileRollsBackWhenCommitFails(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	// A secret already sitting in the worktree makes the commit fail. The new
	// document must not be left behind next to it.
	leak := filepath.Join(c.Path, "projects/acme/api/leak.txt")
	if err := os.WriteFile(leak, []byte("ghp_"+strings.Repeat("a", 30)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/other.md", otherDoc, "", "test"); err == nil {
		t.Fatal("expected commit failure")
	}
	if _, err := os.Stat(filepath.Join(c.Path, "projects/acme/api/flows/other.md")); !os.IsNotExist(err) {
		t.Fatalf("new file left behind after failed commit: %v", err)
	}
}

func TestPatchFileValidatesFinalDocument(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	hash, err := m.HashFile(ctx, c, "flows/legacy.md")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.PatchFile(ctx, c.Area, "flows/legacy.md", hash, []Edit{{Old: "id: legacy\n", New: ""}}, "test")
	if err == nil || !strings.Contains(err.Error(), "patched document is invalid") {
		t.Fatalf("expected validation failure, got %v", err)
	}
	assertCleanWorktree(t, m, c)
}

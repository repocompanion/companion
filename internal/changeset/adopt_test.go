package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
)

func editMirror(t *testing.T, m *Manager, rel, appended string) string {
	t.Helper()
	if err := m.Store.MakeWritable(); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(m.Store.Mirror, filepath.FromSlash(rel))
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, append(b, []byte(appended)...), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func mirrorStatus(t *testing.T, m *Manager) string {
	t.Helper()
	out, err := gitx.Run(context.Background(), m.Store.Mirror, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func TestAdoptIncludesStagedAndUnstagedEdits(t *testing.T) {
	m, _ := newChangesetFixture(t)
	ctx := context.Background()
	rel := "projects/acme/api/flows/legacy.md"
	editMirror(t, m, rel, "\nStaged edit.\n")
	git(t, m.Store.Mirror, "add", rel)
	editMirror(t, m, rel, "\nUnstaged edit.\n")

	c, err := m.Adopt(ctx, "test")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(c.Path, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Staged edit.") || !strings.Contains(string(b), "Unstaged edit.") {
		t.Fatalf("adopted content = %q", string(b))
	}
	if dirty, _ := m.Dirty(ctx, c); len(dirty) > 0 {
		t.Fatalf("adopted changeset not committed: %v", dirty)
	}
	if st := mirrorStatus(t, m); st != "" {
		t.Fatalf("mirror not restored: %q", st)
	}
}

func TestAdoptOnlyStagedEdit(t *testing.T) {
	m, _ := newChangesetFixture(t)
	rel := "projects/acme/api/flows/legacy.md"
	editMirror(t, m, rel, "\nStaged only.\n")
	git(t, m.Store.Mirror, "add", rel)

	c, err := m.Adopt(context.Background(), "test")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(c.Path, rel))
	if !strings.Contains(string(b), "Staged only.") {
		t.Fatalf("staged edit not adopted: %q", string(b))
	}
	if st := mirrorStatus(t, m); st != "" {
		t.Fatalf("mirror not restored: %q", st)
	}
}

func TestAdoptFailureKeepsMirrorEditsAndRemovesChangeset(t *testing.T) {
	m, _ := newChangesetFixture(t)
	rel := "projects/acme/api/flows/legacy.md"
	full := editMirror(t, m, rel, "\nghp_"+strings.Repeat("a", 30)+"\n")
	before, err := m.List()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Adopt(context.Background(), "test"); err == nil || !strings.Contains(err.Error(), "possible secret") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	b, _ := os.ReadFile(full)
	if !strings.Contains(string(b), "ghp_") {
		t.Fatal("mirror edit was discarded after a failed adopt")
	}
	after, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("failed adopt left a changeset behind: %d -> %d", len(before), len(after))
	}
}

package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/state"
)

const secretLeak = "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"

func addSecretLeak(t *testing.T, c state.Change) {
	t.Helper()
	leak := filepath.Join(c.Path, "projects/acme/api/leak.txt")
	if err := os.WriteFile(leak, []byte(secretLeak), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeSecretLeak(t *testing.T, c state.Change) {
	t.Helper()
	if err := os.Remove(filepath.Join(c.Path, "projects/acme/api/leak.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestMoveFileRollsBackWhenCommitFails(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	addSecretLeak(t, c)

	hash, err := m.HashFile(ctx, c, "flows/legacy.md")
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(c.Path, "projects/acme/api/flows/legacy.md")
	newPath := filepath.Join(c.Path, "projects/acme/api/flows/legacy-renamed.md")
	if _, err := m.MoveFile(ctx, c.Area, "flows/legacy.md", "flows/legacy-renamed.md", hash, "test"); err == nil || !strings.Contains(err.Error(), "possible secret") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	b, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("source was not restored: %v", err)
	}
	if string(b) != legacyDoc {
		t.Fatalf("restored content = %q", string(b))
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("destination left behind after failed move: %v", err)
	}

	removeSecretLeak(t, c)
	assertCleanWorktree(t, m, c)
}

func TestDeleteFileRollsBackWhenCommitFails(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	addSecretLeak(t, c)

	hash, err := m.HashFile(ctx, c, "flows/legacy.md")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Path, "projects/acme/api/flows/legacy.md")
	if _, err := m.DeleteFile(ctx, c.Area, "flows/legacy.md", hash, "test"); err == nil || !strings.Contains(err.Error(), "possible secret") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("deleted file was not restored: %v", err)
	}
	if string(b) != legacyDoc {
		t.Fatalf("restored content = %q", string(b))
	}

	removeSecretLeak(t, c)
	assertCleanWorktree(t, m, c)
}

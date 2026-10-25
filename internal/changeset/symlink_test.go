package changeset

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/state"
)

func TestSafeWorktreePathRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "flows"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.Symlink(outside, filepath.Join(root, "flows", "new.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeWorktreePath(root, "flows/new.md"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}

	// A symlinked parent directory must be rejected too.
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside-dir"), filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeWorktreePath(root, "linked-dir/new.md"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected parent symlink rejection, got %v", err)
	}
}

func TestManagerWorktreePathRejectsSymlinksAndOtherAreas(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	root := t.TempDir()
	area := "projects/acme/api"
	m := &Manager{}
	c := state.Change{Area: area, Path: root}

	if err := os.MkdirAll(filepath.Join(root, "projects", "acme", "api", "flows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "acme", "api", "flows", "real.md"), []byte("# real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := m.WorktreePath(c, "projects/acme/api/flows/real.md")
	if err != nil || got != filepath.Join(root, "projects", "acme", "api", "flows", "real.md") {
		t.Fatalf("WorktreePath(real) = %q, %v", got, err)
	}

	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.md"), filepath.Join(root, "projects", "acme", "api", "flows", "leak.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WorktreePath(c, "projects/acme/api/flows/leak.md"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}

	if _, err := m.WorktreePath(c, "org/patterns/other.md"); err == nil || !strings.Contains(err.Error(), "outside area") {
		t.Fatalf("expected area rejection, got %v", err)
	}
}

package changeset

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

package changeset

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStartRejectsNonCanonicalArea(t *testing.T) {
	m, _ := newChangesetFixture(t)
	for _, area := range []string{"org/payments", "teams/payments/extra", "projects/acme/api/extra"} {
		if _, err := m.Start(context.Background(), "nested", area); err == nil || !strings.Contains(err.Error(), "area must be exactly") {
			t.Errorf("Start(%q) error = %v, want canonical-area error", area, err)
		}
	}
}

func TestUniqueWorktreePathOrErrorFindsSuffix(t *testing.T) {
	m := &Manager{}
	base := filepath.Join(t.TempDir(), "docs--topic")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := m.uniqueWorktreePathOrError(base)
	if err != nil {
		t.Fatal(err)
	}
	if want := base + "-2"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestUniqueWorktreePathOrErrorSurfacesStatErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a regular file as a parent component is not consistently ENOTDIR on Windows")
	}
	m := &Manager{}
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := m.uniqueWorktreePathOrError(filepath.Join(blocker, "docs--topic"))
	if err == nil || !strings.Contains(err.Error(), "check worktree path") {
		t.Fatalf("expected a stat error, got %v", err)
	}
}

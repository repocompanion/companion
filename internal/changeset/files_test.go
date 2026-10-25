package changeset

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFilesReturnsGitErrors(t *testing.T) {
	m, c := newChangesetFixture(t)
	c.Path = filepath.Join(t.TempDir(), "missing-worktree")
	if _, err := m.files(context.Background(), c); err == nil {
		t.Fatal("expected an error for a missing worktree")
	}
}

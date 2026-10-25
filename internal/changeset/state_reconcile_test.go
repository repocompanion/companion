package changeset

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/repocompanion/companion/internal/state"
)

func TestReconcileStateRebuildsMissingState(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	if err := os.Remove(m.State.Path); err != nil {
		t.Fatal(err)
	}

	res, err := m.ReconcileState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || !res.Rebuilt {
		t.Fatalf("result = %+v", res)
	}
	st, err := m.State.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Changes) != 1 {
		t.Fatalf("changes = %+v", st.Changes)
	}
	got := st.Changes[0]
	if got.Branch != c.Branch || canonicalPath(got.Path) != canonicalPath(c.Path) || got.Area != c.Area {
		t.Fatalf("rebuilt change = %+v, want branch=%s path=%s area=%s", got, c.Branch, c.Path, c.Area)
	}
	if st.Active[c.Area] != got.ID {
		t.Fatalf("active = %q, want %q", st.Active[c.Area], got.ID)
	}
}

func TestReconcileStateRebuildsCorruptState(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	if err := os.WriteFile(m.State.Path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := m.ReconcileState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || !res.Rebuilt {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(m.State.Path + ".corrupt"); err != nil {
		t.Fatalf("corrupt state was not preserved: %v", err)
	}
	st, err := m.State.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Changes) != 1 || st.Changes[0].Branch != c.Branch {
		t.Fatalf("changes = %+v", st.Changes)
	}
}

func TestReconcileStateRemovesOrphanEntries(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	orphanPath := filepath.Join(m.Store.Changes, "missing--worktree")
	if err := m.State.With(func(f *state.File) error {
		f.Add(state.Change{
			ID: "cs-orphan", Title: "Orphan", Area: "org",
			Branch: "docs/ana/org/missing", Path: orphanPath, PRState: "none",
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res, err := m.ReconcileState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Fatalf("result = %+v", res)
	}
	st, err := m.State.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Changes) != 1 || st.Changes[0].Branch != c.Branch {
		t.Fatalf("changes = %+v", st.Changes)
	}
	for _, change := range st.Changes {
		if change.ID == "cs-orphan" {
			t.Fatal("orphan entry survived reconciliation")
		}
	}
}

func TestReconcileStatePreservesExistingState(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()

	res, err := m.ReconcileState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Removed != 0 || res.Updated != 1 {
		t.Fatalf("result = %+v", res)
	}
	st, err := m.State.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Changes) != 1 || st.Changes[0].ID != c.ID {
		t.Fatalf("changes = %+v, want preserved ID %s", st.Changes, c.ID)
	}
	if st.Active[c.Area] != c.ID {
		t.Fatalf("active = %q, want %q", st.Active[c.Area], c.ID)
	}
}

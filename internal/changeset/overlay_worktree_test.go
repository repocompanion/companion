package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/store"
)

func TestOverlayIncludesUncommittedChanges(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()

	// Uncommitted modification of a tracked document.
	writeFile(t, c.Path, "projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Uncommitted README\n")
	// Uncommitted new document.
	writeFile(t, c.Path, "projects/acme/api/flows/draft.md", "---\nid: draft\nkind: flow\ntitle: Draft\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Draft\n")
	// Uncommitted deletion of a main document.
	if err := os.Remove(filepath.Join(c.Path, "projects/acme/api/flows/legacy.md")); err != nil {
		t.Fatal(err)
	}

	o, err := m.Overlay(ctx, c.Area)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]*store.Doc{}
	for _, d := range o.Docs {
		byPath[d.Path] = d
	}
	if byPath["projects/acme/api/README.md"] == nil {
		t.Fatal("modified doc is missing from overlay")
	}
	if !strings.Contains(byPath["projects/acme/api/README.md"].Raw, "Uncommitted README") {
		t.Fatalf("overlay did not use uncommitted content: %q", byPath["projects/acme/api/README.md"].Raw)
	}
	if o.States["projects/acme/api/README.md"] != DocStateModified {
		t.Fatalf("modified state = %q", o.States["projects/acme/api/README.md"])
	}
	if o.States["projects/acme/api/flows/draft.md"] != DocStateAdded {
		t.Fatalf("added state = %q", o.States["projects/acme/api/flows/draft.md"])
	}
	if byPath["projects/acme/api/flows/legacy.md"] != nil || o.States["projects/acme/api/flows/legacy.md"] != DocStateDeleted {
		t.Fatalf("deleted state = %q", o.States["projects/acme/api/flows/legacy.md"])
	}

	// The deleted tombstone must survive ReadFileState.
	content, source, docState, err := m.ReadFileState(ctx, c.Area, "flows/legacy.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "" || docState != DocStateDeleted || !strings.HasPrefix(source, "changeset ") {
		t.Fatalf("deleted read = %q, %q, %q", content, source, docState)
	}
}

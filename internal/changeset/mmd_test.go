package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileStateSupportsMermaidFiles(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	const content = "graph TD\n  A --> B\n"
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/diagram.mmd", content, "", "test"); err != nil {
		t.Fatal(err)
	}
	got, source, docState, err := m.ReadFileState(ctx, c.Area, "flows/diagram.mmd")
	if err != nil {
		t.Fatal(err)
	}
	if got != content || docState != DocStateAdded || !strings.HasPrefix(source, "changeset ") {
		t.Fatalf("mmd read = %q, %q, %q", got, source, docState)
	}

	// Uncommitted edit of the diagram keeps the overlay state in sync.
	writeFile(t, c.Path, "projects/acme/api/flows/diagram.mmd", "graph LR\n  A --> C\n")
	got, _, docState, err = m.ReadFileState(ctx, c.Area, "flows/diagram.mmd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "graph LR") || docState != DocStateAdded {
		t.Fatalf("mmd edit = %q, %q", got, docState)
	}

	if err := os.Remove(filepath.Join(c.Path, "projects/acme/api/flows/main.mmd")); err != nil {
		t.Fatal(err)
	}
	got, _, docState, err = m.ReadFileState(ctx, c.Area, "flows/main.mmd")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" || docState != DocStateDeleted {
		t.Fatalf("mmd delete = %q, %q", got, docState)
	}
}

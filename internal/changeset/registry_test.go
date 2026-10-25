package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRegistryPrefersWorktreeAndRejectsMalformed(t *testing.T) {
	m, c := newChangesetFixture(t)

	reg, err := m.LoadRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Repos) != 0 {
		t.Fatalf("empty store returned mappings: %+v", reg.Repos)
	}

	writeFile(t, c.Path, ".docsync/registry.yaml", "repos:\n  acme/api:\n    team: payments\n")
	reg, err = m.LoadRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.Repos["acme/api"]; got.Team != "payments" {
		t.Fatalf("worktree registry was not used: %+v", got)
	}

	writeFile(t, c.Path, ".docsync/registry.yaml", "repos: [\n")
	if _, err := m.LoadRegistry(c); err == nil || !strings.Contains(err.Error(), "parse .docsync/registry.yaml") {
		t.Fatalf("expected a parse error, got %v", err)
	}
}

func TestReadWorktreeOrMainFallsBackToMain(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	rel := "projects/acme/api/flows/legacy.md"
	full := filepath.Join(c.Path, filepath.FromSlash(rel))

	b, err := m.readWorktreeOrMain(c, rel)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != legacyDoc {
		t.Fatalf("initial content = %q", string(b))
	}

	if err := os.WriteFile(full, []byte("# Worktree edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err = m.readWorktreeOrMain(c, rel)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "# Worktree edit\n" {
		t.Fatalf("worktree content = %q", string(b))
	}

	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	b, err = m.readWorktreeOrMain(c, rel)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != legacyDoc {
		t.Fatalf("main fallback content = %q", string(b))
	}

	// Restore the tracked file so the fixture cleanup and any later assertion
	// see a clean worktree.
	if _, err := m.wt(ctx, c, "checkout", "--", rel); err != nil {
		t.Fatal(err)
	}
}

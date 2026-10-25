package changeset

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/forge"
	"github.com/repocompanion/companion/internal/state"
	"github.com/repocompanion/companion/internal/store"
)

func TestOverlayReflectsAddModifyDelete(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "remote.git")
	home := filepath.Join(root, "home")
	writeFile(t, seed, "projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Acme API\n")
	writeFile(t, seed, "projects/acme/api/flows/refund.md", "---\nid: refund\nkind: flow\ntitle: Refund\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Refund\n")
	git(t, seed, "init", "-q", "-b", "main")
	git(t, seed, "add", ".")
	git(t, seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed")
	git(t, root, "init", "-q", "--bare", remote)
	git(t, seed, "remote", "add", "origin", remote)
	git(t, seed, "push", "-q", "origin", "main")

	t.Setenv("GIT_AUTHOR_NAME", "Tester")
	t.Setenv("GIT_AUTHOR_EMAIL", "tester@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Tester")
	t.Setenv("GIT_COMMITTER_EMAIL", "tester@example.com")

	st := store.New(home, remote)
	if err := st.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.MakeWritable() })
	ctx := context.Background()
	cfg := config.UserConfig{}
	cfg.Changesets.BranchPrefix = "docs"
	cc := &config.Context{Project: "acme/api", Team: "payments", UserName: "Ana", Machine: "test"}
	m := New(st, state.NewStore(st.StatePath), cfg, cc, &forge.Client{})

	c, err := m.Start(ctx, "overlay", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	const added = "---\nid: added\nkind: flow\ntitle: Added\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Added flow\n"
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/added.md", added, "", "test"); err != nil {
		t.Fatal(err)
	}
	refundHash, err := m.HashFile(ctx, c, "flows/refund.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.PatchFile(ctx, c.Area, "flows/refund.md", refundHash, []Edit{{Old: "# Refund", New: "# Refund flow"}}, "test"); err != nil {
		t.Fatal(err)
	}
	readmeHash, err := m.HashFile(ctx, c, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.DeleteFile(ctx, c.Area, "README.md", readmeHash, "test"); err != nil {
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
	if byPath["projects/acme/api/flows/added.md"] == nil || o.States["projects/acme/api/flows/added.md"] != DocStateAdded {
		t.Fatalf("added doc missing or wrong state: %+v", o.States)
	}
	if !strings.Contains(byPath["projects/acme/api/flows/refund.md"].Raw, "# Refund flow") || o.States["projects/acme/api/flows/refund.md"] != DocStateModified {
		t.Fatalf("modified doc missing or wrong state: %+v", o.States)
	}
	if byPath["projects/acme/api/README.md"] != nil || o.States["projects/acme/api/README.md"] != DocStateDeleted {
		t.Fatalf("deleted doc still visible or wrong state: %+v", o.States)
	}
	content, source, docState, err := m.ReadFileState(ctx, c.Area, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "" || docState != DocStateDeleted || !strings.HasPrefix(source, "changeset ") {
		t.Fatalf("deleted read = %q, %q, %q", content, source, docState)
	}

	all, err := m.OverlayAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range all.Docs {
		if d.Path == "projects/acme/api/README.md" {
			t.Fatal("OverlayAll resurrected a deleted doc")
		}
	}
	hits, err := store.SearchDocs(all.Docs, "added flow", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Path != "projects/acme/api/flows/added.md" {
		t.Fatalf("overlay search hits = %+v", hits)
	}
}

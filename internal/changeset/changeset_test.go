package changeset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/forge"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/state"
	"github.com/bolacha/docsync/internal/store"
)

func TestLocalChangesetLifecycle(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "remote.git")
	home := filepath.Join(root, "home")
	writeFile(t, seed, "projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\n---\n# Acme API\n")
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

	c, err := m.Start(ctx, "refund flow", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if c.Area != "projects/acme/api" || c.Branch == "" {
		t.Fatalf("bad changeset: %+v", c)
	}
	content := "---\nid: refund\nkind: flow\ntitle: Refund\nstatus: approved\n---\n# Refund\n"
	c2, changed, err := m.WriteFile(ctx, c.Area, "flows/refund.md", content, "", "test")
	if err != nil || !changed {
		t.Fatalf("write: %v changed=%v", err, changed)
	}
	if c2.ID != c.ID {
		t.Fatalf("write created unexpected changeset %s", c2.ID)
	}
	res, err := m.Sync(ctx, c.ID, "test")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !res.Pushed {
		t.Fatal("sync did not push")
	}
	if _, err := gitx.Run(ctx, "", "--git-dir", remote, "rev-parse", "--verify", "refs/heads/"+c.Branch); err != nil {
		t.Fatalf("remote branch missing: %v", err)
	}

	// Host-side commit on the shared branch is integrated on the next sync.
	other := filepath.Join(root, "other")
	git(t, root, "clone", "-q", remote, other)
	git(t, other, "checkout", "-q", c.Branch)
	writeFile(t, other, "projects/acme/api/flows/host-branch.md", "---\nid: host-branch\nkind: flow\ntitle: Host branch\nstatus: approved\n---\n# Host branch\n")
	git(t, other, "add", ".")
	git(t, other, "-c", "user.name=Other", "-c", "user.email=other@example.com", "commit", "-qm", "host branch edit")
	git(t, other, "push", "-q", "origin", c.Branch)
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/local-branch.md", "---\nid: local-branch\nkind: flow\ntitle: Local branch\nstatus: approved\n---\n# Local\n", "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx, c.ID, "test"); err != nil {
		t.Fatalf("sync with host-side commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Path, "projects/acme/api/flows/host-branch.md")); err != nil {
		t.Fatal("host-side branch commit was not integrated")
	}

	// A ready changeset is frozen for docsync writes.
	if err := m.State.With(func(f *state.File) error {
		ch := f.ByID(c.ID)
		ch.PRState = "ready"
		ch.Frozen = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/refund.md", content, "", "test"); !errors.Is(err, ErrFrozen) {
		t.Fatalf("expected ErrFrozen, got %v", err)
	}

	// A new topic in the same area becomes active; the old changeset still exists.
	c3, err := m.Start(ctx, "partial refunds", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if c3.ID == c.ID {
		t.Fatal("start reused the frozen changeset")
	}
	active, err := m.Active("projects/acme/api")
	if err != nil || active == nil || active.ID != c3.ID {
		t.Fatalf("active = %+v, err=%v", active, err)
	}

	// Pull a host-side change into the read-only mirror.
	writeFile(t, seed, "projects/acme/api/flows/host.md", "---\nid: host\nkind: flow\ntitle: Host\nstatus: approved\n---\n# Host\n")
	git(t, seed, "add", ".")
	git(t, seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "host")
	git(t, seed, "push", "-q", "origin", "main")
	if _, _, err := st.Pull(ctx); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.Mirror, "projects/acme/api/flows/host.md")); err != nil {
		t.Fatal("pull did not update mirror")
	}

	// Adopt an accidental mirror edit.
	if err := st.MakeWritable(); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(st.Mirror, "projects/acme/api/README.md")
	b, _ := os.ReadFile(readme)
	if err := os.WriteFile(readme, append(b, []byte("\nAccidental edit.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	adopted, err := m.Adopt(ctx, "test")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if adopted.Area != "projects/acme/api" {
		t.Fatalf("adopt area = %s", adopted.Area)
	}
	ab, _ := os.ReadFile(filepath.Join(adopted.Path, "projects/acme/api/README.md"))
	if !strings.Contains(string(ab), "Accidental edit.") {
		t.Fatal("adopted content missing from changeset")
	}
	mb, _ := os.ReadFile(readme)
	if strings.Contains(string(mb), "Accidental edit.") {
		t.Fatal("mirror was not restored")
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := gitx.Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

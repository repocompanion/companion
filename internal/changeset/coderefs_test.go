package changeset

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/repocompanion/companion/internal/codeindex"
	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/forge"
	"github.com/repocompanion/companion/internal/state"
	"github.com/repocompanion/companion/internal/store"
)

func TestValidateCodeRefsBlocksMissingPath(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "remote.git")
	home := filepath.Join(root, "home")
	code := filepath.Join(root, "code")
	writeFile(t, seed, "projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\n---\n# Acme API\n")
	git(t, seed, "init", "-q", "-b", "main")
	git(t, seed, "add", ".")
	git(t, seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed")
	git(t, root, "init", "-q", "--bare", remote)
	git(t, seed, "remote", "add", "origin", remote)
	git(t, seed, "push", "-q", "origin", "main")

	writeFile(t, code, "go.mod", "module example.com/api\n\ngo 1.25.0\n")
	writeFile(t, code, "internal/consumer.go", "package internal\n\nfunc Run() {}\n")
	git(t, code, "init", "-q", "-b", "main")
	git(t, code, "add", ".")
	git(t, code, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed")

	st := store.New(home, remote)
	if err := st.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.MakeWritable() })
	ctx := context.Background()
	cc := &config.Context{Project: "acme/api", RepoRoot: code, UserName: "Ana", Machine: "test"}
	m := New(st, state.NewStore(st.StatePath), config.UserConfig{}, cc, &forge.Client{})
	c, err := m.Start(ctx, "refund flow", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	doc := "---\nid: refund\nkind: flow\ntitle: Refund\nstatus: approved\nreference_impls:\n  - repo: acme/api\n    path: internal/missing.go\n    ref: main\n---\n# Refund\n"
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/refund.md", doc, "", "test"); err != nil {
		t.Fatal(err)
	}
	refs, warnings, err := m.validateCodeRefs(ctx, c)
	if err == nil {
		t.Fatal("expected missing code reference to fail preflight")
	}
	var codeErr *CodeRefsError
	if !errors.As(err, &codeErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if len(warnings) != 0 || len(refs) != 1 || len(refs[0].References) != 1 {
		t.Fatalf("refs=%+v warnings=%v", refs, warnings)
	}
	if refs[0].References[0].PathStatus != codeindex.PathMissing {
		t.Fatalf("path status = %+v", refs[0].References[0])
	}
}

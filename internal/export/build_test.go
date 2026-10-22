package export

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/state"
	"github.com/bolacha/docsync/internal/store"
)

func TestBuildUsesOverlayAndSnapshot(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "remote.git")
	home := filepath.Join(root, "home")
	write := func(rel, content string) {
		p := filepath.Join(seed, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Acme API\n\nAcme API summary paragraph.\n")
	write("projects/acme/api/architecture.md", "---\nid: architecture\nkind: project-doc\ntitle: Architecture\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Architecture\n")
	write("org/patterns/sqs.md", "---\nid: sqs\nkind: pattern\ntitle: SQS consumer\nstatus: approved\nowners: [platform]\nlast_reviewed: 2026-01-01\n---\n# SQS consumer\n\nUse a queue consumer with a DLQ.\n")
	write("projects/acme/api/flows/refund.md", "---\nid: refund\nkind: flow\ntitle: Refund\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Refund\n")
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
	cc := &config.Context{Project: "acme/api", Team: "payments", Machine: "test"}
	m := changeset.New(st, state.NewStore(st.StatePath), config.UserConfig{}, cc, nil)
	c, err := m.Start(ctx, "export", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/deploy.md", "---\nid: deploy\nkind: flow\ntitle: Deploy\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Deploy\n\nOperate deploy rollback and troubleshoot.\n", "", "test"); err != nil {
		t.Fatal(err)
	}
	sess := &runtime.Session{
		Runtime: &runtime.Runtime{Store: st},
		Context: cc,
		Manager: m,
		Repo:    config.RepoConfig{},
	}
	b, err := Build(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if b.Project != "acme/api" || b.Snapshot.MainRevision == "" {
		t.Fatalf("bundle = %+v", b)
	}
	if !strings.Contains(b.Summary, "Acme API summary") {
		t.Fatalf("summary = %q", b.Summary)
	}
	foundDeploy := false
	for _, d := range b.Docs {
		if d.Path == "projects/acme/api/flows/deploy.md" {
			foundDeploy = d.State == changeset.DocStateAdded
		}
	}
	if !foundDeploy {
		t.Fatalf("overlay doc missing: %+v", b.Docs)
	}
	if len(b.Tasks) == 0 || len(b.Groups) == 0 {
		t.Fatalf("tasks/groups = %+v / %+v", b.Tasks, b.Groups)
	}
	if !strings.Contains(strings.Join(b.Snapshot.ActiveChangesets, ","), c.ID) {
		t.Fatalf("snapshot changesets = %+v", b.Snapshot.ActiveChangesets)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := gitx.Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

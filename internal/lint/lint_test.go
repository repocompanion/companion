package lint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/gitx"
	"github.com/repocompanion/companion/internal/store"
)

func TestCanonicalLayoutViolations(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("projects/acme/api/README.md", `---
id: readme
kind: project-doc
title: Acme
status: approved
---
# Acme
`)
	write("projects/acme/api/flows/refund.md", `---
id: refund
kind: flow
title: Refund
status: approved
---
# Refund
`)
	write("projects/acme/api/decisions.md", `---
id: decisions
kind: adr
title: Decisions
status: approved
---
# Decisions
`)
	write("projects/acme/api/operations.md", `---
id: operations
kind: runbook
title: Operations
status: approved
---
# Operations
`)

	res, err := Run(&store.Store{Mirror: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) < 2 {
		t.Fatalf("expected layout issues, got %+v", res.Issues)
	}
	foundADR, foundRunbook := false, false
	for _, i := range res.Issues {
		if i.Path == "projects/acme/api/decisions.md" {
			foundADR = true
		}
		if i.Path == "projects/acme/api/operations.md" {
			foundRunbook = true
		}
	}
	if !foundADR || !foundRunbook {
		t.Fatalf("missing layout errors: %+v", res.Issues)
	}
}

func TestRunFilesUsesCanonicalRules(t *testing.T) {
	root := t.TempDir()
	rel := "projects/acme/api/flows/refund.md"
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`---
id: refund
kind: flow
title: Refund
status: approved
---
# Refund

[missing](does-not-exist.md)
`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunFiles(root, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) == 0 {
		t.Fatal("expected canonical metadata and link errors")
	}
	var owners, link bool
	for _, i := range res.Issues {
		if strings.Contains(i.Message, "owners") {
			owners = true
		}
		if strings.Contains(i.Message, "broken internal link") {
			link = true
		}
	}
	if !owners || !link {
		t.Fatalf("issues = %+v, want owners and broken link", res.Issues)
	}
}

func TestRunFilesSkipsDeletedDocs(t *testing.T) {
	res, err := RunFiles(t.TempDir(), []string{"projects/acme/api/flows/deleted.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) != 0 || res.Docs != 0 {
		t.Fatalf("deleted file should be skipped: %+v", res)
	}
}

func TestRunDirWithCodeRefs(t *testing.T) {
	storeRoot := t.TempDir()
	codeRoot := t.TempDir()
	write := func(root, rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	write(codeRoot, "go.mod", "module example.com/api\n\ngo 1.25.0\n")
	write(codeRoot, "internal/consumer.go", "package internal\n\nfunc Run() {}\n")
	if _, err := gitx.Run(ctx, codeRoot, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, codeRoot, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, codeRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed"); err != nil {
		t.Fatal(err)
	}
	doc := func(path string) string {
		return "---\nid: consumer\nkind: pattern\ntitle: Consumer\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\nreference_impls:\n  - repo: acme/api\n    path: " + path + "\n    ref: main\n---\n# Consumer\n"
	}
	rel := "projects/acme/api/patterns/consumer.md"
	write(storeRoot, rel, doc("internal/consumer.go"))
	s := &store.Store{Mirror: storeRoot}
	res, err := RunDirWithCodeRefs(ctx, s, "", codeRoot, "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range res.Issues {
		if issue.Level == "error" {
			t.Fatalf("valid reference produced lint error: %+v", issue)
		}
	}

	write(storeRoot, rel, doc("internal/missing.go"))
	res, err = RunDirWithCodeRefs(ctx, s, "", codeRoot, "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range res.Issues {
		if issue.Level == "error" && strings.Contains(issue.Message, "does not exist") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing code path was not reported: %+v", res.Issues)
	}
}

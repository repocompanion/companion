package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/store"
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

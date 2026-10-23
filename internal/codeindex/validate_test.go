package codeindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/store"
)

func testRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "consumer.go"), []byte("package internal\n\ntype Consumer struct{}\n\nfunc (c Consumer) Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestValidatePathAndGrepFallback(t *testing.T) {
	root := testRepo(t)
	ctx := context.Background()
	doc := &store.Doc{Path: "projects/acme/api/patterns/x.md", Frontmatter: store.Frontmatter{ReferenceImpls: []store.ReferenceImpl{
		{Repo: "acme/api", Path: "internal/consumer.go", Ref: "main"},
		{Repo: "acme/api", Path: "internal/consumer.go", Symbol: "Consumer", Ref: "main"},
	}}}
	dv, err := (Validator{Root: root, Project: "acme/api"}).ValidateDoc(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(dv.References) != 2 {
		t.Fatalf("references = %+v", dv.References)
	}
	if dv.References[0].PathStatus != PathValid || dv.References[0].SymbolStatus != SymbolNotRequested {
		t.Fatalf("path-only = %+v", dv.References[0])
	}
	if dv.References[1].PathStatus != PathValid || dv.References[1].SymbolStatus != SymbolUnverified || dv.References[1].Provider != "git-grep" || !strings.Contains(dv.References[1].Reason, "literal symbol match") {
		t.Fatalf("grep fallback = %+v", dv.References[1])
	}
	s := Summarize([]DocValidation{dv})
	if s.ValidPaths != 2 || s.GrepMatches != 1 {
		t.Fatalf("summary = %+v", s)
	}
	if !strings.Contains(Format([]DocValidation{dv}), "path=valid") {
		t.Fatal("Format did not render validation")
	}
}

func TestValidateMissingExternalAndUnverified(t *testing.T) {
	root := testRepo(t)
	ctx := context.Background()
	doc := &store.Doc{Path: "projects/acme/api/patterns/x.md", Frontmatter: store.Frontmatter{ReferenceImpls: []store.ReferenceImpl{
		{Repo: "acme/api", Path: "internal/missing.go"},
		{Repo: "acme/other", Path: "internal/consumer.go"},
		{Repo: "acme/api", Path: "internal/consumer.go", Ref: "does-not-exist"},
		{Repo: "acme/api", Path: "../escape.go"},
	}}}
	dv, err := (Validator{Root: root, Project: "acme/api"}).ValidateDoc(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if dv.References[0].PathStatus != PathMissing {
		t.Fatalf("missing = %+v", dv.References[0])
	}
	if dv.References[1].PathStatus != PathExternal || dv.References[1].SymbolStatus != SymbolExternal {
		t.Fatalf("external = %+v", dv.References[1])
	}
	if dv.References[2].PathStatus != PathUnverified {
		t.Fatalf("bad ref = %+v", dv.References[2])
	}
	if dv.References[3].PathStatus != PathInvalid {
		t.Fatalf("escaping path = %+v", dv.References[3])
	}
	noRoot, err := (Validator{Project: "acme/api"}).ValidateDoc(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if noRoot.References[0].PathStatus != PathUnverified {
		t.Fatalf("no root = %+v", noRoot.References[0])
	}
}

func TestValidateDocsSkipsDocsWithoutRefs(t *testing.T) {
	root := testRepo(t)
	docs := []*store.Doc{{Path: "no-refs.md"}, {Path: "with-ref.md", Frontmatter: store.Frontmatter{ReferenceImpls: []store.ReferenceImpl{{Repo: "acme/api", Path: "internal/consumer.go"}}}}}
	out, err := ValidateDocs(context.Background(), root, "acme/api", docs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Path != "with-ref.md" {
		t.Fatalf("docs = %+v", out)
	}
}

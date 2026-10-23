package codeindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/store"
)

func TestCtagsProviderIndexAndResolve(t *testing.T) {
	root := testRepo(t)
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("export function handler(req, res) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "add ts"); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "universal-ctags")
	output := "#!/bin/sh\ncat <<'EOF'\n" +
		`{"_type":"tag","name":"handler","path":"app.ts","kind":"function","line":1,"signature":"(req, res)","scope":"api","language":"TypeScript"}` +
		"\nEOF\n"
	if err := os.WriteFile(script, []byte(output), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &CtagsProvider{Binary: script, ToolVersion: "test", Available: true, CacheDir: t.TempDir()}
	ctx := context.Background()
	snap, err := p.Index(ctx, IndexRequest{Repo: "acme/api", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Symbols) != 1 {
		t.Fatalf("symbols = %+v", snap.Symbols)
	}
	if sym := snap.Symbols[0]; sym.Provider != "ctags" || sym.Kind != "function" || sym.Qualified != "api.handler" || sym.Path != "app.ts" {
		t.Fatalf("symbol = %+v", sym)
	}
	doc := &store.Doc{Path: "projects/acme/api/patterns/x.md", Frontmatter: store.Frontmatter{ReferenceImpls: []store.ReferenceImpl{
		{Repo: "acme/api", Path: "app.ts", Symbol: "handler"},
	}}}
	dv, err := (Validator{Root: root, Project: "acme/api", Registry: NewRegistry(p)}).ValidateDoc(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if dv.References[0].SymbolStatus != SymbolValid || dv.References[0].Provider != "ctags" {
		t.Fatalf("ctags validation = %+v", dv.References[0])
	}
}

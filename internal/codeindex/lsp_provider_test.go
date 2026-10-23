package codeindex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bolacha/docsync/internal/gitx"
)

func TestLSPHelpers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "app.ts")
	if err := os.WriteFile(file, []byte("export class Api {}\nexport function handler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pos, err := findSymbolPosition(file, "Api.handler")
	if err != nil || pos.Line != 1 {
		t.Fatalf("position = %+v, err=%v", pos, err)
	}
	if lastSymbolPart("Api.handler") != "handler" {
		t.Fatal("unexpected symbol suffix")
	}
	uri := pathToFileURI(file)
	got, err := uriToPath(uri)
	if err != nil || got != file {
		t.Fatalf("uri round trip = %q, %v", got, err)
	}
	locs, err := parseLSPLocations([]byte(`[{"uri":"` + uri + `","range":{"start":{"line":1,"character":0}}}]`))
	if err != nil || len(locs) != 1 || locs[0].Line != 1 || locs[0].Path != file {
		t.Fatalf("locations = %+v, err=%v", locs, err)
	}
}

func TestLSPProviderWithoutServers(t *testing.T) {
	p := &LSPProvider{}
	if p.Available || p.SupportsPath("app.ts") {
		t.Fatal("empty provider should be unavailable")
	}
	if _, err := p.Index(nil, IndexRequest{}); err != ErrUnsupported {
		t.Fatalf("Index error = %v", err)
	}
	if _, err := p.Resolve(nil, ResolveRequest{}); err != ErrUnsupported {
		t.Fatalf("Resolve error = %v", err)
	}
}

func TestGoplsDefinition(t *testing.T) {
	gopls, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls is not installed")
	}
	root := testRepo(t)
	commit, err := gitx.Run(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	p := &LSPProvider{Servers: []lspServer{{
		Name: "go", Language: "go", Binary: gopls, Exts: map[string]bool{".go": true},
	}}, Available: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sym, err := p.Resolve(ctx, ResolveRequest{Repo: "acme/api", Root: root, Commit: commit, Path: "internal/consumer.go", Symbol: "Consumer.Run"})
	if err != nil {
		t.Fatalf("gopls resolve: %v", err)
	}
	if sym.Provider != "lsp-go" || sym.Path != "internal/consumer.go" || sym.Line == 0 || !strings.Contains(sym.Signature, "Run") {
		t.Fatalf("gopls symbol = %+v", sym)
	}
}

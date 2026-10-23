package codeindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

func TestSCIPProviderDefinitionsAndReferences(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("export function handler() {}\nhandler();\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const raw = "test npm . . handler."
	idx := &scip.Index{Documents: []*scip.Document{{
		RelativePath: "app.ts", Language: "typescript",
		Symbols: []*scip.SymbolInformation{{
			Symbol: raw, DisplayName: "handler", Kind: scip.SymbolInformation_Function,
			SignatureDocumentation: &scip.Signature{Text: "function handler()"},
		}},
		Occurrences: []*scip.Occurrence{
			{Symbol: raw, Range: []int32{0, 9, 16}, SymbolRoles: int32(scip.SymbolRole_Definition)},
			{Symbol: raw, Range: []int32{1, 0, 7}},
		},
	}}}
	b, err := proto.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	indexFile := filepath.Join(root, "index.scip")
	if err := os.WriteFile(indexFile, b, 0o644); err != nil {
		t.Fatal(err)
	}
	p := &SCIPProvider{IndexFile: indexFile}
	ctx := context.Background()
	snap, err := p.Index(ctx, IndexRequest{Repo: "acme/api", Root: root, Commit: "deadbeef"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Symbols) != 1 {
		t.Fatalf("symbols = %+v", snap.Symbols)
	}
	sym := snap.Symbols[0]
	if sym.Provider != "scip" || sym.SCIP != raw || sym.Path != "app.ts" || sym.Line != 1 || sym.Kind != "function" {
		t.Fatalf("symbol = %+v", sym)
	}
	resolved, err := p.Resolve(ctx, ResolveRequest{Repo: "acme/api", Root: root, Commit: "deadbeef", Path: "app.ts", Symbol: "handler"})
	if err != nil || resolved.SCIP != raw {
		t.Fatalf("resolved = %+v, err=%v", resolved, err)
	}
	refs, err := p.References(ctx, ResolveRequest{Repo: "acme/api", Root: root, Commit: "deadbeef", Path: "app.ts", Symbol: "handler"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Line != 2 {
		t.Fatalf("references = %+v", refs)
	}
	refs, err = p.References(ctx, ResolveRequest{Repo: "acme/api", Root: root, Commit: "deadbeef", Path: "app.ts", Symbol: "handler"}, true)
	if err != nil || len(refs) != 2 {
		t.Fatalf("references with declaration = %+v, err=%v", refs, err)
	}
}

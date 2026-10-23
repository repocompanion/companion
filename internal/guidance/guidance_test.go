package guidance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bolacha/docsync/internal/codeindex"
	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/store"
)

func TestLookupPrefersNarrowScopeAndOverrides(t *testing.T) {
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
	write("org/patterns/sqs.md", `---
id: sqs-consumer
kind: pattern
title: SQS consumer
status: approved
topics: [sqs, queue]
languages: [go]
---
# Org SQS
`)
	write("projects/acme/api/patterns/sqs.md", `---
id: sqs-consumer
kind: pattern
title: Acme SQS consumer
status: approved
overrides: org-sqs
topics: [sqs, queue]
languages: [go]
---
# Project SQS
`)
	write("org/patterns/org-sqs.md", `---
id: org-sqs
kind: pattern
title: Old org SQS
status: approved
topics: [sqs, queue]
---
# Old
`)
	write("org/adr/deprecated.md", `---
id: old-queue
kind: adr
title: Old queue ADR
status: deprecated
superseded_by: sqs-consumer
topics: [sqs]
---
# Deprecated
`)
	s := &store.Store{Mirror: root}
	ctx := &config.Context{Project: "acme/api"}
	res, err := Lookup(s, ctx, "implement an SQS queue consumer in Go", "go", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("no results")
	}
	if res[0].Path != "projects/acme/api/patterns/sqs.md" {
		t.Fatalf("first result = %s, want project override", res[0].Path)
	}
	if res[0].Snippet == "" || len(res[0].MatchedTerms) == 0 {
		t.Fatalf("result lacks BM25 snippet metadata: %+v", res[0])
	}
	for _, r := range res {
		if r.Path == "org/patterns/org-sqs.md" {
			t.Fatal("explicitly overridden org pattern should be excluded")
		}
		if r.Status == "deprecated" {
			t.Fatal("deprecated doc should be excluded when live results exist")
		}
	}
}

func TestApplyCodeContextExposesResolvedSymbols(t *testing.T) {
	results := []Result{{Path: "projects/acme/api/patterns/sqs.md"}}
	byPath := map[string]int{results[0].Path: 0}
	applyCodeContext(results, byPath, []codeindex.DocValidation{{
		Path: results[0].Path,
		References: []codeindex.ReferenceValidation{{
			Reference:         codeindex.Reference{Path: "internal/consumer.go", Symbol: "Consumer.Run"},
			PathStatus:        codeindex.PathValid,
			SymbolStatus:      codeindex.SymbolValid,
			DriftStatus:       codeindex.DriftStale,
			Provider:          "go",
			ResolvedPath:      "internal/consumer.go",
			ResolvedLine:      12,
			ResolvedQualified: "consumer.Consumer.Run",
			ResolvedSignature: "func ()",
			DriftReason:       "symbol fingerprint changed",
		}},
	}})
	if len(results[0].CodeContext) != 1 {
		t.Fatalf("code context = %+v", results[0].CodeContext)
	}
	cc := results[0].CodeContext[0]
	if cc.Line != 12 || cc.Qualified != "consumer.Consumer.Run" || cc.Signature != "func ()" {
		t.Fatalf("code context = %+v", cc)
	}
	if len(results[0].Warnings) != 1 {
		t.Fatalf("warnings = %+v", results[0].Warnings)
	}
}

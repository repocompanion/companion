package search

import (
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/store"
)

func TestBM25FieldWeighting(t *testing.T) {
	docs := []*store.Doc{
		{
			Path: "org/patterns/sqs.md",
			Frontmatter: store.Frontmatter{
				ID: "sqs-consumer", Kind: "pattern", Title: "SQS consumer",
				Status: "approved", Topics: []string{"sqs", "queue", "consumer"},
			},
			Body: "# SQS consumer\n\nUse a dead-letter queue and idempotency keys.\n",
		},
		{
			Path: "org/patterns/other.md",
			Frontmatter: store.Frontmatter{
				ID: "other", Kind: "pattern", Title: "Other pattern", Status: "approved",
			},
			Body: "# Other\n\nThis mentions sqs once in the body only.\n",
		},
	}
	results, err := Search(docs, "sqs consumer", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Path != "org/patterns/sqs.md" {
		t.Fatalf("top result = %s", results[0].Path)
	}
	if len(results[0].MatchedTerms) == 0 || !strings.Contains(results[0].Why, "title") {
		t.Fatalf("matched terms/why = %+v / %q", results[0].MatchedTerms, results[0].Why)
	}
	snippetResults, err := Search(docs, "dead-letter", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(snippetResults) == 0 || !strings.Contains(snippetResults[0].Snippet, "dead-letter") {
		t.Fatalf("snippet = %+v", snippetResults)
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	if _, err := Search(nil, "the", 5); err == nil {
		t.Fatal("expected an error for a query with no searchable terms")
	}
}

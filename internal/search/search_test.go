package search

import (
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/store"
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

func TestTokenizeDeduplicatesQueriesButCountsIndexTokens(t *testing.T) {
	if got := len(Tokenize("sqs sqs queue queue")); got != 2 {
		t.Fatalf("query token count = %d, want 2 unique terms", got)
	}
	if got := len(tokenizeAll("sqs sqs queue queue")); got != 4 {
		t.Fatalf("index token count = %d, want 4 occurrences", got)
	}
}

func TestBM25UsesTermFrequency(t *testing.T) {
	docs := []*store.Doc{
		{
			Path:        "org/patterns/rare.md",
			Frontmatter: store.Frontmatter{Title: "Rare", Status: "approved"},
			Body:        "sqs alpha beta gamma delta epsilon zeta eta theta",
		},
		{
			Path:        "org/patterns/repeated.md",
			Frontmatter: store.Frontmatter{Title: "Repeated", Status: "approved"},
			Body:        "sqs sqs sqs sqs sqs sqs sqs sqs sqs",
		},
	}
	results, err := Search(docs, "sqs", 5)
	if err != nil {
		t.Fatal(err)
	}
	scores := map[string]float64{}
	for _, r := range results {
		scores[r.Path] = r.Score
	}
	if scores["org/patterns/repeated.md"] <= scores["org/patterns/rare.md"] {
		t.Fatalf("term frequency did not affect ranking: repeated=%.4f rare=%.4f", scores["org/patterns/repeated.md"], scores["org/patterns/rare.md"])
	}
}

func TestSnippetLineIsFileRelative(t *testing.T) {
	raw := "---\nid: line-test\ntitle: Line test\nstatus: approved\n---\n# Body\n\nFind me here.\n"
	d, err := store.ParseDoc("projects/acme/api/flows/line.md", raw)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := Search([]*store.Doc{d}, "find", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Line != 8 {
		t.Fatalf("line = %d, want 8", hits[0].Line)
	}
}

package store

import "testing"

func TestParseDoc(t *testing.T) {
	raw := `---
id: sqs-consumer
kind: pattern
title: SQS consumer
status: approved
superseded_by: null
overrides: null
topics: [sqs, queue]
languages: [go]
reference_impls:
  - repo: acme/orders
    path: internal/consumer/
    ref: main
last_reviewed: 2026-01-01
---
# SQS consumer

Body.
`
	d, err := ParseDoc("org/patterns/sqs.md", raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Frontmatter.ID != "sqs-consumer" || d.Frontmatter.Kind != "pattern" {
		t.Fatalf("unexpected frontmatter: %+v", d.Frontmatter)
	}
	if d.Frontmatter.SupersededBy != "" || d.Frontmatter.Overrides != "" {
		t.Fatalf("null fields should be empty: %+v", d.Frontmatter)
	}
	if len(d.Frontmatter.ReferenceImpls) != 1 || d.Frontmatter.ReferenceImpls[0].Repo != "acme/orders" {
		t.Fatalf("reference impls: %+v", d.Frontmatter.ReferenceImpls)
	}
	if d.Body != "# SQS consumer\n\nBody.\n" {
		t.Fatalf("body = %q", d.Body)
	}
}

func TestParseDocWithoutFrontmatter(t *testing.T) {
	d, err := ParseDoc("README.md", "# Hello\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.Frontmatter.ID != "" || d.Body != "# Hello\n" {
		t.Fatalf("unexpected doc: %+v", d)
	}
}

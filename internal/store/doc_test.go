package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

func TestParseDocBlockScalarDoesNotCloseFrontmatter(t *testing.T) {
	raw := `---
id: block
kind: flow
title: Block
status: approved
owners: [a]
last_reviewed: 2026-01-01
description: |
  first line
  ---
  second line
---
# Body

Done.
`
	d, err := ParseDoc("projects/acme/api/flows/block.md", raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Frontmatter.ID != "block" || d.Frontmatter.Title != "Block" {
		t.Fatalf("frontmatter = %+v", d.Frontmatter)
	}
	if d.Body != "# Body\n\nDone.\n" {
		t.Fatalf("body = %q", d.Body)
	}
	if d.BodyLine != 12 {
		t.Fatalf("BodyLine = %d, want 12", d.BodyLine)
	}
}

func TestParseDocUnclosedFrontmatterReturnsError(t *testing.T) {
	if _, err := ParseDoc("broken.md", "---\nid: broken\ntitle: Broken\n"); err == nil {
		t.Fatal("expected an unclosed frontmatter error")
	} else if !strings.Contains(err.Error(), "unclosed frontmatter") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseDocHandlesCRLFAndLeadingBlankLines(t *testing.T) {
	raw := "\r\n---\r\nid: crlf\r\ntitle: CRLF\r\n---\r\n# Body\r\n"
	d, err := ParseDoc("crlf.md", raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Frontmatter.ID != "crlf" || d.Body != "# Body\r\n" {
		t.Fatalf("doc = %+v", d)
	}
	if d.BodyLine != 5 {
		t.Fatalf("BodyLine = %d, want 5", d.BodyLine)
	}
}

func TestParseDocAcceptsYAMLDocumentEndFence(t *testing.T) {
	d, err := ParseDoc("end.md", "---\nid: end\ntitle: End\n...\n# Body\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.Frontmatter.ID != "end" || d.Body != "# Body\n" {
		t.Fatalf("doc = %+v", d)
	}
}

func TestWalkDocsAtSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("# secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("# real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	docs, err := WalkDocsAt(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "real.md" {
		t.Fatalf("docs = %+v, want only real.md", docs)
	}
}

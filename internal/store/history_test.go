package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
)

func TestResolveRevisionMapsVersionLabels(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	if _, err := gitx.Run(ctx, root, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(root, "doc.md")
	commit := func(content, message string) string {
		t.Helper()
		if err := os.WriteFile(doc, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := gitx.Run(ctx, root, "add", "doc.md"); err != nil {
			t.Fatal(err)
		}
		if _, err := gitx.Run(ctx, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", message); err != nil {
			t.Fatal(err)
		}
		sha, err := gitx.Run(ctx, root, "rev-parse", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}
	first := commit("# One\n", "one")
	second := commit("# Two\n", "two")

	s := &Store{Mirror: root}
	v1, err := s.ResolveRevision(ctx, "doc.md", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if v1 != first {
		t.Fatalf("v1 = %s, want %s", v1, first)
	}
	v2, err := s.ResolveRevision(ctx, "doc.md", "V2")
	if err != nil {
		t.Fatal(err)
	}
	if v2 != second {
		t.Fatalf("v2 = %s, want %s", v2, second)
	}
	if _, err := s.ResolveRevision(ctx, "doc.md", "v9"); err == nil {
		t.Fatal("expected missing version error")
	}
	head, err := s.ResolveRevision(ctx, "doc.md", "HEAD")
	if err != nil || head != "HEAD" {
		t.Fatalf("HEAD = %q, %v", head, err)
	}
}

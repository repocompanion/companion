package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/repocompanion/companion/internal/gitx"
)

func TestHashAppliesPathAttributes(t *testing.T) {
	root := t.TempDir()
	if _, err := gitx.Run(context.Background(), root, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("* text=auto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "hello\r\nworld\r\n"
	path := filepath.Join(root, "doc.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Store{Mirror: root}
	got, err := s.Hash(context.Background(), "doc.md", content)
	if err != nil {
		t.Fatal(err)
	}
	want, err := gitx.Run(context.Background(), root, "hash-object", "--", path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("hash = %s, want path-filtered hash %s", got, want)
	}
}

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/gitx"
)

func TestInitBootstrapEmptyStore(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "store.git")
	if _, err := gitx.Run(context.Background(), root, "init", "-q", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	st := New(filepath.Join(root, "home"), remote)
	t.Cleanup(func() { _ = st.MakeWritable() })
	if err := st.Init(context.Background()); !errors.Is(err, ErrEmptyStore) {
		t.Fatalf("Init(empty) = %v, want ErrEmptyStore", err)
	}
	if err := st.Bootstrap(context.Background(), "Tester", "tester@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("Init after bootstrap: %v", err)
	}
	if !st.Exists() {
		t.Fatal("store does not exist after bootstrap")
	}
	raw, err := os.ReadFile(filepath.Join(st.Mirror, ".companion", "registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "teams: {}") {
		t.Fatalf("registry = %q", raw)
	}
}

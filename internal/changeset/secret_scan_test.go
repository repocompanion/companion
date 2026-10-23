package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretScanCoversUntrackedDirectory(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	dir := filepath.Join(c.Path, "projects/acme/api/untracked")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := "ghp_" + strings.Repeat("a", 30)
	if err := os.WriteFile(filepath.Join(dir, "leak.txt"), []byte(secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := m.Sync(ctx, c.ID, "test")
	if err == nil || !strings.Contains(err.Error(), "possible secret") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	out, err := m.wt(ctx, c, "rev-list", "--count", "main..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "0" {
		t.Fatalf("secret changes were committed: %s", out)
	}
}

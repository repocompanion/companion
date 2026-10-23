package changeset

import (
	"context"
	"strings"
	"testing"
)

func TestPatchFileEnforcesMaxSize(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	hash, err := m.HashFile(ctx, c, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	m.Cfg.MCP.MaxFileSize = 32
	_, err = m.PatchFile(ctx, c.Area, "README.md", hash, []Edit{{Old: "# Acme API", New: "# Acme API " + strings.Repeat("x", 64)}}, "test")
	if err == nil || !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatalf("expected max-size rejection, got %v", err)
	}
}

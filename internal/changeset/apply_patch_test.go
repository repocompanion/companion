package changeset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const patchDoc = "---\nid: patch\nkind: flow\ntitle: Patch\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Patch me\n"

func TestApplyPatchFileAppliesSingleFileDiff(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	c, _, err := m.WriteFile(ctx, c.Area, "flows/patch.md", patchDoc, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := m.HashFile(ctx, c, "flows/patch.md")
	if err != nil {
		t.Fatal(err)
	}
	diff := `--- a/projects/acme/api/flows/patch.md
+++ b/projects/acme/api/flows/patch.md
@@ -9,1 +9,1 @@
-# Patch me
+# Patched
`
	if _, err := m.ApplyPatchFile(ctx, c.Area, "flows/patch.md", hash, diff, "test"); err != nil {
		t.Fatalf("apply patch: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(c.Path, "projects/acme/api/flows/patch.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# Patched") {
		t.Fatalf("patched content = %q", string(b))
	}
}

func TestApplyPatchFileLeavesContentOnInvalidPatch(t *testing.T) {
	m, c := newChangesetFixture(t)
	ctx := context.Background()
	c, _, err := m.WriteFile(ctx, c.Area, "flows/patch.md", patchDoc, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := m.HashFile(ctx, c, "flows/patch.md")
	if err != nil {
		t.Fatal(err)
	}
	diff := `--- a/projects/acme/api/flows/patch.md
+++ b/projects/acme/api/flows/patch.md
@@ -9,1 +9,1 @@
-# Does not exist
+# Patched
`
	if _, err := m.ApplyPatchFile(ctx, c.Area, "flows/patch.md", hash, diff, "test"); err == nil {
		t.Fatal("expected invalid patch to fail")
	}
	b, err := os.ReadFile(filepath.Join(c.Path, "projects/acme/api/flows/patch.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != patchDoc {
		t.Fatalf("content changed after failed patch: %q", string(b))
	}
}

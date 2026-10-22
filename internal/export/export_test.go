package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBundle() *Bundle {
	readme := DocRef{Path: "projects/acme/api/README.md", Title: "Acme API", Kind: "project-doc", Status: "approved", State: "main", URL: "https://github.com/acme/docs/blob/main/projects/acme/api/README.md"}
	pattern := DocRef{Path: "org/patterns/sqs.md", Title: "SQS consumer", Kind: "pattern", Status: "approved", State: "main"}
	return &Bundle{
		Project: "acme/api", Area: "projects/acme/api", Team: "payments",
		Summary: "A payments API.", Snapshot: Snapshot{GeneratedAt: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), MainRevision: "abcdef1234567890", ActiveChangesets: []string{"cs-1 (projects/acme/api)"}},
		Required: []RequiredDoc{{Path: "projects/acme/api/README.md", Present: true}, {Path: "projects/acme/api/architecture.md"}},
		Tasks:    []TaskPick{{Task: "implement an integration", Docs: []DocRef{pattern}}},
		Groups:   []Group{{Kind: "project-doc", Title: "Project docs", Docs: []DocRef{readme}}, {Kind: "pattern", Title: "Patterns", Docs: []DocRef{pattern}}},
		Docs:     []DocRef{readme, pattern}, TotalDocs: 2, ReturnedDocs: 2, MaxDocs: 200,
	}
}

func TestRenderExports(t *testing.T) {
	b := testBundle()
	agents := RenderAgentsMD(b)
	for _, want := range []string{ManagedBegin, ManagedEnd, "Acme API", "Snapshot generated", "Required documentation", "SQS consumer", "docsync cat"} {
		if !strings.Contains(agents, want) {
			t.Fatalf("agents-md missing %q:\n%s", want, agents)
		}
	}
	llms := RenderLLMsTxt(b)
	for _, want := range []string{"# acme/api", "SQS consumer", "Required documentation", "Snapshot generated"} {
		if !strings.Contains(llms, want) {
			t.Fatalf("llms-txt missing %q:\n%s", want, llms)
		}
	}
	raw, err := RenderContextJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["project"] != "acme/api" {
		t.Fatalf("context JSON project = %v", decoded["project"])
	}
}

func TestWriteManagedPreservesHumanContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	old := "# Human\n\nbefore\n\n" + ManagedBegin + "\nold block\n" + ManagedEnd + "\n\nafter\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	block := ManagedBegin + "\nnew block\n" + ManagedEnd + "\n"
	if err := WriteManaged(path, block, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "before") || !strings.Contains(string(got), "after") || !strings.Contains(string(got), "new block") || strings.Contains(string(got), "old block") {
		t.Fatalf("managed replacement failed:\n%s", got)
	}
}

func TestWriteManagedRefusesUnmanagedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Human\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManaged(path, ManagedBegin+"\nnew\n"+ManagedEnd+"\n", false); err == nil {
		t.Fatal("expected refusal without --force")
	}
	if err := WriteManaged(path, ManagedBegin+"\nnew\n"+ManagedEnd+"\n", true); err != nil {
		t.Fatal(err)
	}
}

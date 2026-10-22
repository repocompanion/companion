package mcpserver

import (
	"context"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerListsExpectedTools(t *testing.T) {
	ctx := context.Background()
	server, err := NewServer(Options{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{
		"docs_change_join", "docs_change_reopen", "docs_change_start", "docs_change_status",
		"docs_change_submit", "docs_change_sync", "docs_delete", "docs_diff", "docs_get_context",
		"docs_get_guidance", "docs_history", "docs_list", "docs_move", "docs_patch", "docs_propose_pattern",
		"docs_pull", "docs_read", "docs_record_decision", "docs_review_comments",
		"docs_scaffold_project", "docs_search", "docs_set_context", "docs_write",
	}
	if len(names) != len(want) {
		t.Fatalf("got %d tools (%v), want %d", len(names), names, len(want))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tool list = %v, want %v", names, want)
		}
	}
}

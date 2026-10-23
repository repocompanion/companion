package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsAgainstLocalStore(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "store.git")
	home := filepath.Join(root, "home")
	cfgPath := filepath.Join(root, "config.yaml")

	write := func(rel, content string) {
		p := filepath.Join(seed, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("org/patterns/messaging/sqs.md", `---
id: sqs-consumer
kind: pattern
title: SQS consumer
status: approved
topics: [sqs, queue, consumer]
languages: [go]
reference_impls:
  - repo: acme/orders
    path: internal/consumer/
    ref: main
---
# SQS consumer

Use a DLQ.
`)
	write("projects/acme/api/README.md", `---
id: readme
kind: project-doc
title: Acme API
status: approved
last_reviewed: 2020-01-01
reference_impls:
  - repo: acme/api
    path: projects/acme/api/README.md
---
# Acme API
`)
	write("projects/acme/api/flows/legacy.md", `---
id: legacy
kind: flow
title: Legacy
status: approved
owners: [tester]
last_reviewed: 2026-01-01
---
# Legacy flow
`)
	if out, err := gitx.Run(context.Background(), seed, "init", "-q", "-b", "main"); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if _, err := gitx.Run(context.Background(), seed, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "init", "-q", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), seed, "remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), seed, "push", "-q", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("remote: "+remote+"\nforge:\n  type: github\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCSYNC_HOME", home)
	t.Setenv("DOCSYNC_CONFIG", cfgPath)
	t.Setenv("USER", "tester")
	t.Setenv("GIT_AUTHOR_NAME", "Tester")
	t.Setenv("GIT_AUTHOR_EMAIL", "tester@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Tester")
	t.Setenv("GIT_COMMITTER_EMAIL", "tester@example.com")

	st := store.New(home, remote)
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("store init: %v", err)
	}
	t.Cleanup(func() { _ = st.MakeWritable() })

	ctx := context.Background()
	outside := t.TempDir()
	server, err := NewServer(Options{Cwd: outside, Project: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	stt, ctt := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, stt, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	client.AddRoots(&mcp.Root{URI: (&url.URL{Scheme: "file", Path: seed}).String()})
	cs, err := client.Connect(ctx, ctt, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError {
			for _, c := range res.Content {
				if tc, ok := c.(*mcp.TextContent); ok {
					t.Fatalf("%s returned tool error: %s", name, tc.Text)
				}
			}
			t.Fatalf("%s returned tool error", name)
		}
		var text string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text = tc.Text
			}
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("%s output is not JSON: %v\n%s", name, err, text)
		}
		return out
	}

	pullOut := call("docs_pull", map[string]any{})
	if pullOut["rev"] == "" {
		t.Fatal("docs_pull returned no revision")
	}
	callError := func(name string, args map[string]any) string {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !res.IsError {
			t.Fatalf("%s should have failed", name)
		}
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				return tc.Text
			}
		}
		return ""
	}

	ctxOut := call("docs_get_context", map[string]any{})
	if ctxOut["area"] != "projects/acme/api" {
		t.Fatalf("area = %v", ctxOut["area"])
	}
	if ctxOut["structure"] == nil {
		t.Fatal("docs_get_context did not return the canonical structure")
	}
	refsOut := call("docs_validate_code_refs", map[string]any{})
	refsSummary, _ := refsOut["summary"].(map[string]any)
	if refsSummary["valid_paths"].(float64) < 1 || refsSummary["stale_refs"].(float64) < 1 {
		t.Fatalf("code refs were not validated with drift: %v", refsOut)
	}
	staleOut := call("docs_stale", map[string]any{})
	if docs, _ := staleOut["docs"].([]any); len(docs) == 0 {
		t.Fatalf("docs_stale returned no stale docs: %v", staleOut)
	}
	if warnings, _ := ctxOut["warnings"].([]any); len(warnings) == 0 {
		t.Fatalf("context did not include a stale-reference warning: %v", ctxOut)
	}
	rootCtx, _ := ctxOut["context"].(map[string]any)
	if rootCtx["repo_root"] != "" {
		seedReal, _ := filepath.EvalSymlinks(seed)
		repoRoot, _ := rootCtx["repo_root"].(string)
		repoReal, _ := filepath.EvalSymlinks(repoRoot)
		if repoReal != seedReal {
			t.Fatalf("root was not used for context: repo_root=%q seed=%q", repoRoot, seedReal)
		}
	} else {
		t.Fatal("MCP root did not populate repo_root")
	}
	limited := call("docs_get_context", map[string]any{"max_docs": 1})
	if docs, _ := limited["docs"].([]any); len(docs) != 1 {
		t.Fatalf("bounded context returned %d docs", len(docs))
	}
	if limited["truncated"] != true || limited["total_docs"].(float64) <= 1 {
		t.Fatalf("context was not marked truncated: %v", limited)
	}
	if limited["next_cursor"] == nil || limited["next_cursor"] == "" {
		t.Fatalf("context did not return a next cursor: %v", limited)
	}
	contextPage2 := call("docs_get_context", map[string]any{"max_docs": 1, "cursor": limited["next_cursor"]})
	page1Docs, _ := limited["docs"].([]any)
	page2Docs, _ := contextPage2["docs"].([]any)
	if len(page2Docs) != 1 || len(page1Docs) != 1 || page1Docs[0].(map[string]any)["path"] == page2Docs[0].(map[string]any)["path"] {
		t.Fatalf("context pagination repeated or dropped a doc: %v / %v", page1Docs, page2Docs)
	}
	listPage := call("docs_list", map[string]any{"scope": "project", "limit": 1})
	listPageDocs, _ := listPage["docs"].([]any)
	if len(listPageDocs) != 1 || listPage["next_cursor"] == "" {
		t.Fatalf("list pagination failed: %v", listPage)
	}
	listPage2 := call("docs_list", map[string]any{"scope": "project", "limit": 1, "cursor": listPage["next_cursor"]})
	listPage2Docs, _ := listPage2["docs"].([]any)
	if len(listPage2Docs) != 1 || listPageDocs[0].(map[string]any)["path"] == listPage2Docs[0].(map[string]any)["path"] {
		t.Fatalf("list pagination repeated or dropped a doc: %v / %v", listPageDocs, listPage2Docs)
	}
	resources, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil || len(resources.ResourceTemplates) < 2 {
		t.Fatalf("resource templates = %+v, err=%v", resources, err)
	}
	docResource, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "docsync://docs/projects/acme/api/README.md"})
	if err != nil || len(docResource.Contents) == 0 || !strings.Contains(docResource.Contents[0].Text, "Acme API") {
		t.Fatalf("document resource = %+v, err=%v", docResource, err)
	}
	tmplResource, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "docsync://templates/flow"})
	if err != nil || len(tmplResource.Contents) == 0 || !strings.Contains(tmplResource.Contents[0].Text, "## Trigger") {
		t.Fatalf("template resource = %+v, err=%v", tmplResource, err)
	}
	errText := callError("docs_delete", map[string]any{"path": "notes.txt", "base_rev": "deadbeef"})
	if !strings.Contains(errText, "may write or delete .md and .mmd") {
		t.Fatalf("expected delete extension rejection, got %q", errText)
	}
	errText = callError("docs_write", map[string]any{"path": "operations.md", "content": "# Operations\n"})
	if !strings.Contains(errText, "frontmatter") {
		t.Fatalf("expected frontmatter rejection, got %q", errText)
	}
	errText = callError("docs_write", map[string]any{
		"path":    "decisions.md",
		"content": "---\nid: decisions\nkind: adr\ntitle: Decisions\nstatus: approved\nowners: [tester]\nlast_reviewed: 2026-01-01\n---\n# Decisions\n",
	})
	if !strings.Contains(errText, "ADRs must") {
		t.Fatalf("expected ADR path rejection, got %q", errText)
	}
	guidanceOut := call("docs_get_guidance", map[string]any{"task": "implement an SQS consumer in Go"})
	results, _ := guidanceOut["results"].([]any)
	if len(results) == 0 {
		t.Fatal("no guidance results")
	}
	listOut := call("docs_list", map[string]any{"scope": "project"})
	if docs, _ := listOut["docs"].([]any); len(docs) == 0 {
		t.Fatal("no docs listed")
	}

	writeOut := call("docs_write", map[string]any{
		"path":    "flows/refund.md",
		"content": "---\nid: refund\nkind: flow\ntitle: Refund\nstatus: approved\nowners: [tester]\nlast_reviewed: 2026-01-01\n---\n# Refund\n",
	})
	if writeOut["ok"] != true {
		t.Fatalf("write failed: %v", writeOut)
	}
	errText = callError("docs_write", map[string]any{
		"path":    "flows/refund-copy.md",
		"content": "---\nid: refund\nkind: flow\ntitle: Refund copy\nstatus: approved\nowners: [tester]\nlast_reviewed: 2026-01-01\n---\n# Refund copy\n",
	})
	if !strings.Contains(errText, "already exists") {
		t.Fatalf("expected duplicate id rejection, got %q", errText)
	}
	readOut := call("docs_read", map[string]any{"path": "flows/refund.md"})
	if readOut["content"] == "" {
		t.Fatal("empty read after write")
	}
	hash, _ := readOut["hash"].(string)
	patchOut := call("docs_patch", map[string]any{
		"path":     "flows/refund.md",
		"base_rev": hash,
		"edits":    []any{map[string]any{"old": "# Refund", "new": "# Refund flow"}},
	})
	if patchOut["ok"] != true {
		t.Fatalf("patch failed: %v", patchOut)
	}
	patchedRead := call("docs_read", map[string]any{"path": "flows/refund.md"})
	moveOut := call("docs_move", map[string]any{
		"path": "flows/refund.md", "new_path": "flows/refund-v2.md", "base_rev": patchedRead["hash"],
	})
	if moveOut["ok"] != true || moveOut["moved_to"] != "projects/acme/api/flows/refund-v2.md" {
		t.Fatalf("move failed: %v", moveOut)
	}
	listOut = call("docs_list", map[string]any{"scope": "project"})
	if docs, _ := listOut["docs"].([]any); !hasDocState(docs, "projects/acme/api/flows/refund-v2.md", "added") {
		t.Fatalf("pending doc missing from list: %v", listOut["docs"])
	}
	searchOut := call("docs_search", map[string]any{"query": "Refund flow"})
	hits, _ := searchOut["hits"].([]any)
	if !hasHitState(hits, "projects/acme/api/flows/refund-v2.md", "added") {
		t.Fatalf("pending doc missing from search: %v", searchOut["hits"])
	}
	firstHit, _ := hits[0].(map[string]any)
	if firstHit["snippet"] == "" || firstHit["matched_terms"] == nil || firstHit["why"] == "" {
		t.Fatalf("search hit lacks BM25 metadata: %v", firstHit)
	}
	searchPage := call("docs_search", map[string]any{"query": "flow", "limit": 1})
	searchPageHits, _ := searchPage["hits"].([]any)
	if len(searchPageHits) != 1 || searchPage["next_cursor"] == "" {
		t.Fatalf("search pagination failed: %v", searchPage)
	}
	searchPage2 := call("docs_search", map[string]any{"query": "flow", "limit": 1, "cursor": searchPage["next_cursor"]})
	searchPage2Hits, _ := searchPage2["hits"].([]any)
	if len(searchPage2Hits) != 1 || searchPageHits[0].(map[string]any)["path"] == searchPage2Hits[0].(map[string]any)["path"] {
		t.Fatalf("search pagination repeated or dropped a hit: %v / %v", searchPageHits, searchPage2Hits)
	}
	legacyRead := call("docs_read", map[string]any{"path": "flows/legacy.md"})
	legacyDelete := call("docs_delete", map[string]any{"path": "flows/legacy.md", "base_rev": legacyRead["hash"]})
	if legacyDelete["ok"] != true {
		t.Fatalf("delete legacy failed: %v", legacyDelete)
	}
	deletedRead := call("docs_read", map[string]any{"path": "flows/legacy.md"})
	if deletedRead["deleted"] != true || deletedRead["state"] != "deleted" {
		t.Fatalf("deleted doc was not represented: %v", deletedRead)
	}
	listOut = call("docs_list", map[string]any{"scope": "project"})
	if docs, _ := listOut["docs"].([]any); hasPath(docs, "projects/acme/api/flows/legacy.md") {
		t.Fatalf("deleted doc still listed: %v", docs)
	}
	searchOut = call("docs_search", map[string]any{"query": "Legacy flow"})
	if hits, _ := searchOut["hits"].([]any); hasPath(hits, "projects/acme/api/flows/legacy.md") {
		t.Fatalf("deleted doc still searchable: %v", hits)
	}
	ctxOut = call("docs_get_context", map[string]any{})
	if docs, _ := ctxOut["docs"].([]any); hasPath(docs, "projects/acme/api/flows/legacy.md") {
		t.Fatalf("deleted doc still in context: %v", docs)
	}
	_ = call("docs_write", map[string]any{
		"path":    "runbooks/temp.md",
		"content": "---\nid: temp\nkind: runbook\ntitle: Temp\nstatus: approved\nowners: [tester]\nlast_reviewed: 2026-01-01\n---\n# Temp\n",
	})
	tempRead := call("docs_read", map[string]any{"path": "runbooks/temp.md"})
	deleteOut := call("docs_delete", map[string]any{"path": "runbooks/temp.md", "base_rev": tempRead["hash"]})
	if deleteOut["ok"] != true {
		t.Fatalf("delete failed: %v", deleteOut)
	}
	statusOut := call("docs_change_status", map[string]any{})
	if statusOut["frozen"] != false {
		t.Fatalf("unexpected frozen state: %v", statusOut)
	}
	syncOut := call("docs_change_sync", map[string]any{})
	if syncOut["ok"] != true {
		t.Fatalf("sync failed: %v", syncOut)
	}
	decisionOut := call("docs_record_decision", map[string]any{
		"title": "Use an outbox", "context": "Refund events must not be lost.",
		"decision": "Write events to an outbox first.", "consequences": "Adds a relay worker.",
		"scope": "org",
	})
	if decisionOut["ok"] != true {
		t.Fatalf("record decision failed: %v", decisionOut)
	}
	decisionAgain := call("docs_record_decision", map[string]any{
		"title": "Use an outbox", "context": "Refund events must not be lost.",
		"decision": "Write events to an outbox first.", "consequences": "Adds a relay worker.",
		"scope": "org",
	})
	if decisionAgain["existing"] != true || decisionAgain["path"] != decisionOut["path"] {
		t.Fatalf("decision was not idempotent: %v", decisionAgain)
	}
	patternOut := call("docs_propose_pattern", map[string]any{
		"title": "Idempotent consumer", "when_to_use": "When a queue can redeliver.",
		"rules": []any{"Deduplicate by message id."}, "category": "messaging",
	})
	if patternOut["ok"] != true {
		t.Fatalf("propose pattern failed: %v", patternOut)
	}
	patternAgain := call("docs_propose_pattern", map[string]any{
		"title": "Idempotent consumer", "when_to_use": "When a queue can redeliver.",
		"rules": []any{"Deduplicate by message id."}, "category": "messaging",
	})
	if patternAgain["existing"] != true || patternAgain["path"] != patternOut["path"] {
		t.Fatalf("pattern was not idempotent: %v", patternAgain)
	}
	scaffoldOut := call("docs_scaffold_project", map[string]any{"project": "acme/new-service"})
	if scaffoldOut["ok"] != true {
		t.Fatalf("scaffold failed: %v", scaffoldOut)
	}
	scaffoldAgain := call("docs_scaffold_project", map[string]any{"project": "acme/new-service"})
	if scaffoldAgain["existing"] != true {
		t.Fatalf("scaffold was not idempotent: %v", scaffoldAgain)
	}
	registryOut := call("docs_set_context", map[string]any{
		"field": "team", "value": "payments", "scope": "registry", "project": "acme/api",
	})
	if registryOut["ok"] != true {
		t.Fatalf("set registry failed: %v", registryOut)
	}
}

func hasPath(items []any, want string) bool {
	for _, item := range items {
		m, ok := item.(map[string]any)
		if ok && m["path"] == want {
			return true
		}
	}
	return false
}

func hasDocState(items []any, want, state string) bool {
	for _, item := range items {
		m, ok := item.(map[string]any)
		if ok && m["path"] == want && m["state"] == state {
			return true
		}
	}
	return false
}

func hasHitState(items []any, want, state string) bool {
	return hasDocState(items, want, state)
}

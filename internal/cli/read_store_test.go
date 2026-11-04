package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStoreCheckout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	write("org/patterns/sqs.md", `---
id: sqs-consumer
kind: pattern
title: SQS consumer
status: approved
topics: [sqs, queue, consumer, messaging]
languages: [go]
owners: [payments]
last_reviewed: 2026-10-26
---

# SQS consumer

Use a dead-letter queue, idempotency keys and bounded concurrency.

## Rules

- MUST configure a dead-letter queue.
- MUST make the handler idempotent.
`)
	return root
}

// captureStdout runs fn with os.Stdout redirected. The CLI prints through
// fmt/os.Stdout, so this is the least invasive way to assert on command output.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	closeErr := w.Close()
	os.Stdout = old
	out, readErr := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("command failed: %v\n%s", runErr, out)
	}
	if closeErr != nil {
		t.Fatalf("close pipe: %v", closeErr)
	}
	if readErr != nil {
		t.Fatalf("read pipe: %v", readErr)
	}
	return string(out)
}

func TestGuidanceReadsExplicitStoreWithoutHome(t *testing.T) {
	storeRoot := testStoreCheckout(t)
	t.Setenv("COMPANION_HOME", t.TempDir())
	t.Setenv("COMPANION_PROJECT", "env/leak")
	a := &App{Cwd: t.TempDir(), Project: "acme/api", Languages: []string{"go"}, JSON: true}
	cmd := a.guidanceCmd()
	cmd.SetArgs([]string{"--store", storeRoot, "implement an SQS consumer in Go"})

	out := captureStdout(t, cmd.Execute)
	var got []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode guidance: %v\n%s", err, out)
	}
	if len(got) == 0 {
		t.Fatalf("no guidance results\n%s", out)
	}
	if got[0].Path != "org/patterns/sqs.md" {
		t.Fatalf("first result = %q, want org/patterns/sqs.md", got[0].Path)
	}
}

func TestGuidanceFullReadsExplicitStore(t *testing.T) {
	storeRoot := testStoreCheckout(t)
	t.Setenv("COMPANION_HOME", t.TempDir())
	a := &App{Cwd: t.TempDir(), Project: "acme/api", Languages: []string{"go"}}
	cmd := a.guidanceCmd()
	cmd.SetArgs([]string{"--store", storeRoot, "--full", "implement an SQS consumer in Go"})

	out := captureStdout(t, cmd.Execute)
	if !strings.Contains(out, "MUST configure a dead-letter queue") {
		t.Fatalf("full output did not include the document body:\n%s", out)
	}
}

func TestContextReadsExplicitStoreWithoutHome(t *testing.T) {
	storeRoot := testStoreCheckout(t)
	t.Setenv("COMPANION_HOME", t.TempDir())
	a := &App{Cwd: t.TempDir(), Project: "acme/api", Team: "payments", JSON: true}
	cmd := a.contextCmd()
	cmd.SetArgs([]string{"--store", storeRoot})

	out := captureStdout(t, cmd.Execute)
	var got struct {
		Project string            `json:"project"`
		Team    string            `json:"team"`
		Sources map[string]string `json:"sources"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode context: %v\n%s", err, out)
	}
	if got.Project != "acme/api" || got.Team != "payments" {
		t.Fatalf("context = %q/%q, want acme/api/payments", got.Project, got.Team)
	}
	if got.Sources["project"] != "flag" || got.Sources["team"] != "flag" {
		t.Fatalf("sources = %v, want flag for project and team", got.Sources)
	}
}

func TestExplicitStoreIgnoresEnvironment(t *testing.T) {
	storeRoot := testStoreCheckout(t)
	t.Setenv("COMPANION_PROJECT", "env/leak")
	t.Setenv("COMPANION_TEAM", "env-team")
	a := &App{Cwd: t.TempDir()}

	es, err := a.explicitStore(storeRoot)
	if err != nil {
		t.Fatalf("explicitStore: %v", err)
	}
	if es.Ctx.Project == "env/leak" {
		t.Fatalf("project came from the environment: %q", es.Ctx.Project)
	}
	if es.Ctx.Team == "env-team" {
		t.Fatalf("team came from the environment: %q", es.Ctx.Team)
	}
}

func TestExplicitStoreRequiresDirectory(t *testing.T) {
	a := &App{Cwd: t.TempDir()}

	if _, err := a.explicitStore(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing store path was accepted")
	}

	file := filepath.Join(t.TempDir(), "store.md")
	if err := os.WriteFile(file, []byte("# not a store"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := a.explicitStore(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file store path error = %v, want not a directory", err)
	}
}

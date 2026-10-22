package importdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanBuildsMigrationReport(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# Acme API\n\nThe service that handles payments.\n")
	write("architecture.md", "# Architecture\n\n## Components\n\nAPI and database.\n")
	write("adr/2026-01-01-use-outbox.md", "# Use an outbox\n\n## Context\n\nReliable events.\n\n## Decision\n\nUse an outbox.\n")
	write("adr/no-date.md", "# No date\n\n## Context\n\nDecision without a date.\n")
	write("flows/refund.md", "# Refund\n\n## Trigger\n\nA refund request.\n")
	write("runbooks/deploy.md", "# Deploy\n\n## Symptoms\n\nDeployment failure.\n")
	write("patterns/messaging/sqs.md", "# SQS consumer\n\n## When to use\n\nQueues.\n")
	write("standards/errors.md", "# Errors\n\n## Rule\n\nReturn structured errors.\n")
	write("decisions.md", "# Decisions\n\n## One\n\n## Two\n")
	write("nested/README.md", "# Nested\n")

	report, err := Scan(root, "projects/acme/api", Options{
		Owners: []string{"payments"}, Status: "approved",
		Now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		Mapping: map[string]string{
			"adr/no-date.md": "projects/acme/api/adr/2026-02-02-mapped-decision.md",
		},
		Existing: map[string]bool{
			"projects/acme/api/flows/refund.md": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready < 5 {
		t.Fatalf("ready = %d, report = %+v", report.Ready, report)
	}
	bySource := map[string]Entry{}
	for _, e := range report.Entries {
		bySource[e.Source] = e
	}
	if e := bySource["README.md"]; e.Status != StatusReady || e.Destination != "projects/acme/api/README.md" || !strings.Contains(e.Content, "service that handles payments") {
		t.Fatalf("README = %+v", e)
	}
	if e := bySource["patterns/messaging/sqs.md"]; e.Destination != "projects/acme/api/patterns/messaging/sqs.md" {
		t.Fatalf("pattern = %+v", e)
	}
	if e := bySource["adr/no-date.md"]; e.Status != StatusReady || e.Destination != "projects/acme/api/adr/2026-02-02-mapped-decision.md" {
		t.Fatalf("mapped ADR = %+v", e)
	}
	if e := bySource["decisions.md"]; e.Status != StatusUnresolved {
		t.Fatalf("aggregate should be unresolved: %+v", e)
	}
	if e := bySource["nested/README.md"]; e.Status != StatusUnresolved {
		t.Fatalf("nested README should be unresolved: %+v", e)
	}
	if e := bySource["flows/refund.md"]; e.Status != StatusCollision {
		t.Fatalf("existing destination should collide: %+v", e)
	}
	if report.Unresolved < 2 || report.Collisions < 1 {
		t.Fatalf("counts = ready=%d unresolved=%d collisions=%d", report.Ready, report.Unresolved, report.Collisions)
	}
	if !strings.Contains(Format(report), "import report:") {
		t.Fatal("Format did not render a report")
	}
}

func TestScanMapsUnresolvedByDestinationKind(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "misc.md")
	if err := os.WriteFile(p, []byte("# Misc\n\nA decision.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(root, "org", Options{
		Mapping: map[string]string{"misc.md": "org/adr/2026-01-01-misc.md"},
		Owners:  []string{"platform"},
		Now:     time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != StatusReady || report.Entries[0].Kind != "adr" {
		t.Fatalf("entry = %+v", report.Entries)
	}
}

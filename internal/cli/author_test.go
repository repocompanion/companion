package cli

import "testing"

func TestInferTemplate(t *testing.T) {
	cases := map[string]string{
		"projects/acme/api/adr/2026-01-01-test.md": "adr",
		"projects/acme/api/flows/refund.md":        "flow",
		"projects/acme/api/runbooks/restore.md":    "runbook",
		"projects/acme/api/standards/errors.md":    "standard",
		"projects/acme/api/README.md":              "readme",
		"projects/acme/api/architecture.md":        "architecture",
		"projects/acme/api/patterns/retries.md":    "pattern",
	}
	for rel, want := range cases {
		if got := inferTemplate(rel); got != want {
			t.Errorf("inferTemplate(%q) = %q, want %q", rel, got, want)
		}
	}
}

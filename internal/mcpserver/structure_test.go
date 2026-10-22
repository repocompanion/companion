package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bolacha/docsync/internal/lint"
)

func TestAgentValidationMatchesLint(t *testing.T) {
	cases := []struct {
		name    string
		rel     string
		content string
		valid   bool
	}{
		{
			name: "canonical flow",
			rel:  "projects/acme/api/flows/refund.md",
			content: `---
id: refund
kind: flow
title: Refund
status: approved
owners: [payments]
last_reviewed: 2026-01-01
---
# Refund
`,
			valid: true,
		},
		{
			name: "flat ADR",
			rel:  "projects/acme/api/decisions.md",
			content: `---
id: decisions
kind: adr
title: Decisions
status: approved
owners: [payments]
last_reviewed: 2026-01-01
---
# Decisions
`,
		},
		{
			name: "missing owners",
			rel:  "projects/acme/api/flows/refund.md",
			content: `---
id: refund
kind: flow
title: Refund
status: approved
last_reviewed: 2026-01-01
---
# Refund
`,
		},
		{
			name: "unknown status",
			rel:  "projects/acme/api/flows/refund.md",
			content: `---
id: refund
kind: flow
title: Refund
status: draft
owners: [payments]
last_reviewed: 2026-01-01
---
# Refund
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mcpErr := validateAgentDoc(tc.rel, tc.content)

			root := t.TempDir()
			full := filepath.Join(root, filepath.FromSlash(tc.rel))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			res, err := lint.RunFiles(root, []string{tc.rel})
			if err != nil {
				t.Fatal(err)
			}
			lintOK := true
			for _, issue := range res.Issues {
				if issue.Level == "error" {
					lintOK = false
					break
				}
			}
			if (mcpErr == nil) != lintOK {
				t.Fatalf("MCP valid=%v, lint valid=%v (mcp err=%v, issues=%+v)", mcpErr == nil, lintOK, mcpErr, res.Issues)
			}
			if tc.valid != (mcpErr == nil) {
				t.Fatalf("valid=%v, MCP err=%v", tc.valid, mcpErr)
			}
		})
	}
}

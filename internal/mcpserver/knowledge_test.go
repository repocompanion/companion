package mcpserver

import (
	"strings"
	"testing"
)

func TestValidateMCPUserFieldAllowsContextOnly(t *testing.T) {
	allowed := []string{"project", "team", "domain", "languages", "machine", "user.default_team"}
	for _, field := range allowed {
		if err := validateMCPUserField(field); err != nil {
			t.Errorf("field %q should be editable through MCP: %v", field, err)
		}
	}

	denied := []string{
		"remote",
		"forge.type",
		"forge.token",
		"forge.api_url",
		"changesets.branch_prefix",
		"mcp.org_write",
		"user.name",
		"user.email",
		"",
	}
	for _, field := range denied {
		err := validateMCPUserField(field)
		if err == nil {
			t.Errorf("field %q must not be editable through MCP", field)
			continue
		}
		if !strings.Contains(err.Error(), "companion config set") {
			t.Errorf("field %q error should point at the CLI, got %v", field, err)
		}
	}
}

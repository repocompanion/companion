package mcpserver

import (
	"errors"
	"strings"

	"github.com/repocompanion/companion/internal/docrules"
)

// The canonical project structure is deliberately enforced for MCP writes.
// CLI authors can still use templates directly, but agents must not invent a
// flat layout or omit frontmatter.
type structureInfo = docrules.Structure

func canonicalStructure() structureInfo {
	return docrules.CanonicalStructure()
}

func validateAgentDoc(rel, content string) error {
	rel = strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	if !docrules.IsMarkdown(rel) {
		// Mermaid and image content is not frontmatter-validated here.
		return nil
	}
	problems, err := docrules.ValidateMarkdown(rel, content)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return errors.New(problems[0].Message)
	}
	return nil
}

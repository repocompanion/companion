package mcpserver

import "github.com/modelcontextprotocol/go-sdk/mcp"

func boolPtr(v bool) *bool { return &v }

// addTool centralizes tool annotations so clients can distinguish read-only,
// destructive and idempotent operations.
func addTool[In, Out any](s *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	tool.Annotations = annotationsFor(tool.Name)
	mcp.AddTool(s, tool, handler)
}

func annotationsFor(name string) *mcp.ToolAnnotations {
	switch name {
	case "docs_get_context":
		return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(true)}
	case "docs_get_guidance", "docs_list", "docs_search", "docs_read", "docs_history", "docs_diff", "docs_validate_code_refs":
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	case "docs_pull":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(true)}
	case "docs_review_comments":
		return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(true)}
	case "docs_change_start":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false}
	case "docs_change_join":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(true)}
	case "docs_write":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true), IdempotentHint: false}
	case "docs_patch":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false}
	case "docs_move":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false}
	case "docs_delete":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true), IdempotentHint: false}
	case "docs_record_decision", "docs_propose_pattern", "docs_scaffold_project":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(true)}
	case "docs_set_context":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(true)}
	case "docs_change_sync":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false, OpenWorldHint: boolPtr(true)}
	case "docs_change_submit":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: false, OpenWorldHint: boolPtr(true)}
	case "docs_change_reopen":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(true)}
	case "docs_change_status":
		return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(true)}
	default:
		return nil
	}
}

// internal/tools/tool.go
package tools

import (
	"context"
)

// PermissionLevel describes how much trust a tool requires.
type PermissionLevel string

const (
	// PermPublic tools run without any user approval (e.g. GetCurrentTime).
	PermPublic PermissionLevel = "public"
	// PermProtected tools require one-time user approval stored in the DB.
	PermProtected PermissionLevel = "protected"
)

// ToolResult is the structured output of a tool invocation.
type ToolResult struct {
	// Content is the human-readable result returned to the LLM.
	Content string
	// Error is non-nil when the tool failed but execution should continue.
	Error error
}

// Tool is the common interface for all built-in tools.
type Tool interface {
	// Name returns the unique snake_case tool name exposed to the LLM.
	Name() string
	// Description returns a concise description used in LLM prompts.
	Description() string
	// Permission returns the required permission level.
	Permission() PermissionLevel
	// Execute runs the tool and returns a result. A returned error inside
	// ToolResult.Error is non-fatal; the caller should surface it gracefully.
	Execute(ctx context.Context, args map[string]any) ToolResult
}

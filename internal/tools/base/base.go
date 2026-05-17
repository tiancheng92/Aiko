// Package base defines the Tool interface and shared primitive types used by
// all tool sub-packages. It has no internal (aiko/*) dependencies.
package base

import (
	"context"
	"log/slog"

	json "github.com/bytedance/sonic"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// PermissionLevel describes how much trust a tool requires.
type PermissionLevel string

const (
	// PermPublic tools run without any user approval (e.g. GetCurrentTime).
	PermPublic PermissionLevel = "public"
	// PermProtected tools require one-time user approval stored in the DB.
	PermProtected PermissionLevel = "protected"
)

// Tool combines eino's InvokableTool with permission declaration and a stable
// name accessor used by the permission store.
type Tool interface {
	tool.InvokableTool
	// Name returns the stable snake_case name used in permission storage.
	Name() string
	// Permission returns the required permission level.
	Permission() PermissionLevel
}

// EnhancedTool is a tool that may return multimodal (non-text) results.
// It intentionally does NOT embed Tool (which carries the plain-string
// InvokableRun) to avoid a duplicate-method conflict.
type EnhancedTool interface {
	tool.BaseTool
	// Name returns the stable snake_case name used in permission storage.
	Name() string
	// Permission returns the required permission level.
	Permission() PermissionLevel
	InvokableRun(ctx context.Context, arg *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error)
}

// InfoFromSchema builds a *schema.ToolInfo from name, desc and params.
func InfoFromSchema(name, desc string, params map[string]*schema.ParameterInfo) *schema.ToolInfo {
	return &schema.ToolInfo{
		Name:        name,
		Desc:        desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}
}

// ParseArgs unmarshals the JSON input string into a map, returning an empty map on failure.
func ParseArgs(input string) map[string]any {
	args := map[string]any{}
	if input == "" || input == "{}" {
		return args
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		slog.Warn("tool: unmarshal args", "err", err)
	}
	return args
}

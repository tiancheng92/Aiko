// internal/tools/tool.go
package tools

import (
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

// infoFromSchema is a helper to build a *schema.ToolInfo from name, desc and params.
func infoFromSchema(name, desc string, params map[string]*schema.ParameterInfo) *schema.ToolInfo {
	return &schema.ToolInfo{
		Name:        name,
		Desc:        desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}
}

// parseArgs unmarshals the JSON input string into a map, returning an empty map on failure.
func parseArgs(input string) map[string]any {
	args := map[string]any{}
	if input == "" || input == "{}" {
		return args
	}
	_ = json.Unmarshal([]byte(input), &args)
	return args
}

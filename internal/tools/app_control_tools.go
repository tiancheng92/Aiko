// internal/tools/app_control_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// ListRunningAppsTool returns the names of all visible running applications.
type ListRunningAppsTool struct{}

// Name returns the tool identifier.
func (t *ListRunningAppsTool) Name() string { return "list_running_apps" }

// Permission declares this tool as public (read-only, non-destructive).
func (t *ListRunningAppsTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *ListRunningAppsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.Name(),
		Desc:        "列出当前正在运行的所有可见应用程序名称。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// ControlAppTool opens, activates, or quits a named application.
type ControlAppTool struct{}

// Name returns the tool identifier.
func (t *ControlAppTool) Name() string { return "control_app" }

// Permission declares this tool as protected.
func (t *ControlAppTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ControlAppTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"控制 macOS 应用程序：打开并激活、仅激活到前台、或退出。",
		map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "操作类型：open（打开并激活）、activate（仅前台激活）、quit（退出）",
				Required: true,
				Enum:     []string{"open", "activate", "quit"},
			},
			"app_name": {
				Type:     schema.String,
				Desc:     "应用程序名称，如 \"Safari\"、\"Spotify\"、\"Finder\"",
				Required: true,
			},
		},
	), nil
}

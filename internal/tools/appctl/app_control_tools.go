// internal/tools/appctl/app_control_tools.go
package appctl

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/tools/base"
)

// ListRunningAppsTool returns the names of all visible running applications.
type ListRunningAppsTool struct{}

// Name returns the tool identifier.
func (t *ListRunningAppsTool) Name() string { return "list_running_apps" }

// Permission declares this tool as public (read-only, non-destructive).
func (t *ListRunningAppsTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns eino tool metadata.
func (t *ListRunningAppsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.Name(),
		Desc:        "列出当前正在运行的所有可见应用程序名称。在使用 control_app 前可先调用此工具获取准确的应用名称。仅支持 macOS。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// ControlAppTool opens, activates, or quits a named application.
type ControlAppTool struct{}

// Name returns the tool identifier.
func (t *ControlAppTool) Name() string { return "control_app" }

// Permission declares this tool as protected.
func (t *ControlAppTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *ControlAppTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(),
		"控制 macOS 应用程序：打开并激活、仅激活到前台、或退出。仅支持 macOS。",
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

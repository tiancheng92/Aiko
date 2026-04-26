// internal/tools/code_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

// CodeConfirmInfo is the interrupt payload sent to the frontend for user confirmation.
type CodeConfirmInfo struct {
	ID         string `json:"id"`
	Language   string `json:"language"`
	Code       string `json:"code"`
	WorkingDir string `json:"working_dir"`
}

// ExecuteCodeTool runs a code snippet using the system interpreter after user confirmation.
type ExecuteCodeTool struct {
	Cfg           *config.Config
	RegisterCmd   func(id string, cancel func())
	UnregisterCmd func(id string)
}

// Name returns the tool identifier.
func (t *ExecuteCodeTool) Name() string { return "execute_code" }

// Permission declares this tool as protected.
func (t *ExecuteCodeTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ExecuteCodeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "执行代码片段（python/node/ruby/bash，需要用户二次确认）。",
		map[string]*schema.ParameterInfo{
			"language":    {Type: schema.String, Desc: "编程语言：python | node | ruby | bash", Required: true},
			"code":        {Type: schema.String, Desc: "要执行的代码内容", Required: true},
			"working_dir": {Type: schema.String, Desc: "工作目录（可选，默认为用户主目录）", Required: false},
		},
	), nil
}

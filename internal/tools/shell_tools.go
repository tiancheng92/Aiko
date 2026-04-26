// internal/tools/shell_tools.go
package tools

import (
	"context"
	"encoding/gob"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

func init() {
	gob.Register(ShellConfirmInfo{})
	gob.Register(ConfirmResult{})
}

// ShellConfirmInfo is the interrupt payload sent to the frontend for user confirmation.
type ShellConfirmInfo struct {
	ID         string `json:"id"`
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
}

// ConfirmResult is passed as resume data from ConfirmToolExecution to the tool.
type ConfirmResult struct {
	Approved      bool   `json:"approved"`
	EditedContent string `json:"edited_content"` // user-edited command or code
}

// ExecuteShellTool runs a shell command after user confirmation via eino interrupt.
type ExecuteShellTool struct {
	Cfg           *config.Config
	RegisterCmd   func(id string, cancel func()) // called when cmd starts
	UnregisterCmd func(id string)                // called on completion
}

// Name returns the tool identifier.
func (t *ExecuteShellTool) Name() string { return "execute_shell" }

// Permission declares this tool as protected.
func (t *ExecuteShellTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ExecuteShellTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "执行 Shell 命令（需要用户二次确认）。",
		map[string]*schema.ParameterInfo{
			"command":     {Type: schema.String, Desc: "要执行的 Shell 命令", Required: true},
			"working_dir": {Type: schema.String, Desc: "工作目录（可选，默认为用户主目录）", Required: false},
		},
	), nil
}

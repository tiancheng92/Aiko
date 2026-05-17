// internal/tools/exec/shell_tools.go
package exec

import (
	"context"
	"encoding/gob"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
	"aiko/internal/tools/base"
)

func init() {
	gob.Register(base.ShellConfirmInfo{})
	gob.Register(base.ConfirmResult{})
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
func (t *ExecuteShellTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *ExecuteShellTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "在 bash 中执行 Shell 命令，需用户二次确认。适合文件操作、系统管理、调用命令行工具等。多行逻辑代码推荐用 execute_code；执行前请确认命令安全可逆。",
		map[string]*schema.ParameterInfo{
			"command":     {Type: schema.String, Desc: "要执行的 bash 命令，支持管道、重定向等完整 shell 语法", Required: true},
			"working_dir": {Type: schema.String, Desc: "工作目录路径（可选，默认为用户主目录）；仅限白名单路径", Required: false},
		},
	), nil
}

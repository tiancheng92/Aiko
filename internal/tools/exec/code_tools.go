// internal/tools/exec/code_tools.go
package exec

import (
	"context"
	"encoding/gob"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
	"aiko/internal/tools/base"
)

func init() {
	gob.Register(base.CodeConfirmInfo{})
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
func (t *ExecuteCodeTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *ExecuteCodeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "执行多行代码片段，需用户二次确认。适合数据处理、计算、脚本任务等需要标准库的场景。简单命令行操作用 execute_shell 即可。",
		map[string]*schema.ParameterInfo{
			"language":    {Type: schema.String, Desc: "编程语言：python | node | ruby | bash", Required: true},
			"code":        {Type: schema.String, Desc: "要执行的完整代码", Required: true},
			"working_dir": {Type: schema.String, Desc: "工作目录路径（可选，默认为用户主目录）；仅限白名单路径", Required: false},
		},
	), nil
}

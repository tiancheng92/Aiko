// internal/tools/window_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// GetActiveWindowInfoTool returns the frontmost app name, window title,
// and any currently selected text on macOS.
type GetActiveWindowInfoTool struct{}

// Name returns the tool identifier.
func (t *GetActiveWindowInfoTool) Name() string { return "get_active_window_info" }

// Permission declares this tool as public.
func (t *GetActiveWindowInfoTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetActiveWindowInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"获取当前前台应用的名称、窗口标题和选中文字。选中文字通过模拟 ⌘C 读取，如无选中内容则返回空字符串。需要「辅助功能」权限。",
		nil,
	), nil
}

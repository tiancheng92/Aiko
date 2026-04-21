// internal/tools/time_tools.go
package tools

import (
	"context"
	"fmt"
	"time"
)

// GetCurrentTimeTool returns the current local time.
type GetCurrentTimeTool struct{}

func (t *GetCurrentTimeTool) Name() string             { return "get_current_time" }
func (t *GetCurrentTimeTool) Description() string      { return "获取当前本地时间和日期" }
func (t *GetCurrentTimeTool) Permission() PermissionLevel { return PermPublic }

func (t *GetCurrentTimeTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	now := time.Now()
	return ToolResult{
		Content: fmt.Sprintf("当前时间: %s (时区: %s)",
			now.Format("2006-01-02 15:04:05"),
			now.Location().String(),
		),
	}
}

// GetTimezoneTool returns the system timezone.
type GetTimezoneTool struct{}

func (t *GetTimezoneTool) Name() string             { return "get_timezone" }
func (t *GetTimezoneTool) Description() string      { return "获取系统当前时区信息" }
func (t *GetTimezoneTool) Permission() PermissionLevel { return PermPublic }

func (t *GetTimezoneTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	name, offset := time.Now().Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	return ToolResult{
		Content: fmt.Sprintf("时区: %s (UTC%+03d:%02d)", name, hours, minutes),
	}
}

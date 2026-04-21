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

// FormatTimeTool formats the current time using a Go time layout string.
type FormatTimeTool struct{}

func (t *FormatTimeTool) Name() string        { return "format_time" }
func (t *FormatTimeTool) Description() string {
	return `将当前时间按指定格式输出。参数 JSON: {"layout":"<Go time layout>"}，默认格式为 RFC3339。` +
		`常用格式示例: "2006-01-02 15:04:05" (本地), "Monday, 02 Jan 2006" (英文日期)。`
}
func (t *FormatTimeTool) Permission() PermissionLevel { return PermPublic }

// Execute formats time.Now() using the optional "layout" arg (Go time layout string).
func (t *FormatTimeTool) Execute(_ context.Context, args map[string]any) ToolResult {
	layout := time.RFC3339
	if l, ok := args["layout"].(string); ok && l != "" {
		layout = l
	}
	return ToolResult{Content: fmt.Sprintf("格式化后的时间: %s", time.Now().Format(layout))}
}

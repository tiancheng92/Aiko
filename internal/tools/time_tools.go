// internal/tools/time_tools.go
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetCurrentTimeTool returns the current local time.
type GetCurrentTimeTool struct{}

func (t *GetCurrentTimeTool) Name() string             { return "get_current_time" }
func (t *GetCurrentTimeTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for get_current_time.
func (t *GetCurrentTimeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取当前本地时间和日期", nil), nil
}

// InvokableRun returns the current local time as a string.
func (t *GetCurrentTimeTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	now := time.Now()
	return fmt.Sprintf("当前时间: %s (时区: %s)",
		now.Format("2006-01-02 15:04:05"), now.Location().String()), nil
}

// GetTimezoneTool returns the system timezone.
type GetTimezoneTool struct{}

func (t *GetTimezoneTool) Name() string             { return "get_timezone" }
func (t *GetTimezoneTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for get_timezone.
func (t *GetTimezoneTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取系统当前时区信息", nil), nil
}

// InvokableRun returns the system timezone name and UTC offset.
func (t *GetTimezoneTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	name, offset := time.Now().Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	return fmt.Sprintf("时区: %s (UTC%+03d:%02d)", name, hours, minutes), nil
}

// FormatTimeTool formats the current time using a Go time layout string.
type FormatTimeTool struct{}

func (t *FormatTimeTool) Name() string             { return "format_time" }
func (t *FormatTimeTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for format_time.
func (t *FormatTimeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"将当前时间按指定 Go layout 格式输出。默认 RFC3339。",
		map[string]*schema.ParameterInfo{
			"layout": {
				Type: schema.String,
				Desc: `Go time layout 字符串，例如 "2006-01-02 15:04:05" 或 "Monday, 02 Jan 2006"`,
			},
		},
	), nil
}

// InvokableRun formats time.Now() using the optional layout argument.
func (t *FormatTimeTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	layout := time.RFC3339
	if l, ok := args["layout"].(string); ok && l != "" {
		layout = l
	}
	return fmt.Sprintf("格式化后的时间: %s", time.Now().Format(layout)), nil
}

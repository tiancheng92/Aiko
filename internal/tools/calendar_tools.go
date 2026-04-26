// internal/tools/calendar_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// GetCalendarEventsTool retrieves events from macOS Calendar within a date range.
type GetCalendarEventsTool struct{}

// Name returns the tool identifier.
func (t *GetCalendarEventsTool) Name() string { return "get_calendar_events" }

// Permission declares this tool as public.
func (t *GetCalendarEventsTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetCalendarEventsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"查询 macOS 日历中指定日期范围内的事件。返回事件列表，包含标题、开始/结束时间、地点和备注。",
		map[string]*schema.ParameterInfo{
			"start_date": {
				Desc:     "开始日期，格式 YYYY-MM-DD（必填）",
				Required: true,
				Type:     schema.String,
			},
			"end_date": {
				Desc:     "结束日期，格式 YYYY-MM-DD（必填）",
				Required: true,
				Type:     schema.String,
			},
			"calendar_name": {
				Desc:     "日历名称（可选）。留空则查询所有日历。",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}

// CreateCalendarEventTool creates a new event in macOS Calendar.
type CreateCalendarEventTool struct{}

// Name returns the tool identifier.
func (t *CreateCalendarEventTool) Name() string { return "create_calendar_event" }

// Permission declares this tool as protected (modifies user data).
func (t *CreateCalendarEventTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *CreateCalendarEventTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"在 macOS 日历中创建新事件。",
		map[string]*schema.ParameterInfo{
			"title": {
				Desc:     "事件标题（必填）",
				Required: true,
				Type:     schema.String,
			},
			"start_time": {
				Desc:     "开始时间，格式 YYYY-MM-DD HH:MM（必填）",
				Required: true,
				Type:     schema.String,
			},
			"end_time": {
				Desc:     "结束时间，格式 YYYY-MM-DD HH:MM（必填）",
				Required: true,
				Type:     schema.String,
			},
			"calendar_name": {
				Desc:     "目标日历名称（可选）。留空则使用默认日历。",
				Required: false,
				Type:     schema.String,
			},
			"location": {
				Desc:     "地点（可选）",
				Required: false,
				Type:     schema.String,
			},
			"notes": {
				Desc:     "备注（可选）",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}

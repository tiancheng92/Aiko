//go:build !darwin

// internal/tools/calendar_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetCalendarEventsTool is a no-op stub on non-macOS platforms.
type GetCalendarEventsTool struct{}

func (t *GetCalendarEventsTool) Name() string                { return "get_calendar_events" }
func (t *GetCalendarEventsTool) Permission() PermissionLevel { return PermPublic }
func (t *GetCalendarEventsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "查询 macOS 日历事件（仅 macOS 支持）", nil), nil
}
func (t *GetCalendarEventsTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "get_calendar_events 仅支持 macOS", nil
}

// CreateCalendarEventTool is a no-op stub on non-macOS platforms.
type CreateCalendarEventTool struct{}

func (t *CreateCalendarEventTool) Name() string                { return "create_calendar_event" }
func (t *CreateCalendarEventTool) Permission() PermissionLevel { return PermProtected }
func (t *CreateCalendarEventTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "在 macOS 日历中创建事件（仅 macOS 支持）", nil), nil
}
func (t *CreateCalendarEventTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "create_calendar_event 仅支持 macOS", nil
}

//go:build !darwin

// internal/tools/calendar_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
)

// InvokableRun is a stub for GetCalendarEventsTool on non-macOS platforms.
func (t *GetCalendarEventsTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "get_calendar_events 仅支持 macOS", nil
}

// InvokableRun is a stub for CreateCalendarEventTool on non-macOS platforms.
func (t *CreateCalendarEventTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "create_calendar_event 仅支持 macOS", nil
}

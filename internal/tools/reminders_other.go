//go:build !darwin

// internal/tools/reminders_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetRemindersTool is a no-op stub on non-macOS platforms.
type GetRemindersTool struct{}

func (t *GetRemindersTool) Name() string                 { return "get_reminders" }
func (t *GetRemindersTool) Permission() PermissionLevel  { return PermPublic }
func (t *GetRemindersTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取 macOS 提醒事项（仅 macOS 支持）", nil), nil
}
func (t *GetRemindersTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "get_reminders 仅支持 macOS", nil
}

// CompleteReminderTool is a no-op stub on non-macOS platforms.
type CompleteReminderTool struct{}

func (t *CompleteReminderTool) Name() string                 { return "complete_reminder" }
func (t *CompleteReminderTool) Permission() PermissionLevel  { return PermPublic }
func (t *CompleteReminderTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "标记提醒事项为已完成（仅 macOS 支持）", nil), nil
}
func (t *CompleteReminderTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "complete_reminder 仅支持 macOS", nil
}

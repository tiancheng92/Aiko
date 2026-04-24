//go:build !darwin

// internal/tools/mail_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetMailsTool is a no-op stub on non-Darwin platforms.
type GetMailsTool struct{}

// Name returns the tool identifier.
func (t *GetMailsTool) Name() string { return "get_mails" }

// Permission declares this tool as public.
func (t *GetMailsTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetMailsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"获取邮件列表（仅支持 macOS）。",
		map[string]*schema.ParameterInfo{},
	), nil
}

// InvokableRun returns a platform-not-supported message.
func (t *GetMailsTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "此功能仅支持 macOS", nil
}

// GetMailContentTool is a no-op stub on non-Darwin platforms.
type GetMailContentTool struct{}

// Name returns the tool identifier.
func (t *GetMailContentTool) Name() string { return "get_mail_content" }

// Permission declares this tool as public.
func (t *GetMailContentTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetMailContentTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"读取邮件正文（仅支持 macOS）。",
		map[string]*schema.ParameterInfo{},
	), nil
}

// InvokableRun returns a platform-not-supported message.
func (t *GetMailContentTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "此功能仅支持 macOS", nil
}

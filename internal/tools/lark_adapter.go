// internal/tools/lark_adapter.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"desktop-pet/internal/lark"
)

// larkToolAdapter wraps *lark.Tool to satisfy the tools.Tool interface.
type larkToolAdapter struct {
	inner *lark.Tool
}

// WrapLarkTool wraps a *lark.Tool as a tools.Tool.
func WrapLarkTool(t *lark.Tool) Tool {
	return &larkToolAdapter{inner: t}
}

// Name returns the tool identifier.
func (a *larkToolAdapter) Name() string { return a.inner.Name() }

// Permission returns PermProtected — user must explicitly grant lark tool access.
func (a *larkToolAdapter) Permission() PermissionLevel { return PermProtected }

// Info delegates to the inner lark.Tool.
func (a *larkToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return a.inner.Info(ctx)
}

// InvokableRun delegates to the inner lark.Tool.
func (a *larkToolAdapter) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	return a.inner.InvokableRun(ctx, input, opts...)
}

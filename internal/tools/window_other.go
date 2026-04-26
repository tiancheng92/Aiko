//go:build !darwin

// internal/tools/window_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
)

// StartWindowPoller is a no-op on non-macOS platforms.
func StartWindowPoller(_ context.Context) {}

// InvokableRun is a stub for GetActiveWindowInfoTool on non-macOS platforms.
func (t *GetActiveWindowInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "get_active_window_info 仅支持 macOS", nil
}

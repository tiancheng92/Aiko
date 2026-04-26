//go:build !darwin

// internal/tools/app_control_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
)

// InvokableRun is a stub for ListRunningAppsTool on non-macOS platforms.
func (t *ListRunningAppsTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "list_running_apps 仅支持 macOS", nil
}

// InvokableRun is a stub for ControlAppTool on non-macOS platforms.
func (t *ControlAppTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "control_app 仅支持 macOS", nil
}

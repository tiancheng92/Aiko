//go:build !darwin

// internal/tools/clipboard_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
)

// InvokableRun is a stub for non-macOS platforms.
func (t *ReadClipboardTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "read_clipboard 仅支持 macOS", nil
}

// InvokableRun is a stub for non-macOS platforms.
func (t *WriteClipboardTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "write_clipboard 仅支持 macOS", nil
}

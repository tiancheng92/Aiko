//go:build !darwin

// internal/tools/ocr_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
)

// InvokableRun is a stub for OcrScreenTool on non-macOS platforms.
func (t *OcrScreenTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "ocr_screen 仅支持 macOS", nil
}

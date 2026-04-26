//go:build !darwin

package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// InvokableRun is a stub for TakeScreenshotTool on non-macOS platforms.
func (t *TakeScreenshotTool) InvokableRun(_ context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
	return &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{Type: schema.ToolPartTypeText, Text: "take_screenshot 仅支持 macOS"},
		},
	}, nil
}

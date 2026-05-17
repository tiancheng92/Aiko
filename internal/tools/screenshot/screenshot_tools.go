// internal/tools/screenshot/screenshot_tools.go
package screenshot

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/tools/base"
)

// TakeScreenshotTool captures the full screen and returns the image directly
// to the model via eino's EnhancedInvokableTool interface.
type TakeScreenshotTool struct{}

// Name returns the tool identifier.
func (t *TakeScreenshotTool) Name() string { return "take_screenshot" }

// Permission declares this tool as protected.
func (t *TakeScreenshotTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *TakeScreenshotTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.Name(),
		Desc:        "截取当前全屏截图，AI 将直接「看到」屏幕内容并进行分析。仅支持 macOS。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

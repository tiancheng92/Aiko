// internal/tools/ocr_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// OcrScreenTool captures a screen region (or full screen) and returns
// recognized text using macOS Vision framework.
type OcrScreenTool struct{}

// Name returns the tool identifier.
func (t *OcrScreenTool) Name() string { return "ocr_screen" }

// Permission declares this tool as protected (captures screen content).
func (t *OcrScreenTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *OcrScreenTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"截取屏幕区域（或全屏）并使用 macOS Vision 框架进行 OCR 文字识别。支持中文、英文、日文。需要「屏幕录制」权限。",
		map[string]*schema.ParameterInfo{
			"region": {
				Desc:     "截取区域，格式为 \"x,y,width,height\"（CSS 像素，左上角为原点）。省略则截取全屏。",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}

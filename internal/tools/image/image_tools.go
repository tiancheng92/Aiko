// internal/tools/image/image_tools.go
package toolimage

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
	"aiko/internal/tools/base"
)

// ReadImageTool reads a local image file and returns it as a multimodal image
// result so the LLM can visually inspect it.
type ReadImageTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *ReadImageTool) Name() string { return "read_image" }

// Permission declares this tool as protected.
func (t *ReadImageTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *ReadImageTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(),
		"读取本地图片文件（PNG / JPEG / GIF / WebP）并将其内容直接呈现给 AI，让 AI 能够「看到」图片。"+
			"⚠️ 重要：该工具的返回结果会同时在聊天界面中直接展示给用户。"+
			"若需要把图片展示给用户（如截图结果、生成图片、文件预览等），直接调用此工具即可，无需额外说明。"+
			"仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "图片文件的绝对路径", Required: true},
		},
	), nil
}

// SaveImageTool saves a base64-encoded image or downloads a remote image URL
// to a local file.
type SaveImageTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *SaveImageTool) Name() string { return "save_image" }

// Permission declares this tool as protected.
func (t *SaveImageTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *SaveImageTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(),
		"将图片保存到本地文件。source 可以是 http(s):// URL 或 data: URL（base64 编码）。仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"source": {
				Type:     schema.String,
				Desc:     "图片来源：http(s):// URL 或 data:<mime>;base64,<data> 格式的 data URL",
				Required: true,
			},
			"path": {
				Type:     schema.String,
				Desc:     "保存目标的绝对路径（含文件名，例如 /Users/me/Desktop/out.png）",
				Required: true,
			},
		},
	), nil
}

// internal/tools/image_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

// ReadImageTool reads a local image file and returns it as a multimodal image
// result so the LLM can visually inspect it.
type ReadImageTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *ReadImageTool) Name() string { return "read_image" }

// Permission declares this tool as protected.
func (t *ReadImageTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ReadImageTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"读取本地图片文件（PNG / JPEG / GIF / WebP）并将其内容直接呈现给 AI，让 AI 能够「看到」图片。仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "图片文件的绝对路径", Required: true},
		},
	), nil
}

// GenerateImageTool generates an image via the OpenAI images/generations API
// and returns it as a multimodal image result.
type GenerateImageTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *GenerateImageTool) Name() string { return "generate_image" }

// Permission declares this tool as protected.
func (t *GenerateImageTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *GenerateImageTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"根据文字描述调用图像生成 API（OpenAI gpt-image-1，需要 LLM API Key）生成一张图片并直接展示给用户。",
		map[string]*schema.ParameterInfo{
			"prompt": {
				Type:     schema.String,
				Desc:     "图片描述（建议使用英文以获得最佳效果）",
				Required: true,
			},
			"size": {
				Type: schema.String,
				Desc: "图片尺寸：1024x1024（默认）、1536x1024（横向）、1024x1536（纵向）",
			},
		},
	), nil
}

// SaveImageTool saves a base64-encoded image or downloads a remote image URL
// to a local file.
type SaveImageTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *SaveImageTool) Name() string { return "save_image" }

// Permission declares this tool as protected.
func (t *SaveImageTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *SaveImageTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
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

// Package clipboard provides tools to read and write the system clipboard.
package clipboard

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/tools/base"
)

// ReadClipboardTool reads the current clipboard text content.
type ReadClipboardTool struct{}

// Name returns the tool identifier.
func (t *ReadClipboardTool) Name() string { return "read_clipboard" }

// Permission declares this tool as protected.
func (t *ReadClipboardTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *ReadClipboardTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.Name(),
		Desc:        "读取系统剪贴板中的文本内容（仅文本，不支持图片/文件）。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// WriteClipboardTool writes text to the system clipboard.
type WriteClipboardTool struct{}

// Name returns the tool identifier.
func (t *WriteClipboardTool) Name() string { return "write_clipboard" }

// Permission declares this tool as protected.
func (t *WriteClipboardTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *WriteClipboardTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "将文本写入系统剪贴板，用户随后可在任意应用中粘贴。",
		map[string]*schema.ParameterInfo{
			"text": {
				Type:     schema.String,
				Desc:     "要写入剪贴板的文本内容",
				Required: true,
			},
		},
	), nil
}

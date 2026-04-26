// internal/tools/filesystem_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

// ListDirectoryTool lists files and subdirectories at a given path.
type ListDirectoryTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *ListDirectoryTool) Name() string { return "list_directory" }

// Permission declares this tool as protected.
func (t *ListDirectoryTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ListDirectoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "列出指定目录下的文件和子目录。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要列出的目录路径", Required: true},
		},
	), nil
}

// ReadFileTool reads the UTF-8 text content of a file.
type ReadFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *ReadFileTool) Name() string { return "read_file" }

// Permission declares this tool as protected.
func (t *ReadFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ReadFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "读取文件的文本内容（UTF-8）。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "文件路径", Required: true},
		},
	), nil
}

// WriteFileTool writes or appends text content to a file.
type WriteFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *WriteFileTool) Name() string { return "write_file" }

// Permission declares this tool as protected.
func (t *WriteFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *WriteFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "将文本内容写入（或追加到）文件。",
		map[string]*schema.ParameterInfo{
			"path":    {Type: schema.String, Desc: "文件路径", Required: true},
			"content": {Type: schema.String, Desc: "要写入的文本内容", Required: true},
			"append":  {Type: schema.Boolean, Desc: "true 表示追加，false 表示覆盖（默认 false）", Required: false},
		},
	), nil
}

// DeleteFileTool deletes a file at the given path.
type DeleteFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *DeleteFileTool) Name() string { return "delete_file" }

// Permission declares this tool as protected.
func (t *DeleteFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *DeleteFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "删除指定路径的文件。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要删除的文件路径", Required: true},
		},
	), nil
}

// MakeDirectoryTool creates a directory and all necessary parents.
type MakeDirectoryTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *MakeDirectoryTool) Name() string { return "make_directory" }

// Permission declares this tool as protected.
func (t *MakeDirectoryTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *MakeDirectoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "创建目录（包括所有必要的父目录）。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要创建的目录路径", Required: true},
		},
	), nil
}

// MoveFileTool moves or renames a file or directory.
type MoveFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *MoveFileTool) Name() string { return "move_file" }

// Permission declares this tool as protected.
func (t *MoveFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *MoveFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "移动或重命名文件/目录。",
		map[string]*schema.ParameterInfo{
			"source":      {Type: schema.String, Desc: "源路径", Required: true},
			"destination": {Type: schema.String, Desc: "目标路径", Required: true},
		},
	), nil
}

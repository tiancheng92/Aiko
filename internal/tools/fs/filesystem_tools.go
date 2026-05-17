// internal/tools/fs/filesystem_tools.go
package fs

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
	"aiko/internal/tools/base"
)

// ListDirectoryTool lists files and subdirectories at a given path.
type ListDirectoryTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *ListDirectoryTool) Name() string { return "list_directory" }

// Permission declares this tool as protected.
func (t *ListDirectoryTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *ListDirectoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "列出目录下的文件和子目录（含大小、类型、修改时间）。仅限用户授权的白名单路径。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "目录的绝对路径", Required: true},
		},
	), nil
}

// ReadFileTool reads the UTF-8 text content of a file.
type ReadFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *ReadFileTool) Name() string { return "read_file" }

// Permission declares this tool as protected.
func (t *ReadFileTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *ReadFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "读取文件的 UTF-8 文本内容。大文件会截断返回。仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "文件的绝对路径", Required: true},
		},
	), nil
}

// WriteFileTool writes or appends text content to a file.
type WriteFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *WriteFileTool) Name() string { return "write_file" }

// Permission declares this tool as protected.
func (t *WriteFileTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *WriteFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "将文本写入文件（覆盖或追加）；文件不存在时自动创建。仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"path":    {Type: schema.String, Desc: "文件的绝对路径", Required: true},
			"content": {Type: schema.String, Desc: "要写入的文本内容", Required: true},
			"append":  {Type: schema.Boolean, Desc: "true 表示追加到末尾，false 表示覆盖全文（默认 false）", Required: false},
		},
	), nil
}

// DeleteFileTool deletes a file at the given path.
type DeleteFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *DeleteFileTool) Name() string { return "delete_file" }

// Permission declares this tool as protected.
func (t *DeleteFileTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *DeleteFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "永久删除文件（不可恢复，请谨慎操作）。仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要删除的文件绝对路径", Required: true},
		},
	), nil
}

// MakeDirectoryTool creates a directory and all necessary parents.
type MakeDirectoryTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *MakeDirectoryTool) Name() string { return "make_directory" }

// Permission declares this tool as protected.
func (t *MakeDirectoryTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *MakeDirectoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "创建目录（包括所有必要的父目录）。仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要创建的目录绝对路径", Required: true},
		},
	), nil
}

// MoveFileTool moves or renames a file or directory.
type MoveFileTool struct{ Cfg *config.Config }

// Name returns the tool identifier.
func (t *MoveFileTool) Name() string { return "move_file" }

// Permission declares this tool as protected.
func (t *MoveFileTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns eino tool metadata.
func (t *MoveFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "移动或重命名文件/目录（同一路径内改名也用此工具）。仅限白名单路径。",
		map[string]*schema.ParameterInfo{
			"source":      {Type: schema.String, Desc: "源文件或目录的绝对路径", Required: true},
			"destination": {Type: schema.String, Desc: "目标绝对路径（含新文件名）", Required: true},
		},
	), nil
}

// internal/tools/fs/filesystem.go
package fs

import (
	"context"
	json "github.com/bytedance/sonic"
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudwego/eino/components/tool"

	"aiko/internal/tools/base"
)

// InvokableRun lists files and subdirectories at the given path.
func (t *ListDirectoryTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "list_directory 配置缺失，请在设置中完成初始化", nil
	}
	args := base.ParseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := base.CheckPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Sprintf("读取目录失败：%s", err.Error()), nil
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size,omitempty"`
	}
	var result []entry
	for _, e := range entries {
		info, infoErr := e.Info()
		if infoErr != nil {
			slog.Warn("list_directory: DirEntry.Info failed", "path", abs, "name", e.Name(), "err", infoErr)
		}
		var size int64
		if info != nil && !e.IsDir() {
			size = info.Size()
		}
		result = append(result, entry{Name: e.Name(), IsDir: e.IsDir(), Size: size})
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("序列化目录内容失败：%s", err.Error()), nil
	}
	return string(b), nil
}

// InvokableRun reads the text content of a file.
func (t *ReadFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "read_file 配置缺失，请在设置中完成初始化", nil
	}
	args := base.ParseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := base.CheckPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("读取文件失败：%s", err.Error()), nil
	}
	return string(data), nil
}

// InvokableRun writes or appends text to a file.
func (t *WriteFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "write_file 配置缺失，请在设置中完成初始化", nil
	}
	args := base.ParseArgs(input)
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	appendMode, _ := args["append"].(bool)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := base.CheckPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(abs, flag, 0o644)
	if err != nil {
		return fmt.Sprintf("打开文件失败：%s", err.Error()), nil
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return fmt.Sprintf("写入文件失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已写入 %d 字节到 %s", len(content), abs), nil
}

// InvokableRun deletes a file at the given path.
func (t *DeleteFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "delete_file 配置缺失，请在设置中完成初始化", nil
	}
	args := base.ParseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := base.CheckPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Sprintf("删除文件失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已删除 %s", abs), nil
}

// InvokableRun creates a directory and all necessary parents.
func (t *MakeDirectoryTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "make_directory 配置缺失，请在设置中完成初始化", nil
	}
	args := base.ParseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := base.CheckPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Sprintf("创建目录失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已创建目录 %s", abs), nil
}

// InvokableRun moves or renames a file or directory.
func (t *MoveFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "move_file 配置缺失，请在设置中完成初始化", nil
	}
	args := base.ParseArgs(input)
	src, _ := args["source"].(string)
	dst, _ := args["destination"].(string)
	if src == "" || dst == "" {
		return "请提供 source 和 destination 参数", nil
	}
	absSrc, err := base.CheckPath(src, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	absDst, err := base.CheckPath(dst, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.Rename(absSrc, absDst); err != nil {
		return fmt.Sprintf("移动失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已将 %s 移动到 %s", absSrc, absDst), nil
}

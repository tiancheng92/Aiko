// internal/tools/filesystem.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// isPathAllowed reports whether absTarget is inside at least one of the allowed paths.
func isPathAllowed(absTarget string, allowedPaths []string) bool {
	for _, allowed := range allowedPaths {
		abs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absTarget, abs+string(filepath.Separator)) || absTarget == abs {
			return true
		}
	}
	return false
}

// checkPath resolves path to an absolute path and verifies it is within the whitelist.
// Returns the resolved absolute path and nil on success, or an empty string and a
// descriptive error on failure.
func checkPath(path string, allowedPaths []string) (string, error) {
	if len(allowedPaths) == 0 {
		return "", fmt.Errorf("文件系统访问已禁用，请在设置 → 工具设置中添加允许访问的路径")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("无效路径 %q: %w", path, err)
	}
	if !isPathAllowed(abs, allowedPaths) {
		return "", fmt.Errorf("路径 %q 不在允许列表中，请在设置 → 工具设置中添加该路径", abs)
	}
	return abs, nil
}

// InvokableRun lists files and subdirectories at the given path.
func (t *ListDirectoryTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
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
		info, _ := e.Info()
		var size int64
		if info != nil && !e.IsDir() {
			size = info.Size()
		}
		result = append(result, entry{Name: e.Name(), IsDir: e.IsDir(), Size: size})
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

// InvokableRun reads the text content of a file.
func (t *ReadFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
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
	args := parseArgs(input)
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	appendMode, _ := args["append"].(bool)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
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
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
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
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
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
	args := parseArgs(input)
	src, _ := args["source"].(string)
	dst, _ := args["destination"].(string)
	if src == "" || dst == "" {
		return "请提供 source 和 destination 参数", nil
	}
	absSrc, err := checkPath(src, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	absDst, err := checkPath(dst, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.Rename(absSrc, absDst); err != nil {
		return fmt.Sprintf("移动失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已将 %s 移动到 %s", absSrc, absDst), nil
}

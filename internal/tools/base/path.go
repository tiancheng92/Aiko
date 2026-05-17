package base

import (
	"fmt"
	"path/filepath"
	"strings"
)

// IsPathAllowed reports whether absTarget is inside at least one of the allowed paths.
// Allowed paths may contain glob patterns (e.g. /Users/me/projects/*); in that case
// filepath.Match is used against the target path and each of its parent directories.
func IsPathAllowed(absTarget string, allowedPaths []string) bool {
	for _, allowed := range allowedPaths {
		// If the pattern contains no glob metacharacters, use prefix matching.
		if !strings.ContainsAny(allowed, "*?[") {
			abs, err := filepath.Abs(allowed)
			if err != nil {
				continue
			}
			if strings.HasPrefix(absTarget, abs+string(filepath.Separator)) || absTarget == abs {
				return true
			}
			continue
		}
		// Glob pattern: check whether absTarget or any of its ancestors matches.
		check := absTarget
		for {
			matched, err := filepath.Match(allowed, check)
			if err == nil && matched {
				return true
			}
			parent := filepath.Dir(check)
			if parent == check {
				break
			}
			check = parent
		}
	}
	return false
}

// CheckPath resolves path to an absolute path and verifies it is within the whitelist.
// Returns the resolved absolute path and nil on success, or an empty string and a
// descriptive error on failure.
func CheckPath(path string, allowedPaths []string) (string, error) {
	if len(allowedPaths) == 0 {
		return "", fmt.Errorf("文件系统访问已禁用，请在设置 → 工具设置中添加允许访问的路径")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("无效路径 %q: %w", path, err)
	}
	if !IsPathAllowed(abs, allowedPaths) {
		return "", fmt.Errorf("路径 %q 不在允许列表中，请在设置 → 工具设置中添加该路径", abs)
	}
	return abs, nil
}

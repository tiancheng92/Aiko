// internal/tools/system_tools.go
package tools

import (
	"context"
	"fmt"
	"runtime"
)

// GetOSInfoTool returns operating system information.
type GetOSInfoTool struct{}

func (t *GetOSInfoTool) Name() string             { return "get_os_info" }
func (t *GetOSInfoTool) Description() string      { return "获取操作系统名称、版本和架构信息" }
func (t *GetOSInfoTool) Permission() PermissionLevel { return PermProtected }

func (t *GetOSInfoTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	return ToolResult{
		Content: fmt.Sprintf("操作系统: %s, 架构: %s, Go运行时: %s",
			runtime.GOOS, runtime.GOARCH, runtime.Version()),
	}
}

// GetHardwareInfoTool returns basic hardware configuration.
type GetHardwareInfoTool struct{}

func (t *GetHardwareInfoTool) Name() string             { return "get_hardware_info" }
func (t *GetHardwareInfoTool) Description() string      { return "获取CPU核心数等基础硬件信息" }
func (t *GetHardwareInfoTool) Permission() PermissionLevel { return PermProtected }

func (t *GetHardwareInfoTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	return ToolResult{
		Content: fmt.Sprintf("CPU 逻辑核心数: %d", runtime.NumCPU()),
	}
}

// internal/tools/system_tools.go
package tools

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetOSInfoTool returns operating system information.
type GetOSInfoTool struct{}

func (t *GetOSInfoTool) Name() string             { return "get_os_info" }
func (t *GetOSInfoTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for get_os_info.
func (t *GetOSInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取操作系统名称、版本和架构信息", nil), nil
}

// InvokableRun returns OS/arch/Go runtime info.
func (t *GetOSInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return fmt.Sprintf("操作系统: %s, 架构: %s, Go运行时: %s",
		runtime.GOOS, runtime.GOARCH, runtime.Version()), nil
}

// GetHardwareInfoTool returns basic hardware configuration.
type GetHardwareInfoTool struct{}

func (t *GetHardwareInfoTool) Name() string             { return "get_hardware_info" }
func (t *GetHardwareInfoTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for get_hardware_info.
func (t *GetHardwareInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取CPU核心数等基础硬件信息", nil), nil
}

// InvokableRun returns the logical CPU count.
func (t *GetHardwareInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return fmt.Sprintf("CPU 逻辑核心数: %d", runtime.NumCPU()), nil
}

// GetNetworkStatusTool checks internet connectivity by dialing a well-known DNS server.
type GetNetworkStatusTool struct{}

func (t *GetNetworkStatusTool) Name() string             { return "get_network_status" }
func (t *GetNetworkStatusTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for get_network_status.
func (t *GetNetworkStatusTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "检测当前网络连接状态（在线/离线）", nil), nil
}

// InvokableRun dials 1.1.1.1:53 to determine connectivity.
func (t *GetNetworkStatusTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	conn, err := net.DialTimeout("tcp", "1.1.1.1:53", 3*time.Second)
	if err != nil {
		return "网络状态: 离线（无法连接互联网）", nil
	}
	conn.Close()
	return "网络状态: 在线", nil
}

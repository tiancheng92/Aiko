// internal/tools/system_tools.go
package tools

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetOSInfoTool returns operating system information including memory and disk.
type GetOSInfoTool struct{}

func (t *GetOSInfoTool) Name() string              { return "get_os_info" }
func (t *GetOSInfoTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for get_os_info.
func (t *GetOSInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取操作系统名称、版本、架构、主机名、CPU核心数、总内存和磁盘容量", nil), nil
}

// InvokableRun returns OS info plus total memory and disk capacity.
func (t *GetOSInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	var lines []string

	// OS / arch
	lines = append(lines, fmt.Sprintf("操作系统: %s", runtime.GOOS))
	lines = append(lines, fmt.Sprintf("架构: %s", runtime.GOARCH))

	// Hostname
	if hostname, err := getHostname(); err == nil {
		lines = append(lines, fmt.Sprintf("主机名: %s", hostname))
	}

	// System version (macOS only)
	if version, err := getMacOSVersion(); err == nil && version != "" {
		lines = append(lines, fmt.Sprintf("系统版本: %s", version))
	}

	// Uptime
	if uptime, err := getUptime(); err == nil {
		lines = append(lines, fmt.Sprintf("运行时长: %s", fmtDuration(uptime)))
	}

	// CPU
	lines = append(lines, fmt.Sprintf("CPU 逻辑核心数: %d", runtime.NumCPU()))

	// Memory
	if mem, err := getTotalMemory(); err == nil {
		lines = append(lines, fmt.Sprintf("总内存: %s", fmtBytes(mem)))
	}

	// Disk (root partition)
	if disk, err := getRootDiskSize(); err == nil {
		lines = append(lines, fmt.Sprintf("磁盘总容量(/): %s", fmtBytes(disk)))
	}

	return strings.Join(lines, "\n"), nil
}

// GetHardwareInfoTool returns basic hardware configuration.
type GetHardwareInfoTool struct{}

func (t *GetHardwareInfoTool) Name() string              { return "get_hardware_info" }
func (t *GetHardwareInfoTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for get_hardware_info.
func (t *GetHardwareInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取CPU型号、核心数等基础硬件信息", nil), nil
}

// InvokableRun returns CPU model and core counts.
func (t *GetHardwareInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	var lines []string
	lines = append(lines, fmt.Sprintf("CPU 逻辑核心数: %d", runtime.NumCPU()))

	// CPU model (macOS only)
	if model, err := getCPUModel(); err == nil && model != "" {
		lines = append(lines, fmt.Sprintf("CPU 型号: %s", model))
	}

	return strings.Join(lines, "\n"), nil
}

// GetSystemStatsTool reports real-time CPU, memory and disk usage.
type GetSystemStatsTool struct{}

func (t *GetSystemStatsTool) Name() string              { return "get_system_stats" }
func (t *GetSystemStatsTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for get_system_stats.
func (t *GetSystemStatsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取当前 CPU 使用率、内存使用情况和磁盘使用情况（实时状态）", nil), nil
}

// InvokableRun collects CPU, memory and disk usage statistics.
func (t *GetSystemStatsTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	var lines []string

	// CPU usage
	if cpu, err := getCPUUsage(); err == nil {
		lines = append(lines, fmt.Sprintf("CPU 使用率: %.1f%%", cpu))
	}

	// Memory usage
	if memUsed, memTotal, err := getMemoryUsage(); err == nil {
		usedPercent := float64(memUsed) / float64(memTotal) * 100
		lines = append(lines,
			fmt.Sprintf("内存: 已用 %s / 共 %s（%.1f%%）",
				fmtBytes(memUsed), fmtBytes(memTotal), usedPercent),
			fmt.Sprintf("可用内存: %s", fmtBytes(memTotal-memUsed)),
		)
	}

	// Disk usage for root partition
	if diskUsed, diskTotal, err := getDiskUsage("/"); err == nil {
		usedPercent := float64(diskUsed) / float64(diskTotal) * 100
		lines = append(lines,
			fmt.Sprintf("磁盘 /: 已用 %s / 共 %s（%.1f%%）",
				fmtBytes(diskUsed), fmtBytes(diskTotal), usedPercent),
		)
	}

	return strings.Join(lines, "\n"), nil
}

// GetNetworkStatusTool checks internet connectivity by dialing a well-known DNS server.
type GetNetworkStatusTool struct{}

func (t *GetNetworkStatusTool) Name() string              { return "get_network_status" }
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

// Helper functions using system commands

func getHostname() (string, error) {
	out, err := exec.Command("hostname").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getMacOSVersion() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", nil
	}
	out, err := exec.Command("sw_vers", "-productName").Output()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(out))

	out, err = exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(out))

	return fmt.Sprintf("%s %s", name, version), nil
}

func getUptime() (time.Duration, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
		if err != nil {
			return 0, err
		}
		// Parse: { sec = 1735123456, usec = 123456 }
		line := strings.TrimSpace(string(out))
		parts := strings.Split(line, ",")
		if len(parts) < 1 {
			return 0, fmt.Errorf("unexpected boottime format")
		}
		secPart := strings.TrimSpace(strings.Split(parts[0], "=")[1])
		bootTime, err := strconv.ParseInt(secPart, 10, 64)
		if err != nil {
			return 0, err
		}
		return time.Since(time.Unix(bootTime, 0)), nil
	}

	out, err := exec.Command("uptime", "-s").Output()
	if err != nil {
		return 0, err
	}
	bootTimeStr := strings.TrimSpace(string(out))
	bootTime, err := time.Parse("2006-01-02 15:04:05", bootTimeStr)
	if err != nil {
		return 0, err
	}
	return time.Since(bootTime), nil
}

func getTotalMemory() (uint64, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0, err
		}
		memSize, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0, err
		}
		return memSize, nil
	}
	return 0, fmt.Errorf("not supported on %s", runtime.GOOS)
}

func getCPUModel() (string, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "", fmt.Errorf("not supported on %s", runtime.GOOS)
}

func getRootDiskSize() (uint64, error) {
	out, err := exec.Command("df", "-k", "/").Output()
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected df output")
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected df fields")
	}
	sizeKB, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return sizeKB * 1024, nil // Convert KB to bytes
}

func getCPUUsage() (float64, error) {
	if runtime.GOOS == "darwin" {
		// Use iostat to get CPU usage
		out, err := exec.Command("iostat", "-c", "1", "-n", "1").Output()
		if err != nil {
			return 0, err
		}
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 6 && fields[0] != "us" && fields[0] != "" {
				// Fields: us sy id (user system idle)
				if idle, err := strconv.ParseFloat(fields[2], 64); err == nil {
					return 100.0 - idle, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("not supported on %s", runtime.GOOS)
}

func getMemoryUsage() (used, total uint64, err error) {
	if runtime.GOOS == "darwin" {
		// Get total memory
		total, err = getTotalMemory()
		if err != nil {
			return 0, 0, err
		}

		// Get memory pressure using vm_stat
		out, err := exec.Command("vm_stat").Output()
		if err != nil {
			return 0, 0, err
		}

		var pageSize, freePages, inactivePages uint64 = 4096, 0, 0 // Default 4KB pages

		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.Contains(line, "page size of") {
				parts := strings.Fields(line)
				if len(parts) >= 8 {
					if size, err := strconv.ParseUint(parts[7], 10, 64); err == nil {
						pageSize = size
					}
				}
			} else if strings.Contains(line, "Pages free:") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					pageStr := strings.TrimRight(parts[2], ".")
					if pages, err := strconv.ParseUint(pageStr, 10, 64); err == nil {
						freePages = pages
					}
				}
			} else if strings.Contains(line, "Pages inactive:") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					pageStr := strings.TrimRight(parts[2], ".")
					if pages, err := strconv.ParseUint(pageStr, 10, 64); err == nil {
						inactivePages = pages
					}
				}
			}
		}

		freeMemory := (freePages + inactivePages) * pageSize
		used = total - freeMemory
		return used, total, nil
	}
	return 0, 0, fmt.Errorf("not supported on %s", runtime.GOOS)
}

func getDiskUsage(path string) (used, total uint64, err error) {
	out, err := exec.Command("df", "-k", path).Output()
	if err != nil {
		return 0, 0, err
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("unexpected df output")
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return 0, 0, fmt.Errorf("unexpected df fields")
	}

	totalKB, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	usedKB, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	return usedKB * 1024, totalKB * 1024, nil // Convert KB to bytes
}

// fmtBytes formats a byte count as a human-readable string (GB/MB/KB).
func fmtBytes(b uint64) string {
	const (
		GB = 1 << 30
		MB = 1 << 20
		KB = 1 << 10
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// fmtDuration formats a duration as "Xd Xh Xm".
func fmtDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
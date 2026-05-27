// internal/tools/system/system_tools.go
package system

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"golang.org/x/sys/unix"

	"aiko/internal/bytesconv"
	"aiko/internal/tools/base"
)

// GetOSInfoTool returns operating system information including memory and disk.
type GetOSInfoTool struct{}

func (t *GetOSInfoTool) Name() string                    { return "get_os_info" }
func (t *GetOSInfoTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns the eino tool schema for get_os_info.
func (t *GetOSInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "获取系统静态配置：OS 名称/版本、CPU 架构、主机名、逻辑核心数、总内存和磁盘容量。回答「这台电脑是什么型号/配置」时使用。实时使用率请用 get_system_stats。", nil), nil
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

func (t *GetHardwareInfoTool) Name() string                    { return "get_hardware_info" }
func (t *GetHardwareInfoTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns the eino tool schema for get_hardware_info.
func (t *GetHardwareInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "获取 CPU 型号和核心数。若需内存/磁盘容量用 get_os_info；若需实时使用率用 get_system_stats。", nil), nil
}

// InvokableRun returns CPU model and core counts.
func (t *GetHardwareInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	var lines []string
	lines = append(lines, fmt.Sprintf("CPU 逻辑核心数: %d", runtime.NumCPU()))

	// CPU model (macOS only)
	if model, err := GetCPUModel(); err == nil && model != "" {
		lines = append(lines, fmt.Sprintf("CPU 型号: %s", model))
	}

	return strings.Join(lines, "\n"), nil
}

// GetSystemStatsTool reports real-time CPU, memory and disk usage.
type GetSystemStatsTool struct{}

func (t *GetSystemStatsTool) Name() string                    { return "get_system_stats" }
func (t *GetSystemStatsTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns the eino tool schema for get_system_stats.
func (t *GetSystemStatsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "获取实时 CPU 使用率、内存占用和磁盘使用百分比。适合查看「系统现在是否繁忙/内存不足」。静态配置信息请用 get_os_info。", nil), nil
}

// InvokableRun collects CPU, memory and disk usage statistics.
func (t *GetSystemStatsTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	var lines []string

	// CPU usage
	if cpu, err := GetCPUUsage(); err == nil {
		lines = append(lines, fmt.Sprintf("CPU 使用率: %.1f%%", cpu))
	}

	// Memory usage
	if memUsed, memTotal, err := GetMemoryUsage(); err == nil && memTotal > 0 {
		usedPercent := float64(memUsed) / float64(memTotal) * 100
		lines = append(lines,
			fmt.Sprintf("内存: 已用 %s / 共 %s（%.1f%%）",
				fmtBytes(memUsed), fmtBytes(memTotal), usedPercent),
			fmt.Sprintf("可用内存: %s", fmtBytes(memTotal-memUsed)),
		)
	}

	// Disk usage for root partition
	if diskUsed, diskTotal, err := GetDiskUsage("/"); err == nil && diskTotal > 0 {
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

func (t *GetNetworkStatusTool) Name() string                    { return "get_network_status" }
func (t *GetNetworkStatusTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns the eino tool schema for get_network_status.
func (t *GetNetworkStatusTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "检测当前是否能访问互联网（在线/离线）。在执行网络操作前可先调用此工具确认连通性。", nil), nil
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
	return string(bytes.TrimSpace(out)), nil
}

func getMacOSVersion() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", nil
	}
	out, err := exec.Command("sw_vers", "-productName").Output()
	if err != nil {
		return "", err
	}
	name := string(bytes.TrimSpace(out))

	out, err = exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "", err
	}
	version := string(bytes.TrimSpace(out))

	return fmt.Sprintf("%s %s", name, version), nil
}

func getUptime() (time.Duration, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
		if err != nil {
			return 0, err
		}
		// Parse: { sec = 1735123456, usec = 123456 }
		line := string(bytes.TrimSpace(out))
		parts := strings.Split(line, ",")
		eqParts := strings.Split(parts[0], "=")
		if len(eqParts) < 2 {
			return 0, fmt.Errorf("unexpected boottime format: %s", line)
		}
		secPart := strings.TrimSpace(eqParts[1])
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
	bootTimeStr := string(bytes.TrimSpace(out))
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
		memSize, err := strconv.ParseUint(string(bytes.TrimSpace(out)), 10, 64)
		if err != nil {
			return 0, err
		}
		return memSize, nil
	}
	return 0, fmt.Errorf("not supported on %s", runtime.GOOS)
}

// GetCPUModel returns the CPU brand string (e.g. "Apple M3 Pro").
func GetCPUModel() (string, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
		if err != nil {
			return "", err
		}
		return string(bytes.TrimSpace(out)), nil
	}
	return "", fmt.Errorf("not supported on %s", runtime.GOOS)
}

func getRootDiskSize() (uint64, error) {
	out, err := exec.Command("df", "-k", "/").Output()
	if err != nil {
		return 0, err
	}
	lines := strings.Split(bytesconv.BytesToString(out), "\n")
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

// GetCPUUsage returns the current CPU usage percentage (0–100) with sub-percent
// precision. On macOS it uses host_statistics (Mach) to sample cumulative CPU
// ticks and computes the delta since the last call — the same approach as
// Activity Monitor. The first call returns 0 (no baseline yet).
func GetCPUUsage() (float64, error) {
	if runtime.GOOS == "darwin" {
		return SampleCPUDelta(), nil
	}
	return 0, fmt.Errorf("not supported on %s", runtime.GOOS)
}

// GetMemoryUsage returns used and total memory in bytes.
//
// Formula matches Activity Monitor: used = active + inactive + wired + compressed.
// This matches top's PhysMem line (e.g. "74G used (6606M wired, 0B compressor)").
func GetMemoryUsage() (used, total uint64, err error) {
	if runtime.GOOS == "darwin" {
		total, err = getTotalMemory()
		if err != nil {
			return 0, 0, err
		}

		out, err := exec.Command("vm_stat").Output()
		if err != nil {
			return 0, 0, err
		}

		var pageSize, activePages, inactivePages, wiredPages, compressorPages uint64 = 4096, 0, 0, 0, 0

		for _, line := range strings.Split(bytesconv.BytesToString(out), "\n") {
			if strings.Contains(line, "page size of") {
				parts := strings.Fields(line)
				if len(parts) >= 8 {
					if size, err := strconv.ParseUint(parts[7], 10, 64); err == nil {
						pageSize = size
					}
				}
			} else if strings.Contains(line, "Pages active:") {
				activePages = parseVMStatLine(line)
			} else if strings.Contains(line, "Pages inactive:") {
				inactivePages = parseVMStatLine(line)
			} else if strings.Contains(line, "Pages wired down:") {
				wiredPages = parseVMStatLine(line)
			} else if strings.Contains(line, "Pages occupied by compressor:") {
				compressorPages = parseVMStatLine(line)
			}
		}

		usedPages := activePages + inactivePages + wiredPages + compressorPages
		used = usedPages * pageSize

		return used, total, nil
	}
	return 0, 0, fmt.Errorf("not supported on %s", runtime.GOOS)
}

// parseVMStatLine extracts the page count from a vm_stat line like
// "Pages active: 2383049." or "Pages wired down: 408334."
func parseVMStatLine(line string) uint64 {
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return 0
	}
	pageStr := strings.TrimRight(parts[len(parts)-1], ".")
	v, err := strconv.ParseUint(pageStr, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// GetDiskUsage returns used and total disk bytes for the given path.
func GetDiskUsage(path string) (used, total uint64, err error) {
	out, err := exec.Command("df", "-k", path).Output()
	if err != nil {
		return 0, 0, err
	}
	lines := strings.Split(bytesconv.BytesToString(out), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("unexpected df output")
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return 0, 0, fmt.Errorf("unexpected df fields")
	}

	totalKB, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	availKB, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	// used = total - available (more accurate than the Used column on APFS,
	// which can be inflated by snapshots and purgable space).
	usedKB := totalKB - availKB
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

// NetworkStats holds download and upload rates in bytes per second.
type NetworkStats struct {
	DownRate float64 `json:"downRate"`
	UpRate   float64 `json:"upRate"`
}

// SampleNetworkBytes returns the current cumulative bytes received and
// transmitted across all non-loopback network interfaces via sysctl
// NET_RT_IFLIST2 (the same 64-bit kernel counters Activity Monitor uses).
// It is stateless — the caller computes rates from successive samples.
func SampleNetworkBytes() (bytesIn, bytesOut uint64) {
	if runtime.GOOS != "darwin" {
		return 0, 0
	}

	// net.route sysctl must be called with an explicit MIB because
	// unix.SysctlRaw's nametomib cannot resolve "net.route".
	mib := []int32{unix.CTL_NET, unix.AF_ROUTE, 0, 0, unix.NET_RT_IFLIST2, 0}

	// Find buffer size.
	var n uintptr
	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		uintptr(len(mib)),
		0,
		uintptr(unsafe.Pointer(&n)),
		0,
		0,
	)
	if errno != 0 {
		return 0, 0
	}
	if n == 0 {
		return 0, 0
	}

	buf := make([]byte, n)
	_, _, errno = syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)),
		0,
		0,
	)
	if errno != 0 {
		return 0, 0
	}
	buf = buf[:n]

	for len(buf) > 4 {
		msglen := int(binary.LittleEndian.Uint16(buf[:2]))
		if msglen < 4 || msglen > len(buf) {
			break
		}

		// RTM_IFINFO2 (0x12) carries 64-bit interface counters via if_data64.
		// Skip non-IFINFO2 messages and loopback (ifm_index == 1 at offset 12).
		if buf[3] == 0x12 && msglen >= 112 && binary.LittleEndian.Uint16(buf[12:14]) != 1 {
			// if_data64 starts at byte 32 in the message.
			// ifi_ibytes at 32+64=96, ifi_obytes at 32+72=104.
			bytesIn += binary.LittleEndian.Uint64(buf[96:104])
			bytesOut += binary.LittleEndian.Uint64(buf[104:112])
		}

		buf = buf[msglen:]
	}
	return bytesIn, bytesOut
}

// ProcessInfo holds basic info for a single process.
type ProcessInfo struct {
	PID    int     `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
}

// TopProcesses holds top CPU and memory consuming processes.
type TopProcesses struct {
	TopCPU    []ProcessInfo `json:"topCpu"`
	TopMemory []ProcessInfo `json:"topMemory"`
}

// GetTopProcesses returns the top 5 processes by CPU and memory usage via ps.
// Runs two separate ps commands with native sorting, piped to head, so only
// the needed rows are parsed and no Go-side sorting is required.
func GetTopProcesses() (*TopProcesses, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("not supported on %s", runtime.GOOS)
	}

	cpuOut, err := exec.Command("sh", "-c", "ps -eo pid,%cpu,%mem,comm -r | head -6").Output()
	if err != nil {
		return nil, fmt.Errorf("ps cpu: %w", err)
	}

	memOut, err := exec.Command("sh", "-c", "ps -eo pid,%cpu,%mem,comm -m | head -6").Output()
	if err != nil {
		return nil, fmt.Errorf("ps mem: %w", err)
	}

	return &TopProcesses{
		TopCPU:    parseTopProcesses(cpuOut),
		TopMemory: parseTopProcesses(memOut),
	}, nil
}

// parseTopProcesses parses ps output with columns pid,%cpu,%mem,comm where comm
// is the last field and may contain spaces.
func parseTopProcesses(out []byte) []ProcessInfo {
	lines := strings.Split(bytesconv.BytesToString(out), "\n")
	procs := make([]ProcessInfo, 0, 5)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		mem, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		name := strings.Join(fields[3:], " ")
		procs = append(procs, ProcessInfo{
			PID:    pid,
			Name:   filepath.Base(name),
			CPU:    cpu,
			Memory: mem,
		})
	}
	return procs
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

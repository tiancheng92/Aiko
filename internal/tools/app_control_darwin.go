//go:build darwin

// internal/tools/app_control_darwin.go
package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// InvokableRun lists visible running applications via System Events osascript.
func (t *ListRunningAppsTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	script := `tell application "System Events"
	get name of every process whose background only is false
end tell`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", fmt.Errorf("list apps: %w", err)
	}
	apps := strings.TrimSpace(string(out))
	if apps == "" {
		return "没有正在运行的应用", nil
	}
	return "当前运行的应用：" + apps, nil
}

// InvokableRun controls the named application via AppleScript.
func (t *ControlAppTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return "请提供 action 参数（open/activate/quit）", nil
	}
	appName, ok := args["app_name"].(string)
	if !ok || appName == "" {
		return "请提供 app_name 参数", nil
	}

	var script string
	switch action {
	case "open", "activate":
		script = fmt.Sprintf(`tell application %q to activate`, appName)
	case "quit":
		script = fmt.Sprintf(`tell application %q to quit`, appName)
	default:
		return fmt.Sprintf("不支持的 action：%s，请使用 open/activate/quit", action), nil
	}

	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("control_app %s %s: %w — %s", action, appName, err, strings.TrimSpace(string(out)))
	}
	switch action {
	case "open", "activate":
		return fmt.Sprintf("已激活 %s", appName), nil
	case "quit":
		return fmt.Sprintf("已退出 %s", appName), nil
	}
	return "操作完成", nil
}

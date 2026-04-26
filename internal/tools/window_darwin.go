//go:build darwin

// internal/tools/window_darwin.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// windowInfo is the JSON-serialisable result returned by GetActiveWindowInfoTool.
type windowInfo struct {
	App          string `json:"app"`
	WindowTitle  string `json:"window_title"`
	SelectedText string `json:"selected_text"`
}

// InvokableRun gets the frontmost app/window and any selected text.
func (t *GetActiveWindowInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	// Step 1: get app name and window title via System Events.
	appScript := `tell application "System Events"
	set p to first process whose frontmost is true
	set appName to name of p
	set winTitle to ""
	if (count of windows of p) > 0 then
		set winTitle to name of first window of p
	end if
	return appName & "||" & winTitle
end tell`

	appRaw, err := runAppleScript(appScript)
	if err != nil {
		return fmt.Sprintf("获取窗口信息失败：%s\n请在「系统设置 → 隐私与安全性 → 辅助功能」中授权 Aiko。", err.Error()), nil
	}

	parts := strings.SplitN(appRaw, "||", 2)
	info := windowInfo{App: strings.TrimSpace(parts[0])}
	if len(parts) == 2 {
		info.WindowTitle = strings.TrimSpace(parts[1])
	}

	// Step 2: save current clipboard, simulate ⌘C, read new clipboard, restore.
	origClip, _ := exec.Command("pbpaste").Output()

	// Simulate ⌘C in the frontmost app.
	copyScript := `tell application "System Events" to keystroke "c" using command down`
	_ = exec.Command("osascript", "-e", copyScript).Run()

	// Wait for clipboard to update.
	time.Sleep(120 * time.Millisecond)

	newClip, _ := exec.Command("pbpaste").Output()
	newText := strings.TrimRight(string(newClip), "\n")
	origText := strings.TrimRight(string(origClip), "\n")

	// Restore original clipboard.
	restore := exec.Command("pbcopy")
	restore.Stdin = strings.NewReader(origText)
	_ = restore.Run()

	if newText != origText {
		info.SelectedText = newText
	}

	b, _ := json.Marshal(info)
	return string(b), nil
}

//go:build darwin

// internal/tools/window_darwin.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// windowInfo is the JSON-serialisable result returned by GetActiveWindowInfoTool.
type windowInfo struct {
	App          string `json:"app"`
	WindowTitle  string `json:"window_title"`
	SelectedText string `json:"selected_text"`
}

var (
	lastWindowMu   sync.RWMutex
	lastWindowInfo windowInfo // last non-Aiko frontmost window, cached by background poller
)

// StartWindowPoller launches a background goroutine that polls the frontmost
// non-Aiko application every second and caches the result. Call once at startup.
func StartWindowPoller(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				info := pollFrontWindow()
				if info.App != "" && info.App != "Aiko" {
					lastWindowMu.Lock()
					lastWindowInfo = info
					lastWindowMu.Unlock()
				}
			}
		}
	}()
}

// pollFrontWindow queries System Events for the current frontmost app/window.
func pollFrontWindow() windowInfo {
	script := `tell application "System Events"
	set p to first process whose frontmost is true
	set appName to name of p
	set winTitle to ""
	if (count of windows of p) > 0 then
		set winTitle to name of first window of p
	end if
	return appName & "||" & winTitle
end tell`

	raw, err := runAppleScript(script)
	if err != nil {
		return windowInfo{}
	}
	parts := strings.SplitN(strings.TrimSpace(raw), "||", 2)
	info := windowInfo{App: parts[0]}
	if len(parts) == 2 {
		info.WindowTitle = parts[1]
	}
	return info
}

// InvokableRun returns the last non-Aiko frontmost window and any selected text.
// Selected text is captured from the cached app via ⌘C simulation.
func (t *GetActiveWindowInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	lastWindowMu.RLock()
	info := lastWindowInfo
	lastWindowMu.RUnlock()

	if info.App == "" {
		return `{"app":"","window_title":"","selected_text":""}`, nil
	}

	// Capture selected text: bring the cached app to front briefly,
	// simulate ⌘C, read clipboard, then return focus to Aiko.
	origClip, _ := exec.Command("pbpaste").Output()
	origText := strings.TrimRight(string(origClip), "\n")

	// Activate the target app and send ⌘C.
	copyScript := fmt.Sprintf(`tell application "%s" to activate
delay 0.15
tell application "System Events" to keystroke "c" using command down`, info.App)
	_ = exec.Command("osascript", "-e", copyScript).Run()

	time.Sleep(150 * time.Millisecond)

	newClip, _ := exec.Command("pbpaste").Output()
	newText := strings.TrimRight(string(newClip), "\n")

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

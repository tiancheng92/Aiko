# macOS Integration B Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four macOS-native tools to Aiko — `get_calendar_events`, `create_calendar_event`, `get_active_window_info`, and `ocr_screen` — following the existing osascript/darwin-split pattern.

**Architecture:** Each tool group gets three files: a `_tools.go` with struct definitions and `Info()`, a `_darwin.go` with the real implementation using osascript or Swift, and an `_other.go` stub. All four tools are registered in `registry.go`'s `All()` slice so they get automatic permission rows and eino wrapping.

**Tech Stack:** Go standard library, osascript (AppleScript via `exec.Command`), macOS Vision framework (via `swift` CLI), `github.com/cloudwego/eino/schema` for tool metadata, `github.com/bytedance/sonic` for JSON.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/tools/calendar_tools.go` | Create | `GetCalendarEventsTool` + `CreateCalendarEventTool` struct defs and `Info()` |
| `internal/tools/calendar_darwin.go` | Create | osascript implementations for both calendar tools |
| `internal/tools/calendar_other.go` | Create | Non-darwin stubs |
| `internal/tools/window_tools.go` | Create | `GetActiveWindowInfoTool` struct def and `Info()` |
| `internal/tools/window_darwin.go` | Create | osascript + ⌘C clipboard trick implementation |
| `internal/tools/window_other.go` | Create | Non-darwin stub |
| `internal/tools/ocr_tools.go` | Create | `OcrScreenTool` struct def and `Info()` |
| `internal/tools/ocr_darwin.go` | Create | screencapture + Swift Vision OCR implementation |
| `internal/tools/ocr_other.go` | Create | Non-darwin stub |
| `internal/tools/registry.go` | Modify | Add 4 tools to `All()` |

---

## Task 1: Calendar tool structs and stubs

**Files:**
- Create: `internal/tools/calendar_tools.go`
- Create: `internal/tools/calendar_other.go`

- [ ] **Step 1: Create `calendar_tools.go`**

```go
// internal/tools/calendar_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// GetCalendarEventsTool retrieves events from macOS Calendar within a date range.
type GetCalendarEventsTool struct{}

// Name returns the tool identifier.
func (t *GetCalendarEventsTool) Name() string { return "get_calendar_events" }

// Permission declares this tool as public.
func (t *GetCalendarEventsTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetCalendarEventsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"查询 macOS 日历中指定日期范围内的事件。返回事件列表，包含标题、开始/结束时间、地点和备注。",
		map[string]*schema.ParameterInfo{
			"start_date": {
				Desc:     "开始日期，格式 YYYY-MM-DD（必填）",
				Required: true,
				Type:     schema.String,
			},
			"end_date": {
				Desc:     "结束日期，格式 YYYY-MM-DD（必填）",
				Required: true,
				Type:     schema.String,
			},
			"calendar_name": {
				Desc:     "日历名称（可选）。留空则查询所有日历。",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}

// CreateCalendarEventTool creates a new event in macOS Calendar.
type CreateCalendarEventTool struct{}

// Name returns the tool identifier.
func (t *CreateCalendarEventTool) Name() string { return "create_calendar_event" }

// Permission declares this tool as protected (modifies user data).
func (t *CreateCalendarEventTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *CreateCalendarEventTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"在 macOS 日历中创建新事件。",
		map[string]*schema.ParameterInfo{
			"title": {
				Desc:     "事件标题（必填）",
				Required: true,
				Type:     schema.String,
			},
			"start_time": {
				Desc:     "开始时间，格式 YYYY-MM-DD HH:MM（必填）",
				Required: true,
				Type:     schema.String,
			},
			"end_time": {
				Desc:     "结束时间，格式 YYYY-MM-DD HH:MM（必填）",
				Required: true,
				Type:     schema.String,
			},
			"calendar_name": {
				Desc:     "目标日历名称（可选）。留空则使用默认日历。",
				Required: false,
				Type:     schema.String,
			},
			"location": {
				Desc:     "地点（可选）",
				Required: false,
				Type:     schema.String,
			},
			"notes": {
				Desc:     "备注（可选）",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}
```

- [ ] **Step 2: Create `calendar_other.go`**

```go
//go:build !darwin

// internal/tools/calendar_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetCalendarEventsTool is a no-op stub on non-macOS platforms.
type GetCalendarEventsTool struct{}

func (t *GetCalendarEventsTool) Name() string                { return "get_calendar_events" }
func (t *GetCalendarEventsTool) Permission() PermissionLevel { return PermPublic }
func (t *GetCalendarEventsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "查询 macOS 日历事件（仅 macOS 支持）", nil), nil
}
func (t *GetCalendarEventsTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "get_calendar_events 仅支持 macOS", nil
}

// CreateCalendarEventTool is a no-op stub on non-macOS platforms.
type CreateCalendarEventTool struct{}

func (t *CreateCalendarEventTool) Name() string                { return "create_calendar_event" }
func (t *CreateCalendarEventTool) Permission() PermissionLevel { return PermProtected }
func (t *CreateCalendarEventTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "在 macOS 日历中创建事件（仅 macOS 支持）", nil), nil
}
func (t *CreateCalendarEventTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "create_calendar_event 仅支持 macOS", nil
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/tools/calendar_tools.go internal/tools/calendar_other.go
git commit -m "feat(tools): add calendar tool structs and non-darwin stubs"
```

---

## Task 2: Calendar darwin implementation

**Files:**
- Create: `internal/tools/calendar_darwin.go`

- [ ] **Step 1: Create `calendar_darwin.go`**

```go
//go:build darwin

// internal/tools/calendar_darwin.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// calendarEvent is the JSON-serialisable representation of a Calendar event.
type calendarEvent struct {
	Title    string `json:"title"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Location string `json:"location"`
	Notes    string `json:"notes"`
}

// formatCalDate converts a YYYY-MM-DD string to the AppleScript date literal
// format: "Sunday, January 1, 2006 at 00:00:00".
func formatCalDate(dateStr string, endOfDay bool) (string, error) {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return "", fmt.Errorf("invalid date %q: %w", dateStr, err)
	}
	if endOfDay {
		t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	}
	// AppleScript expects: "Monday, April 26, 2026 at 09:00:00"
	return t.Format("Monday, January 2, 2006 at 15:04:05"), nil
}

// InvokableRun queries Calendar.app for events in the given date range.
func (t *GetCalendarEventsTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	startDate, _ := args["start_date"].(string)
	endDate, _ := args["end_date"].(string)
	calName, _ := args["calendar_name"].(string)

	if startDate == "" || endDate == "" {
		return "请提供 start_date 和 end_date（格式 YYYY-MM-DD）", nil
	}

	startLiteral, err := formatCalDate(startDate, false)
	if err != nil {
		return fmt.Sprintf("日期格式错误：%s", err.Error()), nil
	}
	endLiteral, err := formatCalDate(endDate, true)
	if err != nil {
		return fmt.Sprintf("日期格式错误：%s", err.Error()), nil
	}

	calFilter := ""
	if calName != "" {
		calFilter = fmt.Sprintf(`if name of cal is not "%s" then
			set i to i + 1
			repeat
			end repeat
		end if`, calName)
	}

	script := fmt.Sprintf(`
tell application "Calendar"
	set startDate to date "%s"
	set endDate to date "%s"
	set output to ""
	set calList to every calendar
	set i to 1
	repeat with cal in calList
		%s
		set calName to name of cal
		set evList to (every event of cal whose start date >= startDate and start date <= endDate)
		repeat with ev in evList
			set evTitle to summary of ev
			set evStart to start date of ev as string
			set evEnd to end date of ev as string
			set evLoc to ""
			try
				set evLoc to location of ev
				if evLoc is missing value then set evLoc to ""
			end try
			set evNotes to ""
			try
				set evNotes to description of ev
				if evNotes is missing value then set evNotes to ""
			end try
			set output to output & evTitle & "||" & evStart & "||" & evEnd & "||" & evLoc & "||" & evNotes & "|||"
		end repeat
		set i to i + 1
	end repeat
	if output is "" then return "（该时间段内没有日历事件）"
	return output
end tell`, startLiteral, endLiteral, calFilter)

	raw, err := runAppleScript(script)
	if err != nil {
		return fmt.Sprintf("日历访问失败：%s\n请在「系统设置 → 隐私与安全性 → 日历」中授权 Aiko。", err.Error()), nil
	}

	if strings.HasPrefix(raw, "（") {
		return raw, nil
	}

	// Parse "title||start||end||loc||notes|||" records
	var events []calendarEvent
	for _, record := range strings.Split(raw, "|||") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "||", 5)
		if len(parts) < 5 {
			continue
		}
		events = append(events, calendarEvent{
			Title:    strings.TrimSpace(parts[0]),
			Start:    strings.TrimSpace(parts[1]),
			End:      strings.TrimSpace(parts[2]),
			Location: strings.TrimSpace(parts[3]),
			Notes:    strings.TrimSpace(parts[4]),
		})
	}

	b, _ := json.Marshal(events)
	return string(b), nil
}

// formatCalDateTime converts "YYYY-MM-DD HH:MM" to AppleScript date literal.
func formatCalDateTime(s string) (string, error) {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		return "", fmt.Errorf("invalid datetime %q (expected YYYY-MM-DD HH:MM): %w", s, err)
	}
	return t.Format("Monday, January 2, 2006 at 15:04:05"), nil
}

// InvokableRun creates a new event in Calendar.app.
func (t *CreateCalendarEventTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	title, _ := args["title"].(string)
	startTime, _ := args["start_time"].(string)
	endTime, _ := args["end_time"].(string)
	calName, _ := args["calendar_name"].(string)
	location, _ := args["location"].(string)
	notes, _ := args["notes"].(string)

	if title == "" || startTime == "" || endTime == "" {
		return "请提供 title、start_time 和 end_time", nil
	}

	startLiteral, err := formatCalDateTime(startTime)
	if err != nil {
		return fmt.Sprintf("时间格式错误：%s", err.Error()), nil
	}
	endLiteral, err := formatCalDateTime(endTime)
	if err != nil {
		return fmt.Sprintf("时间格式错误：%s", err.Error()), nil
	}

	// Resolve target calendar: use named calendar or default (first writable).
	var calResolve string
	if calName != "" {
		calResolve = fmt.Sprintf(`set targetCal to first calendar whose name is "%s"`, calName)
	} else {
		calResolve = `set targetCal to first calendar`
	}

	script := fmt.Sprintf(`
tell application "Calendar"
	%s
	set newEvent to make new event at end of events of targetCal with properties {summary:"%s", start date:date "%s", end date:date "%s", location:"%s", description:"%s"}
	set usedCal to name of targetCal
	return "事件「" & summary of newEvent & "」已创建到日历「" & usedCal & "」"
end tell`, calResolve, title, startLiteral, endLiteral, location, notes)

	result, err := runAppleScript(script)
	if err != nil {
		return fmt.Sprintf("创建日历事件失败：%s\n请在「系统设置 → 隐私与安全性 → 日历」中授权 Aiko。", err.Error()), nil
	}
	return result, nil
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/calendar_darwin.go
git commit -m "feat(tools): implement get_calendar_events and create_calendar_event for macOS"
```

---

## Task 3: Window tool structs, stub, and darwin implementation

**Files:**
- Create: `internal/tools/window_tools.go`
- Create: `internal/tools/window_other.go`
- Create: `internal/tools/window_darwin.go`

- [ ] **Step 1: Create `window_tools.go`**

```go
// internal/tools/window_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// GetActiveWindowInfoTool returns the frontmost app name, window title,
// and any currently selected text on macOS.
type GetActiveWindowInfoTool struct{}

// Name returns the tool identifier.
func (t *GetActiveWindowInfoTool) Name() string { return "get_active_window_info" }

// Permission declares this tool as public.
func (t *GetActiveWindowInfoTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetActiveWindowInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"获取当前前台应用的名称、窗口标题和选中文字。选中文字通过模拟 ⌘C 读取，如无选中内容则返回空字符串。需要「辅助功能」权限。",
		nil,
	), nil
}
```

- [ ] **Step 2: Create `window_other.go`**

```go
//go:build !darwin

// internal/tools/window_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetActiveWindowInfoTool is a no-op stub on non-macOS platforms.
type GetActiveWindowInfoTool struct{}

func (t *GetActiveWindowInfoTool) Name() string                { return "get_active_window_info" }
func (t *GetActiveWindowInfoTool) Permission() PermissionLevel { return PermPublic }
func (t *GetActiveWindowInfoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "获取前台窗口信息（仅 macOS 支持）", nil), nil
}
func (t *GetActiveWindowInfoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "get_active_window_info 仅支持 macOS", nil
}
```

- [ ] **Step 3: Create `window_darwin.go`**

```go
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
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/window_tools.go internal/tools/window_other.go internal/tools/window_darwin.go
git commit -m "feat(tools): add get_active_window_info tool"
```

---

## Task 4: OCR tool structs, stub, and darwin implementation

**Files:**
- Create: `internal/tools/ocr_tools.go`
- Create: `internal/tools/ocr_other.go`
- Create: `internal/tools/ocr_darwin.go`

- [ ] **Step 1: Create `ocr_tools.go`**

```go
// internal/tools/ocr_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// OcrScreenTool captures a screen region (or full screen) and returns
// recognized text using macOS Vision framework.
type OcrScreenTool struct{}

// Name returns the tool identifier.
func (t *OcrScreenTool) Name() string { return "ocr_screen" }

// Permission declares this tool as protected (captures screen content).
func (t *OcrScreenTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *OcrScreenTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"截取屏幕区域（或全屏）并使用 macOS Vision 框架进行 OCR 文字识别。支持中文、英文、日文。需要「屏幕录制」权限。",
		map[string]*schema.ParameterInfo{
			"region": {
				Desc:     "截取区域，格式为 \"x,y,width,height\"（CSS 像素，左上角为原点）。省略则截取全屏。",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}
```

- [ ] **Step 2: Create `ocr_other.go`**

```go
//go:build !darwin

// internal/tools/ocr_other.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// OcrScreenTool is a no-op stub on non-macOS platforms.
type OcrScreenTool struct{}

func (t *OcrScreenTool) Name() string                { return "ocr_screen" }
func (t *OcrScreenTool) Permission() PermissionLevel { return PermProtected }
func (t *OcrScreenTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "屏幕 OCR 文字识别（仅 macOS 支持）", nil), nil
}
func (t *OcrScreenTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "ocr_screen 仅支持 macOS", nil
}
```

- [ ] **Step 3: Create `ocr_darwin.go`**

```go
//go:build darwin

// internal/tools/ocr_darwin.go
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// ocrSwiftScript is the Vision OCR Swift program written once to a temp file.
const ocrSwiftScript = `import Vision
import AppKit
import Foundation

let path = CommandLine.arguments[1]
let imgURL = URL(fileURLWithPath: path)
guard let imgSrc = CGImageSourceCreateWithURL(imgURL as CFURL, nil),
      let cgImage = CGImageSourceCreateImageAtIndex(imgSrc, 0, nil) else {
    fputs("ERROR: could not load image at \(path)\n", stderr)
    exit(1)
}
let request = VNRecognizeTextRequest()
request.recognitionLevel = .accurate
request.usesLanguageCorrection = true
request.recognitionLanguages = ["zh-Hans", "zh-Hant", "en-US", "ja"]
let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
try? handler.perform([request])
let lines = (request.results as? [VNRecognizedTextObservation] ?? [])
    .compactMap { $0.topCandidates(1).first?.string }
print(lines.joined(separator: "\n"))
`

var (
	ocrScriptOnce sync.Once
	ocrScriptPath string
	ocrScriptErr  error
)

// ensureOCRScript writes the Swift OCR script to a stable temp path (once).
func ensureOCRScript() (string, error) {
	ocrScriptOnce.Do(func() {
		p := filepath.Join(os.TempDir(), "aiko_ocr_vision.swift")
		ocrScriptErr = os.WriteFile(p, []byte(ocrSwiftScript), 0o644)
		if ocrScriptErr == nil {
			ocrScriptPath = p
		}
	})
	return ocrScriptPath, ocrScriptErr
}

// InvokableRun captures the screen and performs OCR via macOS Vision.
func (t *OcrScreenTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	region, _ := args["region"].(string)

	// Build screencapture command.
	imgPath := filepath.Join(os.TempDir(), fmt.Sprintf("aiko_ocr_%d.png", time.Now().UnixNano()))
	defer os.Remove(imgPath)

	var captureArgs []string
	captureArgs = append(captureArgs, "-x") // silent
	if region != "" {
		// Validate format: "x,y,width,height"
		parts := strings.Split(region, ",")
		if len(parts) != 4 {
			return "OCR 失败：region 格式应为 \"x,y,width,height\"", nil
		}
		captureArgs = append(captureArgs, "-R", region)
	}
	captureArgs = append(captureArgs, imgPath)

	if out, err := exec.Command("screencapture", captureArgs...).CombinedOutput(); err != nil {
		return fmt.Sprintf("OCR 失败：截图错误 — %s\n请在「系统设置 → 隐私与安全性 → 屏幕录制」中授权 Aiko。", strings.TrimSpace(string(out))), nil
	}

	// Ensure the Swift script file exists.
	scriptPath, err := ensureOCRScript()
	if err != nil {
		return fmt.Sprintf("OCR 失败：无法写入 Swift 脚本 — %s", err.Error()), nil
	}

	// Run Swift OCR script.
	out, err := exec.Command("swift", scriptPath, imgPath).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Sprintf("OCR 失败：%s", result), nil
	}
	if result == "" {
		return "（未识别到文字）", nil
	}
	return result, nil
}
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/ocr_tools.go internal/tools/ocr_other.go internal/tools/ocr_darwin.go
git commit -m "feat(tools): add ocr_screen tool using macOS Vision framework"
```

---

## Task 5: Register all four tools in registry.go

**Files:**
- Modify: `internal/tools/registry.go`

- [ ] **Step 1: Add four tools to `All()`**

In `internal/tools/registry.go`, update `All()` to add the four new tools after `&ControlAppTool{}`:

```go
func All() []Tool {
	return []Tool{
		&GetCurrentTimeTool{},
		&GetTimezoneTool{},
		&FormatTimeTool{},
		&GetOSInfoTool{},
		&GetHardwareInfoTool{},
		&GetSystemStatsTool{},
		&GetNetworkStatusTool{},
		&GetLocationTool{},
		&GetWeatherTool{},
		&WebSearchTool{},
		&WebFetchTool{},
		&GetBrowserURLTool{},
		&GetRemindersTool{},
		&CompleteReminderTool{},
		&GetMailsTool{},
		&GetMailContentTool{},
		&ReadClipboardTool{},
		&WriteClipboardTool{},
		&ListRunningAppsTool{},
		&ControlAppTool{},
		&GetCalendarEventsTool{},
		&CreateCalendarEventTool{},
		&GetActiveWindowInfoTool{},
		&OcrScreenTool{},
	}
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 3: Verify tools appear in the list**

```bash
go run . &
# open Aiko settings → 工具权限
# Confirm these four tool names appear:
# get_calendar_events, create_calendar_event, get_active_window_info, ocr_screen
# Then kill the process
kill %1
```

- [ ] **Step 4: Commit**

```bash
git add internal/tools/registry.go
git commit -m "feat(tools): register get_calendar_events, create_calendar_event, get_active_window_info, ocr_screen"
```

---

## Task 6: Update CLAUDE.md current status

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update current status section in CLAUDE.md**

In `CLAUDE.md`, add the following lines to the `## 当前状态` section:

```
- ✅ macOS 日历读写（osascript 读取事件、创建新事件）
- ✅ 前台窗口上下文（App 名称、窗口标题、选中文字）
- ✅ 屏幕 OCR（screencapture + macOS Vision 框架，支持中英日文）
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md status for macOS integration B tools"
```

---

## Self-Review

**Spec coverage:**
- ✅ `get_calendar_events` — Task 1 (structs/stubs) + Task 2 (darwin impl)
- ✅ `create_calendar_event` — Task 1 (structs/stubs) + Task 2 (darwin impl)
- ✅ `get_active_window_info` — Task 3
- ✅ `ocr_screen` — Task 4
- ✅ Registry registration — Task 5
- ✅ Error handling (TCC permission denied) — inline in each darwin impl
- ✅ `sync.Once` for OCR script file — Task 4, Step 3
- ✅ Non-darwin stubs — Tasks 1, 3, 4

**Placeholder scan:** None found.

**Type consistency:**
- `calendarEvent` struct defined in `calendar_darwin.go`, used only in that file ✅
- `windowInfo` struct defined in `window_darwin.go`, used only in that file ✅
- `parseArgs`, `infoFromSchema`, `runAppleScript` — all pre-existing helpers in the package ✅
- `formatCalDate` and `formatCalDateTime` — defined and used within `calendar_darwin.go` ✅

# macOS 系统集成扩展 (Sub-project B) — Design Spec

## Goal

Add four new built-in tools to Aiko: calendar read/write, active window context (with selected text), and screen OCR via macOS Vision framework. All follow the existing osascript / darwin-split pattern.

## Tools

| Tool name | Interface | Permission | Platform |
|---|---|---|---|
| `get_calendar_events` | Tool | PermPublic | darwin / stub |
| `create_calendar_event` | Tool | PermProtected | darwin / stub |
| `get_active_window_info` | Tool | PermPublic | darwin / stub |
| `ocr_screen` | Tool | PermProtected | darwin / stub |

## File Structure

```
internal/tools/
  calendar_tools.go          # struct defs + Info() for GetCalendarEventsTool, CreateCalendarEventTool
  calendar_darwin.go         # osascript implementations (reuses runAppleScript from reminders_darwin.go)
  calendar_other.go          # stubs returning "not supported on this platform"
  window_tools.go            # struct def + Info() for GetActiveWindowInfoTool
  window_darwin.go           # osascript System Events + ⌘C clipboard trick
  window_other.go            # stub
  ocr_tools.go               # struct def + Info() for OcrScreenTool
  ocr_darwin.go              # screencapture + swift -e Vision OCR
  ocr_other.go               # stub
```

`runAppleScript` is already defined in `reminders_darwin.go`; all darwin files in the same package can call it directly.

## Tool Designs

### get_calendar_events

**Input schema:**
```json
{
  "start_date": "2026-04-26",   // YYYY-MM-DD, required
  "end_date":   "2026-04-30",   // YYYY-MM-DD, required
  "calendar_name": ""            // optional; empty = all calendars
}
```

**AppleScript logic:**
```applescript
tell application "Calendar"
  set startDate to date "Saturday, April 26, 2026 at 00:00:00"
  set endDate   to date "Wednesday, April 30, 2026 at 23:59:59"
  set result to {}
  repeat with cal in calendars
    -- filter by calendar_name if provided
    repeat with ev in (every event of cal whose start date >= startDate and start date <= endDate)
      set end of result to (summary of ev) & "|" & (start date of ev) & "|" & (end date of ev) & "|" & (location of ev) & "|" & (description of ev)
    end repeat
  end repeat
  return result as string
end tell
```

Go parses `|`-delimited fields, returns JSON array:
```json
[
  {"title":"Team Standup","start":"2026-04-26 09:00","end":"2026-04-26 09:30","location":"Zoom","notes":""}
]
```

---

### create_calendar_event

**Input schema:**
```json
{
  "title":         "Team Standup",      // required
  "start_time":    "2026-04-27 09:00",  // YYYY-MM-DD HH:MM, required
  "end_time":      "2026-04-27 09:30",  // YYYY-MM-DD HH:MM, required
  "calendar_name": "",                   // optional; empty = default calendar
  "location":      "",                   // optional
  "notes":         ""                    // optional
}
```

**AppleScript logic:**
```applescript
tell application "Calendar"
  set targetCal to first calendar whose name is "calendar_name"  -- or default calendar
  make new event at end of events of targetCal with properties {
    summary: "title",
    start date: date "...",
    end date:   date "...",
    location:   "location",
    description: "notes"
  }
end tell
```

Returns: `"Event 'title' created in calendar 'name'."` on success.

---

### get_active_window_info

**Input schema:** none (no parameters)

**Implementation (darwin):**

1. osascript `System Events` → frontmost process name + window title:
```applescript
tell application "System Events"
  set p to first process whose frontmost is true
  set appName to name of p
  set winTitle to ""
  if (count of windows of p) > 0 then
    set winTitle to name of first window of p
  end if
  return appName & "|" & winTitle
end tell
```

2. Save current clipboard (`pbpaste`).
3. Simulate ⌘C via osascript:
```applescript
tell application "System Events" to keystroke "c" using command down
```
4. Sleep 100ms (allow clipboard to update).
5. Read new clipboard (`pbpaste`).
6. Restore original clipboard (`pbcopy`).
7. If new clipboard ≠ original → `selected_text` = new clipboard content; else `""`.

**Returns JSON:**
```json
{
  "app": "Xcode",
  "window_title": "AppDelegate.swift — MyProject",
  "selected_text": "func application(_ application: UIApplication..."
}
```

**Edge cases:**
- If ⌘C simulation fails or clipboard unchanged → `selected_text: ""`; no error returned.
- Clipboard restore uses `echo -n | pbcopy` for empty original.

---

### ocr_screen

**Input schema:**
```json
{
  "region": "100,200,800,600"   // "x,y,width,height" in CSS pixels; omit for full screen
}
```

**Implementation (darwin):**

1. Generate temp file path: `/tmp/aiko_ocr_<timestamp>.png`
2. Run `screencapture -x [-R x,y,w,h] /tmp/aiko_ocr_<timestamp>.png`
3. Run inline Swift script via `swift -e '...'`:

```swift
import Vision
import AppKit
import Foundation

let path = CommandLine.arguments[1]
let imgURL = URL(fileURLWithPath: path)
guard let imgSrc = CGImageSourceCreateWithURL(imgURL as CFURL, nil),
      let cgImage = CGImageSourceCreateImageAtIndex(imgSrc, 0, nil) else {
    print("ERROR: could not load image")
    exit(1)
}
let request = VNRecognizeTextRequest()
request.recognitionLevel = .accurate
request.usesLanguageCorrection = true
request.recognitionLanguages = ["zh-Hans", "zh-Hant", "en-US", "ja"]
let handler = VNImageRequestHandler(cgImage: cgImage)
try? handler.perform([request])
let lines = (request.results as? [VNRecognizedTextObservation] ?? [])
    .compactMap { $0.topCandidates(1).first?.string }
print(lines.joined(separator: "\n"))
```

Swift script is written to a temp `.swift` file and executed as `swift /tmp/aiko_ocr_script.swift /tmp/aiko_ocr_<ts>.png` (passing path as argument avoids shell escaping issues with inline `-e`).

4. Delete both temp files.
5. Return recognized text (may be empty string if no text found).

**Cold start:** ~1-2s for Swift JIT compilation on first call; subsequent calls reuse the same script file path (script file is created once, reused across calls via package-level `sync.Once` to write it).

---

## Registry

Add to `registry.go` `All()`:
```go
&GetCalendarEventsTool{},
&CreateCalendarEventTool{},
&GetActiveWindowInfoTool{},
&OcrScreenTool{},
```

`AllEino()` wraps them automatically. All four get `EnsureRow` at startup via the existing loop over `All()`.

## Non-darwin Stubs

Each `_other.go` returns:
```
"此工具仅支持 macOS"
```

## Error Handling

- Calendar tools: if Calendar.app not running → AppleScript error returned as tool result string (not Go error), prefixed with `"日历访问失败: "`.
- `get_active_window_info`: clipboard trick failures are silently degraded (`selected_text: ""`); osascript window title failure → return app name only.
- `ocr_screen`: screencapture or Swift failure → return error string `"OCR 失败: <reason>"`.
- All tools: system permission denied (TCC) returns descriptive Chinese error message guiding user to grant access in System Preferences.

## Permissions (macOS TCC)

| Tool | Required TCC permission |
|---|---|
| `get_calendar_events` | Calendar |
| `create_calendar_event` | Calendar |
| `get_active_window_info` | Accessibility (for ⌘C simulation) |
| `ocr_screen` | Screen Recording |

Users must grant these in System Preferences → Privacy & Security. Tools return a helpful error message if denied.

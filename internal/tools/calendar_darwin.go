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

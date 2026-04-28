//go:build darwin

// internal/tools/calendar_darwin.go
package tools

import (
	"context"
	json "github.com/bytedance/sonic"
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

// calDateComponents holds the year/month/day/seconds fields needed to
// construct an AppleScript date in a locale-independent way.
type calDateComponents struct {
	Year, Month, Day, Seconds int
}

// parseCalDate parses a YYYY-MM-DD string into calDateComponents.
// If endOfDay is true, Seconds is set to 86399 (23:59:59).
func parseCalDate(dateStr string, endOfDay bool) (calDateComponents, error) {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return calDateComponents{}, fmt.Errorf("invalid date %q: %w", dateStr, err)
	}
	secs := 0
	if endOfDay {
		secs = 86399
	}
	return calDateComponents{
		Year: t.Year(), Month: int(t.Month()), Day: t.Day(), Seconds: secs,
	}, nil
}

// parseCalDateTime parses a "YYYY-MM-DD HH:MM" string into calDateComponents.
func parseCalDateTime(s string) (calDateComponents, error) {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		return calDateComponents{}, fmt.Errorf("invalid datetime %q (expected YYYY-MM-DD HH:MM): %w", s, err)
	}
	secs := t.Hour()*3600 + t.Minute()*60
	return calDateComponents{
		Year: t.Year(), Month: int(t.Month()), Day: t.Day(), Seconds: secs,
	}, nil
}

// appleScriptDateBlock emits the AppleScript snippet that constructs a date
// object into the given variable name, locale-independently.
// Day is reset to 1 before setting the month to avoid overflow when the
// current machine day (e.g. 31) exceeds the target month's length.
func appleScriptDateBlock(varName string, c calDateComponents) string {
	return fmt.Sprintf(
		"set %s to current date\n\tset year of %s to %d\n\tset day of %s to 1\n\tset month of %s to %d\n\tset day of %s to %d\n\tset time of %s to %d",
		varName,
		varName, c.Year,
		varName,
		varName, c.Month,
		varName, c.Day,
		varName, c.Seconds,
	)
}

// escapeAppleScriptString escapes double-quotes and backslashes in a string
// so it is safe to embed inside an AppleScript double-quoted literal.
func escapeAppleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
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

	startComp, err := parseCalDate(startDate, false)
	if err != nil {
		return fmt.Sprintf("日期格式错误：%s", err.Error()), nil
	}
	endComp, err := parseCalDate(endDate, true)
	if err != nil {
		return fmt.Sprintf("日期格式错误：%s", err.Error()), nil
	}

	// Build calendar name filter using else-branch to skip non-matching calendars
	// (AppleScript has no continue statement).
	calFilter := ""
	if calName != "" {
		calFilter = fmt.Sprintf(`if name of cal is "%s" then`, escapeAppleScriptString(calName))
	}
	calFilterEnd := ""
	if calName != "" {
		calFilterEnd = "end if"
	}

	script := fmt.Sprintf(`
tell application "Calendar"
	%s
	%s
	set output to ""
	set calList to every calendar
	repeat with cal in calList
		%s
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
		%s
	end repeat
	if output is "" then return "（该时间段内没有日历事件）"
	return output
end tell`,
		appleScriptDateBlock("startDate", startComp),
		appleScriptDateBlock("endDate", endComp),
		calFilter,
		calFilterEnd)

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

	startComp, err := parseCalDateTime(startTime)
	if err != nil {
		return fmt.Sprintf("时间格式错误：%s", err.Error()), nil
	}
	endComp, err := parseCalDateTime(endTime)
	if err != nil {
		return fmt.Sprintf("时间格式错误：%s", err.Error()), nil
	}

	// Resolve target calendar: use named calendar or default (first writable).
	var calResolve string
	if calName != "" {
		calResolve = fmt.Sprintf(`set targetCal to first calendar whose name is "%s"`, escapeAppleScriptString(calName))
	} else {
		calResolve = `set targetCal to first calendar`
	}

	script := fmt.Sprintf(`
tell application "Calendar"
	%s
	%s
	%s
	set newEvent to make new event at end of events of targetCal with properties {summary:"%s", start date:evStart, end date:evEnd, location:"%s", description:"%s"}
	set usedCal to name of targetCal
	return "事件「" & summary of newEvent & "」已创建到日历「" & usedCal & "」"
end tell`,
		calResolve,
		appleScriptDateBlock("evStart", startComp),
		appleScriptDateBlock("evEnd", endComp),
		escapeAppleScriptString(title),
		escapeAppleScriptString(location),
		escapeAppleScriptString(notes))

	result, err := runAppleScript(script)
	if err != nil {
		return fmt.Sprintf("创建日历事件失败：%s\n请在「系统设置 → 隐私与安全性 → 日历」中授权 Aiko。", err.Error()), nil
	}
	return result, nil
}

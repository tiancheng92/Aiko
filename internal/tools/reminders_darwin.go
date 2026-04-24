//go:build darwin

// internal/tools/reminders_darwin.go
package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// runAppleScript executes an AppleScript and returns trimmed stdout or error.
func runAppleScript(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, result)
	}
	return result, nil
}

// ---- GetRemindersTool --------------------------------------------------

// GetRemindersTool fetches reminders from macOS Reminders via AppleScript.
// Optional filter: list name; if empty, all incomplete reminders are returned.
type GetRemindersTool struct{}

// Name returns the tool identifier.
func (t *GetRemindersTool) Name() string { return "get_reminders" }

// Permission declares this tool as public.
func (t *GetRemindersTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetRemindersTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"获取 macOS 提醒事项。可按清单名过滤；不传则返回所有清单中未完成的提醒。",
		map[string]*schema.ParameterInfo{
			"list": {
				Desc:     "清单名称（可选）。留空则获取所有清单的未完成提醒。",
				Required: false,
				Type:     schema.String,
			},
			"include_completed": {
				Desc:     "是否包含已完成的提醒，默认 false。",
				Required: false,
				Type:     schema.Boolean,
			},
		},
	), nil
}

// InvokableRun fetches reminders via osascript.
func (t *GetRemindersTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	listName, _ := args["list"].(string)
	includeCompleted, _ := args["include_completed"].(bool)

	var script string
	completedFilter := "completed is false"
	if includeCompleted {
		completedFilter = "true"
	}

	if listName != "" {
		script = fmt.Sprintf(`
tell application "Reminders"
	set output to ""
	if not (exists list "%s") then return "清单不存在：%s"
	set theList to list "%s"
	set theReminders to (reminders of theList whose %s)
	repeat with r in theReminders
		set rName to name of r
		set rDue to ""
		if due date of r is not missing value then
			set rDue to " [截止: " & (due date of r as string) & "]"
		end if
		set rDone to ""
		if completed of r then set rDone to " [已完成]"
		set output to output & "- " & rName & rDue & rDone & linefeed
	end repeat
	if output is "" then return "（无提醒）"
	return output
end tell`, listName, listName, listName, completedFilter)
	} else {
		script = fmt.Sprintf(`
tell application "Reminders"
	set output to ""
	repeat with theList in lists
		set listName to name of theList
		set theReminders to (reminders of theList whose %s)
		if (count of theReminders) > 0 then
			set output to output & "【" & listName & "】" & linefeed
			repeat with r in theReminders
				set rName to name of r
				set rDue to ""
				if due date of r is not missing value then
					set rDue to " [截止: " & (due date of r as string) & "]"
				end if
				set rDone to ""
				if completed of r then set rDone to " [已完成]"
				set output to output & "  - " & rName & rDue & rDone & linefeed
			end repeat
		end if
	end repeat
	if output is "" then return "（没有未完成的提醒事项）"
	return output
end tell`, completedFilter)
	}

	result, err := runAppleScript(script)
	if err != nil {
		return fmt.Sprintf("获取提醒事项失败：%s\n请确认已在「系统设置 → 隐私与安全性 → 提醒事项」中授权 Aiko。", err.Error()), nil
	}
	return result, nil
}

// ---- CompleteReminderTool ----------------------------------------------

// CompleteReminderTool marks a reminder as completed by name (and optional list).
type CompleteReminderTool struct{}

// Name returns the tool identifier.
func (t *CompleteReminderTool) Name() string { return "complete_reminder" }

// Permission declares this tool as public.
func (t *CompleteReminderTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *CompleteReminderTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"将 macOS 提醒事项标记为已完成。需要提供提醒名称，可选提供清单名称以避免同名歧义。",
		map[string]*schema.ParameterInfo{
			"name": {
				Desc:     "提醒事项的名称（精确匹配）。",
				Required: true,
				Type:     schema.String,
			},
			"list": {
				Desc:     "清单名称（可选）。提供后只在该清单中查找。",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}

// InvokableRun marks the reminder as completed via osascript.
func (t *CompleteReminderTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	name, _ := args["name"].(string)
	if name == "" {
		return "参数 name 不能为空", nil
	}
	listName, _ := args["list"].(string)

	var script string
	if listName != "" {
		script = fmt.Sprintf(`
tell application "Reminders"
	if not (exists list "%s") then return "清单不存在：%s"
	set theList to list "%s"
	set matched to (reminders of theList whose name is "%s" and completed is false)
	if (count of matched) = 0 then return "未找到未完成的提醒：%s"
	set completed of item 1 of matched to true
	return "已完成：%s"
end tell`, listName, listName, listName, name, name, name)
	} else {
		script = fmt.Sprintf(`
tell application "Reminders"
	repeat with theList in lists
		set matched to (reminders of theList whose name is "%s" and completed is false)
		if (count of matched) > 0 then
			set completed of item 1 of matched to true
			return "已完成：%s（清单：" & (name of theList) & "）"
		end if
	end repeat
	return "未找到未完成的提醒：%s"
end tell`, name, name, name)
	}

	result, err := runAppleScript(script)
	if err != nil {
		return fmt.Sprintf("标记提醒失败：%s\n请确认已在「系统设置 → 隐私与安全性 → 提醒事项」中授权 Aiko。", err.Error()), nil
	}
	return result, nil
}

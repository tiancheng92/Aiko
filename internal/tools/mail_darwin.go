//go:build darwin

// internal/tools/mail_darwin.go
package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ---- GetMailsTool ------------------------------------------------------

// GetMailsTool fetches a list of emails from macOS Mail.app via AppleScript.
// Supports filtering by mailbox, time range, and read/unread status.
type GetMailsTool struct{}

// Name returns the tool identifier.
func (t *GetMailsTool) Name() string { return "get_mails" }

// Permission declares this tool as public.
func (t *GetMailsTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetMailsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"获取 macOS Mail.app 中的邮件列表。支持按邮箱名、时间范围（since/until）和已读状态过滤。返回每封邮件的发件人、主题、日期和已读状态，不含正文（用 get_mail_content 读取正文）。",
		map[string]*schema.ParameterInfo{
			"mailbox": {
				Desc:     "邮箱名称（可选），如 \"收件箱\" 或 \"inbox\"。留空则查询所有账户的收件箱。",
				Required: false,
				Type:     schema.String,
			},
			"since": {
				Desc:     "起始时间（可选），格式 \"YYYY-MM-DD\" 或 \"YYYY-MM-DD HH:MM:SS\"。只返回此时间之后的邮件。",
				Required: false,
				Type:     schema.String,
			},
			"until": {
				Desc:     "截止时间（可选），格式 \"YYYY-MM-DD\" 或 \"YYYY-MM-DD HH:MM:SS\"。只返回此时间之前的邮件。",
				Required: false,
				Type:     schema.String,
			},
			"unread_only": {
				Desc:     "仅返回未读邮件，默认 false。",
				Required: false,
				Type:     schema.Boolean,
			},
			"limit": {
				Desc:     "最多返回邮件数量，默认 20，最大 100。",
				Required: false,
				Type:     schema.Integer,
			},
		},
	), nil
}

// InvokableRun fetches mail list via osascript.
func (t *GetMailsTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	mailbox, _ := args["mailbox"].(string)
	since, _ := args["since"].(string)
	until, _ := args["until"].(string)
	unreadOnly, _ := args["unread_only"].(bool)
	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		limit = min(limit, 100)
	}

	// Build date filter clauses for AppleScript.
	sinceClause := ""
	if since != "" {
		sinceClause = fmt.Sprintf(`set sinceDate to date "%s"`, since)
	}
	untilClause := ""
	if until != "" {
		untilClause = fmt.Sprintf(`set untilDate to date "%s"`, until)
	}

	unreadFilter := ""
	if unreadOnly {
		unreadFilter = "if read status of m is true then\nset doSkip to true\nend if\n"
	}

	// Determine mailbox target: named mailbox or all inboxes.
	// Note: `inbox of acc` is unreliable for IMAP accounts (returns missing value).
	// Instead we find the mailbox whose name is "INBOX" or matches the requested name.
	var boxScript string
	if mailbox != "" {
		boxScript = fmt.Sprintf(`
set theBoxes to {}
repeat with acc in accounts
	repeat with mb in (mailboxes of acc)
		if (name of mb) is "%s" then
			set theBoxes to theBoxes & {mb}
		end if
	end repeat
end repeat`, mailbox)
	} else {
		boxScript = `
set theBoxes to {}
repeat with acc in accounts
	repeat with mb in (mailboxes of acc)
		set mbName to name of mb
		if mbName is "INBOX" or mbName is "inbox" or mbName is "Inbox" then
			set theBoxes to theBoxes & {mb}
		end if
	end repeat
end repeat`
	}

	script := fmt.Sprintf(`
tell application "Mail"
	%s
	%s
	%s
	set output to ""
	set msgCount to 0
	%s
	repeat with theBox in theBoxes
		set allMessages to (messages of theBox)
		repeat with m in allMessages
			if msgCount >= %d then exit repeat
			set doSkip to false
			%s
			set msgDate to date sent of m
			%s
			%s
			if doSkip then
				set doSkip to false
			else
				set msgSubject to subject of m
				set msgSender to sender of m
				set msgRead to read status of m
				set readLabel to "[未读]"
				if msgRead then set readLabel to "[已读]"
				set output to output & readLabel & " " & msgSender & " | " & msgSubject & " | " & (msgDate as string) & linefeed
				set msgCount to msgCount + 1
			end if
		end repeat
	end repeat
	if output is "" then return "（没有符合条件的邮件）"
	return output
end tell`,
		sinceClause,
		untilClause,
		boxScript,
		"", // placeholder for set msgCount
		limit,
		unreadFilter,
		sinceFilterBlock(since),
		untilFilterBlock(until),
	)

	result, err := runAppleScript(script)
	if err != nil {
		return fmt.Sprintf("获取邮件失败：%s\n请确认已在「系统设置 → 隐私与安全性 → 完整磁盘访问权限」或「自动化」中授权 Aiko 访问 Mail。", err.Error()), nil
	}
	return result, nil
}

// sinceFilterBlock returns the AppleScript block that sets doSkip=true for messages before sinceDate.
func sinceFilterBlock(since string) string {
	if since == "" {
		return ""
	}
	return `if msgDate < sinceDate then
				set doSkip to true
			end if`
}

// untilFilterBlock returns the AppleScript block that sets doSkip=true for messages after untilDate.
func untilFilterBlock(until string) string {
	if until == "" {
		return ""
	}
	return `if msgDate > untilDate then
				set doSkip to true
			end if`
}

// ---- GetMailContentTool ------------------------------------------------

// GetMailContentTool reads the full plain-text body of a specific email.
// The email is identified by subject and optionally sender, within a mailbox.
type GetMailContentTool struct{}

// Name returns the tool identifier.
func (t *GetMailContentTool) Name() string { return "get_mail_content" }

// Permission declares this tool as public.
func (t *GetMailContentTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetMailContentTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"读取 macOS Mail.app 中某封邮件的完整正文。通过主题和可选的发件人来定位邮件。如有多封匹配，返回最新一封。",
		map[string]*schema.ParameterInfo{
			"subject": {
				Desc:     "邮件主题（精确匹配或包含匹配）。",
				Required: true,
				Type:     schema.String,
			},
			"sender": {
				Desc:     "发件人地址或名称（可选），用于缩小范围。",
				Required: false,
				Type:     schema.String,
			},
			"mailbox": {
				Desc:     "邮箱名称（可选），如 \"收件箱\"。留空则搜索所有收件箱。",
				Required: false,
				Type:     schema.String,
			},
		},
	), nil
}

// InvokableRun retrieves the full body of a matching email via osascript.
func (t *GetMailContentTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	subject, _ := args["subject"].(string)
	if subject == "" {
		return "参数 subject 不能为空", nil
	}
	sender, _ := args["sender"].(string)
	mailbox, _ := args["mailbox"].(string)

	var boxScript string
	if mailbox != "" {
		boxScript = fmt.Sprintf(`
set theBoxes to {}
repeat with acc in accounts
	repeat with mb in (mailboxes of acc)
		if (name of mb) is "%s" then
			set theBoxes to theBoxes & {mb}
		end if
	end repeat
end repeat`, mailbox)
	} else {
		boxScript = `
set theBoxes to {}
repeat with acc in accounts
	repeat with mb in (mailboxes of acc)
		set mbName to name of mb
		if mbName is "INBOX" or mbName is "inbox" or mbName is "Inbox" then
			set theBoxes to theBoxes & {mb}
		end if
	end repeat
end repeat`
	}

	senderFilter := ""
	if sender != "" {
		senderFilter = fmt.Sprintf(`if (sender of m) does not contain "%s" then
					set doSkip to true
				end if`, sender)
	}

	script := fmt.Sprintf(`
tell application "Mail"
	%s
	set bestMsg to missing value
	repeat with theBox in theBoxes
		repeat with m in (messages of theBox)
			if (subject of m) contains "%s" then
				set doSkip to false
				%s
				if not doSkip then
					if bestMsg is missing value then
						set bestMsg to m
					else if (date sent of m) > (date sent of bestMsg) then
						set bestMsg to m
					end if
				end if
			end if
		end repeat
	end repeat
	if bestMsg is missing value then return "未找到匹配主题的邮件：%s"
	set msgSubject to subject of bestMsg
	set msgSender to sender of bestMsg
	set msgDate to date sent of bestMsg as string
	set msgBody to content of bestMsg
	return "主题：" & msgSubject & linefeed & "发件人：" & msgSender & linefeed & "日期：" & msgDate & linefeed & "---" & linefeed & msgBody
end tell`,
		boxScript,
		subject,
		senderFilter,
		subject,
	)

	result, err := runAppleScript(script)
	if err != nil {
		return fmt.Sprintf("读取邮件正文失败：%s\n请确认已授权 Aiko 访问 Mail。", err.Error()), nil
	}
	return result, nil
}

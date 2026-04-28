package proactive

import (
	"context"
	json "github.com/bytedance/sonic"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	interntools "aiko/internal/tools"
)

// ScheduleFollowupTool lets the agent schedule a proactive follow-up message.
// It implements interntools.Tool so it can be wrapped by the permission gate.
type ScheduleFollowupTool struct {
	Store Store
}

// NewScheduleFollowupTool returns a ScheduleFollowupTool backed by store.
func NewScheduleFollowupTool(store Store) *ScheduleFollowupTool {
	return &ScheduleFollowupTool{Store: store}
}

// Name returns the stable tool name used for permission storage.
func (t *ScheduleFollowupTool) Name() string { return "schedule_followup" }

// Permission requires one-time user approval.
func (t *ScheduleFollowupTool) Permission() interntools.PermissionLevel {
	return interntools.PermProtected
}

// Info returns the eino tool schema.
func (t *ScheduleFollowupTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: "安排一条主动跟进消息，在指定时间主动提醒用户。当对话中发现用户有未来计划、待办事项或值得跟进的内容时调用。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"when": {
				Type:     schema.String,
				Desc:     "触发时间，ISO 8601 本地时间格式，例如 2026-04-26T09:00:00",
				Required: true,
			},
			"message": {
				Type:     schema.String,
				Desc:     "触发时发送给 AI 的提示词，说明要跟进什么内容",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun validates inputs and inserts the proactive item into the store.
func (t *ScheduleFollowupTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseToolArgs(input)

	whenStr, _ := args["when"].(string)
	message, _ := args["message"].(string)

	if message == "" {
		return "请提供 message 参数说明要跟进的内容", nil
	}
	if whenStr == "" {
		return "请提供 when 参数（ISO 8601 本地时间，例如 2026-04-26T09:00:00）", nil
	}

	when, err := time.ParseInLocation("2006-01-02T15:04:05", whenStr, time.Local)
	if err != nil {
		return fmt.Sprintf("时间格式无效，请使用 2006-01-02T15:04:05 格式，收到：%q", whenStr), nil
	}
	when = when.UTC()

	now := time.Now()
	if when.Before(now) {
		return "指定时间已过去，请提供未来的时间", nil
	}
	if when.After(now.Add(30 * 24 * time.Hour)) {
		return "指定时间超过30天，请安排30天内的跟进", nil
	}

	if err := t.Store.Insert(ctx, when, message); err != nil {
		return "", fmt.Errorf("schedule followup: %w", err)
	}

	return fmt.Sprintf("已安排：将在 %s 提醒你", when.In(time.Local).Format("2006年01月02日 15:04")), nil
}

// parseToolArgs unmarshals JSON input into a map. Returns empty map on failure.
func parseToolArgs(input string) map[string]any {
	args := map[string]any{}
	if input == "" || input == "{}" {
		return args
	}
	_ = json.Unmarshal([]byte(input), &args)
	return args
}

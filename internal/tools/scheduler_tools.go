// internal/tools/scheduler_tools.go
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/scheduler"
)

// CronTool manages scheduled tasks with add/list/remove actions.
type CronTool struct {
	Scheduler *scheduler.Scheduler
}

func (t *CronTool) Name() string             { return "cron" }
func (t *CronTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for cron.
func (t *CronTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "管理定时任务：创建、列出、删除。action 可为 add / list / remove",
		map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "操作类型：add（创建）、list（列出）、remove（删除）",
				Required: true,
				Enum:     []string{"add", "list", "remove"},
			},
			"name": {
				Type: schema.String,
				Desc: "任务名称（add 时必填）",
			},
			"description": {
				Type: schema.String,
				Desc: "任务描述（add 时可选）",
			},
			"schedule": {
				Type: schema.String,
				Desc: `cron 表达式，例如 "0 8 * * *" 表示每天早8点（add 时必填）`,
			},
			"prompt": {
				Type: schema.String,
				Desc: "任务触发时发送给 AI 的消息（add 时必填）",
			},
			"id": {
				Type: schema.Integer,
				Desc: "任务 ID（remove 时必填）",
			},
		},
	), nil
}

// InvokableRun dispatches to add/list/remove based on the action field.
func (t *CronTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	action, _ := args["action"].(string)
	switch action {
	case "add":
		return t.add(ctx, args)
	case "list":
		return t.list(ctx)
	case "remove":
		return t.remove(ctx, args)
	default:
		return "请指定 action：add / list / remove", nil
	}
}

// add creates a new scheduled job.
func (t *CronTool) add(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	desc, _ := args["description"].(string)
	schedule, _ := args["schedule"].(string)
	prompt, _ := args["prompt"].(string)
	if name == "" || schedule == "" || prompt == "" {
		return "请提供 name、schedule 和 prompt 参数", nil
	}
	j, err := t.Scheduler.CreateJob(ctx, name, desc, schedule, prompt)
	if err != nil {
		return "", fmt.Errorf("create cron job: %w", err)
	}
	return fmt.Sprintf("定时任务 \"%s\" 已创建（ID: %d），计划: %s", j.Name, j.ID, j.Schedule), nil
}

// list returns all scheduled jobs.
func (t *CronTool) list(ctx context.Context) (string, error) {
	jobs, err := t.Scheduler.ListJobs(ctx)
	if err != nil {
		return "", fmt.Errorf("list cron jobs: %w", err)
	}
	if len(jobs) == 0 {
		return "当前没有定时任务", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "共 %d 个定时任务：\n\n", len(jobs))
	for _, j := range jobs {
		status := "启用"
		if !j.Enabled {
			status = "禁用"
		}
		fmt.Fprintf(&sb, "ID %d: %s (%s)\n  计划: %s\n  描述: %s\n\n",
			j.ID, j.Name, status, j.Schedule, j.Description)
	}
	return sb.String(), nil
}

// remove deletes a scheduled job by ID.
func (t *CronTool) remove(ctx context.Context, args map[string]any) (string, error) {
	idFloat, ok := args["id"].(float64)
	if !ok || idFloat <= 0 {
		return "请提供有效的任务 ID（数字）", nil
	}
	id := int64(idFloat)
	if err := t.Scheduler.DeleteJob(ctx, id); err != nil {
		return "", fmt.Errorf("delete cron job %d: %w", id, err)
	}
	return fmt.Sprintf("定时任务 ID %d 已删除", id), nil
}

// internal/tools/scheduler_tools.go
package tools

import (
    "context"
    "fmt"
    "strings"

    "desktop-pet/internal/scheduler"
)

// CreateCronJobTool lets the AI create a new scheduled task.
type CreateCronJobTool struct {
    Scheduler *scheduler.Scheduler
}

func (t *CreateCronJobTool) Name() string { return "create_cron_job" }
func (t *CreateCronJobTool) Description() string {
    return `创建一个定时任务。参数 JSON:
{"name":"任务名","description":"描述","schedule":"cron表达式(如 0 8 * * * 表示每天早8点)","prompt":"触发时发送给AI的消息"}`
}
func (t *CreateCronJobTool) Permission() PermissionLevel { return PermProtected }

func (t *CreateCronJobTool) Execute(ctx context.Context, args map[string]any) ToolResult {
    name, _ := args["name"].(string)
    desc, _ := args["description"].(string)
    schedule, _ := args["schedule"].(string)
    prompt, _ := args["prompt"].(string)
    if name == "" || schedule == "" || prompt == "" {
        return ToolResult{Content: "请提供 name、schedule 和 prompt 参数"}
    }
    j, err := t.Scheduler.CreateJob(ctx, name, desc, schedule, prompt)
    if err != nil {
        return ToolResult{Error: fmt.Errorf("create cron job: %w", err)}
    }
    return ToolResult{Content: fmt.Sprintf("定时任务 \"%s\" 已创建（ID: %d），计划: %s", j.Name, j.ID, j.Schedule)}
}

// ListCronJobsTool returns all scheduled tasks.
type ListCronJobsTool struct {
    Scheduler *scheduler.Scheduler
}

func (t *ListCronJobsTool) Name() string        { return "list_cron_jobs" }
func (t *ListCronJobsTool) Description() string { return "列出所有已创建的定时任务" }
func (t *ListCronJobsTool) Permission() PermissionLevel { return PermPublic }

func (t *ListCronJobsTool) Execute(ctx context.Context, _ map[string]any) ToolResult {
    jobs, err := t.Scheduler.ListJobs(ctx)
    if err != nil {
        return ToolResult{Error: fmt.Errorf("list cron jobs: %w", err)}
    }
    if len(jobs) == 0 {
        return ToolResult{Content: "当前没有定时任务"}
    }
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("共 %d 个定时任务：\n\n", len(jobs)))
    for _, j := range jobs {
        status := "启用"
        if !j.Enabled {
            status = "禁用"
        }
        sb.WriteString(fmt.Sprintf("ID %d: %s (%s)\n  计划: %s\n  描述: %s\n\n",
            j.ID, j.Name, status, j.Schedule, j.Description))
    }
    return ToolResult{Content: sb.String()}
}

// DeleteCronJobTool removes a scheduled task by ID.
type DeleteCronJobTool struct {
    Scheduler *scheduler.Scheduler
}

func (t *DeleteCronJobTool) Name() string        { return "delete_cron_job" }
func (t *DeleteCronJobTool) Description() string {
    return `删除一个定时任务。参数 JSON: {"id": <任务ID(数字)>}`
}
func (t *DeleteCronJobTool) Permission() PermissionLevel { return PermProtected }

func (t *DeleteCronJobTool) Execute(ctx context.Context, args map[string]any) ToolResult {
    idFloat, ok := args["id"].(float64)
    if !ok || idFloat <= 0 {
        return ToolResult{Content: "请提供有效的任务 ID（数字）"}
    }
    id := int64(idFloat)
    if err := t.Scheduler.DeleteJob(ctx, id); err != nil {
        return ToolResult{Error: fmt.Errorf("delete cron job %d: %w", id, err)}
    }
    return ToolResult{Content: fmt.Sprintf("定时任务 ID %d 已删除", id)}
}
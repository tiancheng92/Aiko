# 三期定时任务系统 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现定时任务系统：AI 自然语言创建/列出/删除定时任务、后端 cron 调度器执行任务并将结果通过通用气泡通知展示（依赖 Plan C NotificationBubble）。

**Architecture:**
- 后端：`internal/scheduler/` 包，`Scheduler` 持有 cron job 列表（SQLite 持久化），每个 job 包含 cron 表达式 + 自然语言描述 + 执行 prompt；触发时通过独立执行路径调用 LLM（**不写入对话记忆**），结果通过 Wails 事件 `notification:show` 推送。
- 工具层：`create_cron_job`、`list_cron_jobs`、`delete_cron_job` 三个 AI 工具，依赖注入 Scheduler。
- 前端：复用 Plan C 的 `NotificationBubble.vue` 组件，Scheduler 结果 emit `notification:show` 即可展示气泡。
- 预设任务：Scheduler 初始化时写入 seed 数据（天气、早报），可在设置 → 定时任务中删除。

**前置依赖：Plan C Task 5（NotificationBubble.vue）必须先完成。**

**Tech Stack:** Go `github.com/robfig/cron/v3`（需添加依赖）、SQLite、Wails Events、Vue 3

---

## 文件结构

| 操作 | 文件 | 说明 |
|---|---|---|
| Create | `internal/scheduler/scheduler.go` | Scheduler 核心：job CRUD、cron 引擎、触发逻辑 |
| Create | `internal/tools/scheduler_tools.go` | create_cron_job / list_cron_jobs / delete_cron_job |
| Modify | `internal/db/sqlite.go` | 添加 cron_jobs 表 migration |
| Modify | `internal/tools/registry.go` | AllContextual 加入 scheduler 工具 |
| Modify | `app.go` | startup 创建 Scheduler；initLLMComponents 注入工具 |

---

### Task 1: 添加 cron/v3 依赖 + DB migration

**Files:**
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: 安装 robfig/cron/v3**

```bash
go get github.com/robfig/cron/v3
```

Expected: `go.mod` 新增 `github.com/robfig/cron/v3` 行。

- [ ] **Step 2: 在 `migrate()` 中追加 cron_jobs 表**

在 `migrate` 函数的 SQL 末尾（`);` 之前）追加：

```sql
CREATE TABLE IF NOT EXISTS cron_jobs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    schedule    TEXT NOT NULL,
    prompt      TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    last_run    DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 4: Commit**

```bash
git add internal/db/sqlite.go go.mod go.sum
git commit -m "feat: add cron_jobs table and robfig/cron dependency"
```

---

### Task 2: 实现 Scheduler

**Files:**
- Create: `internal/scheduler/scheduler.go`

- [ ] **Step 1: 创建 `internal/scheduler/scheduler.go`**

```go
// internal/scheduler/scheduler.go
package scheduler

import (
    "context"
    "database/sql"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/robfig/cron/v3"
)

// Job represents a single scheduled task persisted in SQLite.
type Job struct {
    ID          int64
    Name        string
    Description string
    Schedule    string // cron expression e.g. "0 8 * * *"
    Prompt      string // the message to send to the agent
    Enabled     bool
    LastRun     *time.Time
    CreatedAt   time.Time
}

// ResultFunc is called when a job fires, with the job and the agent's response.
type ResultFunc func(job Job, result string, err error)

// Scheduler manages cron jobs backed by SQLite.
type Scheduler struct {
    mu       sync.Mutex
    db       *sql.DB
    cr       *cron.Cron
    entryIDs map[int64]cron.EntryID // job.ID -> cron entry ID
    chatFn   func(ctx context.Context, prompt string) (string, error)
    onResult ResultFunc
}

// New creates a Scheduler. chatFn is called to execute each job's prompt.
// onResult is called with the job output after each execution.
func New(db *sql.DB, chatFn func(ctx context.Context, prompt string) (string, error), onResult ResultFunc) *Scheduler {
    s := &Scheduler{
        db:       db,
        cr:       cron.New(),
        entryIDs: make(map[int64]cron.EntryID),
        chatFn:   chatFn,
        onResult: onResult,
    }
    return s
}

// Start loads all enabled jobs from DB and starts the cron engine.
func (s *Scheduler) Start(ctx context.Context) error {
    jobs, err := s.ListJobs(ctx)
    if err != nil {
        return fmt.Errorf("load jobs: %w", err)
    }
    for _, j := range jobs {
        if j.Enabled {
            if err := s.scheduleJob(j); err != nil {
                slog.Warn("failed to schedule job", "job", j.Name, "err", err)
            }
        }
    }
    s.cr.Start()
    return nil
}

// Stop halts the cron engine.
func (s *Scheduler) Stop() {
    s.cr.Stop()
}

// CreateJob persists a new job and schedules it immediately.
func (s *Scheduler) CreateJob(ctx context.Context, name, description, schedule, prompt string) (Job, error) {
    // Validate the cron expression before persisting.
    if _, err := cron.ParseStandard(schedule); err != nil {
        return Job{}, fmt.Errorf("invalid cron expression %q: %w", schedule, err)
    }
    res, err := s.db.ExecContext(ctx, `
        INSERT INTO cron_jobs(name, description, schedule, prompt, enabled)
        VALUES (?, ?, ?, ?, 1)
    `, name, description, schedule, prompt)
    if err != nil {
        return Job{}, fmt.Errorf("insert job: %w", err)
    }
    id, _ := res.LastInsertId()
    j := Job{ID: id, Name: name, Description: description, Schedule: schedule, Prompt: prompt, Enabled: true}
    if err := s.scheduleJob(j); err != nil {
        return j, fmt.Errorf("schedule job: %w", err)
    }
    return j, nil
}

// DeleteJob removes a job from cron and from the DB.
func (s *Scheduler) DeleteJob(ctx context.Context, id int64) error {
    s.mu.Lock()
    if eid, ok := s.entryIDs[id]; ok {
        s.cr.Remove(eid)
        delete(s.entryIDs, id)
    }
    s.mu.Unlock()
    _, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
    return err
}

// ListJobs returns all jobs ordered by created_at.
func (s *Scheduler) ListJobs(ctx context.Context) ([]Job, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT id, name, description, schedule, prompt, enabled, last_run, created_at
        FROM cron_jobs ORDER BY created_at ASC
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var jobs []Job
    for rows.Next() {
        var j Job
        var enabled int
        var lastRun sql.NullTime
        var createdAt string
        if err := rows.Scan(&j.ID, &j.Name, &j.Description, &j.Schedule, &j.Prompt, &enabled, &lastRun, &createdAt); err != nil {
            return nil, err
        }
        j.Enabled = enabled == 1
        if lastRun.Valid {
            j.LastRun = &lastRun.Time
        }
        jobs = append(jobs, j)
    }
    return jobs, rows.Err()
}

// scheduleJob registers a job with the cron engine.
func (s *Scheduler) scheduleJob(j Job) error {
    eid, err := s.cr.AddFunc(j.Schedule, func() {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
        defer cancel()
        // Update last_run.
        _, _ = s.db.ExecContext(ctx, `UPDATE cron_jobs SET last_run=? WHERE id=?`, time.Now(), j.ID)
        slog.Info("cron job fired", "job", j.Name)
        result, err := s.chatFn(ctx, j.Prompt)
        if s.onResult != nil {
            s.onResult(j, result, err)
        }
    })
    if err != nil {
        return err
    }
    s.mu.Lock()
    s.entryIDs[j.ID] = eid
    s.mu.Unlock()
    return nil
}
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 3: Commit**

```bash
git add internal/scheduler/scheduler.go
git commit -m "feat: implement Scheduler with SQLite-backed cron jobs"
```

---

### Task 3: 实现 scheduler AI 工具

**Files:**
- Create: `internal/tools/scheduler_tools.go`

- [ ] **Step 1: 创建 `internal/tools/scheduler_tools.go`**

```go
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
```

- [ ] **Step 2: 在 `registry.go` 中更新 `AllContextual` 函数签名，加入 scheduler**

将现有 `AllContextual` 替换为：

```go
// AllContextual returns tools that require runtime dependencies.
func AllContextual(
    permStore *PermissionStore,
    knowledgeSt *knowledge.Store,
    sched *scheduler.Scheduler,
) []tool.BaseTool {
    contextTools := []Tool{
        &SearchKnowledgeTool{KnowledgeSt: knowledgeSt},
        &CreateCronJobTool{Scheduler: sched},
        &ListCronJobsTool{Scheduler: sched},
        &DeleteCronJobTool{Scheduler: sched},
    }
    result := make([]tool.BaseTool, len(contextTools))
    for i, t := range contextTools {
        result[i] = ToEino(t, permStore)
    }
    return result
}
```

在 `registry.go` import 中加入 `"desktop-pet/internal/scheduler"`（`"desktop-pet/internal/memory"` 不需要）。

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

Expected: 编译失败，`app.go` 中 `AllContextual` 调用参数不匹配 — 这是预期的，下一步修复。

- [ ] **Step 4: 在 `internal/agent/agent.go` 中添加 `ChatDirect` 方法**

`ChatDirect` 与 `Chat` 逻辑相同，但跳过 `persistAndMigrate`，确保定时任务的 prompt 和结果不写入短期/长期记忆：

```go
// ChatDirect sends a prompt to the agent and streams tokens without persisting
// the exchange to memory. Used by the scheduler to avoid polluting chat history.
func (a *Agent) ChatDirect(ctx context.Context, prompt string) <-chan StreamResult {
    ch := make(chan StreamResult, 64)

    go func() {
        defer close(ch)

        iter := a.runner.Query(ctx, prompt)

        var sb strings.Builder
        for {
            event, ok := iter.Next()
            if !ok {
                break
            }
            if event.Err != nil {
                ch <- StreamResult{Err: event.Err}
                return
            }
            if event.Output == nil || event.Output.MessageOutput == nil {
                continue
            }
            mo := event.Output.MessageOutput
            if mo.IsStreaming {
                for {
                    msg, recvErr := mo.MessageStream.Recv()
                    if recvErr != nil {
                        if recvErr == io.EOF {
                            break
                        }
                        ch <- StreamResult{Err: recvErr}
                        return
                    }
                    if msg != nil && msg.Content != "" {
                        ch <- StreamResult{Token: msg.Content}
                        sb.WriteString(msg.Content)
                    }
                }
            } else if mo.Message != nil && mo.Message.Content != "" {
                ch <- StreamResult{Token: mo.Message.Content}
                sb.WriteString(mo.Message.Content)
            }
        }
        ch <- StreamResult{Done: true}
        // NOTE: No persistAndMigrate call here — intentional.
    }()

    return ch
}
```

- [ ] **Step 5: 在 `app.go` 中集成 Scheduler**

在 `App` struct 中加入：

```go
scheduler    *scheduler.Scheduler
```

在 import 中加入 `"desktop-pet/internal/scheduler"`。

在 `startup` 的 `enableClickThrough()` 之前（longMem/knowledgeSt 尚不存在，先创建空 scheduler）：

```go
// Scheduler is started after LLM components init in initLLMComponents.
```

在 `initLLMComponents` 末尾（`a.mu.Lock()` 之前）创建并启动 scheduler：

```go
// Build a chat function for the scheduler.
// IMPORTANT: Scheduler jobs use a direct LLM call that bypasses persistAndMigrate,
// so job prompts and results are NOT written to short/long-term memory.
chatFn := func(ctx context.Context, prompt string) (string, error) {
    a.mu.RLock()
    ag := a.petAgent
    a.mu.RUnlock()
    if ag == nil {
        return "", fmt.Errorf("agent not ready")
    }
    ch := ag.ChatDirect(ctx, prompt) // ChatDirect skips memory persistence
    var sb strings.Builder
    for r := range ch {
        if r.Err != nil {
            return "", r.Err
        }
        if r.Done {
            break
        }
        sb.WriteString(r.Token)
    }
    return sb.String(), nil
}

onResult := func(job scheduler.Job, result string, err error) {
    if err != nil {
        slog.Error("cron job failed", "job", job.Name, "err", err)
        return
    }
    // Emit to the unified notification channel consumed by NotificationBubble.vue.
    wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
        "title":   job.Name,
        "message": result,
    })
}

sched := scheduler.New(a.sqlDB, chatFn, onResult)
if err := sched.Start(a.ctx); err != nil {
    slog.Error("scheduler start failed", "err", err)
}
```

将 `initLLMComponents` 末尾的 `a.mu.Lock()` 块更新为：

```go
a.mu.Lock()
if a.scheduler != nil {
    a.scheduler.Stop()
}
a.scheduler = sched
a.longMem = longMem
a.knowledgeSt = knowledgeSt
a.petAgent = newAgent
a.mu.Unlock()
```

将 `contextTools := internaltools.AllContextual(...)` 调用改为：

```go
contextTools := internaltools.AllContextual(a.permStore, knowledgeSt, sched)
```

在 `startup` 的 `EnsureRow` 循环后追加新工具行注册：

```go
for _, t := range []internaltools.Tool{
    &internaltools.SearchKnowledgeTool{},
    &internaltools.CreateCronJobTool{},
    &internaltools.ListCronJobsTool{},
    &internaltools.DeleteCronJobTool{},
} {
    _ = a.permStore.EnsureRow(toolsCtx, t)
}
```

- [ ] **Step 6: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 7: Commit**

```bash
git add internal/tools/scheduler_tools.go internal/tools/registry.go app.go internal/scheduler/ internal/agent/agent.go
git commit -m "feat: add cron job AI tools and wire scheduler into app; add ChatDirect for memory-free execution"
```

---

### Task 4: 预设任务 seed 数据

**Files:**
- Modify: `internal/scheduler/scheduler.go`

- [ ] **Step 1: 添加 `SeedDefaultJobs` 函数**

在 `scheduler.go` 末尾追加：

```go
// SeedDefaultJobs inserts built-in preset jobs if they don't already exist.
// Each job is inserted only if no job with the same name exists.
func (s *Scheduler) SeedDefaultJobs(ctx context.Context) error {
    presets := []struct {
        name     string
        desc     string
        schedule string
        prompt   string
    }{
        {
            name:     "每日早报",
            desc:     "每天早上8点生成简短早报",
            schedule: "0 8 * * *",
            prompt:   "请生成今天的简短早报，包含日期、星期、一句激励语和今日注意事项提示。",
        },
        {
            name:     "定时天气提醒",
            desc:     "每天中午提醒查看天气",
            schedule: "0 12 * * *",
            prompt:   "请提醒我去查看今天下午和明天的天气预报，做好出行准备。",
        },
    }
    for _, p := range presets {
        var count int
        err := s.db.QueryRowContext(ctx,
            `SELECT COUNT(*) FROM cron_jobs WHERE name = ?`, p.name,
        ).Scan(&count)
        if err != nil || count > 0 {
            continue
        }
        if _, err := s.CreateJob(ctx, p.name, p.desc, p.schedule, p.prompt); err != nil {
            slog.Warn("seed default job failed", "name", p.name, "err", err)
        }
    }
    return nil
}
```

- [ ] **Step 2: 在 `app.go` 的 `initLLMComponents` 中调用 seed**

在 `sched.Start(a.ctx)` 之后添加：

```go
if err := sched.SeedDefaultJobs(a.ctx); err != nil {
    slog.Warn("seed default jobs failed", "err", err)
}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/scheduler/scheduler.go app.go
git commit -m "feat: seed default daily report and weather reminder cron jobs"
```

---

## Self-Review

**Spec coverage:**
- ✅ #5 定时任务（自然语言设定）— Task 3 (create_cron_job 工具)
- ✅ #5 定时任务增删改查 — Task 3 (list / delete 工具；设置界面的管理可通过 AI 对话完成)
- ✅ #10 预设内置定时任务 — Task 4 (SeedDefaultJobs)
- ✅ #11 气泡通知 — 复用 Plan C Task 5 NotificationBubble；本 Plan 通过 `notification:show` 事件触发
- ✅ 定时任务不写记忆 — `ChatDirect`（Task 3 Step 4）跳过 `persistAndMigrate`

**Placeholder scan:** 无 TBD / TODO。`chatFn` 在 Task 3 Step 5 中有完整实现。

**Type consistency:** `scheduler.Job`、`scheduler.Scheduler`、`ResultFunc` 在 Task 2 中定义；Task 3/4 均正确引用。`AllContextual` 去掉 `longMem`、去掉 `SearchMemoryTool`，新增 `*scheduler.Scheduler`，在 Task 3 Step 2 和 Step 5 同步更新。`ChatDirect` 返回 `<-chan StreamResult`，与 `chatFn` 消费方式一致。

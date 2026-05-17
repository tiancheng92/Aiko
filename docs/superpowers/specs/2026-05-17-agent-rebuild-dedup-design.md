# 子项目 3：消除 initLLMComponents / rebuildAgentTools 中的重复 Agent 构建逻辑

**日期：** 2026-05-17  
**范围：** `app.go`  
**目标：** 将 `initLLMComponents` 和 `rebuildAgentTools` 中重复的工具列表构建 + `agent.New` 调用提取为私有辅助方法，消除约 40 行重复代码。

---

## 背景

`initLLMComponents`（~492 行）和 `rebuildAgentTools`（~760 行）都需要构建 eino Agent。两者构建 Agent 的代码完全相同：

**重复部分（约 40 行）：**

```go
builtinTools := internaltools.AllEino(a.permStore)
contextTools := internaltools.AllContextual(
    a.permStore,
    knowledgeSt,
    sched,
    longMem,
    dataDir,
    a.cfg,
    func(id string, cancel func()) { ... },
    func(id string) { ... },
    func() { go a.initLLMComponents(a.ctx) },
    a.InstallUpdate,
    func(event string, data any) { wailsruntime.EventsEmit(a.ctx, event, data) },
    version,
)
proactiveStore := proactive.NewStore(a.sqlDB)
followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), a.permStore)
allTools := append(append(builtinTools, contextTools...), extraTools...)
allTools = append(allTools, followupTool)

autoSkillsDir := filepath.Join(dataDir, "auto-skills")
skillDirs := append(append([]string{}, cfgSkillsDirs...), autoSkillsDir)
skillMW, err := skill.NewMiddleware(ctx, skillDirs)
// ...

mw := middleware.Chain(
    middleware.Logging(),
    middleware.Retry(3, 200*time.Millisecond),
    middleware.ErrorRecovery(),
)

newAgent, err := agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW, dataDir,
    &a.pendingConfirms,
    func(event string, data ...any) { wailsruntime.EventsEmit(a.ctx, event, data...) },
)
```

**两者的差异（不重复，各自独有）：**

| 方面 | `initLLMComponents` | `rebuildAgentTools` |
|------|---------------------|---------------------|
| chatFn（传给 scheduler.New） | 富流式版，含 SaveToMemory 分支 | 简单 `ChatDirect` 版 |
| 额外工具 | 无额外工具（0 个） | 传入的 `mcpTools` |
| 上下文依赖 | 自行创建 chatModel/embedder/longMem | 从字段快照读取已有组件 |
| 锁定后的 swap | 完整替换 scheduler/proactiveEngine/TTS 等 | 仅替换 petAgent/mcpClosers |

---

## 设计方案

### 提取私有辅助方法 `buildAgent`

新增一个私有方法，接受所有已解析的依赖，返回构建好的 `*agent.Agent`：

```go
// buildAgent assembles the tool list and constructs a new eino Agent.
// All inputs must be fully resolved before calling; the caller is responsible
// for swapping a.petAgent under a.mu once the agent is ready.
func (a *App) buildAgent(
    ctx context.Context,
    chatModel eino.ChatModel,
    longMem *memory.LongStore,
    knowledgeSt *knowledge.Store,
    sched *scheduler.Scheduler,
    extraTools []tool.BaseTool, // nil for phase-1 build; mcpTools for phase-2
    cfgSkillsDirs []string,     // snapshot of cfg.SkillsDirs at call time
) (*agent.Agent, error) {
    dataDir := a.dataDir

    builtinTools := internaltools.AllEino(a.permStore)
    contextTools := internaltools.AllContextual(
        a.permStore,
        knowledgeSt,
        sched,
        longMem,
        dataDir,
        a.cfg,
        func(id string, cancel func()) {
            a.runningCmds.Store(id, cancel)
            wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]any{"id": id})
        },
        func(id string) {
            a.runningCmds.Delete(id)
            wailsruntime.EventsEmit(a.ctx, "tool:executed", map[string]any{"id": id})
        },
        func() { go a.initLLMComponents(a.ctx) },
        a.InstallUpdate,
        func(event string, data any) { wailsruntime.EventsEmit(a.ctx, event, data) },
        version,
    )
    proactiveStore := proactive.NewStore(a.sqlDB)
    followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), a.permStore)

    allTools := append(builtinTools, contextTools...)
    if len(extraTools) > 0 {
        allTools = append(allTools, extraTools...)
    }
    allTools = append(allTools, followupTool)

    autoSkillsDir := filepath.Join(dataDir, "auto-skills")
    skillDirs := append(append([]string{}, cfgSkillsDirs...), autoSkillsDir)
    skillMW, err := skill.NewMiddleware(ctx, skillDirs)
    if err != nil {
        return nil, fmt.Errorf("load skills: %w", err)
    }

    mw := middleware.Chain(
        middleware.Logging(),
        middleware.Retry(3, 200*time.Millisecond),
        middleware.ErrorRecovery(),
    )

    return agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW, dataDir,
        &a.pendingConfirms,
        func(event string, data ...any) {
            wailsruntime.EventsEmit(a.ctx, event, data...)
        },
    )
}
```

### 简化后的调用方

**`initLLMComponents` 中的 agent 构建段**（替换掉约 40 行重复代码）：

```go
newAgent, err := a.buildAgent(ctx, chatModel, longMem, knowledgeSt, sched, nil, cfg.SkillsDirs)
if err != nil {
    return fmt.Errorf("build agent: %w", err)
}
```

**`rebuildAgentTools` 中的 agent 构建段**（替换掉约 40 行重复代码）：

```go
newAgent, err := a.buildAgent(ctx, chatModel, longMem, knowledgeSt, sched, mcpTools, cfgSnapshot.SkillsDirs)
if err != nil {
    return fmt.Errorf("build agent: %w", err)
}
```

注意：`rebuildAgentTools` 中那个简单的 `chatFn` 闭包（只用了 `ChatDirect`，当前在 `contextTools` 结构之外但实际上 **没有被任何 tool 使用**——它被赋值给了一个局部变量，随后只有 `_ = chatFn` 这行存在）。这个 chatFn 是历史遗留的死代码，可以在此次重构中一并删除。

### 死代码清理

`rebuildAgentTools` 中存在以下死代码：

```go
chatFn := func(ctx context.Context, prompt string) (string, error) { ... }
// ...
_ = chatFn // 用于抑制 "declared but not used" 编译错误
```

这个 `chatFn` 在 `rebuildAgentTools` 中没有实际用途（`sched` 是从 `a.scheduler` 快照的，scheduler 已经在 `initLLMComponents` 中创建时绑定了 chatFn）。随着 `buildAgent` 的引入，`rebuildAgentTools` 中的 `chatFn` 和 `_ = chatFn` 行可以一并删除。

---

## 约束

- `buildAgent` 是私有方法（小写），不暴露给 Wails
- `initLLMComponents` 和 `rebuildAgentTools` 的签名不变
- 不改变任何业务逻辑，只是等价提取
- 净减少约 40 行代码（提取 ~45 行，两个调用方各减少 ~40 行）

---

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `app.go` | 修改 | 新增 `buildAgent` 私有方法；简化 `initLLMComponents` 和 `rebuildAgentTools` |

## 测试策略

- 无新增测试（纯等价重构）
- 构建验证：`go build ./...` 和 `go build -race ./...`
- 人工验证：`make run` 启动，确认聊天正常、MCP 工具加载成功（日志出现 `mcp async load: agent rebuilt`）

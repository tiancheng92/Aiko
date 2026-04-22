# 三期基础增强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 迁移日志到 slog、修复全局快捷键、添加历史记忆搜索和知识库检索两个 AI 工具。

**Architecture:** slog 替换 `log.Printf`，统一结构化日志；全局快捷键通过 macOS NSEvent 全局监听器（CGo）实现，无需辅助功能权限（仅监听 key-down）；memory_search / knowledge_search 作为依赖注入型工具，在 `initLLMComponents` 中构造后传入 agent。

**Tech Stack:** Go `log/slog`（标准库）、Objective-C NSEvent（CGo，macos.go）、`desktop-pet/internal/tools`（现有 Tool 接口）

---

## 文件结构

| 操作 | 文件 | 说明 |
|---|---|---|
| Modify | `main.go` | 启动时初始化 slog 全局 logger |
| Modify | `internal/agent/agent.go` | 替换 `log.Printf` → `slog` |
| Modify | `internal/agent/middleware/logging.go` | 替换 `log.Printf` → `slog` |
| Modify | `macos.go` | 添加全局 NSEvent key-down 监听 + Go 回调 |
| Modify | `app.go` | startup 中注册全局热键；initLLMComponents 注入新工具 |
| Create | `internal/tools/context_tools.go` | `SearchKnowledgeTool` |
| Modify | `internal/tools/registry.go` | `AllContextual()` 返回需要注入的工具 |

---

### Task 1: 迁移 log → slog

**Files:**
- Modify: `main.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/middleware/logging.go`

- [ ] **Step 1: 在 `main.go` 中初始化 slog，替换默认 logger**

```go
// main.go — 在 main() 最开头添加（import 加 "log/slog", "os"）
func main() {
    // 使用 TEXT handler 输出到 stderr，带时间戳和来源文件
    h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
        Level:     slog.LevelDebug,
        AddSource: true,
    })
    slog.SetDefault(slog.New(h))

    app := NewApp()
    // ... 其余不变
```

- [ ] **Step 2: 替换 `internal/agent/agent.go` 中的 `log.Printf`**

删除 `import "log"`，改为 `import "log/slog"`，并替换所有调用：

```go
// 原: log.Printf("short memory Recent error: %v", err)
slog.Warn("short memory Recent error", "err", err)

// 原: log.Printf("agent: failed to save user message: %v", err)
slog.Error("save user message failed", "err", err)

// 原: log.Printf("agent: failed to save assistant message: %v", err)
slog.Error("save assistant message failed", "err", err)

// 原: log.Printf("agent: failed to count messages: %v", err)
slog.Error("count messages failed", "err", err)

// 原: log.Printf("agent: failed to get oldest messages: %v", err)
slog.Error("get oldest messages failed", "err", err)

// 原: log.Printf("agent: failed to store long-term memory: %v", err)
slog.Error("store long-term memory failed", "err", err)

// 原: log.Printf("agent: failed to delete migrated messages: %v", err)
slog.Error("delete migrated messages failed", "err", err)
```

- [ ] **Step 3: 替换 `internal/agent/middleware/logging.go`**

删除 `import "log"`，改为 `import "log/slog"`：

```go
// internal/agent/middleware/logging.go
package middleware

import (
    "context"
    "log/slog"
    "time"
)

// Logging returns a Middleware that logs each tool invocation with its duration.
func Logging() Middleware {
    return func(name string, next Handler) Handler {
        return func(ctx context.Context, input string) (string, error) {
            start := time.Now()
            out, err := next(ctx, input)
            elapsed := time.Since(start)
            if err != nil {
                slog.Error("tool invocation failed", "tool", name, "err", err, "elapsed", elapsed)
            } else {
                slog.Debug("tool invoked", "tool", name, "elapsed", elapsed)
            }
            return out, err
        }
    }
}
```

- [ ] **Step 4: 替换 `app.go` 中的 `fmt.Fprintf(os.Stderr, ...)`**

```go
// 原: fmt.Fprintf(os.Stderr, "init llm components: %v\n", err)
slog.Error("init llm components failed", "err", err)
```

删除 `app.go` 顶部 import 中的 `"os"`（如其他地方仍用则保留），加入 `"log/slog"`。

- [ ] **Step 5: 验证编译通过**

```bash
go build ./...
```

Expected: 无输出（编译成功）。

- [ ] **Step 6: Commit**

```bash
git add main.go internal/agent/agent.go internal/agent/middleware/logging.go app.go
git commit -m "refactor: migrate from log to slog for structured logging"
```

---

### Task 2: 修复全局快捷键 Cmd+Shift+P

**背景：** Wails 菜单快捷键只在 app 为前台 app 时生效。由于窗口设置了 `setIgnoresMouseEvents:YES`，其他 app 始终为前台 app，所以菜单快捷键从不触发。
**方案：** 在 `macos.go` 中添加 `NSEvent` 全局 key-down 监听，检测 Cmd+Shift+P，通过 CGo 回调 Go 函数触发 `bubble:toggle` 事件。

> **注意：** macOS 14+ 上 `addGlobalMonitorForEventsMatchingMask:NSEventMaskKeyDown` 需要在 "系统设置 → 隐私与安全性 → 输入监控" 中授权本 app。首次触发时系统会自动弹框请求。

**Files:**
- Modify: `macos.go`
- Modify: `app.go`

- [ ] **Step 1: 在 `macos.go` 中添加热键回调机制**

在 `macos.go` 的 C 代码块中（`import "C"` 之前）添加：

```c
// 全局 Go 函数指针，由 Go 侧在 startup 时设置
static void (*gHotkeyCallback)(void) = NULL;

// setHotkeyCallback 由 Go 侧调用，注册热键触发时的回调。
void setHotkeyCallback(void (*cb)(void)) {
    gHotkeyCallback = cb;
}

// enableGlobalHotkey 注册 Cmd+Shift+P 的全局 NSEvent 监听器。
void enableGlobalHotkey() {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSEventMask keyMask = NSEventMaskKeyDown;
        [NSEvent addGlobalMonitorForEventsMatchingMask:keyMask
            handler:^(NSEvent *evt) {
                // keyCode 35 = P, flags & (Cmd|Shift)
                NSEventModifierFlags flags = evt.modifierFlags &
                    (NSEventModifierFlagCommand | NSEventModifierFlagShift);
                if (evt.keyCode == 35 &&
                    flags == (NSEventModifierFlagCommand | NSEventModifierFlagShift)) {
                    if (gHotkeyCallback) {
                        dispatch_async(dispatch_get_main_queue(), ^{
                            gHotkeyCallback();
                        });
                    }
                }
            }];
    });
}
```

- [ ] **Step 2: 在 `macos.go` 中添加 Go 侧导出函数和包装**

在 `import "C"` 之后，`macos.go` 末尾添加：

```go
import (
    "C"
    "unsafe"
)

// hotkeyHandler is set by RegisterHotkeyCallback and called from C on Cmd+Shift+P.
var hotkeyHandler func()

//export goHotkeyFired
func goHotkeyFired() {
    if hotkeyHandler != nil {
        hotkeyHandler()
    }
}

// RegisterHotkeyCallback registers fn to be called when Cmd+Shift+P is pressed globally.
func RegisterHotkeyCallback(fn func()) {
    hotkeyHandler = fn
    C.setHotkeyCallback((*[0]byte)(unsafe.Pointer(C.goHotkeyFired)))
    C.enableGlobalHotkey()
}
```

注意：顶部已有 `import "C"`，合并 import 即可（`unsafe` 加入 Go import 块）。

- [ ] **Step 3: 在 `app.go` 的 `startup` 中注册回调**

在 `enableClickThrough()` 之后添加：

```go
// Register global Cmd+Shift+P hotkey to toggle the chat bubble.
RegisterHotkeyCallback(func() {
    wailsruntime.EventsEmit(a.ctx, "bubble:toggle")
})
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 5: Commit**

```bash
git add macos.go app.go
git commit -m "feat: register global Cmd+Shift+P hotkey via NSEvent monitor"
```

---

### Task 3: 添加 search_knowledge 工具

> **注意：** `search_memory` 已不再需要。Plan E（MemPalace 记忆架构升级）后，`agent.go` 的 `buildHistoryPrefix` 在每次对话时自动调用 `longMem.Search`，长期记忆已透明注入上下文，AI 无需主动调用工具检索。`search_knowledge` 仍需要，因为知识库不在自动检索范围内。

**Files:**
- Create: `internal/tools/context_tools.go`
- Modify: `internal/tools/registry.go`
- Modify: `app.go`

- [ ] **Step 1: 创建 `internal/tools/context_tools.go`**

```go
// internal/tools/context_tools.go
package tools

import (
    "context"
    "fmt"
    "strings"

    "desktop-pet/internal/knowledge"
)

// SearchKnowledgeTool searches the knowledge base for relevant document chunks.
type SearchKnowledgeTool struct {
    KnowledgeSt *knowledge.Store
}

func (t *SearchKnowledgeTool) Name() string { return "search_knowledge" }
func (t *SearchKnowledgeTool) Description() string {
    return `搜索已导入的知识库文档，返回与查询最相关的段落。参数 JSON: {"query":"<搜索词>"}`
}
func (t *SearchKnowledgeTool) Permission() PermissionLevel { return PermPublic }

// Execute searches the knowledge store for the given query string.
func (t *SearchKnowledgeTool) Execute(ctx context.Context, args map[string]any) ToolResult {
    if t.KnowledgeSt == nil {
        return ToolResult{Content: "知识库未启用（需配置 Embedding 模型并导入文档）"}
    }
    query, _ := args["query"].(string)
    if query == "" {
        return ToolResult{Content: "请提供搜索词"}
    }
    results, err := t.KnowledgeSt.Search(ctx, query, 5)
    if err != nil {
        return ToolResult{Error: fmt.Errorf("search knowledge: %w", err)}
    }
    if len(results) == 0 {
        return ToolResult{Content: "知识库中未找到相关内容"}
    }
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("找到 %d 条相关知识库内容：\n\n", len(results)))
    for i, r := range results {
        sb.WriteString(fmt.Sprintf("--- 片段 %d ---\n%s\n\n", i+1, r))
    }
    return ToolResult{Content: sb.String()}
}
```

- [ ] **Step 2: 在 `registry.go` 中添加 `AllContextual()` 函数**

在 `AllEino()` 之后追加：

```go
// AllContextual returns tools that require runtime dependencies (knowledge store).
// These are registered separately in initLLMComponents after the store is created.
func AllContextual(
    permStore *PermissionStore,
    knowledgeSt *knowledge.Store,
) []tool.BaseTool {
    contextTools := []Tool{
        &SearchKnowledgeTool{KnowledgeSt: knowledgeSt},
    }
    result := make([]tool.BaseTool, len(contextTools))
    for i, t := range contextTools {
        result[i] = ToEino(t, permStore)
    }
    return result
}
```

在 `registry.go` 顶部 import 中加入：

```go
"desktop-pet/internal/knowledge"
```

- [ ] **Step 3: 在 `app.go` 的 `initLLMComponents` 中注册新工具**

在 `startup` 的 `EnsureRow` 循环中添加 contextual 工具的注册：

```go
// Ensure contextual tool permission rows (store not needed for row creation).
_ = a.permStore.EnsureRow(toolsCtx, &internaltools.SearchKnowledgeTool{})
```

在 `initLLMComponents` 里，`builtinTools := internaltools.AllEino(a.permStore)` 之后：

```go
// Built-in tools + context-aware tools (knowledge) + skill tools
builtinTools := internaltools.AllEino(a.permStore)
contextTools := internaltools.AllContextual(a.permStore, knowledgeSt)
skillTools, err := skill.LoadAll(a.cfg.SkillsDir)
if err != nil {
    return fmt.Errorf("load skills: %w", err)
}
allTools := append(builtinTools, contextTools...)
allTools = append(allTools, skillTools...)
```

（删除原 `skillTools, err := ...` 和 `allTools := append(builtinTools, skillTools...)` 两行）

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 5: Commit**

```bash
git add internal/tools/context_tools.go internal/tools/registry.go app.go
git commit -m "feat: add search_knowledge tool with dependency injection"
```

---

## Self-Review

**Spec coverage:**
- ✅ #1 slog — Task 1
- ✅ #4 快捷键 — Task 2
- ✅ #7 知识库检索 — Task 3 (SearchKnowledgeTool)
- ✅ #6 历史记忆搜索 — 由 Plan E LongStore 升级后自动处理，无需独立工具

**Placeholder scan:** 无 TBD / TODO。

**Type consistency:** `SearchKnowledgeTool.KnowledgeSt *knowledge.Store` — 与 `app.go` 字段类型一致。`AllContextual` 参数类型与 `initLLMComponents` 内变量一致。

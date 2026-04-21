# 桌面宠物二期剩余功能实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Phase 2 设计文档中尚未落地的四项功能：新增工具（FormatTime / GetNetworkStatus）、导出聊天记录、宠物右键菜单增强（切换表情 / 更换模型）、首次使用欢迎引导。

**Architecture:** 后端在 `internal/tools/` 补充两个工具并注册；`app.go` 新增 `ExportChatHistory` / `IsFirstLaunch` / `MarkWelcomeShown` 三个 Wails 绑定。前端 `ChatBubble.vue` 右键菜单添加"导出聊天记录"入口；`Live2DPet.vue` 右键菜单添加"切换表情"和"更换模型"；`ChatPanel.vue` 首次打开时注入静态欢迎消息。LocationTools（需要 CoreLocation entitlements）和 PermRestricted 级别因需要系统权限申请框架超出当前范围，暂不实现。

**Tech Stack:** Go + Wails v2 runtime（SaveFileDialog）+ Vue 3 `<script setup>` + pixi-live2d-display 0.4

---

## 文件索引

### 修改文件

| 文件 | 改动摘要 |
|------|---------|
| `internal/tools/tool.go` | 新增 `PermRestricted` 常量（供将来使用） |
| `internal/tools/time_tools.go` | 新增 `FormatTimeTool` |
| `internal/tools/system_tools.go` | 新增 `GetNetworkStatusTool` |
| `internal/tools/registry.go` | 在 `All()` 中注册两个新工具 |
| `app.go` | 新增 `ExportChatHistory` / `IsFirstLaunch` / `MarkWelcomeShown` |
| `frontend/src/components/ChatBubble.vue` | 右键菜单加"导出聊天记录"，调用 `ExportChatHistory` |
| `frontend/src/components/Live2DPet.vue` | 右键菜单加"切换表情" / "更换模型"，循环切换逻辑 |
| `frontend/src/components/ChatPanel.vue` | `onMounted` 中检测首次启动并插入欢迎消息 |

---

## Task 1：补充工具 — FormatTime + GetNetworkStatus

**Files:**
- Modify: `internal/tools/tool.go`
- Modify: `internal/tools/time_tools.go`
- Modify: `internal/tools/system_tools.go`
- Modify: `internal/tools/registry.go`

- [ ] **Step 1: 在 tool.go 中添加 PermRestricted 常量**

读取 `internal/tools/tool.go`，在 `PermProtected` 常量后追加：

```go
// PermRestricted tools require explicit user confirmation on every invocation.
// Reserved for future use (e.g. location, microphone access).
PermRestricted PermissionLevel = "restricted"
```

完整 const 块变为：

```go
const (
	// PermPublic tools run without any user approval (e.g. GetCurrentTime).
	PermPublic PermissionLevel = "public"
	// PermProtected tools require one-time user approval stored in the DB.
	PermProtected PermissionLevel = "protected"
	// PermRestricted tools require explicit user confirmation on every invocation.
	// Reserved for future use (e.g. location, microphone access).
	PermRestricted PermissionLevel = "restricted"
)
```

- [ ] **Step 2: 在 time_tools.go 末尾添加 FormatTimeTool**

在 `internal/tools/time_tools.go` 末尾追加：

```go
// FormatTimeTool formats the current time using a Go time layout string.
type FormatTimeTool struct{}

func (t *FormatTimeTool) Name() string             { return "format_time" }
func (t *FormatTimeTool) Description() string {
	return `将当前时间按指定格式输出。参数 JSON: {"layout":"<Go time layout>"}，默认格式为 RFC3339。` +
		`常用格式示例: "2006-01-02 15:04:05" (本地), "Monday, 02 Jan 2006" (英文日期)。`
}
func (t *FormatTimeTool) Permission() PermissionLevel { return PermPublic }

// Execute formats time.Now() using the optional "layout" arg (Go time layout string).
func (t *FormatTimeTool) Execute(_ context.Context, args map[string]any) ToolResult {
	layout := time.RFC3339
	if l, ok := args["layout"].(string); ok && l != "" {
		layout = l
	}
	return ToolResult{Content: fmt.Sprintf("格式化时间: %s", time.Now().Format(layout))}
}
```

- [ ] **Step 3: 在 system_tools.go 末尾添加 GetNetworkStatusTool**

在 `internal/tools/system_tools.go` 顶部 import 中添加 `"net"` 和 `"time"`（已有 `"fmt"` 和 `"runtime"`）：

```go
import (
	"context"
	"fmt"
	"net"
	"runtime"
	"time"
)
```

在文件末尾追加：

```go
// GetNetworkStatusTool checks internet connectivity by dialing a well-known DNS server.
type GetNetworkStatusTool struct{}

func (t *GetNetworkStatusTool) Name() string             { return "get_network_status" }
func (t *GetNetworkStatusTool) Description() string      { return "检测当前网络连接状态（在线/离线）" }
func (t *GetNetworkStatusTool) Permission() PermissionLevel { return PermProtected }

// Execute dials 1.1.1.1:53 with a 3-second timeout to determine connectivity.
func (t *GetNetworkStatusTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	conn, err := net.DialTimeout("tcp", "1.1.1.1:53", 3*time.Second)
	if err != nil {
		return ToolResult{Content: "网络状态: 离线（无法连接互联网）"}
	}
	conn.Close()
	return ToolResult{Content: "网络状态: 在线"}
}
```

- [ ] **Step 4: 在 registry.go 的 All() 中注册新工具**

将 `All()` 函数改为：

```go
// All returns all built-in Tool instances in registration order.
func All() []Tool {
	return []Tool{
		&GetCurrentTimeTool{},
		&GetTimezoneTool{},
		&FormatTimeTool{},
		&GetOSInfoTool{},
		&GetHardwareInfoTool{},
		&GetNetworkStatusTool{},
	}
}
```

- [ ] **Step 5: 验证编译**

```bash
cd /Users/xutiancheng/code/self/desktop-pet
go build ./...
```

预期：无错误输出。

- [ ] **Step 6: Commit**

```bash
git add internal/tools/tool.go internal/tools/time_tools.go internal/tools/system_tools.go internal/tools/registry.go
git commit -m "feat(tools): add FormatTime, GetNetworkStatus tools and PermRestricted level"
```

---

## Task 2：后端新增 ExportChatHistory / IsFirstLaunch / MarkWelcomeShown

**Files:**
- Modify: `app.go`

- [ ] **Step 1: 在 app.go 末尾添加三个方法**

在 `app.go` 末尾（`GetAvailableModels` 之后）追加：

```go
// ExportChatHistory opens a native save dialog and writes the recent 1000
// messages as plain text to the user-chosen file. Returns nil if the user
// cancels without choosing a file.
func (a *App) ExportChatHistory() error {
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出聊天记录",
		DefaultFilename: fmt.Sprintf("chat-export-%s.txt", time.Now().Format("20060102-150405")),
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "文本文件", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return nil // user cancelled
	}

	msgs, err := a.shortMem.Recent(1000)
	if err != nil {
		return fmt.Errorf("load messages: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("聊天记录导出 — %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	for _, m := range msgs {
		label := m.Role
		switch m.Role {
		case "user":
			label = "用户"
		case "assistant":
			label = "宠物"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n%s\n\n", m.CreatedAt, label, m.Content))
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// IsFirstLaunch reports whether the welcome message has never been shown.
func (a *App) IsFirstLaunch() bool {
	var val string
	err := a.sqlDB.QueryRowContext(a.ctx,
		`SELECT value FROM settings WHERE key = 'welcome_shown'`).Scan(&val)
	return err != nil // row absent ⇒ first launch
}

// MarkWelcomeShown records that the welcome message has been displayed.
func (a *App) MarkWelcomeShown() error {
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key, value) VALUES('welcome_shown','1')
		 ON CONFLICT(key) DO UPDATE SET value='1'`)
	return err
}
```

`app.go` 顶部 import 区域已包含 `"os"`, `"strings"`, `"fmt"`, `"time"` 和 `wailsruntime`。确认无需新增 import。

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

预期：无错误。

- [ ] **Step 3: Commit**

```bash
git add app.go
git commit -m "feat(app): add ExportChatHistory, IsFirstLaunch, MarkWelcomeShown bindings"
```

---

## Task 3：前端 — ChatBubble 右键菜单添加导出聊天记录

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue`

- [ ] **Step 1: 在 script setup 添加 ExportChatHistory import 和导出函数**

在 `ChatBubble.vue` 的 import 行添加 `ExportChatHistory`：

```js
import { ExportChatHistory } from '../../wailsjs/go/main/App'
```

在 `clearHistory` 函数后追加：

```js
/** exportHistory opens a native save dialog and writes chat history to a file. */
async function exportHistory() {
  try {
    await ExportChatHistory()
  } catch (e) {
    console.error('export chat history failed:', e)
  }
}
```

- [ ] **Step 2: 在 chatMenuItems 中添加导出项**

将 `chatMenuItems` computed 改为：

```js
const chatMenuItems = computed(() => [
  { icon: '💾', label: '导出聊天记录', action: exportHistory },
  { icon: '🗑️', label: '清空聊天历史', action: clearHistory },
  { divider: true },
  { icon: '⚙️', label: '打开设置', action: () => emit('open-settings') },
])
```

- [ ] **Step 3: 验证前端构建**

```bash
cd /Users/xutiancheng/code/self/desktop-pet/frontend && yarn build 2>&1 | tail -5
```

预期：`✓ built in` 无报错。

- [ ] **Step 4: Commit**

```bash
cd ..
git add frontend/src/components/ChatBubble.vue
git commit -m "feat(frontend): add export chat history to bubble context menu"
```

---

## Task 4：前端 — Live2DPet 右键菜单增强（切换表情 / 更换模型）

**Files:**
- Modify: `frontend/src/components/Live2DPet.vue`

当前 `Live2DPet.vue` 的 `useModelPath` 只解构了 `{ modelPath, loadModels }`。

- [ ] **Step 1: 扩展 useModelPath 解构，加入 currentModel 和 availableModels**

将 `Live2DPet.vue` 中：

```js
const { modelPath, loadModels } = useModelPath()
```

改为：

```js
const { currentModel, availableModels, modelPath, loadModels } = useModelPath()
```

- [ ] **Step 2: 添加 SaveConfig import**

在 import 行中，将 `GetBallPosition, SaveBallPosition, GetScreenSize` 扩展为：

```js
import { GetBallPosition, SaveBallPosition, GetScreenSize, GetConfig, SaveConfig } from '../../wailsjs/go/main/App'
```

- [ ] **Step 3: 添加切换表情和切换模型的逻辑，更新 petMenuItems**

在 `const petMenuRef = ref(null)` 之后，将现有的 `petMenuItems` 及右键逻辑替换为：

```js
// Expression cycle — index into a fixed list; pixi-live2d silently ignores unknown IDs.
const EXPRESSIONS = ['f01', 'f02', 'f03', 'f04', 'f05']
let exprIdx = 0

/** cycleExpression advances the Live2D model to the next expression in rotation. */
function cycleExpression() {
  if (!live2dModel) return
  exprIdx = (exprIdx + 1) % EXPRESSIONS.length
  live2dModel.expression(EXPRESSIONS[exprIdx])
}

/** switchToNextModel cycles availableModels and persists the selection. */
async function switchToNextModel() {
  const models = availableModels.value
  if (models.length <= 1) return
  const idx = models.indexOf(currentModel.value)
  const next = models[(idx + 1) % models.length]
  // Emit immediately for instant visual feedback via the composable's EventsOn listener.
  EventsEmit('config:model:changed', next)
  try {
    const cfg = await GetConfig()
    if (cfg) {
      cfg.Live2DModel = next
      await SaveConfig(cfg)
    }
  } catch (e) {
    console.warn('switchToNextModel: failed to save config', e)
  }
}

const petMenuItems = [
  { icon: '🎭', label: '切换表情', action: cycleExpression },
  { icon: '👗', label: '更换模型', action: switchToNextModel },
  { divider: true },
  { icon: '⚙️', label: '打开设置', action: () => emit('open-settings') },
  { divider: true },
  { icon: '❌', label: '退出程序', action: () => Quit() },
]
```

还需要确保 `EventsEmit` 已 import。在 script setup 的 import 区域添加（如不存在）：

```js
import { EventsEmit } from '../../wailsjs/runtime/runtime'
```

（当前 `Live2DPet.vue` 只 import 了 `Quit`，没有 `EventsEmit`。）

- [ ] **Step 4: 验证前端构建**

```bash
cd /Users/xutiancheng/code/self/desktop-pet/frontend && yarn build 2>&1 | tail -5
```

预期：`✓ built in` 无报错。

- [ ] **Step 5: Commit**

```bash
cd ..
git add frontend/src/components/Live2DPet.vue
git commit -m "feat(frontend): add cycle-expression and switch-model to pet context menu"
```

---

## Task 5：前端 — ChatPanel 首次使用欢迎消息

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: 在 ChatPanel.vue 中添加 IsFirstLaunch / MarkWelcomeShown import**

在 `ChatPanel.vue` 的 import 行，将：

```js
import { SendMessage, GetMessages, ClearChatHistory } from '../../wailsjs/go/main/App'
```

改为：

```js
import { SendMessage, GetMessages, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: 在 onMounted 加载历史后插入欢迎消息逻辑**

在 `onMounted` 的 `scrollToBottom()` 调用之后，`offClear = EventsOn(...)` 之前，插入：

```js
  // Show welcome message on first launch when chat history is empty.
  if ((history || []).length === 0) {
    try {
      const first = await IsFirstLaunch()
      if (first) {
        messages.value.push({
          role: 'assistant',
          content: '你好！👋 我是你的 AI 桌面宠物。\n\n我支持：\n- 💬 **自然语言对话**\n- 🔧 **工具调用**（查询时间、系统信息、网络状态等）\n- 📚 **知识库问答**（在设置中导入文档）\n\n**快速操作提示：**\n- 右键点击我 → 切换表情 / 更换模型 / 打开设置\n- 右键点击聊天框 → 导出聊天记录\n\n请先在 ⚙️ **设置** 中配置 LLM 模型后开始聊天。',
        })
        scrollToBottom()
        await MarkWelcomeShown()
      }
    } catch (e) {
      console.warn('welcome check failed:', e)
    }
  }
```

- [ ] **Step 3: 验证前端构建**

```bash
cd /Users/xutiancheng/code/self/desktop-pet/frontend && yarn build 2>&1 | tail -5
```

预期：`✓ built in` 无报错。

- [ ] **Step 4: Commit**

```bash
cd ..
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): show welcome message on first launch in ChatPanel"
```

---

## Task 6：整体验证

- [ ] **Step 1: 完整构建**

```bash
cd /Users/xutiancheng/code/self/desktop-pet
go build ./... && cd frontend && yarn build && cd ..
```

预期：均无报错。

- [ ] **Step 2: 运行 wails dev 手动验证**

```bash
wails dev
```

手动验证清单：
- [ ] 设置 → 工具权限：新出现 `format_time`（public）和 `get_network_status`（protected）
- [ ] 开启 `get_network_status` 权限，向 AI 问"现在网络状态怎样" → AI 返回在线/离线结果
- [ ] 向 AI 问"帮我把时间格式化为 Monday, 02 Jan 2006" → AI 调用 `format_time` 并返回
- [ ] 右键聊天框 → 点击"导出聊天记录" → 弹出系统保存对话框 → 文件写入成功
- [ ] 右键宠物 → 点击"切换表情" → 宠物表情变化
- [ ] 右键宠物 → 点击"更换模型" → 宠物切换到下一个模型（循环）
- [ ] 删除 `~/.desktop-pet/desktop-pet.db` 后重启 → 聊天框打开后出现欢迎消息
- [ ] 再次重启（不删数据库）→ 欢迎消息不再出现

- [ ] **Step 3: 最终 Commit**

```bash
git add -A
git commit -m "feat: phase 2 remaining features — new tools, export chat, richer menus, welcome flow"
```

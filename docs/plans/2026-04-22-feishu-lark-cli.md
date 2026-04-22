# 飞书 lark-cli 接入实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将飞书 lark-cli 作为一个 AI 工具接入 desktop-pet，让 Agent 能通过 `lark-cli` 子进程操作飞书（发消息、查日历、读文档等），并在设置界面提供 lark-cli 的配置与鉴权引导。

**Architecture:** 新增 `internal/lark/` 包封装 lark-cli 子进程调用逻辑；新增 `LarkTool`（eino InvokableTool）注册到工具链；在 `Config` 中新增 `LarkCLIPath` 字段；在 `SettingsWindow.vue` 中新增「🪶 飞书」tab 提供路径配置、状态检测和鉴权引导。

**Tech Stack:** Go `os/exec`、lark-cli（npm 全局安装）、Vue 3 Composition API、现有 `internal/tools` 接口

---

## 文件结构

| 文件 | 操作 | 职责 |
|---|---|---|
| `internal/lark/client.go` | 新建 | 封装 lark-cli 子进程调用：`Run(args)`、`Status()`、`FindCLI()` |
| `internal/lark/tool.go` | 新建 | `LarkTool` 实现 `tools.Tool` 接口 |
| `internal/config/config.go` | 修改 | 新增 `LarkCLIPath string` 字段 |
| `app.go` | 修改 | 新增 `LarkStatus()`、`LarkAuthLogin()`、`LarkConfigInit()` Wails bindings；将 `LarkTool` 注入工具链 |
| `frontend/src/components/SettingsWindow.vue` | 修改 | 新增「🪶 飞书」tab：路径配置、状态检测、鉴权引导 |

---

## Task 1: lark-cli 子进程封装（`internal/lark/client.go`）

**Files:**
- Create: `internal/lark/client.go`

- [ ] **Step 1: 创建 `internal/lark/client.go`**

```go
// internal/lark/client.go
package lark

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Client wraps lark-cli subprocess calls.
type Client struct {
	// CLIPath is the path to the lark-cli executable. If empty, "lark-cli" is used.
	CLIPath string
}

// NewClient creates a Client. cliPath may be empty to use PATH resolution.
func NewClient(cliPath string) *Client {
	if cliPath == "" {
		cliPath = "lark-cli"
	}
	return &Client{CLIPath: cliPath}
}

// Run executes lark-cli with the given arguments and returns stdout.
// stderr is captured and appended to the error message on failure.
func (c *Client) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.CLIPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("lark-cli %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Status returns the output of `lark-cli auth status`.
// Returns an error if lark-cli is not installed or not authenticated.
func (c *Client) Status(ctx context.Context) (string, error) {
	return c.Run(ctx, "auth", "status")
}

// FindCLI returns the absolute path of lark-cli resolved from PATH,
// or an empty string if not found.
func FindCLI() string {
	p, err := exec.LookPath("lark-cli")
	if err != nil {
		return ""
	}
	return p
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/lark/...
```

Expected: 无输出，编译通过。

- [ ] **Step 3: commit**

```bash
git add internal/lark/client.go
git commit -m "feat: add lark client wrapping lark-cli subprocess"
```

---

## Task 2: LarkTool（`internal/lark/tool.go`）

**Files:**
- Create: `internal/lark/tool.go`

- [ ] **Step 1: 创建 `internal/lark/tool.go`**

```go
// internal/lark/tool.go
package lark

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Tool implements tools.Tool for lark-cli subprocess calls.
// It is injected with a Client at startup.
type Tool struct {
	Client *Client
}

// Name returns the tool identifier.
func (t *Tool) Name() string { return "lark" }

// Info returns the eino tool schema.
func (t *Tool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: "操作飞书：发消息、查日历、读文档等。通过 lark-cli 子进程执行，需提前完成 `lark-cli auth login`。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"args": {
				Type:     schema.String,
				Desc:     `lark-cli 命令参数，空格分隔，例如 "im +messages-send --chat-id oc_xxx --text Hello" 或 "calendar +agenda"`,
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun parses args and delegates to lark-cli.
func (t *Tool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var params struct {
		Args string `json:"args"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if params.Args == "" {
		return "请提供 lark-cli 命令参数", nil
	}
	parts := splitArgs(params.Args)
	// Always append --format json for structured output.
	parts = append(parts, "--format", "json")
	return t.Client.Run(ctx, parts...)
}

// splitArgs splits a shell-like argument string, respecting quoted strings.
func splitArgs(s string) []string {
	var args []string
	var cur []byte
	inQ := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			inQ = !inQ
		case c == ' ' && !inQ:
			if len(cur) > 0 {
				args = append(args, string(cur))
				cur = cur[:0]
			}
		default:
			cur = append(cur, c)
		}
	}
	if len(cur) > 0 {
		args = append(args, string(cur))
	}
	return args
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/lark/...
```

Expected: 无输出。

- [ ] **Step 3: commit**

```bash
git add internal/lark/tool.go
git commit -m "feat: add LarkTool implementing eino InvokableTool via lark-cli"
```

---

## Task 3: Tool 接口适配

`tools.Tool` 接口要求 `Permission() PermissionLevel`，但 `LarkTool` 在 `internal/lark` 包中，不能直接 import `internal/tools`（循环依赖）。解法：在 `internal/tools/registry.go` 中用一个适配器包装 `*lark.Tool`。

**Files:**
- Modify: `internal/tools/registry.go`
- Create: `internal/tools/lark_adapter.go`

- [ ] **Step 1: 创建 `internal/tools/lark_adapter.go`**

```go
// internal/tools/lark_adapter.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"desktop-pet/internal/lark"
)

// larkToolAdapter wraps *lark.Tool to satisfy the tools.Tool interface.
type larkToolAdapter struct {
	inner *lark.Tool
}

// WrapLarkTool wraps a *lark.Tool as a tools.Tool.
func WrapLarkTool(t *lark.Tool) Tool {
	return &larkToolAdapter{inner: t}
}

func (a *larkToolAdapter) Name() string                { return a.inner.Name() }
func (a *larkToolAdapter) Permission() PermissionLevel { return PermProtected }

func (a *larkToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return a.inner.Info(ctx)
}

func (a *larkToolAdapter) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	return a.inner.InvokableRun(ctx, input, opts...)
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/tools/... ./internal/lark/...
```

Expected: 无输出。

- [ ] **Step 3: commit**

```bash
git add internal/tools/lark_adapter.go
git commit -m "feat: add lark tool adapter for tools.Tool interface"
```

---

## Task 4: Config 新增 LarkCLIPath

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: 修改 `internal/config/config.go`**

在 `Config` struct 末尾新增字段：
```go
LarkCLIPath    string // lark-cli 可执行文件路径，空串表示从 PATH 自动查找
```

在 `Load()` 中 `cfg := &Config{...}` 块末尾新增：
```go
LarkCLIPath:    m["lark_cli_path"],
```

在 `Save()` 的 `pairs` map 中新增：
```go
"lark_cli_path": cfg.LarkCLIPath,
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/config/...
```

Expected: 无输出。

- [ ] **Step 3: commit**

```bash
git add internal/config/config.go
git commit -m "feat: add LarkCLIPath to config"
```

---

## Task 5: app.go — 注入 LarkTool + Wails bindings

**Files:**
- Modify: `app.go`

- [ ] **Step 1: 在 `initLLMComponents` 中注入 LarkTool**

在 `app.go` 顶部 import 块中新增：
```go
"desktop-pet/internal/lark"
```

在 `initLLMComponents` 的 `contextTools` 构建处修改 `AllContextual` 调用，将 lark tool 一并传入。先修改函数签名（见下一步），或直接在 `contextTools` 后 append：

在 `contextTools := internaltools.AllContextual(...)` 这行之后插入：
```go
// Inject lark tool if lark-cli is configured or discoverable.
larkCLIPath := a.cfg.LarkCLIPath
if larkCLIPath == "" {
    larkCLIPath = lark.FindCLI()
}
if larkCLIPath != "" {
    larkClient := lark.NewClient(larkCLIPath)
    larkTool := internaltools.WrapLarkTool(&lark.Tool{Client: larkClient})
    _ = a.permStore.EnsureRow(ctx, larkTool)
    contextTools = append(contextTools, internaltools.ToEino(larkTool, a.permStore))
}
```

- [ ] **Step 2: 新增 Wails bindings**

在 `app.go` 末尾（`SetCronJobEnabled` 之后）新增：

```go
// LarkStatus returns the output of `lark-cli auth status`.
func (a *App) LarkStatus() (string, error) {
	a.mu.RLock()
	cliPath := a.cfg.LarkCLIPath
	a.mu.RUnlock()
	if cliPath == "" {
		cliPath = lark.FindCLI()
	}
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装，请运行：npm install -g @larksuite/cli")
	}
	c := lark.NewClient(cliPath)
	return c.Status(a.ctx)
}

// LarkRunCommand executes an arbitrary lark-cli command and returns stdout.
func (a *App) LarkRunCommand(args string) (string, error) {
	a.mu.RLock()
	cliPath := a.cfg.LarkCLIPath
	a.mu.RUnlock()
	if cliPath == "" {
		cliPath = lark.FindCLI()
	}
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装")
	}
	c := lark.NewClient(cliPath)
	// Split args string into slice.
	parts := strings.Fields(args)
	return c.Run(a.ctx, parts...)
}
```

- [ ] **Step 3: 编译验证**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 4: 重新生成 Wails bindings**

```bash
wails generate module
```

Expected: 输出 KnownStructs 信息，无错误。验证：

```bash
grep -n "LarkStatus\|LarkRunCommand" frontend/wailsjs/go/main/App.js
```

Expected: 找到两个函数定义。

- [ ] **Step 5: commit**

```bash
git add app.go frontend/wailsjs/
git commit -m "feat: inject LarkTool into agent, add LarkStatus/LarkRunCommand bindings"
```

---

## Task 6: SettingsWindow.vue — 飞书 tab

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: 新增 import 和响应式状态**

在 script setup 的 import 行新增：
```js
import { LarkStatus, LarkRunCommand } from '../../wailsjs/go/main/App'
```

在 cron 状态下方新增：
```js
// Lark
const larkStatus = ref('')        // auth status 输出
const larkStatusLoading = ref(false)
const larkStatusError = ref('')
```

- [ ] **Step 2: 新增 `fetchLarkStatus` 函数**

```js
/** fetchLarkStatus checks lark-cli auth status. */
async function fetchLarkStatus() {
  larkStatusLoading.value = true
  larkStatusError.value = ''
  try {
    larkStatus.value = await LarkStatus()
  } catch (e) {
    larkStatusError.value = String(e)
    larkStatus.value = ''
  } finally {
    larkStatusLoading.value = false
  }
}
```

- [ ] **Step 3: 在 `onMounted` 末尾调用**

在 `onMounted` 的 `await fetchCronJobs()` 之后新增：
```js
fetchLarkStatus()
```

- [ ] **Step 4: 新增 tab 按钮**

在侧边栏 `⏰ 定时任务` 按钮之后新增：
```html
<button :class="{ active: activeTab === 'lark' }" @click="activeTab = 'lark'">🪶 飞书</button>
```

- [ ] **Step 5: 新增 tab 面板（在 cron tab 的 `</div>` 之后）**

```html
<!-- 飞书 lark-cli -->
<div v-if="activeTab === 'lark'" class="tab-pane">
  <label>lark-cli 路径
    <div class="url-row">
      <input v-model="cfg.LarkCLIPath" placeholder="留空自动从 PATH 查找（lark-cli）" />
      <button class="fetch-btn" @click="fetchLarkStatus" :disabled="larkStatusLoading">
        {{ larkStatusLoading ? '检测中...' : '检测状态' }}
      </button>
    </div>
  </label>

  <div v-if="larkStatus" class="lark-status lark-status--ok">
    <pre>{{ larkStatus }}</pre>
  </div>
  <div v-else-if="larkStatusError" class="lark-status lark-status--err">{{ larkStatusError }}</div>

  <div class="section-header" style="margin-top:8px">
    <h3>快速引导</h3>
  </div>
  <div class="lark-guide">
    <div class="lark-guide-step">
      <span class="lark-step-num">1</span>
      <div class="lark-step-body">
        <div class="lark-step-title">安装 lark-cli</div>
        <code class="lark-code">npm install -g @larksuite/cli</code>
      </div>
    </div>
    <div class="lark-guide-step">
      <span class="lark-step-num">2</span>
      <div class="lark-step-body">
        <div class="lark-step-title">初始化应用凭证（App ID / App Secret）</div>
        <code class="lark-code">lark-cli config init</code>
        <p class="lark-step-hint">在终端中运行，按提示填入飞书开放平台的 App ID 与 App Secret</p>
      </div>
    </div>
    <div class="lark-guide-step">
      <span class="lark-step-num">3</span>
      <div class="lark-step-body">
        <div class="lark-step-title">登录（获取用户 Access Token）</div>
        <code class="lark-code">lark-cli auth login --recommend</code>
        <p class="lark-step-hint">浏览器扫码授权后即可以用户身份访问飞书</p>
      </div>
    </div>
    <div class="lark-guide-step">
      <span class="lark-step-num">4</span>
      <div class="lark-step-body">
        <div class="lark-step-title">完成后点击"检测状态"验证</div>
      </div>
    </div>
  </div>

  <p class="lark-hint">
    配置完成后，AI 可通过 <code>lark</code> 工具操作飞书，例如：发消息、查日历、读文档等。<br>
    在「工具权限」中启用 <strong>lark</strong> 工具后生效。
  </p>
</div>
```

- [ ] **Step 6: 新增 CSS**

在 `<style scoped>` 中已有样式之后新增：

```css
/* Lark tab */
.lark-status {
  padding: 10px 12px;
  border-radius: 8px;
  font-size: 12px;
  font-family: 'Fira Code', monospace;
  white-space: pre-wrap;
  word-break: break-all;
}
.lark-status--ok  { background: rgba(34,197,94,0.08); border: 1px solid rgba(34,197,94,0.2); color: #4ade80; }
.lark-status--err { background: rgba(239,68,68,0.08); border: 1px solid rgba(239,68,68,0.2); color: #f87171; }
.lark-status pre { margin: 0; }
.lark-guide { display: flex; flex-direction: column; gap: 10px; }
.lark-guide-step {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  background: rgba(255,255,255,0.03);
  border: 1px solid rgba(255,255,255,0.07);
  border-radius: 8px;
}
.lark-step-num {
  flex-shrink: 0;
  width: 22px; height: 22px;
  border-radius: 50%;
  background: rgba(99,102,241,0.25);
  color: #a5b4fc;
  font-size: 12px;
  font-weight: 700;
  display: flex; align-items: center; justify-content: center;
}
.lark-step-body { display: flex; flex-direction: column; gap: 4px; }
.lark-step-title { font-size: 12px; font-weight: 600; color: #f9fafb; }
.lark-code {
  display: inline-block;
  font-family: 'Fira Code', monospace;
  font-size: 11px;
  background: rgba(0,0,0,0.35);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 4px;
  padding: 2px 8px;
  color: #e2e8f0;
  user-select: text;
}
.lark-step-hint { font-size: 11px; color: #9ca3af; margin: 0; }
.lark-hint {
  font-size: 11px;
  color: #6b7280;
  line-height: 1.6;
  padding: 8px 12px;
  background: rgba(255,255,255,0.02);
  border-radius: 6px;
}
.lark-hint code { font-family: 'Fira Code', monospace; color: #a5b4fc; }
```

- [ ] **Step 7: 编译验证**

```bash
cd frontend && yarn build
```

Expected: 构建成功，无错误。

- [ ] **Step 8: commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat: add Lark settings tab with lark-cli status and setup guide"
```

---

## Task 7: 在 startup 时注册 lark 工具权限行

**Files:**
- Modify: `app.go`

`LarkTool` 是运行时按需注入的（仅在 lark-cli 存在时），但权限行需要在 startup 时预注册，否则用户在「工具权限」里看不到它。

- [ ] **Step 1: 在 `startup` 的 EnsureRow 块中添加 lark 工具**

在 `startup` 函数中，现有的 contextual tool EnsureRow 块之后新增：

```go
// Pre-register lark tool permission row so it appears in settings.
_ = a.permStore.EnsureRow(toolsCtx, internaltools.WrapLarkTool(&lark.Tool{}))
```

- [ ] **Step 2: 编译验证**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 3: 最终前端构建**

```bash
cd frontend && yarn build
```

Expected: 构建成功。

- [ ] **Step 4: 最终 commit**

```bash
git add app.go
git commit -m "feat: pre-register lark tool permission row at startup"
```

---

## Self-Review

**Spec coverage:**
- ✅ 以用户身份访问飞书 — lark-cli 的 `auth login` 获取 user access token，`--as user` 默认行为
- ✅ 设置界面配置 lark-cli 路径 — Task 6 的 lark tab
- ✅ 减少安装依赖流程 — 只需 `npm install -g @larksuite/cli`，引导步骤内嵌在设置界面
- ✅ AI 可调用飞书操作 — LarkTool 注入 agent 工具链，PermProtected 级别
- ✅ 权限控制 — 在「工具权限」tab 中可独立开关 lark 工具

**Placeholder scan:** 无 TBD/TODO。

**Type consistency:**
- `lark.Client` / `lark.NewClient(path)` — Task 1 定义，Task 5 使用 ✅
- `lark.Tool` / `lark.FindCLI()` — Task 1/2 定义，Task 5 使用 ✅
- `internaltools.WrapLarkTool(*lark.Tool)` — Task 3 定义，Task 5/7 使用 ✅
- `LarkStatus()` / `LarkRunCommand()` — Task 5 定义，Task 6 import 使用 ✅

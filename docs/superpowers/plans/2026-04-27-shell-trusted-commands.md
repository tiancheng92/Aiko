# Shell 免确认命令白名单 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `ExecuteShellTool` 添加可信命令前缀白名单，白名单内的命令直接执行，跳过 eino interrupt 确认流程。

**Architecture:** 在 `Config` 结构体中新增 `ShellTrustedCommands []string` 字段，持久化到 SQLite；在 `shell.go` 的 `InvokableRun` 中于 interrupt 前做前缀匹配检查；前端 SettingsWindow 新增与 AllowedPaths 风格一致的列表编辑组件。

**Tech Stack:** Go (strings), Vue 3 `<script setup>`, SQLite via existing config.Store

---

## 文件变更一览

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/config/config.go` | 修改 | 新增字段、Load/Save 逻辑 |
| `internal/tools/shell.go` | 修改 | 新增 `isTrustedCommand`，修改 `InvokableRun` |
| `frontend/src/components/SettingsWindow.vue` | 修改 | 新增白名单列表 UI 及脚本 |

---

### Task 1: Config 层——新增 ShellTrustedCommands 字段

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: 在 Config 结构体中新增字段**

在 `ShellTimeout` 字段后插入（约第 24 行）：

```go
ShellTrustedCommands []string // 免确认的命令前缀列表
```

- [ ] **Step 2: 在 Load() 中读取该字段**

在 `cfg.ShellTimeout = parseInt(...)` 之后（约第 86 行）插入：

```go
cfg.ShellTrustedCommands = splitLines(m["shell_trusted_commands"])
```

- [ ] **Step 3: 在 Save() 中持久化该字段**

在 `pairs` map 中（`"shell_timeout"` 附近）新增：

```go
"shell_trusted_commands": joinLines(cfg.ShellTrustedCommands),
```

- [ ] **Step 4: 编译验证**

```bash
go build ./...
```

期望：无编译错误。

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add ShellTrustedCommands field"
```

---

### Task 2: 逻辑层——isTrustedCommand 及 InvokableRun 修改

**Files:**
- Modify: `internal/tools/shell.go`

- [ ] **Step 1: 编写 isTrustedCommand 的失败测试**

在 `internal/tools/` 下新建 `shell_test.go`：

```go
package tools

import (
	"testing"
)

func TestIsTrustedCommand(t *testing.T) {
	cases := []struct {
		command string
		trusted []string
		want    bool
	}{
		{"git status", []string{"git"}, true},
		{"gitk", []string{"git"}, false},           // 无空格边界，不匹配
		{"git", []string{"git"}, true},             // 完全匹配
		{"ls -la", []string{"ls", "cat"}, true},
		{"rm -rf /", []string{}, false},            // 空白名单
		{"rm -rf /", nil, false},                   // nil 白名单
		{" git status", []string{"git"}, true},     // 首部空白
		{"cat /etc/passwd", []string{"cat"}, true},
	}
	for _, c := range cases {
		got := isTrustedCommand(c.command, c.trusted)
		if got != c.want {
			t.Errorf("isTrustedCommand(%q, %v) = %v, want %v", c.command, c.trusted, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./internal/tools/ -run TestIsTrustedCommand -v
```

期望：`FAIL — isTrustedCommand undefined`

- [ ] **Step 3: 实现 isTrustedCommand 并修改 InvokableRun**

在 `shell.go` 文件末尾追加 `isTrustedCommand`，并在 `InvokableRun` 中插入白名单检查。

**追加到 `shell.go` 末尾：**

```go
// isTrustedCommand reports whether command matches any trusted prefix.
// It checks exact equality or prefix + space to avoid "gitk" matching "git".
func isTrustedCommand(command string, trusted []string) bool {
	cmd := strings.TrimLeft(command, " \t")
	for _, t := range trusted {
		if cmd == t || strings.HasPrefix(cmd, t+" ") {
			return true
		}
	}
	return false
}
```

同时在 `shell.go` 的 import 块中确认包含 `"strings"`（若无则添加）。

**在 `InvokableRun` 中，解析 `workingDir` 之后、resume 检查之前插入：**

```go
// Bypass confirmation for trusted commands.
if isTrustedCommand(command, t.Cfg.ShellTrustedCommands) {
    return runShellCommand(ctx, command, workingDir, t.Cfg.ShellTimeout, t.RegisterCmd, t.UnregisterCmd)
}
```

插入位置（现有代码参考）：

```go
if workingDir == "" {
    home, _ := os.UserHomeDir()
    workingDir = home
}

// ← 在此处插入白名单检查 ↑

// Check if this is a resume (user has already confirmed).
isTarget, hasData, confirmResult := einotool.GetResumeContext[ConfirmResult](ctx)
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/tools/ -run TestIsTrustedCommand -v
```

期望：`PASS`

- [ ] **Step 5: 编译验证**

```bash
go build ./...
```

期望：无编译错误。

- [ ] **Step 6: Commit**

```bash
git add internal/tools/shell.go internal/tools/shell_test.go
git commit -m "feat(tools): add isTrustedCommand and bypass confirm for whitelisted shell commands"
```

---

### Task 3: 前端——SettingsWindow 白名单列表 UI

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: 在 defaultCfg 中初始化字段**

在 `defaultCfg` 对象的 `AllowedPaths: []` 一行后（约第 42 行）添加：

```js
ShellTrustedCommands: [],
```

- [ ] **Step 2: 添加 ref 和脚本函数**

在 `const newPathInput = ref('')` 一行后（约第 55 行）添加：

```js
const newTrustedCmdInput = ref('') // input buffer for adding trusted commands
```

在 `removePath` 函数后（约第 484 行）添加：

```js
/** addTrustedCommand appends a command prefix to ShellTrustedCommands. */
function addTrustedCommand() {
  const cmd = newTrustedCmdInput.value.trim()
  if (!cmd) return
  if (!cfg.value.ShellTrustedCommands) cfg.value.ShellTrustedCommands = []
  if (!cfg.value.ShellTrustedCommands.includes(cmd)) {
    cfg.value.ShellTrustedCommands.push(cmd)
  }
  newTrustedCmdInput.value = ''
}

/** removeTrustedCommand removes the command prefix at the given index. */
function removeTrustedCommand(index) {
  cfg.value.ShellTrustedCommands.splice(index, 1)
}
```

- [ ] **Step 3: 在模板中新增白名单区块**

在 `<!-- 执行超时 -->` 的 `<div class="settings-section" style="margin-top:16px">` 之前（约第 1030 行）插入：

```html
<div class="settings-section" style="margin-top:16px">
  <h3 class="section-title">免确认命令白名单</h3>
  <p class="section-hint">以这些命令名开头的 Shell 命令将直接执行，无需二次确认（如 git、ls）</p>
  <div class="path-list">
    <div v-for="(cmd, i) in cfg.ShellTrustedCommands" :key="i" class="path-row">
      <span class="path-text">{{ cmd }}</span>
      <button class="btn-danger-small" @click="removeTrustedCommand(i)">删除</button>
    </div>
    <p v-if="!cfg.ShellTrustedCommands || cfg.ShellTrustedCommands.length === 0" class="empty-hint">暂无白名单命令，所有 Shell 命令均需确认</p>
  </div>
  <div class="path-add-row" style="margin-top:8px">
    <input
      v-model="newTrustedCmdInput"
      class="path-input"
      placeholder="git"
      @keydown.enter="addTrustedCommand"
    />
    <button class="btn-small" @click="addTrustedCommand">添加</button>
  </div>
</div>
```

- [ ] **Step 4: 构建前端验证**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build
```

期望：构建成功，无报错。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(frontend): add ShellTrustedCommands list editor in settings"
```

---

### Task 4: 集成验证

- [ ] **Step 1: 构建并运行应用**

```bash
make run
```

- [ ] **Step 2: 手动验证白名单生效**
  1. 打开设置 → 工具 → 设置 tab
  2. 在「免确认命令白名单」中添加 `git`，保存配置
  3. 让 Agent 执行 `git status` → 应直接执行，无确认框弹出
  4. 让 Agent 执行 `rm -rf /tmp/test` → 应弹出确认框

- [ ] **Step 3: 验证空白名单行为不变**
  1. 清空白名单，保存
  2. 执行任意 Shell 命令 → 应弹出确认框

- [ ] **Step 4: 运行全量 Go 测试**

```bash
go test ./...
```

期望：全部通过。

# Claude Code 面板增强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Claude Code 状态面板新增实时耗时计时器、工具参数副标题、工具调用计数、× 手动关闭按钮四项功能。

**Architecture:** 后端 `internal/claudecco/server.go` 在 `sessionSnapshot` 增加 `ToolInput` 字段并在 `emitStatus` 中填充；前端 `ClaudeStatusPanel.vue` 完全负责计时、计数、副标题解析和关闭逻辑，不引入新文件。

**Tech Stack:** Go 1.26、Vue 3 `<script setup>`、Wails 事件系统

---

## 文件清单

| 文件 | 操作 |
|---|---|
| `internal/claudecco/server.go` | 修改：`sessionSnapshot` 加 `ToolInput` 字段；`emitStatus` 填充逻辑 |
| `frontend/src/components/ClaudeStatusPanel.vue` | 修改：新增计时器、计数、副标题、× 按钮的逻辑与模板 |

---

## Task 1：后端 — 在 `sessionSnapshot` 增加 `ToolInput` 字段

**Files:**
- Modify: `internal/claudecco/server.go:330-337`

- [ ] **Step 1：修改 `sessionSnapshot` 结构体**

将 `internal/claudecco/server.go` 中的：

```go
type sessionSnapshot struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CWD           string `json:"cwd"`
	State         string `json:"state"`
	ToolName      string `json:"toolName,omitempty"`
	HookEventName string `json:"hookEventName,omitempty"`
}
```

改为：

```go
type sessionSnapshot struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CWD           string `json:"cwd"`
	State         string `json:"state"`
	ToolName      string `json:"toolName,omitempty"`
	HookEventName string `json:"hookEventName,omitempty"`
	ToolInput     string `json:"toolInput,omitempty"`
}
```

- [ ] **Step 2：在 `emitStatus` 填充 `ToolInput`**

将 `internal/claudecco/server.go` 中 `emitStatus` 的：

```go
if id == input.SessionID {
    snap.ToolName = input.ToolName
    snap.HookEventName = input.HookEventName
}
```

改为：

```go
if id == input.SessionID {
    snap.ToolName = input.ToolName
    snap.HookEventName = input.HookEventName
    if input.ToolInput != nil {
        if b, err := json.Marshal(input.ToolInput); err == nil {
            s := string(b)
            runes := []rune(s)
            if len(runes) > 120 {
                s = string(runes[:120]) + "…"
            }
            snap.ToolInput = s
        }
    }
}
```

- [ ] **Step 3：确认 `encoding/json` 已在 import 中**

检查文件头部 import 块，`encoding/json` 已存在（用于 `json.Unmarshal`），无需修改。

- [ ] **Step 4：编译验证**

```bash
go build ./...
```

期望输出：无错误

- [ ] **Step 5：Commit**

```bash
git add internal/claudecco/server.go
git commit -m "feat(claudecco): add ToolInput to sessionSnapshot"
```

---

## Task 2：前端 — 计时器、计数、副标题（script 部分）

**Files:**
- Modify: `frontend/src/components/ClaudeStatusPanel.vue`

- [ ] **Step 1：扩展 import，新增响应式变量**

将 `<script setup>` 顶部的 import 行：

```js
import { computed, onMounted, onUnmounted, ref } from "vue";
```

改为（已包含所需，无需改动）。

在 `const panelRef = ref(null)` 之后，`let offStatus = null` 之前插入：

```js
// 计时器相关
const tick = ref(0);
let tickTimer = null;
const sessionStartTimes = new Map(); // id → timestamp ms，首次 thinking 时记录
const sessionToolCounts = new Map(); // id → number，PreToolUse 事件计数
const dismissed = ref(new Set());   // 手动关闭的 session id
```

- [ ] **Step 2：修改 `onMounted` 中的事件处理器**

将原来的：

```js
onMounted(() => {
  offStatus = EventsOn("claudecco:status", (data) => {
    sessions.value = data.sessions || [];
  });
});
```

改为：

```js
onMounted(() => {
  tickTimer = setInterval(() => { tick.value++; }, 1000);
  offStatus = EventsOn("claudecco:status", (data) => {
    const incoming = data.sessions || [];
    for (const s of incoming) {
      if (s.state === "thinking") {
        if (!sessionStartTimes.has(s.id)) {
          sessionStartTimes.set(s.id, Date.now());
        }
        if (s.hookEventName === "PreToolUse") {
          sessionToolCounts.set(s.id, (sessionToolCounts.get(s.id) || 0) + 1);
        }
      }
    }
    sessions.value = incoming;
  });
});
```

- [ ] **Step 3：修改 `onUnmounted` 清理 timer**

将原来的：

```js
onUnmounted(() => {
  offStatus?.();
  if (cancelAnim) cancelAnim();
});
```

改为：

```js
onUnmounted(() => {
  offStatus?.();
  if (cancelAnim) cancelAnim();
  clearInterval(tickTimer);
  tickTimer = null;
});
```

- [ ] **Step 4：新增三个工具函数**

在 `defineExpose({ show, hide })` 之前插入：

```js
/** elapsedLabel returns a human-readable elapsed time string for a session. */
function elapsedLabel(id) {
  tick.value; // 触发响应式追踪
  const start = sessionStartTimes.get(id);
  if (!start) return "";
  const sec = Math.floor((Date.now() - start) / 1000);
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60), s = sec % 60;
  if (m < 60) return `${m}m ${s}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

/** toolInputLabel extracts a human-readable summary from a JSON ToolInput string. */
function toolInputLabel(raw) {
  if (!raw) return "";
  try {
    const obj = JSON.parse(raw);
    const key = ["command", "cmd", "file_path", "path", "query", "url", "skill", "prompt"]
      .find((k) => obj[k] && typeof obj[k] === "string");
    if (key) {
      let val = obj[key];
      if (val.length > 60) val = val.slice(0, 60) + "…";
      return val;
    }
  } catch {}
  return raw.length > 60 ? raw.slice(0, 60) + "…" : raw;
}

/** dismiss removes an idle session from the panel for this session lifetime. */
function dismiss(id) {
  dismissed.value = new Set([...dismissed.value, id]);
  sessionStartTimes.delete(id);
  sessionToolCounts.delete(id);
}
```

- [ ] **Step 5：修改 `visibleSessions` computed（替换 `groups`）**

原有的 `groups` computed 直接用 `sessions.value`，现在需要先过滤 dismissed。将原来的：

```js
const groups = computed(() => {
  const map = new Map();
  for (const s of sessions.value) {
    const cwd = s.cwd || "";
    if (!map.has(cwd)) map.set(cwd, []);
    map.get(cwd).push(s);
  }
  return Array.from(map, ([cwd, items]) => ({ cwd, items }));
});
```

改为：

```js
const groups = computed(() => {
  const map = new Map();
  for (const s of sessions.value) {
    if (dismissed.value.has(s.id)) continue;
    const cwd = s.cwd || "";
    if (!map.has(cwd)) map.set(cwd, []);
    map.get(cwd).push(s);
  }
  return Array.from(map, ([cwd, items]) => ({ cwd, items }));
});
```

同时修改 `hasThinking` 也过滤 dismissed：

```js
const hasThinking = computed(() =>
  sessions.value.some((s) => !dismissed.value.has(s.id) && s.state === "thinking")
);
```

- [ ] **Step 6：编译前端确认无语法错误**

```bash
cd frontend && yarn build 2>&1 | tail -20
```

期望输出：`built in Xs`，无 error

---

## Task 3：前端 — 模板与样式

**Files:**
- Modify: `frontend/src/components/ClaudeStatusPanel.vue`

- [ ] **Step 1：替换 `<template>` 中的行结构**

将原来的 `<template>` 中的整个 `<template v-else ...>` 块：

```html
<template v-else v-for="group in groups" :key="group.cwd">
  <div class="cp-group-label">{{ cwdLabel(group.cwd) }}</div>
  <div
    v-for="s in group.items"
    :key="s.id"
    class="cp-row"
  >
    <span class="cp-dot" :class="cfg(s.state).class">
      <svg v-if="s.state === 'idle'" width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="currentColor"/></svg>
      <svg v-else-if="s.state === 'thinking'" width="12" height="12" viewBox="0 0 12 12" class="spin-svg"><circle cx="6" cy="6" r="4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-dasharray="6 3"/></svg>
      <svg v-else width="12" height="12" viewBox="0 0 12 12"><line x1="3.5" y1="3.5" x2="8.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="8.5" y1="3.5" x2="3.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
    </span>
    <span class="cp-session">{{ s.name }}</span>
    <span v-if="s.toolName" class="cp-tool">
      <svg v-if="toolIcon(s.toolName)" width="11" height="11" viewBox="0 0 24 24" class="cp-tool-icon" v-html="toolIcon(s.toolName)"></svg>
      {{ toolLabel(s.toolName) }}
    </span>
    <span class="cp-status">{{ t("claudeStatus." + cfg(s.state).label) }}</span>
  </div>
</template>
```

替换为：

```html
<template v-else v-for="group in groups" :key="group.cwd">
  <div class="cp-group-label">{{ cwdLabel(group.cwd) }}</div>
  <div
    v-for="s in group.items"
    :key="s.id"
    class="cp-row"
  >
    <!-- 第一行：状态点 · 会话名 · 计数 · 耗时 · 状态文字 · × 按钮 -->
    <div class="cp-row-main">
      <span class="cp-dot" :class="cfg(s.state).class">
        <svg v-if="s.state === 'idle'" width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="currentColor"/></svg>
        <svg v-else-if="s.state === 'thinking'" width="12" height="12" viewBox="0 0 12 12" class="spin-svg"><circle cx="6" cy="6" r="4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-dasharray="6 3"/></svg>
        <svg v-else width="12" height="12" viewBox="0 0 12 12"><line x1="3.5" y1="3.5" x2="8.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="8.5" y1="3.5" x2="3.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
      </span>
      <span class="cp-session">{{ s.name }}</span>
      <span v-if="sessionToolCounts.get(s.id)" class="cp-count">×{{ sessionToolCounts.get(s.id) }}</span>
      <span v-if="elapsedLabel(s.id)" class="cp-elapsed">{{ elapsedLabel(s.id) }}</span>
      <span v-if="s.toolName" class="cp-tool">
        <svg v-if="toolIcon(s.toolName)" width="11" height="11" viewBox="0 0 24 24" class="cp-tool-icon" v-html="toolIcon(s.toolName)"></svg>
        {{ toolLabel(s.toolName) }}
      </span>
      <span class="cp-status">{{ t("claudeStatus." + cfg(s.state).label) }}</span>
      <button v-if="s.state === 'idle'" class="cp-dismiss" @click.stop="dismiss(s.id)" :title="t('claudeStatus.dismiss')">×</button>
    </div>
    <!-- 第二行：工具参数副标题 -->
    <div v-if="toolInputLabel(s.toolInput)" class="cp-row-sub">
      {{ toolInputLabel(s.toolInput) }}
    </div>
  </div>
</template>
```

- [ ] **Step 2：修改 `cp-row` 的布局为 flex-column，新增子元素样式**

在 `<style scoped>` 中，将原来的：

```css
.cp-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 4px;
  border-radius: 6px;
  transition: background 0.12s var(--ease-enter);
}
```

改为：

```css
.cp-row {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 4px 4px;
  border-radius: 6px;
  transition: background 0.12s var(--ease-enter);
}
```

然后在 `.cp-row:hover` 之后追加以下新样式（追加在现有 `.cp-tool-icon` 之后即可）：

```css
.cp-row-main {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.cp-row-sub {
  font-size: 10px;
  color: var(--text-tertiary);
  padding-left: 18px; /* 与 cp-session 对齐（dot 12px + gap 6px） */
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
  opacity: 0.75;
}

.cp-count {
  font-size: 10px;
  font-weight: 500;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
}

.cp-elapsed {
  font-size: 10px;
  font-weight: 500;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
}

.cp-dismiss {
  margin-left: auto;
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
  color: var(--text-tertiary);
  opacity: 0.3;
  border-radius: 3px;
  padding: 0;
  transition: opacity 0.15s, background 0.15s;
}

.cp-row:hover .cp-dismiss {
  opacity: 0.7;
}

.cp-dismiss:hover {
  opacity: 1 !important;
  background: var(--lg-surface-hover);
  color: var(--text-primary);
}
```

- [ ] **Step 3：确认 `cp-status` 的 `margin-left: auto` 需要调整**

由于 `cp-row-main` 是新的 flex 容器，原来 `.cp-status` 的 `margin-left: auto` 会把状态文字推到右侧。但现在 `cp-dismiss` 按钮也用了 `margin-left: auto`，需要让 `cp-status` 不再自动推右，改为自然流排列：

将：

```css
.cp-status {
  margin-left: auto;
  font-size: 10px;
  font-weight: 500;
  color: var(--text-secondary);
  flex-shrink: 0;
}
```

改为：

```css
.cp-status {
  font-size: 10px;
  font-weight: 500;
  color: var(--text-secondary);
  flex-shrink: 0;
}
```

- [ ] **Step 4：在 i18n 文件中补充 `dismiss` key**

检查 `frontend/src/locales/zh-CN.json` 中 `claudeStatus` 对象，添加：

```json
"dismiss": "关闭"
```

同样在 `frontend/src/locales/en.json` 中添加：

```json
"dismiss": "Dismiss"
```

在 `frontend/src/locales/ja.json` 中添加：

```json
"dismiss": "閉じる"
```

在 `frontend/src/locales/ko.json` 中添加：

```json
"dismiss": "닫기"
```

- [ ] **Step 5：编译并目视验证**

```bash
cd frontend && yarn build 2>&1 | tail -10
```

期望：`built in Xs`，无 error

如有条件，运行 `make run` 启动应用，打开 Claude Code 面板，用 Claude Code 触发一个工具调用，观察：
- 会话行出现耗时（`Xs` 滚动）
- 工具调用后出现 `×N` 计数
- 工具 badge 下方出现副标题（如 `git status`）
- Stop 后出现 × 按钮，点击后会话消失

- [ ] **Step 6：Commit**

```bash
git add frontend/src/components/ClaudeStatusPanel.vue \
        frontend/src/locales/zh-CN.json \
        frontend/src/locales/en.json \
        frontend/src/locales/ja.json \
        frontend/src/locales/ko.json
git commit -m "feat(claude-panel): add elapsed timer, tool count, input subtitle, dismiss button"
```

---

## 验收标准

| 功能 | 预期行为 |
|---|---|
| 实时耗时 | thinking 状态每秒更新，idle 后固定显示最终值 |
| 工具调用计数 | 每次 `PreToolUse` 事件 +1，格式 `×N`，0 时不显示 |
| 工具参数副标题 | 有 `toolInput` 时在工具 badge 下方显示关键参数，最长 60 字符 |
| × 关闭按钮 | 仅 idle 状态出现，hover 时可见，点击后该行消失 |
| 已有功能不退化 | 状态点动画、CWD 分组、panel 进出动画、thinking 边框高亮正常 |

# 面板关闭按钮 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为三个小面板（PomodoroPanel、ClaudeStatusPanel、SystemResourcePanel）的标题栏统一添加 × 关闭按钮。

**Architecture:** 每个面板在标题栏加 `<button class="panel-close-btn">` + `emit('close')`；App.vue 绑定对应的 close handler 设置 `xxxPanelOpen = false`；i18n 的 `common.close` key 供 `:title` tooltip 使用。PomodoroPanel 关闭仅隐藏面板，不停止计时。

**Tech Stack:** Vue 3 `<script setup>`、vue-i18n、Wails 事件系统

---

## 文件清单

| 文件 | 操作 |
|---|---|
| `frontend/src/components/PomodoroPanel.vue` | 新增 `onClose()`，标题栏加按钮，加 `.panel-close-btn` CSS |
| `frontend/src/components/ClaudeStatusPanel.vue` | 新增 `defineEmits(['close'])`，标题栏加按钮，加 CSS |
| `frontend/src/components/SystemResourcePanel.vue` | 新增 `defineEmits(['close'])`，标题栏加按钮，加 CSS |
| `frontend/src/App.vue` | 新增 `closeClaudePanel()`，模板补 `@close` 绑定 |
| `frontend/src/locales/zh-CN.json` | 新增顶层 `"common": { "close": "关闭" }` |
| `frontend/src/locales/en.json` | 新增顶层 `"common": { "close": "Close" }` |
| `frontend/src/locales/ja.json` | 新增顶层 `"common": { "close": "閉じる" }` |
| `frontend/src/locales/ko.json` | 新增顶层 `"common": { "close": "닫기" }` |

---

## Task 1：i18n — 新增 `common.close` key

**Files:**
- Modify: `frontend/src/locales/zh-CN.json`
- Modify: `frontend/src/locales/en.json`
- Modify: `frontend/src/locales/ja.json`
- Modify: `frontend/src/locales/ko.json`

- [ ] **Step 1：zh-CN.json 加 `common` 对象**

在 `frontend/src/locales/zh-CN.json` 的顶层 JSON 对象末尾（最后一个 key 之后）新增：

```json
"common": {
  "close": "关闭"
}
```

- [ ] **Step 2：en.json 加 `common` 对象**

在 `frontend/src/locales/en.json` 的顶层 JSON 对象末尾新增：

```json
"common": {
  "close": "Close"
}
```

- [ ] **Step 3：ja.json 加 `common` 对象**

在 `frontend/src/locales/ja.json` 的顶层 JSON 对象末尾新增：

```json
"common": {
  "close": "閉じる"
}
```

- [ ] **Step 4：ko.json 加 `common` 对象**

在 `frontend/src/locales/ko.json` 的顶层 JSON 对象末尾新增：

```json
"common": {
  "close": "닫기"
}
```

- [ ] **Step 5：验证 JSON 合法**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && for f in src/locales/*.json; do python3 -c "import json,sys; json.load(open('$f'))" && echo "$f OK"; done
```

期望：每个文件输出 `OK`，无错误

- [ ] **Step 6：Commit**

```bash
git add frontend/src/locales/zh-CN.json frontend/src/locales/en.json frontend/src/locales/ja.json frontend/src/locales/ko.json
git commit -m "feat(i18n): add common.close key to all locales"
```

---

## Task 2：PomodoroPanel — 关闭按钮

**Files:**
- Modify: `frontend/src/components/PomodoroPanel.vue`

PomodoroPanel 已有 `const emit = defineEmits(['close'])` 和 `const { t } = useI18n()`，无需新增。

- [ ] **Step 1：在标题栏加关闭按钮**

将 `frontend/src/components/PomodoroPanel.vue` 中的：

```html
<div class="pomo-titlebar">
  <span class="pomo-titlebar-label">番茄钟</span>
</div>
```

改为：

```html
<div class="pomo-titlebar">
  <span class="pomo-titlebar-label">番茄钟</span>
  <button class="panel-close-btn" @click="onClose" :title="t('common.close')">×</button>
</div>
```

- [ ] **Step 2：新增 `onClose` 函数**

在 `onStop` 函数之前插入（`frontend/src/components/PomodoroPanel.vue`）：

```js
/** onClose hides the panel without stopping the pomodoro timer. */
function onClose() {
  visible.value = false
  emit('close')
}
```

- [ ] **Step 3：在 CSS 中新增 `.panel-close-btn` 样式**

在 `.pomo-titlebar-label { ... }` 规则块之后插入：

```css
.panel-close-btn {
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
  font-size: 13px;
  line-height: 1;
  color: var(--text-tertiary);
  opacity: 0.3;
  border-radius: 3px;
  padding: 0;
  transition: opacity 0.15s, background 0.15s;
}

.panel-close-btn:hover {
  opacity: 1;
  background: var(--lg-surface-hover);
  color: var(--text-primary);
}
```

- [ ] **Step 4：编译验证**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：`built in Xs`，无 error

- [ ] **Step 5：Commit**

```bash
git add frontend/src/components/PomodoroPanel.vue
git commit -m "feat(pomodoro): add close button to titlebar"
```

---

## Task 3：ClaudeStatusPanel — 关闭按钮

**Files:**
- Modify: `frontend/src/components/ClaudeStatusPanel.vue`

ClaudeStatusPanel 已有 `const { t } = useI18n()`，无需新增。

- [ ] **Step 1：新增 `defineEmits`**

在 `frontend/src/components/ClaudeStatusPanel.vue` 的 `<script setup>` 中，在 `const { t } = useI18n()` 之后插入：

```js
const emit = defineEmits(['close']);
```

- [ ] **Step 2：在标题栏加关闭按钮**

将：

```html
<div class="cp-titlebar">
  <span class="cp-titlebar-label">Claude Code</span>
</div>
```

改为：

```html
<div class="cp-titlebar">
  <span class="cp-titlebar-label">Claude Code</span>
  <button class="panel-close-btn" @click="emit('close')" :title="t('common.close')">×</button>
</div>
```

- [ ] **Step 3：在 CSS 中新增 `.panel-close-btn` 样式**

在 `.cp-titlebar-label { ... }` 规则块之后插入：

```css
.panel-close-btn {
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
  font-size: 13px;
  line-height: 1;
  color: var(--text-tertiary);
  opacity: 0.3;
  border-radius: 3px;
  padding: 0;
  transition: opacity 0.15s, background 0.15s;
}

.panel-close-btn:hover {
  opacity: 1;
  background: var(--lg-surface-hover);
  color: var(--text-primary);
}
```

- [ ] **Step 4：编译验证**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：`built in Xs`，无 error

- [ ] **Step 5：Commit**

```bash
git add frontend/src/components/ClaudeStatusPanel.vue
git commit -m "feat(claude-panel): add close button to titlebar"
```

---

## Task 4：SystemResourcePanel — 关闭按钮

**Files:**
- Modify: `frontend/src/components/SystemResourcePanel.vue`

SystemResourcePanel 已有 `const { t } = useI18n()`，无需新增。

- [ ] **Step 1：新增 `defineEmits`**

在 `frontend/src/components/SystemResourcePanel.vue` 的 `<script setup>` 中，在 `const { t } = useI18n()` 之后插入：

```js
const emit = defineEmits(['close']);
```

- [ ] **Step 2：在标题栏加关闭按钮**

将：

```html
<div class="sys-titlebar">
  <span class="sys-titlebar-label">系统</span>
</div>
```

改为：

```html
<div class="sys-titlebar">
  <span class="sys-titlebar-label">系统</span>
  <button class="panel-close-btn" @click="emit('close')" :title="t('common.close')">×</button>
</div>
```

- [ ] **Step 3：在 CSS 中新增 `.panel-close-btn` 样式**

在 `.sys-titlebar-label { ... }` 规则块之后插入：

```css
.panel-close-btn {
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
  font-size: 13px;
  line-height: 1;
  color: var(--text-tertiary);
  opacity: 0.3;
  border-radius: 3px;
  padding: 0;
  transition: opacity 0.15s, background 0.15s;
}

.panel-close-btn:hover {
  opacity: 1;
  background: var(--lg-surface-hover);
  color: var(--text-primary);
}
```

- [ ] **Step 4：编译验证**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：`built in Xs`，无 error

- [ ] **Step 5：Commit**

```bash
git add frontend/src/components/SystemResourcePanel.vue
git commit -m "feat(system-panel): add close button to titlebar"
```

---

## Task 5：App.vue — 新增 `closeClaudePanel` 并绑定 `@close`

**Files:**
- Modify: `frontend/src/App.vue`

注：`closeSystemPanel()` 已存在于 App.vue，无需重复添加。

- [ ] **Step 1：新增 `closeClaudePanel` 函数**

在 `closePomodoro()` 函数之后插入：

```js
/** closeClaudePanel hides the Claude Code status panel. */
function closeClaudePanel() {
  claudePanelOpen.value = false
}
```

- [ ] **Step 2：绑定 `@close` 到 ClaudeStatusPanel**

将模板中的：

```html
<ClaudeStatusPanel
  v-if="claudePanelOpen"
  ref="claudePanelRef"
/>
```

改为：

```html
<ClaudeStatusPanel
  v-if="claudePanelOpen"
  ref="claudePanelRef"
  @close="closeClaudePanel"
/>
```

- [ ] **Step 3：绑定 `@close` 到 SystemResourcePanel**

将模板中的：

```html
<SystemResourcePanel
  v-if="systemPanelOpen"
  ref="systemPanelRef"
/>
```

改为：

```html
<SystemResourcePanel
  v-if="systemPanelOpen"
  ref="systemPanelRef"
  @close="closeSystemPanel"
/>
```

- [ ] **Step 4：编译验证**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：`built in Xs`，无 error

- [ ] **Step 5：Commit**

```bash
git add frontend/src/App.vue
git commit -m "feat(app): wire close handlers for claude and system panels"
```

---

## 验收标准

| 面板 | 预期行为 |
|---|---|
| PomodoroPanel | × 按钮出现在标题栏右侧；点击后面板消失，番茄钟继续计时；再次打开面板仍显示运行状态 |
| ClaudeStatusPanel | × 按钮出现在标题栏右侧；点击后面板消失 |
| SystemResourcePanel | × 按钮出现在标题栏右侧；点击后面板消失 |
| 所有面板 | 按钮默认半透明，hover 时完全可见；不影响已有 toggle 逻辑 |

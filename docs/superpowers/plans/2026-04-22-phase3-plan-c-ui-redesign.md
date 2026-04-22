# 三期前端样式优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 全面重设计聊天界面（ChatBubble + ChatPanel）和设置界面（SettingsWindow），告别方块堆砌风格，打造现代、轻盈的毛玻璃设计语言；同时实现通用气泡通知组件（新用户欢迎 + 定时任务结果 + 任意后端推送）。

**Architecture:** CSS 部分仅修改样式，不改 JS/Go 逻辑。气泡通知组件 `NotificationBubble.vue` 作为公共组件挂载在 `App.vue`，监听多个 Wails 事件（`notification:show`）；后端任何需要展示气泡的场景（定时任务结果、新用户欢迎）统一 emit `notification:show`；聊天面板未打开时才展示气泡，打开时直接进聊天流。

**Tech Stack:** Vue 3 CSS scoped、`backdrop-filter`（macOS WebView 支持）、Wails Events

---

## 文件结构

| 操作 | 文件 | 说明 |
|---|---|---|
| Modify | `frontend/src/components/ChatBubble.vue` | 毛玻璃容器、标题栏 |
| Modify | `frontend/src/components/ChatPanel.vue` | 消息气泡、输入框 |
| Modify | `frontend/src/components/SettingsWindow.vue` | 整体视觉重设计 |
| Modify | `frontend/src/components/ContextMenu.vue` | 毛玻璃右键菜单 |
| Create | `frontend/src/components/NotificationBubble.vue` | 通用气泡通知组件 |
| Modify | `frontend/src/App.vue` | 挂载 NotificationBubble；新用户欢迎改为气泡展示 |

---

### Task 1: 重设计 ChatBubble 容器

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue` (style scoped 区域)

- [ ] **Step 1: 替换 ChatBubble.vue 的 `<style scoped>` 内容**

保持 `<script setup>` 和 `<template>` 不变，仅替换整个 `<style scoped>` 块：

```css
<style scoped>
.chat-bubble {
  position: fixed;
  width: clamp(340px, 24vw, 520px);
  height: clamp(400px, 58vh, 680px);
  background: rgba(15, 18, 30, 0.82);
  backdrop-filter: blur(20px) saturate(180%);
  -webkit-backdrop-filter: blur(20px) saturate(180%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 20px;
  box-shadow:
    0 8px 32px rgba(0, 0, 0, 0.6),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  display: flex;
  flex-direction: column;
  z-index: 9998;
  overflow: hidden;
}
.title-bar {
  display: flex;
  align-items: center;
  padding: 0 14px;
  height: 44px;
  flex-shrink: 0;
  user-select: none;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.03);
}
.title {
  flex: 1;
  color: rgba(255, 255, 255, 0.85);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.close-btn {
  background: none;
  border: none;
  color: rgba(255, 255, 255, 0.3);
  padding: 8px;
  cursor: pointer;
  font-size: 13px;
  border-radius: 6px;
  transition: background 0.15s, color 0.15s;
  line-height: 1;
}
.close-btn:hover {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
}
.content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
</style>
```

- [ ] **Step 2: 同步更新 ChatBubble.vue 中 bubbleW 的计算（让气泡更宽一点）**

在 `<script setup>` 中找到：

```js
const bubbleW = computed(() => Math.min(480, Math.max(320, window.innerWidth  * 0.22)))
const bubbleH = computed(() => Math.min(620, Math.max(360, window.innerHeight * 0.55)))
```

改为：

```js
const bubbleW = computed(() => Math.min(520, Math.max(340, window.innerWidth  * 0.24)))
const bubbleH = computed(() => Math.min(680, Math.max(400, window.innerHeight * 0.58)))
```

- [ ] **Step 3: 验证前端编译**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: `Done in ...s`，无错误。

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ChatBubble.vue
git commit -m "style: redesign chat bubble with glassmorphism"
```

---

### Task 2: 重设计 ChatPanel 消息区

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue` (style scoped 区域)

- [ ] **Step 1: 替换 ChatPanel.vue 的 `<style scoped>` 块**

```css
<style scoped>
.chat-panel { display: flex; flex-direction: column; height: 100%; }

/* Messages list */
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.1) transparent;
}
.messages::-webkit-scrollbar { width: 4px; }
.messages::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.12); border-radius: 2px; }

/* Row */
.msg { display: flex; }
.msg.user { justify-content: flex-end; }
.msg.assistant, .msg.system { justify-content: flex-start; }

/* Wrap */
.bubble-wrap { position: relative; max-width: 82%; display: flex; flex-direction: column; }
.msg.user .bubble-wrap { align-items: flex-end; }

/* Bubble base */
.bubble {
  padding: 9px 14px;
  border-radius: 16px;
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
}

/* User bubble */
.user .bubble {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #fff;
  border-radius: 16px 16px 4px 16px;
  white-space: pre-wrap;
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.4);
}

/* Assistant bubble */
.assistant .bubble {
  background: rgba(55, 65, 81, 0.7);
  color: #e5e7eb;
  border-radius: 16px 16px 16px 4px;
  border: 1px solid rgba(255,255,255,0.06);
}

/* System / error bubble */
.system .bubble {
  background: rgba(220, 38, 38, 0.15);
  color: #fca5a5;
  border: 1px solid rgba(220, 38, 38, 0.3);
  border-radius: 10px;
  font-size: 12px;
  white-space: pre-wrap;
}

/* Cursor blink */
.cursor { animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }

/* Thinking dots */
.thinking-bubble { display: flex; align-items: center; gap: 5px; padding: 12px 16px; }
.dot {
  width: 7px; height: 7px;
  background: rgba(156, 163, 175, 0.7);
  border-radius: 50%;
  display: inline-block;
  animation: bounce 1.2s infinite ease-in-out;
}
.dot:nth-child(1) { animation-delay: 0s; }
.dot:nth-child(2) { animation-delay: 0.2s; }
.dot:nth-child(3) { animation-delay: 0.4s; }
@keyframes bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
  40% { transform: translateY(-5px); opacity: 1; }
}

/* Copy button */
.copy-btn {
  position: absolute;
  top: 4px; right: -30px;
  background: rgba(55, 65, 81, 0.8);
  border: 1px solid rgba(255,255,255,0.08);
  color: rgba(156, 163, 175, 0.8);
  border-radius: 6px;
  width: 24px; height: 24px;
  cursor: pointer;
  font-size: 12px;
  display: flex; align-items: center; justify-content: center;
  opacity: 0;
  transition: opacity 0.15s, background 0.15s;
  padding: 0;
}
.bubble-wrap:hover .copy-btn { opacity: 1; }
.copy-btn:hover { background: rgba(75, 85, 99, 0.9); color: #f9fafb; }

/* Markdown prose */
.bubble.markdown :deep(p) { margin: 0 0 6px; }
.bubble.markdown :deep(p:last-child) { margin-bottom: 0; }
.bubble.markdown :deep(pre) {
  background: rgba(10, 10, 20, 0.6);
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 8px;
  padding: 10px 12px;
  overflow-x: auto;
  margin: 6px 0;
}
.bubble.markdown :deep(code) { font-family: 'Fira Code', 'JetBrains Mono', monospace; font-size: 12px; }
.bubble.markdown :deep(pre code) { background: none; padding: 0; }
.bubble.markdown :deep(:not(pre) > code) {
  background: rgba(79, 70, 229, 0.2);
  color: #a5b4fc;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 12px;
}
.bubble.markdown :deep(ul), .bubble.markdown :deep(ol) { padding-left: 18px; margin: 4px 0; }
.bubble.markdown :deep(li) { margin: 2px 0; }
.bubble.markdown :deep(blockquote) {
  border-left: 3px solid #6366f1;
  margin: 6px 0;
  padding-left: 10px;
  color: #9ca3af;
  background: rgba(99, 102, 241, 0.05);
  border-radius: 0 6px 6px 0;
}
.bubble.markdown :deep(h1), .bubble.markdown :deep(h2), .bubble.markdown :deep(h3) {
  margin: 8px 0 4px; font-size: 14px; color: #f9fafb;
}
.bubble.markdown :deep(a) { color: #818cf8; text-decoration: none; }
.bubble.markdown :deep(a:hover) { text-decoration: underline; }
.bubble.markdown :deep(table) { border-collapse: collapse; margin: 6px 0; font-size: 12px; width: 100%; }
.bubble.markdown :deep(th) { background: rgba(255,255,255,0.05); }
.bubble.markdown :deep(th), .bubble.markdown :deep(td) {
  border: 1px solid rgba(255,255,255,0.08);
  padding: 4px 8px;
}

/* Input row */
.input-row {
  display: flex;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid rgba(255,255,255,0.06);
  background: rgba(255,255,255,0.02);
  flex-shrink: 0;
}
.input-row textarea {
  flex: 1;
  background: rgba(31, 41, 55, 0.6);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  padding: 8px 12px;
  color: #f9fafb;
  font-size: 13px;
  outline: none;
  resize: none;
  font-family: inherit;
  transition: border-color 0.15s;
  line-height: 1.5;
}
.input-row textarea:focus { border-color: rgba(99, 102, 241, 0.6); }
.input-row textarea::placeholder { color: rgba(156, 163, 175, 0.5); }
.input-row button {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #fff;
  border: none;
  border-radius: 10px;
  padding: 8px 16px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: opacity 0.15s, transform 0.1s;
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.35);
}
.input-row button:hover:not(:disabled) { opacity: 0.9; transform: translateY(-1px); }
.input-row button:active:not(:disabled) { transform: translateY(0); }
.input-row button:disabled { opacity: 0.4; cursor: not-allowed; box-shadow: none; }
</style>
```

- [ ] **Step 2: 验证编译**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: `Done in ...s`。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "style: redesign chat panel - gradient bubbles, better typography"
```

---

### Task 3: 重设计 SettingsWindow

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue` (style scoped 区域)

- [ ] **Step 1: 替换 SettingsWindow.vue 的 `<style scoped>` 块**

```css
<style scoped>
/* Window container */
.settings-win {
  position: fixed;
  z-index: 99990;
  width: 640px;
  height: 520px;
  background: rgba(13, 17, 28, 0.88);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
  box-shadow:
    0 24px 64px rgba(0, 0, 0, 0.8),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-size: 13px;
  color: #e5e7eb;
}

/* Title bar */
.win-titlebar {
  display: flex;
  align-items: center;
  padding: 0 16px;
  height: 44px;
  cursor: move;
  flex-shrink: 0;
  user-select: none;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.02);
}
.win-title { flex: 1; font-weight: 600; font-size: 13px; letter-spacing: 0.02em; color: rgba(255,255,255,0.85); }
.win-close {
  background: none;
  border: none;
  color: rgba(255,255,255,0.3);
  cursor: pointer;
  font-size: 14px;
  padding: 6px 8px;
  border-radius: 6px;
  transition: background 0.15s, color 0.15s;
}
.win-close:hover { background: rgba(239,68,68,0.15); color: #ef4444; }

/* Layout */
.win-body { flex: 1; display: flex; overflow: hidden; }

/* Sidebar */
.win-sidebar {
  width: 130px;
  background: rgba(255, 255, 255, 0.02);
  border-right: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  flex-direction: column;
  padding: 10px 6px;
  gap: 2px;
  flex-shrink: 0;
}
.win-sidebar button {
  background: none;
  border: none;
  color: rgba(156, 163, 175, 0.8);
  padding: 9px 12px;
  cursor: pointer;
  font-size: 12px;
  text-align: left;
  border-radius: 8px;
  transition: background 0.15s, color 0.15s;
}
.win-sidebar button:hover { background: rgba(255,255,255,0.06); color: #f9fafb; }
.win-sidebar button.active {
  background: rgba(99, 102, 241, 0.15);
  color: #a5b4fc;
  font-weight: 500;
}

/* Content */
.win-content { flex: 1; overflow-y: auto; padding: 20px; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.1) transparent; }
.win-content::-webkit-scrollbar { width: 4px; }
.win-content::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 2px; }

/* Form */
.tab-pane { display: flex; flex-direction: column; gap: 14px; }
label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: rgba(156, 163, 175, 0.8); font-weight: 500; letter-spacing: 0.01em; }
input, textarea, select {
  background: rgba(31, 41, 55, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 7px 11px;
  color: #f9fafb;
  font-size: 13px;
  outline: none;
  font-family: inherit;
  transition: border-color 0.15s;
}
input:focus, textarea:focus, select:focus { border-color: rgba(99, 102, 241, 0.6); }
input::placeholder, textarea::placeholder { color: rgba(156, 163, 175, 0.4); }
textarea { resize: vertical; }
select {
  cursor: pointer;
  appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='8' viewBox='0 0 12 8'%3E%3Cpath d='M1 1l5 5 5-5' stroke='%236b7280' stroke-width='1.5' fill='none' stroke-linecap='round'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  padding-right: 28px;
}

/* URL row with fetch button */
.url-row { display: flex; gap: 8px; align-items: center; }
.url-row input { flex: 1; }
.fetch-btn {
  background: rgba(55, 65, 81, 0.6);
  color: rgba(209, 213, 219, 0.9);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  font-size: 12px;
  white-space: nowrap;
  flex-shrink: 0;
  transition: background 0.15s, border-color 0.15s;
}
.fetch-btn:hover:not(:disabled) { background: rgba(75, 85, 99, 0.7); border-color: rgba(255,255,255,0.15); }
.fetch-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.select-row { display: flex; }
.select-row select, .select-row input { flex: 1; }

/* Tool permissions */
.hint { color: rgba(107, 114, 128, 0.8); font-size: 12px; margin: 0 0 8px; line-height: 1.5; }
.perm-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 0;
  border-bottom: 1px solid rgba(255,255,255,0.04);
}
.perm-row:last-child { border-bottom: none; }
.perm-info { display: flex; align-items: center; gap: 10px; }
.perm-name { font-size: 13px; color: #e5e7eb; }
.perm-level { font-size: 10px; padding: 2px 7px; border-radius: 20px; font-weight: 500; letter-spacing: 0.03em; }
.perm-level.public { background: rgba(6, 95, 70, 0.4); color: #6ee7b7; border: 1px solid rgba(110, 231, 183, 0.2); }
.perm-level.protected { background: rgba(124, 45, 18, 0.4); color: #fdba74; border: 1px solid rgba(253, 186, 116, 0.2); }

/* Toggle switch */
.toggle { display: flex; align-items: center; cursor: pointer; }
.toggle input { display: none; }
.toggle-track {
  width: 36px; height: 20px;
  background: rgba(55, 65, 81, 0.8);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  position: relative;
  transition: background 0.2s, border-color 0.2s;
}
.toggle input:checked ~ .toggle-track { background: rgba(99, 102, 241, 0.8); border-color: rgba(99, 102, 241, 0.4); }
.toggle-track::after {
  content: '';
  position: absolute;
  top: 2px; left: 2px;
  width: 14px; height: 14px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.2s;
  box-shadow: 0 1px 3px rgba(0,0,0,0.3);
}
.toggle input:checked ~ .toggle-track::after { transform: translateX(16px); }
.toggle input:disabled ~ .toggle-track { opacity: 0.35; cursor: not-allowed; }

/* Buttons */
button {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #fff;
  border: none;
  border-radius: 8px;
  padding: 7px 16px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: opacity 0.15s;
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.3);
}
button:hover:not(:disabled) { opacity: 0.9; }
button:disabled { opacity: 0.4; cursor: not-allowed; box-shadow: none; }

/* Knowledge list */
ul { list-style: none; padding: 0; margin-top: 4px; }
li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 7px 0;
  border-bottom: 1px solid rgba(255,255,255,0.04);
  font-size: 12px;
  color: #d1d5db;
}
li:last-child { border-bottom: none; }
li button {
  background: rgba(55, 65, 81, 0.6);
  border: 1px solid rgba(255,255,255,0.06);
  padding: 3px 10px;
  font-size: 11px;
  box-shadow: none;
}
li button:hover { background: rgba(220, 38, 38, 0.25); border-color: rgba(220, 38, 38, 0.3); color: #fca5a5; }

.empty { color: rgba(107, 114, 128, 0.6); font-size: 12px; margin-top: 6px; }
.progress { color: rgba(156, 163, 175, 0.7); font-size: 12px; margin: 6px 0; }

/* Model grid */
.model-grid { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 4px; }
.model-btn {
  background: rgba(31, 41, 55, 0.6);
  color: rgba(156, 163, 175, 0.8);
  border: 1px solid rgba(255,255,255,0.08);
  border-radius: 8px;
  padding: 6px 14px;
  cursor: pointer;
  font-size: 12px;
  transition: border-color 0.15s, color 0.15s, background 0.15s;
  box-shadow: none;
}
.model-btn:hover { border-color: rgba(99,102,241,0.5); color: #f9fafb; background: rgba(99,102,241,0.08); }
.model-btn.selected {
  background: rgba(99, 102, 241, 0.2);
  border-color: rgba(99, 102, 241, 0.5);
  color: #a5b4fc;
}

/* Footer */
.win-footer {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  border-top: 1px solid rgba(255,255,255,0.06);
  flex-shrink: 0;
  background: rgba(255,255,255,0.01);
}
.status-msg { color: rgba(107, 114, 128, 0.8); font-size: 12px; flex: 1; }
</style>
```

- [ ] **Step 2: 验证编译**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: `Done in ...s`。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "style: redesign settings window - glassmorphism, better form UX"
```

---

### Task 4: 重设计 ContextMenu

**Files:**
- Modify: `frontend/src/components/ContextMenu.vue` (style scoped)

- [ ] **Step 1: 替换 ContextMenu.vue 的 `<style scoped>` 块**

```css
<style scoped>
.ctx-menu {
  position: fixed;
  z-index: 99999;
  background: rgba(15, 20, 35, 0.88);
  backdrop-filter: blur(20px) saturate(180%);
  -webkit-backdrop-filter: blur(20px) saturate(180%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
  padding: 5px 0;
  min-width: 172px;
  box-shadow:
    0 16px 40px rgba(0, 0, 0, 0.6),
    0 1px 0 rgba(255, 255, 255, 0.05) inset;
  user-select: none;
}
.ctx-item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  background: none;
  border: none;
  color: rgba(229, 231, 235, 0.9);
  padding: 7px 14px;
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  border-radius: 0;
  box-shadow: none;
  transition: background 0.12s;
  font-weight: 400;
}
.ctx-item:hover { background: rgba(99, 102, 241, 0.15); color: #f9fafb; }
.ctx-icon { font-size: 14px; width: 18px; text-align: center; flex-shrink: 0; }
.ctx-divider { height: 1px; background: rgba(255, 255, 255, 0.06); margin: 4px 8px; }
</style>
```

- [ ] **Step 2: 验证编译**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: `Done in ...s`。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ContextMenu.vue
git commit -m "style: redesign context menu with glassmorphism"
```

---

---

### Task 5: 通用气泡通知组件 NotificationBubble.vue

> 这是公共基础设施组件，Plan D（定时任务）和首次启动欢迎均依赖它。所有需要展示气泡的场景统一通过 Wails `notification:show` 事件触发，payload: `{ title: string, message: string }`。聊天框已打开时不显示气泡（消息会直接进聊天流）。

**Files:**
- Create: `frontend/src/components/NotificationBubble.vue`
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: 创建 `NotificationBubble.vue`**

```vue
<!-- frontend/src/components/NotificationBubble.vue -->
<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const props = defineProps({
  petPos:    { type: Object,  default: () => ({ x: -1, y: -1 }) },
  petSize:   { type: Number,  default: 160 },
  bubbleOpen: { type: Boolean, default: false },
})

const notification = ref(null) // { title, message, ts }
let hideTimer = null
let offShow   = null

/** pos places the notification bubble above the pet. */
const pos = computed(() => {
  if (props.petPos.x < 0) return { x: 40, y: 40 }
  return {
    x: props.petPos.x - 20,
    y: props.petPos.y - 170,
  }
})

/** dismiss hides the notification and clears the auto-hide timer. */
function dismiss() {
  notification.value = null
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}

onMounted(() => {
  // Listen for the unified notification event.
  offShow = EventsOn('notification:show', (data) => {
    // If chat bubble is open, skip the overlay — message is in chat stream.
    if (props.bubbleOpen) return
    if (hideTimer) clearTimeout(hideTimer)
    notification.value = { title: data.title || '通知', message: data.message, ts: new Date() }
    // Auto-dismiss after 10 minutes.
    hideTimer = setTimeout(dismiss, 10 * 60 * 1000)
  })
})

onUnmounted(() => {
  offShow?.()
  if (hideTimer) clearTimeout(hideTimer)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="notification"
      class="notif-bubble"
      :style="{ left: pos.x + 'px', top: pos.y + 'px' }"
    >
      <div class="notif-header">
        <span class="notif-icon">🔔</span>
        <span class="notif-title">{{ notification.title }}</span>
        <button class="notif-close" @click="dismiss">✕</button>
      </div>
      <div class="notif-body">{{ notification.message }}</div>
      <button class="notif-ack" @click="dismiss">知道了</button>
    </div>
  </Teleport>
</template>

<style scoped>
.notif-bubble {
  position: fixed;
  z-index: 99997;
  width: 280px;
  background: rgba(13, 17, 28, 0.92);
  backdrop-filter: blur(20px) saturate(160%);
  -webkit-backdrop-filter: blur(20px) saturate(160%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  box-shadow: 0 12px 32px rgba(0,0,0,0.6);
  padding: 12px 14px 10px;
  animation: popIn 0.2s ease-out;
}
@keyframes popIn {
  from { opacity: 0; transform: translateY(8px) scale(0.96); }
  to   { opacity: 1; transform: translateY(0) scale(1); }
}
.notif-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}
.notif-icon { font-size: 14px; }
.notif-title {
  flex: 1;
  font-size: 12px;
  font-weight: 600;
  color: rgba(255,255,255,0.85);
  letter-spacing: 0.01em;
}
.notif-close {
  background: none;
  border: none;
  color: rgba(255,255,255,0.3);
  font-size: 11px;
  cursor: pointer;
  padding: 2px 4px;
  border-radius: 4px;
  box-shadow: none;
  transition: color 0.15s;
}
.notif-close:hover { color: #ef4444; background: rgba(239,68,68,0.1); }
.notif-body {
  font-size: 12px;
  color: rgba(209, 213, 219, 0.85);
  line-height: 1.6;
  max-height: 120px;
  overflow-y: auto;
  margin-bottom: 10px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.1) transparent;
  white-space: pre-wrap;
  word-break: break-word;
}
.notif-ack {
  width: 100%;
  background: rgba(99, 102, 241, 0.15);
  border: 1px solid rgba(99, 102, 241, 0.25);
  color: #a5b4fc;
  border-radius: 8px;
  padding: 5px;
  font-size: 12px;
  cursor: pointer;
  box-shadow: none;
  transition: background 0.15s;
}
.notif-ack:hover { background: rgba(99, 102, 241, 0.25); }
</style>
```

- [ ] **Step 2: 在 `App.vue` 中挂载 NotificationBubble**

在 `<script setup>` import 区域加入：

```js
import NotificationBubble from './components/NotificationBubble.vue'
```

在 `<template>` 的 `<SettingsWindow ... />` 之后追加：

```html
<NotificationBubble
  :pet-pos="ballPos"
  :pet-size="ballSize"
  :bubble-open="bubbleOpen"
/>
```

- [ ] **Step 3: 将新用户欢迎改为气泡展示**

将 `App.vue` 的 `onMounted` 中首次启动逻辑从直接打开设置改为先发气泡：

```js
import { MissingRequiredConfig, IsFirstLaunch, MarkWelcomeShown } from '../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime'

// 在 onMounted 内替换：
const missing = await MissingRequiredConfig()
const firstLaunch = await IsFirstLaunch()
if (firstLaunch) {
  await MarkWelcomeShown()
  // Show welcome notification bubble above the pet.
  EventsEmit('notification:show', {
    title: '你好！我是你的桌面宠物 ✨',
    message: '请先在设置中配置 LLM 接口，然后就可以开始聊天了~',
  })
}
if (missing && missing.length > 0) {
  setTimeout(() => { settingsOpen.value = true }, firstLaunch ? 2500 : 0)
}
offToggle = EventsOn('bubble:toggle', () => { bubbleOpen.value = !bubbleOpen.value })
```

- [ ] **Step 4: 验证编译和前端构建**

```bash
go build ./... && cd frontend && yarn build 2>&1 | tail -5
```

Expected: `Done in ...s`。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/NotificationBubble.vue frontend/src/App.vue
git commit -m "feat: add NotificationBubble as shared component; replace welcome dialog with bubble"
```

---

## Self-Review

**Spec coverage:**
- ✅ #3 聊天界面优化 — Task 1 (ChatBubble) + Task 2 (ChatPanel)
- ✅ #3 设置界面优化 — Task 3 (SettingsWindow)
- ✅ 顺带优化右键菜单 — Task 4 (ContextMenu)
- ✅ #11 气泡通知（通用公共组件）— Task 5 (NotificationBubble)
- ✅ 新用户欢迎改为气泡展示 — Task 5 Step 3（App.vue，emit `notification:show`）
- ✅ 聊天框未打开时才展示气泡 — Task 5 Step 1（`bubbleOpen` prop 控制）

**Placeholder scan:** 无 TBD / TODO，所有 CSS 和 JS 均为完整实现。

**Type consistency:** 仅 CSS/模板变更；`notification:show` 事件 payload `{ title, message }` 在 Task 5 Step 1 定义，App.vue Step 3 中 emit 使用相同结构。

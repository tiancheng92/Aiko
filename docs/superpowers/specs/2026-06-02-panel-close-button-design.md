# 面板关闭按钮设计

**日期**：2026-06-02  
**范围**：`PomodoroPanel.vue`、`ClaudeStatusPanel.vue`、`SystemResourcePanel.vue`、`App.vue`

---

## 目标

为三个小面板的标题栏统一添加 × 关闭按钮，行为如下：

| 面板 | 关闭行为 |
|---|---|
| **PomodoroPanel** | 仅隐藏面板（`visible = false`），番茄钟继续在后台计时 |
| **ClaudeStatusPanel** | 隐藏面板（`emit('close')`） |
| **SystemResourcePanel** | 隐藏面板（`emit('close')`） |

---

## 各组件改动

### PomodoroPanel.vue

**标题栏模板**

将：
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

**新增函数**（在 `onStop` 之前）：
```js
/** onClose hides the panel without stopping the pomodoro timer. */
function onClose() {
  visible.value = false
  emit('close')
}
```

**CSS**（追加在现有 `.pomo-titlebar-label` 之后）：
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

---

### ClaudeStatusPanel.vue

**新增 emit**（在 `<script setup>` 顶部）：
```js
const emit = defineEmits(['close'])
```

**标题栏模板**

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

**CSS**（追加在现有 `.cp-titlebar-label` 之后）：
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

---

### SystemResourcePanel.vue

**新增 emit**（`<script setup>` 顶部）：
```js
const emit = defineEmits(['close'])
```

**标题栏模板**

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

**CSS**（追加在现有 `.sys-titlebar-label` 之后）：
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

---

### App.vue

**新增 `closeClaudePanel` 函数**（在 `closePomodoro` 附近）：
```js
/** closeClaudePanel hides the Claude Code status panel. */
function closeClaudePanel() {
  claudePanelOpen.value = false
}
```

**绑定 `@close`** 到两个面板（模板中）：

将：
```html
<ClaudeStatusPanel
  v-if="claudePanelOpen"
  ref="claudePanelRef"
/>
<SystemResourcePanel
  v-if="systemPanelOpen"
  ref="systemPanelRef"
/>
```
改为：
```html
<ClaudeStatusPanel
  v-if="claudePanelOpen"
  ref="claudePanelRef"
  @close="closeClaudePanel"
/>
<SystemResourcePanel
  v-if="systemPanelOpen"
  ref="systemPanelRef"
  @close="closeSystemPanel"
/>
```

注：`closeSystemPanel()` 已存在于 App.vue（line 503），无需新增。

---

## i18n

在四个 locale 文件的顶层添加 `common` 对象（若已存在则只加 `close` key）：

| 文件 | 内容 |
|---|---|
| `zh-CN.json` | `"common": { "close": "关闭" }` |
| `en.json` | `"common": { "close": "Close" }` |
| `ja.json` | `"common": { "close": "閉じる" }` |
| `ko.json` | `"common": { "close": "닫기" }` |

---

## 文件清单

| 文件 | 操作 |
|---|---|
| `frontend/src/components/PomodoroPanel.vue` | 新增 `onClose()`，标题栏加按钮，加 CSS |
| `frontend/src/components/ClaudeStatusPanel.vue` | 新增 `defineEmits`，标题栏加按钮，加 CSS |
| `frontend/src/components/SystemResourcePanel.vue` | 新增 `defineEmits`，标题栏加按钮，加 CSS |
| `frontend/src/App.vue` | 新增 `closeClaudePanel()`，模板绑定 `@close` |
| `frontend/src/locales/zh-CN.json` | 加 `common.close` |
| `frontend/src/locales/en.json` | 加 `common.close` |
| `frontend/src/locales/ja.json` | 加 `common.close` |
| `frontend/src/locales/ko.json` | 加 `common.close` |

---

## 不做的事

- 不修改番茄钟的停止逻辑（`onStop` 保持不变）
- 不持久化面板关闭状态
- 不添加键盘快捷键（ESC 等）

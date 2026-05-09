# Liquid Glass 主题系统 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Liquid Glass / Frosted Glass 双主题系统，用户可在设置界面实时切换，选择持久化到 SQLite。

**Architecture:** 后端 `Config.ThemeStyle` 字段持久化主题选择；前端用 `html[data-theme]` CSS 选择器切换两套 CSS 变量（`tokens.css`）；所有组件的 surface/blur/shadow/border 硬编码值统一改为引用 token；`SettingsWindow` appearance tab 新增切换 UI，复用现有 `backend-btn` 样式。

**Tech Stack:** Go（config store）、Vue 3 Composition API（`<script setup>`）、CSS 自定义属性

---

### Task 1: Config 后端 — ThemeStyle 字段

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: 写失败的测试**

在 `internal/config/config_test.go` 末尾追加：

```go
// TestConfigThemeStyle_RoundTrip tests that ThemeStyle round-trips through Save/Load.
func TestConfigThemeStyle_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	cfg := &config.Config{ThemeStyle: "frosted"}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ThemeStyle != "frosted" {
		t.Errorf("ThemeStyle: got %q, want %q", loaded.ThemeStyle, "frosted")
	}
}

// TestConfigThemeStyle_Default tests that ThemeStyle defaults to "liquid-glass".
func TestConfigThemeStyle_Default(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ThemeStyle != "liquid-glass" {
		t.Errorf("ThemeStyle default: got %q, want %q", loaded.ThemeStyle, "liquid-glass")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
cd /Users/xutiancheng/code/self/Aiko
go test ./internal/config/... -run TestConfigThemeStyle -v
```

期望：`FAIL — config.Config has no field ThemeStyle`

- [ ] **Step 3: 在 Config struct 新增字段**

在 `internal/config/config.go` 的 `Config` struct 末尾（`TTSBackend` 字段后）追加：

```go
// ThemeStyle controls the UI visual style. Values: "liquid-glass" | "frosted".
ThemeStyle string
```

- [ ] **Step 4: Load() 新增读取**

在 `Load()` 中，`cfg.TTSAutoPlay = ...` 行之后追加：

```go
cfg.ThemeStyle = orDefault(m["theme_style"], "liquid-glass")
```

- [ ] **Step 5: Save() 新增写入**

在 `Save()` 的 `pairs` map 中，`"code_timeout"` 行之后追加：

```go
"theme_style": cfg.ThemeStyle,
```

- [ ] **Step 6: 运行测试，确认通过**

```bash
go test ./internal/config/... -run TestConfigThemeStyle -v
```

期望：`PASS`

- [ ] **Step 7: 跑全量 config 测试确保无回归**

```bash
go test ./internal/config/... -v
```

期望：所有已有测试仍然 PASS

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add ThemeStyle field with liquid-glass default"
```

---

### Task 2: 前端 tokens.css — 双主题 CSS 变量

**Files:**
- Create: `frontend/src/styles/tokens.css`
- Modify: `frontend/src/main.js`

- [ ] **Step 1: 创建 `frontend/src/styles/` 目录并写 tokens.css**

```bash
mkdir -p /Users/xutiancheng/code/self/Aiko/frontend/src/styles
```

新建 `frontend/src/styles/tokens.css`，内容：

```css
/* ── Liquid Glass（默认主题）── */
html[data-theme="liquid-glass"],
html:not([data-theme]) {
  --lg-surface:          rgba(255, 255, 255, 0.08);
  --lg-surface-elevated: rgba(255, 255, 255, 0.11);
  --lg-surface-modal:    rgba(255, 255, 255, 0.10);
  --lg-surface-input:    rgba(255, 255, 255, 0.07);
  --lg-surface-input-h:  rgba(255, 255, 255, 0.10);

  --lg-blur:    blur(72px) saturate(220%) brightness(1.08);
  --lg-blur-sm: blur(48px) saturate(200%) brightness(1.06);

  --lg-border:        rgba(255, 255, 255, 0.18);
  --lg-border-subtle: rgba(255, 255, 255, 0.10);

  --lg-shadow:
    0 32px 80px rgba(0, 0, 0, 0.45),
    0 0 0 0.5px rgba(0, 0, 0, 0.25),
    0 1px 0 rgba(255, 255, 255, 0.16) inset;
  --lg-shadow-sm:
    0 20px 52px rgba(0, 0, 0, 0.40),
    0 0 0 0.5px rgba(0, 0, 0, 0.22),
    0 1px 0 rgba(255, 255, 255, 0.14) inset;
}

/* ── Frosted Glass（经典毛玻璃）── */
html[data-theme="frosted"] {
  --lg-surface:          rgba(28, 28, 32, 0.78);
  --lg-surface-elevated: rgba(20, 20, 24, 0.82);
  --lg-surface-modal:    rgba(42, 42, 48, 0.92);
  --lg-surface-input:    rgba(255, 255, 255, 0.06);
  --lg-surface-input-h:  rgba(255, 255, 255, 0.09);

  --lg-blur:    blur(40px) saturate(180%);
  --lg-blur-sm: blur(32px) saturate(160%);

  --lg-border:        rgba(255, 255, 255, 0.12);
  --lg-border-subtle: rgba(255, 255, 255, 0.08);

  --lg-shadow:
    0 24px 64px rgba(0, 0, 0, 0.55),
    0 0 0 0.5px rgba(0, 0, 0, 0.30),
    0 1px 0 rgba(255, 255, 255, 0.07) inset;
  --lg-shadow-sm:
    0 20px 52px rgba(0, 0, 0, 0.55),
    0 0 0 0.5px rgba(0, 0, 0, 0.30),
    0 1px 0 rgba(255, 255, 255, 0.08) inset;
}

/* ── 两套主题共用 ── */
:root {
  --text-primary:   rgba(255, 255, 255, 0.94);
  --text-secondary: rgba(255, 255, 255, 0.66);
  --text-tertiary:  rgba(255, 255, 255, 0.44);
  --accent:         #0A84FF;
  --danger:         rgba(255, 69, 58, 1);
}
```

- [ ] **Step 2: 更新 `frontend/src/main.js`**

将 `frontend/src/main.js` 改为：

```js
import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './style.css'

import { GetConfig } from './wailsjs/go/main/App'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'liquid-glass'
}).catch(() => {
  document.documentElement.dataset.theme = 'liquid-glass'
})

createApp(App).mount('#app')
```

- [ ] **Step 3: 验证编译无报错**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -20
```

期望：`✓ built in` 无 error

- [ ] **Step 4: Commit**

```bash
git add frontend/src/styles/tokens.css frontend/src/main.js
git commit -m "feat(theme): add tokens.css with liquid-glass/frosted variables"
```

---

### Task 3: ChatBubble.vue — 引用 token

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue`

- [ ] **Step 1: 替换 `.chat-bubble` 的 surface/blur/shadow/border**

在 `frontend/src/components/ChatBubble.vue` 的 `<style scoped>` 中，将 `.chat-bubble` 规则从：

```css
.chat-bubble {
  --surface: rgba(28, 28, 32, 0.78);
  --text-primary: rgba(255, 255, 255, 0.94);
  --text-tertiary: rgba(255, 255, 255, 0.44);
  --border-subtle: rgba(255, 255, 255, 0.08);

  position: fixed;
  background: var(--surface);
  backdrop-filter: blur(40px) saturate(180%);
  -webkit-backdrop-filter: blur(40px) saturate(180%);
  border: 1px solid var(--border-subtle);
  border-radius: 14px;
  box-shadow:
    0 24px 64px rgba(0, 0, 0, 0.55),
    0 0 0 0.5px rgba(0, 0, 0, 0.3),
    0 1px 0 rgba(255, 255, 255, 0.07) inset;
```

改为：

```css
.chat-bubble {
  position: fixed;
  background: var(--lg-surface);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 14px;
  box-shadow: var(--lg-shadow);
```

同时删除文件中已不再使用的局部变量声明（`--surface`、`--text-primary`、`--text-tertiary`、`--border-subtle`）——这些值现在由 `tokens.css` 的全局变量覆盖。凡是组件内引用 `var(--text-primary)` 等的地方**保持不变**，全局 token 已定义这些变量。

- [ ] **Step 2: 替换 title-bar border 和 icon-btn hover**

将 `.title-bar` 的 `border-bottom`:

```css
border-bottom: 1px solid var(--border-subtle);
```

改为：

```css
border-bottom: 1px solid var(--lg-border-subtle);
```

将 `.latency-badge` 的 `border`:

```css
border: 1px solid var(--border-subtle);
```

改为：

```css
border: 1px solid var(--lg-border-subtle);
```

将 `.icon-btn:hover` 的 `background`:

```css
background: rgba(255, 255, 255, 0.08);
```

改为：

```css
background: var(--lg-surface-input);
```

- [ ] **Step 3: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：无 error

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ChatBubble.vue
git commit -m "feat(theme): migrate ChatBubble to CSS token variables"
```

---

### Task 4: ChatPanel.vue — 引用 token

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: 替换局部变量声明**

在 `<style scoped>` 的 `.chat-panel` 规则中，将以下局部变量声明删除：

```css
--surface-card: rgba(255, 255, 255, 0.05);
--surface-input: rgba(255, 255, 255, 0.06);
--surface-input-hover: rgba(255, 255, 255, 0.09);
--border-subtle: rgba(255, 255, 255, 0.08);
--border-default: rgba(255, 255, 255, 0.12);
```

并在使用这些变量的地方作以下替换（`replace_all: true`）：

| 旧值 | 新值 |
|---|---|
| `var(--surface-card)` | `var(--lg-surface)` |
| `var(--surface-input)` | `var(--lg-surface-input)` |
| `var(--surface-input-hover)` | `var(--lg-surface-input-h)` |
| `var(--border-subtle)` | `var(--lg-border-subtle)` |
| `var(--border-default)` | `var(--lg-border)` |

- [ ] **Step 2: 替换 assistant 消息气泡的 backdrop-filter 和 box-shadow**

找到 `/* Assistant bubble — glass surface */` 注释下方的规则，将：

```css
backdrop-filter: blur(40px) saturate(180%);
-webkit-backdrop-filter: blur(40px) saturate(180%);
```

改为：

```css
backdrop-filter: var(--lg-blur-sm);
-webkit-backdrop-filter: var(--lg-blur-sm);
```

将消息气泡的 `box-shadow`（`0 12px 36px rgba(0, 0, 0, 0.55), ...`）改为：

```css
box-shadow: var(--lg-shadow-sm);
```

- [ ] **Step 3: 替换渐变遮罩中的硬编码 rgba**

找到底部渐变遮罩（gradient overlay）中的 `rgba(44, 44, 48, ...)` 系列颜色，这些是渐变遮罩，**不使用 surface token**，改为基于 `transparent` 到 `rgba(0,0,0,0.4)` 的通用遮罩：

```css
/* 将 */
rgba(44, 44, 48, 0)    0%,
rgba(44, 44, 48, 0.55) 55%,
rgba(44, 44, 48, 0.92) 100%
/* 改为 */
rgba(0, 0, 0, 0)    0%,
rgba(0, 0, 0, 0.38) 55%,
rgba(0, 0, 0, 0.72) 100%
```

- [ ] **Step 4: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：无 error

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(theme): migrate ChatPanel to CSS token variables"
```

---

### Task 5: NotificationBubble.vue + ToolConfirmModal.vue — 引用 token

**Files:**
- Modify: `frontend/src/components/NotificationBubble.vue`
- Modify: `frontend/src/components/ToolConfirmModal.vue`

- [ ] **Step 1: NotificationBubble.vue — 替换局部变量和属性**

在 `<style scoped>` 中，将局部变量声明块：

```css
--surface: rgba(28, 28, 32, 0.82);
--text-primary: rgba(255, 255, 255, 0.94);
--text-secondary: rgba(255, 255, 255, 0.68);
--text-tertiary: rgba(255, 255, 255, 0.42);
--border-subtle: rgba(255, 255, 255, 0.10);
```

全部删除。然后作以下替换（`replace_all: true`）：

| 旧值 | 新值 |
|---|---|
| `background: var(--surface);` | `background: var(--lg-surface);` |
| `backdrop-filter: blur(40px) saturate(180%);` | `backdrop-filter: var(--lg-blur);` |
| `-webkit-backdrop-filter: blur(40px) saturate(180%);` | `-webkit-backdrop-filter: var(--lg-blur);` |
| `0 20px 52px rgba(0, 0, 0, 0.55),` | （合并到 `var(--lg-shadow-sm)` — 见下）|
| `border: ... var(--border-subtle)` | `border: 1px solid var(--lg-border-subtle);` |

将 box-shadow 三行替换为：

```css
box-shadow: var(--lg-shadow-sm);
```

`@media (prefers-reduced-motion: reduce)` 块保持不动。

- [ ] **Step 2: ToolConfirmModal.vue — 替换局部变量和属性**

将局部变量声明块：

```css
--surface: rgba(42, 42, 48, 0.92);
--surface-input: rgba(255, 255, 255, 0.06);
--surface-input-hover: rgba(255, 255, 255, 0.09);
--border-subtle: rgba(255, 255, 255, 0.09);
--border-default: rgba(255, 255, 255, 0.14);
```

全部删除。然后作以下替换：

| 旧值 | 新值 |
|---|---|
| `background: var(--surface)` | `background: var(--lg-surface-modal)` |
| `backdrop-filter: blur(40px) saturate(180%)` | `backdrop-filter: var(--lg-blur)` |
| `-webkit-backdrop-filter: blur(40px) saturate(180%)` | `-webkit-backdrop-filter: var(--lg-blur)` |
| `var(--surface-input)` | `var(--lg-surface-input)` |
| `var(--surface-input-hover)` | `var(--lg-surface-input-h)` |
| `var(--border-subtle)` | `var(--lg-border-subtle)` |
| `var(--border-default)` | `var(--lg-border)` |

box-shadow 三行替换为：

```css
box-shadow: var(--lg-shadow);
```

`.modal-backdrop`（半透明遮罩层）的 `background` 保持原值不动——它是遮罩，不是 surface。

- [ ] **Step 3: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：无 error

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/NotificationBubble.vue frontend/src/components/ToolConfirmModal.vue
git commit -m "feat(theme): migrate NotificationBubble and ToolConfirmModal to CSS tokens"
```

---

### Task 6: SettingsWindow.vue — 引用 token + 新增主题切换 UI

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: 替换局部变量声明**

在 `<style scoped>` 中找到以下局部变量声明（通常在 `.settings-win` 规则内），全部删除：

```css
--surface: rgba(28, 28, 32, 0.78);
--surface-sidebar: rgba(20, 20, 24, 0.55);
/* 以及所有其他 rgba surface 变量 */
```

然后替换（`replace_all: true`）：

| 旧值 | 新值 |
|---|---|
| 主 surface（`rgba(28,28,32,0.78)`） | `var(--lg-surface)` |
| sidebar surface（`rgba(20,20,24,0.55)`） | `var(--lg-surface-elevated)` |
| input 背景（`rgba(255,255,255,0.06)`） | `var(--lg-surface-input)` |
| input hover（`rgba(255,255,255,0.09)`） | `var(--lg-surface-input-h)` |
| `backdrop-filter: blur(40px) saturate(180%)` | `var(--lg-blur)` |
| `-webkit-backdrop-filter: blur(40px) saturate(180%)` | `var(--lg-blur)` |
| 主 box-shadow | `var(--lg-shadow)` |
| border subtle | `var(--lg-border-subtle)` |
| border default | `var(--lg-border)` |

- [ ] **Step 2: 新增 `setThemeStyle` 函数**

在 `<script setup>` 中，紧接 `setRenderBackend` 函数之后追加：

```js
/** setThemeStyle updates UI theme and applies it immediately to the document root. */
function setThemeStyle(style) {
  cfg.value.ThemeStyle = style
  document.documentElement.dataset.theme = style
}
```

- [ ] **Step 3: 新增主题切换 UI**

在 `appearance` tab 的 `<div v-if="activeTab === 'appearance'" class="tab-pane">` 中，紧接「渲染后端」`<label>` 块之后追加：

```html
<!-- 界面风格 -->
<label>界面风格
  <div class="backend-toggle">
    <button
      :class="['backend-btn', cfg.ThemeStyle !== 'frosted' ? 'active' : '']"
      @click="setThemeStyle('liquid-glass')"
    >液态玻璃</button>
    <button
      :class="['backend-btn', cfg.ThemeStyle === 'frosted' ? 'active' : '']"
      @click="setThemeStyle('frosted')"
    >毛玻璃</button>
  </div>
</label>
<p class="sms-desc" style="margin-top:4px;margin-bottom:16px">液态玻璃为近透明折射风格；毛玻璃为经典深色风格。切换后点击「保存」持久化。</p>
```

- [ ] **Step 4: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：无 error

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(theme): migrate SettingsWindow to tokens + add theme switcher UI"
```

---

### Task 7: FloatingBall.vue — 新增 backdrop-filter

**Files:**
- Modify: `frontend/src/components/FloatingBall.vue`

- [ ] **Step 1: 将 solid 背景改为半透明 + blur**

在 `<style scoped>` 中，将 `.floating-ball` 的 background 和相关样式从：

```css
background: rgba(79, 70, 229, 0.9);
```

改为：

```css
background: rgba(79, 70, 229, 0.35);
backdrop-filter: var(--lg-blur-sm);
-webkit-backdrop-filter: var(--lg-blur-sm);
```

将 `.floating-ball:hover` 的 background 从：

```css
background: rgba(99, 90, 255, 0.95);
```

改为：

```css
background: rgba(99, 90, 255, 0.45);
```

- [ ] **Step 2: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -5
```

期望：无 error

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/FloatingBall.vue
git commit -m "feat(theme): add backdrop-filter to FloatingBall for glass effect"
```

---

### Task 8: 运行时测试与验证

- [ ] **Step 1: 构建并启动应用**

```bash
cd /Users/xutiancheng/code/self/Aiko && make run
```

- [ ] **Step 2: 验证默认主题（液态玻璃）**

打开应用，检查：
- 聊天气泡背景近透明，桌面内容透过来
- blur 效果明显（背景折射）
- 边框高光可见
- 文字可读（`rgba(255,255,255,0.94)`）

- [ ] **Step 3: 验证主题切换**

打开设置 → 外观 → 点击「毛玻璃」按钮：
- 聊天气泡背景立即变深（`rgba(28,28,32,0.78)`）
- 不需要刷新或重启
- 点击「保存」后关闭重启应用，确认毛玻璃风格恢复

- [ ] **Step 4: 切回液态玻璃并保存**

点击「液态玻璃」→「保存」→ 重启确认默认风格持久化

- [ ] **Step 5: 跑 Go 测试确认无回归**

```bash
cd /Users/xutiancheng/code/self/Aiko && go test ./... 2>&1 | grep -E "FAIL|ok"
```

期望：所有包 `ok`，无 `FAIL`

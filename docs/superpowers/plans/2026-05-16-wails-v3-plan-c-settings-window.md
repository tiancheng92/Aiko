# Wails v3 迁移 Plan C：设置窗口原生多窗口化

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将设置界面从主窗口内的覆盖层（`SettingsWindow.vue`）改为独立的 v3 `WebviewWindow`，彻底移除 NSPanel hack 相关代码。

**Architecture:** 设置窗口加载独立的前端入口（新增 `settings.html` 或通过 URL 参数路由），`SettingsWindow.vue` 的内容提取为独立页面。`WindowService.OpenSettings()` 直接操作 v3 window API。主窗口不再渲染设置组件。

**前置条件：** Plan A 和 Plan B 均已完成。

**Tech Stack:** Vue 3 + Vite，`github.com/wailsapp/wails/v3 v3.0.0-alpha.92`

---

## 文件变更总览

| 操作 | 文件 |
|------|------|
| 新增 | `frontend/src/SettingsApp.vue` — 设置窗口根组件 |
| 新增 | `frontend/settings.html` — 设置窗口 HTML 入口 |
| 修改 | `frontend/vite.config.js` — 添加 settings 多页面构建 |
| 修改 | `frontend/src/App.vue` — 移除 `<SettingsWindow>` 渲染和 `settings:open` 监听 |
| 修改 | `frontend/src/components/SettingsWindow.vue` — 重构为纯内容组件（无 open/close 逻辑） |
| 修改 | `main.go` — 更新 settingsWin URL 为 `/settings.html` |
| 修改 | `macos.go` — 移除 NSPanel 相关代码（`acquireKeyWindow`、`releaseKeyWindow` 若仅用于 NSPanel） |
| 修改 | `internal/services/window_service.go` — `OpenSettings` 使用 v3 Window API |
| 更新 | `CLAUDE.md` — 更新 hitTest 选择器说明 |

---

## Task 1：添加 Vite 多页面构建配置

**Files:**
- Create: `frontend/settings.html`
- Modify: `frontend/vite.config.js`

设置窗口需要独立的 HTML 入口，Vite 支持多页面（MPA）模式。

- [ ] **Step 1: 查看当前 vite.config.js**

```bash
cat frontend/vite.config.js
```

- [ ] **Step 2: 创建 settings.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8"/>
    <meta content="width=device-width, initial-scale=1.0" name="viewport"/>
    <title>Aiko Settings</title>
</head>
<body>
<div id="settings-app"></div>
<script type="module" src="/src/settings-main.js"></script>
</body>
</html>
```

- [ ] **Step 3: 修改 vite.config.js 添加多页面入口**

在 `vite.config.js` 的 `build` 配置中添加 `rollupOptions.input`：

```js
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  build: {
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        settings: resolve(__dirname, 'settings.html'),
      },
    },
  },
  // 保留其他现有配置
})
```

- [ ] **Step 4: 验证 Vite 配置**

```bash
cd frontend && yarn build 2>&1 | tail -20
```

预期：构建成功，`dist/` 下包含 `index.html` 和 `settings.html`。

- [ ] **Step 5: 提交**

```bash
git add frontend/settings.html frontend/vite.config.js
git commit -m "chore: add Vite multi-page build for settings window"
```

---

## Task 2：创建设置窗口独立入口

**Files:**
- Create: `frontend/src/settings-main.js`
- Create: `frontend/src/SettingsApp.vue`

- [ ] **Step 1: 创建 settings-main.js**

```js
import { createApp } from 'vue'
import SettingsApp from './SettingsApp.vue'
import './styles/tokens.css'
import './style.css'
import { GetConfig } from '../bindings/aiko/internal/services/ConfigService'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'frosted'
}).catch(() => {
  document.documentElement.dataset.theme = 'frosted'
})

createApp(SettingsApp).mount('#settings-app')
```

- [ ] **Step 2: 创建 SettingsApp.vue**

`SettingsApp.vue` 是设置窗口的根组件，直接渲染 `SettingsWindow.vue` 的内容（无需 open/close 状态管理，窗口显示由 Go 控制）：

```vue
<script setup>
import SettingsWindow from './components/SettingsWindow.vue'
import { onMounted } from 'vue'
import { EventsOn } from '@wailsio/runtime'

// 监听关闭请求（可选：允许 Go 端发事件关闭窗口）
onMounted(() => {
  // 设置窗口无需 open/close 状态，始终显示内容
})
</script>

<template>
  <div class="settings-root">
    <SettingsWindow :standalone="true" />
  </div>
</template>

<style>
html, body, #settings-app, .settings-root {
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
  overflow: hidden;
}
</style>
```

- [ ] **Step 3: 前端构建验证**

```bash
cd frontend && yarn build
```

预期：`dist/settings.html` 存在，构建无错误。

- [ ] **Step 4: 提交**

```bash
git add frontend/src/settings-main.js frontend/src/SettingsApp.vue
git commit -m "feat: add settings window standalone entry point"
```

---

## Task 3：重构 SettingsWindow.vue — 移除 open/close 逻辑

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

当前 `SettingsWindow.vue` 有 `open` prop 和 `settings:open` 事件监听控制显示/隐藏。改为纯内容组件，由父级（`SettingsApp.vue`）或 Go 窗口层控制显示。

- [ ] **Step 1: 检查 SettingsWindow.vue 的当前结构**

```bash
grep -n "open\|EventsOn\|settings:open\|v-if\|v-show\|emit\|defineProps" frontend/src/components/SettingsWindow.vue | head -30
```

- [ ] **Step 2: 移除 open 相关 prop 和事件监听**

在 `SettingsWindow.vue` 中：
- 移除 `const props = defineProps({ open: Boolean })` 或等价的 open prop
- 移除 `EventsOn('settings:open', ...)` 监听器及其 `onUnmounted` 清理
- 移除最外层的 `v-if="open"` 或 `v-show="open"`（组件始终渲染）
- 保留所有设置内容、表单、保存逻辑不变

添加可选的 `standalone` prop（供 `SettingsApp.vue` 传入），控制样式微调（如是否显示关闭按钮）：

```js
const props = defineProps({
  standalone: {
    type: Boolean,
    default: false,
  },
})
```

- [ ] **Step 3: 验证 SettingsWindow.vue 内容完整**

```bash
cd frontend && yarn build
```

预期：无错误。

- [ ] **Step 4: 提交**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "refactor: SettingsWindow.vue — remove open/close logic, become pure content component"
```

---

## Task 4：更新 App.vue — 移除设置覆盖层

**Files:**
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: 在 App.vue 中移除 SettingsWindow 渲染**

找到并删除：
- `import SettingsWindow from './components/SettingsWindow.vue'`
- `<SettingsWindow :open="settingsOpen" />` 模板标签
- `const settingsOpen = ref(false)` 状态
- `offSettings = EventsOn('settings:open', () => { settingsOpen.value = true })` 监听（及对应 off 清理）

- [ ] **Step 2: 更新 settings:open 事件处理**

`settings:open` 事件现在由后端 `WindowService` 处理（调用 `settingsWin.Show()`）。但为了保持前端菜单快捷键和老代码的兼容，在 `App.vue` 中将 `settings:open` 事件改为调用后端 `OpenSettings()`：

```js
import { OpenSettings } from '../bindings/aiko/internal/services/WindowService'

// 替换原来的 settingsOpen.value = true
offSettings = EventsOn('settings:open', () => {
  OpenSettings()
})
```

- [ ] **Step 3: 验证 App.vue 中无 SettingsWindow 相关代码**

```bash
grep "SettingsWindow\|settingsOpen" frontend/src/App.vue
```

预期：无输出。

- [ ] **Step 4: 前端构建验证**

```bash
cd frontend && yarn build
```

- [ ] **Step 5: 提交**

```bash
git add frontend/src/App.vue
git commit -m "refactor: remove SettingsWindow overlay from App.vue"
```

---

## Task 5：更新 main.go — settingsWin URL

**Files:**
- Modify: `main.go`

- [ ] **Step 1: 更新 settingsWin 的 URL**

在 `main.go` 中找到：

```go
settingsWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
    // ...
    URL: "/?settings=1",
})
```

替换为：

```go
settingsWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
    Name:   "settings",
    Width:  900,
    Height: 680,
    Title:  "Aiko Settings",
    Hidden: true,
    URL:    "/settings.html",
})
```

- [ ] **Step 2: 编译验证**

```bash
go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add main.go
git commit -m "feat: settings window loads /settings.html"
```

---

## Task 6：移除 NSPanel hack 相关代码

**Files:**
- Modify: `macos.go`
- Modify: `internal/services/window_service.go`

当前 `macos.go` 中的 `acquireKeyWindow` / `releaseKeyWindow` 是 NSPanel hack 的核心。检查是否仍有其他用途：

- [ ] **Step 1: 检查 acquireKeyWindow / releaseKeyWindow 使用情况**

```bash
grep -rn "acquireKeyWindow\|releaseKeyWindow\|NSPanel\|convertToPanel" . --include="*.go" | grep -v ".git"
```

- [ ] **Step 2: 若仅用于设置窗口，移除 NSPanel 相关 CGO 代码**

在 `macos.go` 中找到并删除：
- `acquireKeyWindow()` / `releaseKeyWindow()` 函数定义
- 对应的 Objective-C CGO 实现（`makeKeyWindow`、`resignKeyWindow` 等）
- `convertToPanel` / `convertToWindow` 相关代码

- [ ] **Step 3: 更新 WindowService**

`WindowService` 中若有 `AcquireKeyWindow` / `ReleaseKeyWindow` 方法，将其改为空实现或删除（v3 多窗口不需要手动管理 key window）：

```go
// AcquireKeyWindow is a no-op under Wails v3 multi-window.
func (w *WindowService) AcquireKeyWindow() {}

// ReleaseKeyWindow is a no-op under Wails v3 multi-window.
func (w *WindowService) ReleaseKeyWindow() {}
```

保留方法签名（前端可能仍在调用），只清空实现。

- [ ] **Step 4: 编译验证**

```bash
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add macos.go internal/services/window_service.go
git commit -m "refactor: remove NSPanel hack — settings window is now native v3 WebviewWindow"
```

---

## Task 7：更新 CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: 更新 CLAUDE.md 中的相关条目**

找到以下条目并更新：

1. **hitTest 选择器**：`.settings-win` 条目可能需要更新，因为设置窗口现在是独立窗口，不再是主窗口的覆盖层
2. **wailsjs 路径**：将 `wailsjs/go/main/App` 更新为 `bindings/aiko/internal/services/XxxService`
3. **Wails 版本**：更新 v2 → v3 引用
4. **开发命令**：`wails dev` → `wails3 dev`，`wails generate module` → `wails3 generate bindings`

- [ ] **Step 2: 提交**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for Wails v3 and service split"
```

---

## Task 8：完整集成测试

- [ ] **Step 1: 全量构建**

```bash
make build
```

- [ ] **Step 2: 启动验证**

```bash
make run
```

- [ ] **Step 3: 核心功能验证清单**

- [ ] 主窗口宠物正常显示
- [ ] 聊天气泡切换（Cmd+Shift+P）
- [ ] 发送消息，流式响应正常
- [ ] 菜单 Cmd+, 打开设置窗口（独立窗口，非覆盖层）
- [ ] 设置窗口内容完整，可修改并保存配置
- [ ] 设置窗口关闭后主窗口正常工作
- [ ] 长按 Option 键触发语音（`voice:start` 事件正常发出）
- [ ] hitTest 点击穿透正常（点击透明区域穿透到桌面）
- [ ] MCP 工具列表正常加载

- [ ] **Step 4: 提交修复**

```bash
git add -A
git commit -m "fix: integration fixes after settings window migration"
```

---

## 完成标准

- [ ] `make build` 成功
- [ ] 设置窗口是独立的原生窗口，不再是主窗口覆盖层
- [ ] NSPanel hack 相关代码已移除
- [ ] 主窗口 `App.vue` 中无 `SettingsWindow` 组件引用
- [ ] 所有核心功能验证通过

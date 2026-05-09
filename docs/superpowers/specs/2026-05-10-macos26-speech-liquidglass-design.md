# macOS 26 适配：SpeechAnalyzer 迁移 + Liquid Glass 视觉改造（含主题切换）

**日期**：2026-05-10  
**状态**：已审核，待实现

---

## 背景

macOS 26 (Tahoe) 引入了两个对 Aiko 有直接价值的变化：

1. **SpeechAnalyzer 框架**：替代 `AVAudioEngine + SFSpeechRecognizer` 的新语音识别 API，完全本地运行，比 Whisper Large V3 Turbo 快约 2 倍，噪声适应更强。
2. **Liquid Glass 设计语言**：系统级视觉材质升级，高透明度 + 强折射效果，与 Aiko 的浮层 overlay 定位天然契合。

用户可通过设置界面在 **液态玻璃** 与 **毛玻璃** 两种风格之间切换，选择持久化到 SQLite，重启后恢复。

---

## 一、SpeechAnalyzer 迁移

### 目标

- macOS 26+ 自动使用 `SpeechAnalyzer / DictationTranscriber`
- macOS < 26 自动 fallback 到现有 `SFSpeechRecognizer` 路径
- Go 侧、前端事件系统、Wails 绑定：**零改动**

### 改动范围

**唯一改动文件**：`macos.go`

### 架构

```
startVoiceRecognition()
  ├─ @available(macOS 26.0, *)
  │   └─ startVoiceRecognition_SpeechAnalyzer()   ← 新增函数
  └─ else
      └─ 现有 SFSpeechRecognizer 逻辑（原封不动）

stopVoiceRecognition()
  ├─ @available(macOS 26.0, *)
  │   └─ [gSpeechAnalyzer stopAnalysis] + 清空新全局变量
  └─ else
      └─ 现有 stop 逻辑（原封不动）
```

pipe 架构不变：ObjC 回调 → `sendVoiceText()` → `gVoicePipeFd` → Go goroutine → Wails 事件。

### 新增全局变量

```objc
// macOS 26+ SpeechAnalyzer globals
static id gSpeechAnalyzer       = nil;  // SpeechAnalyzer*
static id gDictationTranscriber = nil;  // DictationTranscriber*
static id gAnalysisTask          = nil; // analysis task handle
```

用 `id` 类型声明，避免在旧系统上编译报错。实际类型在 `@available(macOS 26.0, *)` 块内操作。

### 新函数 `startVoiceRecognition_SpeechAnalyzer()`

在 `@available(macOS 26.0, *)` 块内：

1. 创建 `SpeechAnalyzer`，添加 `DictationTranscriber` 模块
2. 复用现有 `AVAudioEngine` + `inputNode installTapOnBus:` 捕音逻辑，将 PCM buffer 送入 `[analyzer appendAudioPCMBuffer:]`
3. 设置结果回调（`setResultHandler:`）：
   - partial 结果：`sendVoiceText([result.text UTF8String])`
   - isFinal 结果：`sendVoiceText([@"FINAL:..." UTF8String])`
   - 错误：`sendVoiceText([@"ERROR:recognition:..." UTF8String])`
4. 所有回调均用 `@try/@catch` 包裹，与现有路径行为一致

pipe 写入格式与 `SFSpeechRecognizer` 路径**完全相同**，Go 侧无需感知差异。

### 权限

- mic 权限：沿用现有 `AVCaptureDevice` 逻辑，不变
- speech recognition 权限：`SFSpeechRecognizer.authorizationStatus` 在 macOS 26 继续有效，不变

### 错误处理

所有错误统一格式 `ERROR:<type>:<description>` 写入 pipe，Go 侧 `readVoicePipe()` 的 switch-case 无需修改。

---

## 二、Liquid Glass 视觉改造（含主题切换）

### 目标

- 两套主题：**`liquid-glass`**（近透明折射，默认）和 **`frosted`**（现有深色毛玻璃）
- 用户在设置界面 → 外观 tab 切换，实时生效，重启后恢复
- 所有 6 个主要 UI 组件共享 token，CSS 结构不变，JS 逻辑零改动

### 改动范围

| 文件 | 改动类型 |
|---|---|
| `internal/config/config.go` | 新增 `ThemeStyle string` 字段 + Load/Save |
| `frontend/src/styles/tokens.css` | 新建，定义两套主题变量 |
| `frontend/src/main.js` | 新增 import + 启动时应用主题 |
| `frontend/src/components/SettingsWindow.vue` | appearance tab 新增主题切换 UI |
| `ChatBubble.vue` / `ChatPanel.vue` / `SettingsWindow.vue` / `NotificationBubble.vue` / `ToolConfirmModal.vue` / `FloatingBall.vue` | 局部 CSS 变量改为引用 token |

### 后端：Config 新增字段

**`internal/config/config.go`**：

```go
// ThemeStyle controls the UI visual style. Values: "liquid-glass" | "frosted".
ThemeStyle string
```

`Load()` 新增：
```go
cfg.ThemeStyle = orDefault(m["theme_style"], "liquid-glass")
```

`Save()` `pairs` map 新增：
```go
"theme_style": cfg.ThemeStyle,
```

无需 DB migration，`settings` 表使用 `key/value` 模式，直接 upsert 即可。

### 前端：设计 Token

新建 `frontend/src/styles/tokens.css`，通过 `html[data-theme]` 属性选择器切换两套变量：

```css
/* ── Liquid Glass（默认）── */
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

/* ── 文字与 accent（两套主题共用）── */
:root {
  --text-primary:   rgba(255, 255, 255, 0.94);
  --text-secondary: rgba(255, 255, 255, 0.66);
  --text-tertiary:  rgba(255, 255, 255, 0.44);
  --accent:         #0A84FF;
}
```

**Frosted 的值直接来自现有代码**，所以视觉上是零变化的安全 fallback。

### 前端：主题应用时机

`frontend/src/main.js` 顶部：

```js
import './styles/tokens.css'
```

在 Wails `domready` 之后，读取初始配置并设置 `data-theme`：

```js
import { GetConfig } from './wailsjs/go/main/App'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'liquid-glass'
})
```

此后 `SettingsWindow.vue` 的 `setThemeStyle()` 在用户切换时：

```js
function setThemeStyle(style) {
  cfg.value.ThemeStyle = style
  document.documentElement.dataset.theme = style
  // SaveConfig 由现有的 saveConfig() 统一调用，无需额外处理
}
```

`document.documentElement.dataset.theme` 变更后，CSS 选择器立即生效，**切换实时无需刷新**。

### 前端：设置界面 UI

在 `SettingsWindow.vue` 的 `appearance` tab，紧接「渲染后端」控件之后新增：

```html
<!-- UI 风格 -->
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
```

复用现有 `backend-toggle` / `backend-btn` CSS class，零新样式。

### 各组件 CSS 改动规则

**通用替换**（适用于所有组件的 `<style scoped>`）：

| 现有硬编码值 | 替换为 token |
|---|---|
| `rgba(28,28,32,0.78)` / `rgba(42,42,48,0.92)` 等 surface 色 | `var(--lg-surface)` / `var(--lg-surface-modal)` |
| `rgba(255,255,255,0.06)` input 背景 | `var(--lg-surface-input)` |
| `rgba(255,255,255,0.09)` input hover | `var(--lg-surface-input-h)` |
| `backdrop-filter: blur(40px) saturate(180%)` | `var(--lg-blur)` |
| `-webkit-backdrop-filter: blur(40px) saturate(180%)` | `var(--lg-blur)` |
| 主 box-shadow（大阴影） | `var(--lg-shadow)` |
| 小 box-shadow（消息气泡等） | `var(--lg-shadow-sm)` |
| `rgba(255,255,255,0.08)` 边框 | `var(--lg-border-subtle)` |
| `rgba(255,255,255,0.12)` 边框 | `var(--lg-border)` |

**各组件特殊处理**：

| 组件 | 说明 |
|---|---|
| `ChatBubble.vue` | 标准替换 |
| `ChatPanel.vue` | `--surface-card` → `var(--lg-surface)`；`--surface-input` → `var(--lg-surface-input)`；消息气泡 shadow → `var(--lg-shadow-sm)` |
| `SettingsWindow.vue` | sidebar 背景用 `var(--lg-surface-elevated)`；主面板用 `var(--lg-surface)` |
| `NotificationBubble.vue` | 标准替换；现有 `@media (prefers-reduced-motion)` 保留不动 |
| `ToolConfirmModal.vue` | modal 背景用 `var(--lg-surface-modal)` |
| `FloatingBall.vue` | **唯一新增 `backdrop-filter`**；background 从 solid 改为 `rgba(79,70,229,0.35)` + `var(--lg-blur-sm)` |

---

## 实现顺序

1. **`internal/config/config.go`** — `ThemeStyle` 字段 + Load/Save
2. **`tokens.css`** + **`main.js`** import + 启动时 `data-theme` 初始化
3. **`ChatBubble.vue`** — CSS 变量替换（最直观，优先验证视觉效果）
4. **`ChatPanel.vue`** — CSS 变量替换
5. **`NotificationBubble.vue`** + **`ToolConfirmModal.vue`**
6. **`SettingsWindow.vue`** — CSS 变量替换 + 新增主题切换 UI + `setThemeStyle()`
7. **`FloatingBall.vue`** — 补 `backdrop-filter`
8. **`macos.go`** — SpeechAnalyzer 分支（需 macOS 26 真机 / beta 测试）

---

## 不在本次范围内

- `SpeechDetector` 声控触发（替代 Option 键长押）
- macOS 版本检测 flag 传给前端
- 深色 / 浅色模式切换（当前 Aiko 仅支持深色）
- `SettingsWindow.vue` 结构重构
- 任何 Wails 绑定新增 / 前端事件系统改动

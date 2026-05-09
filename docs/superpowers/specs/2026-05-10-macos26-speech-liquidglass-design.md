# macOS 26 适配：SpeechAnalyzer 迁移 + Liquid Glass 视觉改造

**日期**：2026-05-10  
**状态**：已审核，待实现

---

## 背景

macOS 26 (Tahoe) 引入了两个对 Aiko 有直接价值的变化：

1. **SpeechAnalyzer 框架**：替代 `AVAudioEngine + SFSpeechRecognizer` 的新语音识别 API，完全本地运行，比 Whisper Large V3 Turbo 快约 2 倍，噪声适应更强。
2. **Liquid Glass 设计语言**：系统级视觉材质升级，高透明度 + 强折射效果，与 Aiko 的浮层 overlay 定位天然契合。

两项改动互相独立，可分别实现和回滚。

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
static id gSpeechAnalyzer      = nil;  // SpeechAnalyzer*
static id gDictationTranscriber = nil; // DictationTranscriber*
static id gAnalysisTask         = nil; // analysis task handle
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

## 二、Liquid Glass 视觉改造

### 目标

- 所有 6 个主要 UI 组件统一使用 Liquid Glass 风格
- 不区分 macOS 版本（macOS < 26 也能呈现，效果略弱但视觉一致）
- JS 逻辑零改动，只改 CSS

### 改动范围

- **新增**：`frontend/src/styles/tokens.css`
- **修改**：`frontend/src/main.js`（新增一行 import）
- **修改**：6 个组件的 `<style scoped>` CSS 部分

### 设计 Token

新建 `frontend/src/styles/tokens.css`：

```css
:root {
  /* Surface */
  --lg-surface:          rgba(255, 255, 255, 0.08);
  --lg-surface-elevated: rgba(255, 255, 255, 0.11);
  --lg-surface-modal:    rgba(255, 255, 255, 0.10);

  /* Backdrop */
  --lg-blur:    blur(72px) saturate(220%) brightness(1.08);
  --lg-blur-sm: blur(48px) saturate(200%) brightness(1.06);

  /* Border */
  --lg-border:        rgba(255, 255, 255, 0.18);
  --lg-border-subtle: rgba(255, 255, 255, 0.10);

  /* Shadow */
  --lg-shadow:
    0 32px 80px rgba(0, 0, 0, 0.45),
    0 0 0 0.5px rgba(0, 0, 0, 0.25),
    0 1px 0 rgba(255, 255, 255, 0.16) inset;
  --lg-shadow-sm:
    0 20px 52px rgba(0, 0, 0, 0.40),
    0 0 0 0.5px rgba(0, 0, 0, 0.22),
    0 1px 0 rgba(255, 255, 255, 0.14) inset;

  /* Text（沿用现有值，统一到 token） */
  --text-primary:   rgba(255, 255, 255, 0.94);
  --text-secondary: rgba(255, 255, 255, 0.66);
  --text-tertiary:  rgba(255, 255, 255, 0.44);

  /* Accent */
  --accent: #0A84FF;
}
```

### 参数变化对比

| 属性 | 现有 | Liquid Glass |
|---|---|---|
| 背景色 | `rgba(28,28,32,0.78)` 深色 | `rgba(255,255,255,0.08)` 近透明 |
| Blur | `blur(40px) saturate(180%)` | `blur(72px) saturate(220%) brightness(1.08)` |
| 顶部高光 inset | `rgba(255,255,255,0.07)` | `rgba(255,255,255,0.16)` |
| 边框 | `rgba(255,255,255,0.08)` | `rgba(255,255,255,0.18)` |
| 外阴影 | `rgba(0,0,0,0.55)` | `rgba(0,0,0,0.45)` |

**核心思路**：从深色不透明遮罩变为近透明折射层。桌面背景颜色和纹理通过高 blur + saturate + brightness 渗入界面，近似 Liquid Glass 的「lensing」效果。

### 各组件改动说明

**通用规则**（适用于所有组件）：
- `background: rgba(28,28,32,0.78)` → `background: var(--lg-surface)`
- `backdrop-filter: blur(40px) saturate(180%)` → `backdrop-filter: var(--lg-blur)`
- `-webkit-backdrop-filter` 同步更新
- `box-shadow: 0 24px 64px rgba(0,0,0,0.55), ...` → `box-shadow: var(--lg-shadow)`
- `border: 1px solid rgba(255,255,255,0.08)` → `border: 1px solid var(--lg-border-subtle)`

| 组件 | 特殊处理 |
|---|---|
| `ChatBubble.vue` | 标准替换 |
| `ChatPanel.vue` | `--surface-card`、`--surface-input`、`--surface-input-hover` 引用 token；消息气泡 box-shadow 改用 `--lg-shadow-sm` |
| `SettingsWindow.vue` | sidebar 背景用 `--lg-surface`（比主面板略深，复用同 token）|
| `NotificationBubble.vue` | 标准替换；已有 `@media (prefers-reduced-motion)` 保留不动 |
| `ToolConfirmModal.vue` | modal 背景用 `--lg-surface-modal`；backdrop overlay 不变 |
| `FloatingBall.vue` | **唯一需要新增 `backdrop-filter`** 的组件（原为 solid 背景色）；background 改为 `rgba(99, 90, 255, 0.35)` + blur |

### `main.js` 改动

```js
import './styles/tokens.css'  // 新增，其余不变
```

---

## 实现顺序建议

1. `tokens.css` + `main.js` import（验证全局变量生效）
2. `ChatBubble.vue`（最直观，效果立竿见影）
3. `ChatPanel.vue`（消息气泡逐一检查）
4. `NotificationBubble.vue` + `ToolConfirmModal.vue`（结构简单）
5. `SettingsWindow.vue`（最复杂，独立验证）
6. `FloatingBall.vue`（补 backdrop-filter）
7. `macos.go` SpeechAnalyzer 分支（需 macOS 26 真机测试）

---

## 不在本次范围内

- `SpeechDetector` 声控触发（替代 Option 键长按）
- macOS 版本检测 flag 传给前端
- `SettingsWindow.vue` 结构重构
- 任何 Go 侧 / Wails 绑定 / 前端事件系统改动

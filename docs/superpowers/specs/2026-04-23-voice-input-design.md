# 语音输入功能设计文档

**日期：** 2026-04-23  
**阶段：** 阶段4 — 语音输入  
**状态：** 待实现

---

## 概述

通过长按 Option 键（全局热键，≥1s）触发语音输入，利用 macOS 原生 `SFSpeechRecognizer` + `AVAudioEngine` 实现实时语音转文字，并在聊天对话框中实时显示识别结果。录音期间显示 Siri 风格屏幕边框彩虹光 + 居中圆形波形特效。

---

## 1. Option 按键状态机

### 现有逻辑

`macos.go` 中通过 `NSEventMaskFlagsChanged` 全局监控 Option 键：双击（两次 up 间隔 < 0.5s）→ pipe 写入 `byte(1)` → Go 侧触发气泡切换。

### 新状态机

在同一个 `NSEventMaskFlagsChanged` handler 中扩展，通过时长区分双击和长按：

```
Option 按下：
  → 记录按下时刻，启动 1s 定时器（dispatch_after）
  → 若 1s 内 Option 未释放 → 触发长按：
      pipe 写入 byte(2)（开始录音）
      标记 gLongPressTriggered = YES
      取消双击计数

Option 释放：
  → 若 gLongPressTriggered == YES：
      pipe 写入 byte(3)（录音结束）
      清除 gLongPressTriggered
  → 否则走现有双击检测逻辑（不变）
```

**Pipe 信号约定：**

| 字节值 | 含义 |
|--------|------|
| `1`    | 双击 Option → 切换气泡（现有） |
| `2`    | 长按 Option ≥1s → 开始录音 |
| `3`    | Option 释放 → 停止录音 |

### 关键约束

- 长按触发后，释放时**不再触发**双击逻辑（`gLongPressTriggered` 标记保护）
- 双击逻辑完全保留，时序不变

---

## 2. STT 管道（Objective-C CGO）

### 技术选型

- **音频采集：** `AVAudioEngine` — 实时麦克风 tap
- **语音识别：** `SFSpeechRecognizer` — Apple 原生 STT，支持离线/在线，免费
- **结果传递：** CGO exported callback → Go → Wails 事件

### 核心 Objective-C 函数

```objc
// 启动语音识别
void startVoiceRecognition();

// 停止语音识别
void stopVoiceRecognition();
```

### 生命周期

```
startVoiceRecognition()
  1. 检查麦克风权限（AVAudioApplication）
     → 未授权：requestRecordPermission → 拒绝则 callback 传 "ERROR:mic_denied"
  2. 检查语音识别权限（SFSpeechRecognizer.requestAuthorization）
     → 拒绝则 callback 传 "ERROR:speech_denied"
  3. 初始化 SFSpeechRecognizer（locale: zh-CN，fallback: en-US）
  4. AVAudioEngine 安装 inputNode tap（native format）
  5. 启动 SFSpeechRecognitionTask（continuous mode，requiresOnDeviceRecognition: NO）
  6. 每次 partial result → voiceTranscriptCallback(bestTranscription.formattedString)

stopVoiceRecognition()
  1. 结束 SFSpeechRecognitionTask（finish）
  2. 移除 AVAudioEngine inputNode tap
  3. 停止 AVAudioEngine
  4. 最终结果通过同一 callback 传出（isFinal = YES）
```

### CGO Callback

```go
//export voiceTranscriptCallback
func voiceTranscriptCallback(text *C.char) {
    // 在 Go 侧调用，推送 Wails 事件
    t := C.GoString(text)
    if strings.HasPrefix(t, "ERROR:") {
        wailsruntime.EventsEmit(globalAppCtx, "voice:error", t[6:])
        return
    }
    wailsruntime.EventsEmit(globalAppCtx, "voice:transcript", t)
}
```

### Go 侧 pipe 读取扩展

现有 `registerGlobalHotkey()` 的 goroutine 读取循环扩展：

```go
switch b[0] {
case 1:
    // 现有：切换气泡
    wailsruntime.EventsEmit(globalAppCtx, "bubble:toggle", nil)
case 2:
    // 新增：开始录音
    wailsruntime.EventsEmit(globalAppCtx, "voice:start", nil)
    C.startVoiceRecognition()
case 3:
    // 新增：停止录音
    C.stopVoiceRecognition()
    wailsruntime.EventsEmit(globalAppCtx, "voice:end", nil)
}
```

### 权限配置

`wails.json` 的 `info.plist` 字段（或 `build/darwin/Info.plist`）新增：

```xml
<key>NSSpeechRecognitionUsageDescription</key>
<string>Aiko 需要语音识别权限以将您的语音转换为文字。</string>
<key>NSMicrophoneUsageDescription</key>
<string>Aiko 需要麦克风权限以接收语音输入。</string>
```

---

## 3. 前端 UI

### 3.1 Wails 事件一览

| 事件 | 方向 | 含义 |
|------|------|------|
| `voice:start` | backend→frontend | 长按触发，开始录音 |
| `voice:transcript` | backend→frontend | 实时识别文字（partial/final） |
| `voice:end` | backend→frontend | Option 释放，录音结束 |
| `voice:error` | backend→frontend | 权限拒绝或识别失败 |

### 3.2 屏幕边框彩虹光（Siri 风格）

在根组件（`App.vue`）添加全屏覆盖层：

```html
<div class="siri-border" :class="{ active: voiceActive }"></div>
```

**CSS 方案：**
- 四条细线（top/bottom/left/right）用 `linear-gradient` + `background-size` 动画
- 颜色序列：`#3b82f6`（蓝）→ `#8b5cf6`（紫）→ `#ec4899`（粉）→ `#f97316`（橙）→ 循环
- `pointer-events: none`，`z-index: 9999`，不阻断点击穿透
- 动画：`@keyframes siri-flow`，颜色位移 3s linear infinite

### 3.3 居中圆形波形

同层添加：

```html
<div class="voice-wave" v-if="voiceActive">
  <div class="wave-ring" v-for="i in 4" :key="i" :style="{ animationDelay: `${(i-1)*0.3}s` }"></div>
</div>
```

- 4 个同心圆，`@keyframes wave-pulse`：scale 1→2.5，opacity 0.6→0
- 交错 delay，视觉上形成持续向外扩散效果
- 颜色与边框光一致（蓝紫渐变）

### 3.4 ChatPanel 实时文字

在 `ChatPanel.vue` 中：

1. **监听 `voice:start`：**
   - 若气泡隐藏，emit `bubble:toggle` 显示气泡
   - 在消息列表末尾插入临时状态条：`{ role: 'voice-hint', content: '' }`
   - 聚焦 textarea

2. **监听 `voice:transcript`：**
   - 实时更新 `input.value`（textarea 内容）
   - 同步更新状态条 content 为当前识别文字（提供视觉反馈）

3. **监听 `voice:end`：**
   - 移除状态条
   - textarea 保留最终识别文字，用户可确认/编辑后手动发送

4. **监听 `voice:error`：**
   - 移除状态条
   - 显示错误提示（复用现有 `notification:show` 事件）
   - 清空 `input.value`

### 3.5 状态条样式

```
┌─────────────────────────────────┐
│ 🎙️  正在识别... "你好 Aiko"     │
└─────────────────────────────────┘
```

- 背景：半透明蓝紫渐变，圆角
- 麦克风图标 + 动态省略号动画
- 位于消息列表末尾，非正式 message（不存入数据库）

---

## 4. 文件变更清单

| 文件 | 变更内容 |
|------|---------|
| `macos.go` | 扩展 Option 监控：长按检测 + `startVoiceRecognition` / `stopVoiceRecognition` Objective-C 实现 + CGO callback |
| `app.go` | pipe 读取 switch 新增 case 2/3；`globalAppCtx` 已存在，直接复用 |
| `build/darwin/Info.plist` 或 `wails.json` | 新增麦克风 + 语音识别权限描述 |
| `frontend/src/App.vue` | 新增 `siri-border` + `voice-wave` 覆盖层 + `voiceActive` 状态管理 |
| `frontend/src/components/ChatPanel.vue` | 新增 4 个 Wails 事件监听 + 实时文字更新逻辑 + 状态条渲染 |

---

## 5. 不在本阶段范围内

- 释放 Option 后自动发送给 LLM（设置开关，后续阶段实现）
- 语音输出 / TTS
- Windows/Linux 支持
- STT 引擎可配置切换（Whisper API 等）

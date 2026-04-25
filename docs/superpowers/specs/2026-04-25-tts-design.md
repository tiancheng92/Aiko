# TTS 语音输出功能设计文档

**日期：** 2026-04-25
**阶段：** 阶段5 — 语音输出
**状态：** 待实现

---

## 概述

为 Aiko 添加 LLM 回复朗读功能。LLM 回复超过字数阈值时先用 LLM 摘要，再通过 OpenAI TTS 兼容接口（主路径）或 macOS `say` 命令（降级）将文字转为语音播放。支持声线切换，用户可手动点击播放，也可开启自动朗读。

---

## 1. 配置层

### 1.1 ModelProfile 扩展

在 `internal/config/profile.go` 的 `ModelProfile` 结构体新增三个字段，复用同一套 BaseURL/APIKey：

```go
TTSModel  string  `json:"tts_model"`  // TTS 模型名，如 OuteTTS-1.0-1B；空则降级 macOS say
TTSVoice  string  `json:"tts_voice"`  // 声线名，如 tara
TTSSpeed  float64 `json:"tts_speed"`  // 语速，0.5–2.0，默认 1.0
```

数据库 `model_profiles` 表新增三列（migration）：
```sql
ALTER TABLE model_profiles ADD COLUMN tts_model TEXT NOT NULL DEFAULT '';
ALTER TABLE model_profiles ADD COLUMN tts_voice TEXT NOT NULL DEFAULT '';
ALTER TABLE model_profiles ADD COLUMN tts_speed REAL NOT NULL DEFAULT 1.0;
```

`ProfileStore.List/Get/Save` 同步更新 SQL，`Config.ApplyProfile` 把三个字段复制到 `Config`。

### 1.2 Config / settings 表扩展

`Config` 新增：

```go
TTSAutoPlay           bool // 外观与交互：chat:done 后自动朗读
TTSSummarizeThreshold int  // 摘要字数阈值，默认 200；0 表示禁用摘要
// 以下三个从 active profile 复制，不直接存 settings
TTSModel  string
TTSVoice  string
TTSSpeed  float64
```

`settings` 表新增两个 key-value：

| key | 默认值 | 含义 |
|-----|--------|------|
| `tts_auto_play` | `false` | 自动朗读回复 |
| `tts_summarize_threshold` | `200` | 摘要字数阈值 |

---

## 2. TTS 服务层

### 2.1 包结构

新建 `internal/tts/` 包：

```
internal/tts/
├── tts.go          # Speaker 接口定义
├── openai.go       # OpenAISpeaker 实现
└── system.go       # SystemSpeaker（macOS say）降级实现
```

### 2.2 接口

```go
// Speaker 是 TTS 的统一抽象。
type Speaker interface {
    // Speak 接受文本，返回 WAV 音频字节。
    Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error)
    // Voices 返回可用声线列表。
    Voices(ctx context.Context) ([]string, error)
}

// New 根据配置返回合适的 Speaker。TTSModel 为空时返回 SystemSpeaker。
func New(baseURL, apiKey, model string) Speaker
```

### 2.3 OpenAISpeaker

调用 `POST {BaseURL}/v1/audio/speech`：

```json
{
  "model": "OuteTTS-1.0-1B",
  "input": "...",
  "voice": "tara",
  "speed": 1.0,
  "response_format": "wav"
}
```

返回音频字节。`Voices()` 调用 `GET {BaseURL}/v1/audio/voices`（若端点不存在则返回空列表，前端降为手动输入）。

### 2.4 SystemSpeaker（降级）

`Speak()`：调 `osascript -e 'say "{text}" using "{voice}"'`，录制为临时 AIFF 文件后读取字节返回（或直接播放后返回空字节让前端跳过 Web Audio）。

简化：SystemSpeaker 直接在后端播放，不传音频字节给前端；前端收到 `tts:done` 即可。

`Voices()`：解析 `say -v ?` 输出，提取中文和英文语音列表。

---

## 3. app.go 层

### 3.1 字段

```go
type App struct {
    // ...现有字段...
    ttsSpeaker tts.Speaker  // 当前 Speaker，initLLMComponents 时初始化
    ttsCancel  context.CancelFunc // 用于中止当前朗读
}
```

`initLLMComponents` 末尾：
```go
a.ttsSpeaker = tts.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.TTSModel)
```

### 3.2 Wails 绑定方法

```go
// SpeakText 朗读文本。字数超过阈值时先摘要。
func (a *App) SpeakText(text string) error

// StopTTS 中止当前朗读。
func (a *App) StopTTS()

// GetTTSVoices 返回当前 TTS 后端支持的声线列表。
func (a *App) GetTTSVoices() ([]string, error)
```

### 3.3 SpeakText 流程

```
1. 若 ttsCancel != nil，先取消上一次
2. 新建 ctx + cancel，存入 a.ttsCancel
3. emit tts:start
4. 若 len([]rune(text)) > TTSSummarizeThreshold && threshold > 0：
      调 LLM（a.petAgent 或直接 HTTP）做摘要
      prompt: "请用简洁的中文口语总结以下内容，控制在100字以内，适合朗读：\n{text}"
5. a.ttsSpeaker.Speak(ctx, finalText, cfg.TTSVoice, cfg.TTSSpeed)
6. 若 Speaker 为 OpenAISpeaker：
      emit tts:audio { data: base64(wav), format: "wav" }
   若 Speaker 为 SystemSpeaker：
      后端直接播放，不 emit tts:audio
7. emit tts:done（出错则 emit tts:error）
```

### 3.4 Wails 事件一览

| 事件 | 方向 | 数据 | 含义 |
|------|------|------|------|
| `tts:start` | backend→frontend | — | 开始 TTS 处理 |
| `tts:audio` | backend→frontend | `{data: string, format: "wav"}` | 音频 base64，前端播放 |
| `tts:done` | backend→frontend | — | 播放结束或被停止 |
| `tts:error` | backend→frontend | `string` | 错误信息 |

---

## 4. 前端

### 4.1 ChatPanel.vue

**每条 assistant 消息**右下角（markdown 工具栏旁）新增喇叭按钮：

- 默认：🔊 图标，点击调 `SpeakText(message.content)`
- 朗读中：⏹ 图标，点击调 `StopTTS()`
- 用 `activeTTSMsgId` ref 跟踪当前朗读的消息 id

**自动朗读**：`chat:done` 回调末尾，若 `ttsAutoPlay` 为 true 且非语音输入触发（避免双重播放）：
```js
SpeakText(fullText)
```

**音频播放**（`tts:audio` 事件）：
```js
const bytes = Uint8Array.from(atob(data), c => c.charCodeAt(0))
const blob  = new Blob([bytes], { type: 'audio/wav' })
const url   = URL.createObjectURL(blob)
const audio = new Audio(url)
audio.play()
audio.onended = () => URL.revokeObjectURL(url)
```

**事件监听**：`tts:start`、`tts:done`、`tts:error` 管理 `activeTTSMsgId` 状态。

### 4.2 SettingsWindow.vue

**ModelProfile 编辑表单**（Embedding Model 之后追加）：

```
TTS Model    [文本输入 placeholder="OuteTTS-1.0-1B，留空则用系统 say"]
TTS Voice    [下拉，选项来自 GetTTSVoices()；TTS Model 非空时加载]
TTS Speed    [滑块 0.5–2.0，步长 0.1，默认 1.0]
```

**外观与交互 tab** 新增开关：
```
自动朗读回复   [toggle，对应 TTSAutoPlay]
```

---

## 5. 文件变更清单

| 文件 | 变更内容 |
|------|---------|
| `internal/config/profile.go` | `ModelProfile` 新增 `TTSModel/Voice/Speed`；SQL CRUD 更新 |
| `internal/config/config.go` | `Config` 新增 `TTSAutoPlay/SummarizeThreshold/TTSModel/Voice/Speed`；`Load/Save/ApplyProfile` 更新 |
| `internal/db/migrations.go` | `model_profiles` 表 ALTER 三列；`settings` 新增两个默认 key |
| `internal/tts/tts.go` | `Speaker` 接口 + `New` 工厂函数 |
| `internal/tts/openai.go` | `OpenAISpeaker` 实现 |
| `internal/tts/system.go` | `SystemSpeaker`（macOS say）降级实现 |
| `app.go` | `ttsSpeaker/ttsCancel` 字段；`SpeakText/StopTTS/GetTTSVoices` 绑定；`initLLMComponents` 初始化 Speaker |
| `frontend/src/components/ChatPanel.vue` | 喇叭按钮；自动朗读；tts 事件监听；Web Audio 播放 |
| `frontend/src/components/SettingsWindow.vue` | ModelProfile 表单新增 TTS 三字段；外观与交互新增自动朗读开关 |

---

## 6. 不在本阶段范围内

- 流式 TTS（PCM 分块推送）
- 语音唤醒
- Windows/Linux 降级实现
- TTS 速度/音调实时调节（非配置项级别）

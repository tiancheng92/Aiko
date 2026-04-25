# 剪贴板、截图、应用控制、图片粘贴功能设计

**日期：** 2026-04-25

---

## 目标

新增四个功能，共享「图片传给 AI」通道：

1. **剪贴板读写工具** — AI 可读取/写入剪贴板
2. **截图工具** — AI 可截全屏并直接「看到」图片内容
3. **应用控制工具** — AI 可查询运行中应用、打开/激活/退出应用
4. **聊天框图片粘贴** — 用户可在输入框粘贴图片，随文字一起发给 AI

---

## 架构概览

```
工具层（Go）                 全栈层
─────────────────────────    ───────────────────────────────────
clipboard_tools.go           ChatPanel.vue（paste 监听 + 预览）
clipboard_darwin.go               ↓ SendMessageWithImages()
screenshot_tools.go          app.go（解析 data URL → Message）
screenshot_darwin.go              ↓ Agent.ChatWithMessage()
app_control_tools.go         agent.go（构造 UserInputMultiContent）
app_control_darwin.go
```

截图工具和图片粘贴共享同一条消息通道：两者最终都通过 `UserInputMultiContent` 把图片 base64 传给模型。

---

## 新增文件

```
internal/tools/
  clipboard_tools.go       # read_clipboard / write_clipboard 工具定义
  clipboard_darwin.go      # pbpaste / pbcopy 实现
  clipboard_other.go       # stub（non-darwin）
  screenshot_tools.go      # take_screenshot 工具定义
  screenshot_darwin.go     # screencapture 实现
  screenshot_other.go      # stub
  app_control_tools.go     # list_running_apps / control_app 工具定义
  app_control_darwin.go    # osascript 实现
  app_control_other.go     # stub
```

修改文件：
- `internal/tools/registry.go` — 注册 5 个新工具
- `internal/agent/agent.go` — 新增 `ChatWithMessage()`
- `app.go` — 新增 `SendMessageWithImages()`
- `frontend/src/components/ChatPanel.vue` — paste 监听 + 图片预览 UI

---

## 各功能详述

### 1. 剪贴板工具

**文件：** `clipboard_tools.go` + `clipboard_darwin.go` + `clipboard_other.go`

两个工具：

#### `read_clipboard`
- **权限：** Protected
- **参数：** 无
- **实现（darwin）：** `exec.Command("pbpaste").Output()`
- **返回：** 剪贴板文本内容；若为空返回「剪贴板为空」
- **stub：** 返回「仅支持 macOS」

#### `write_clipboard`
- **权限：** Protected
- **参数：** `text string`（必填）
- **实现（darwin）：** `cmd := exec.Command("pbcopy"); cmd.Stdin = strings.NewReader(text); cmd.Run()`
- **返回：** 「已写入剪贴板」
- **stub：** 返回「仅支持 macOS」

---

### 2. 截图工具

**文件：** `screenshot_tools.go` + `screenshot_darwin.go` + `screenshot_other.go`

#### `take_screenshot`
- **权限：** Protected
- **参数：** 无（始终截全屏）
- **实现（darwin）：**
  1. 生成临时路径：`/tmp/aiko_shot_<unix_ns>.png`
  2. `exec.Command("screencapture", "-x", path).Run()`（`-x` 静默，不播放快门音）
  3. 读取文件 → base64 编码
  4. 删除临时文件（`os.Remove`）
  5. 返回 `ToolResult`，`Parts` 含一个 `ToolOutputImage`：
     ```go
     schema.ToolOutputPart{
         Type: schema.ToolPartTypeImage,
         Image: &schema.ToolOutputImage{
             MessagePartCommon: schema.MessagePartCommon{
                 Base64Data: &b64str,
                 MIMEType:   "image/png",
             },
         },
     }
     ```
- **stub：** 返回「仅支持 macOS」
- **说明：** eino `ToolResult.ToMessageInputParts()` 自动把 `ToolOutputImage` 转为 `MessageInputPart`，模型直接在 tool call 结果中看到截图内容，无需前端参与。

---

### 3. 应用控制工具

**文件：** `app_control_tools.go` + `app_control_darwin.go` + `app_control_other.go`

两个工具：

#### `list_running_apps`
- **权限：** Public
- **参数：** 无
- **实现（darwin）：** osascript：
  ```applescript
  tell application "System Events"
    get name of every process whose background only is false
  end tell
  ```
- **返回：** 逗号分隔的应用名列表

#### `control_app`
- **权限：** Protected
- **参数：**
  - `action` string 必填，枚举：`open`（打开并激活）、`activate`（仅前台激活）、`quit`（退出）
  - `app_name` string 必填，应用名称（如 "Safari"、"Spotify"）
- **实现（darwin）：**
  - `open`/`activate`：`tell application "<app_name>" to activate`
  - `quit`：`tell application "<app_name>" to quit`
- **返回：** 操作结果描述
- **stub：** 返回「仅支持 macOS」

---

### 4. 聊天框图片粘贴

#### 前端（ChatPanel.vue）

**paste 事件处理：**
```js
function onPaste(e) {
  const items = [...(e.clipboardData?.items ?? [])]
  const imageItem = items.find(i => i.type.startsWith('image/'))
  if (!imageItem) return
  e.preventDefault()
  const blob = imageItem.getAsFile()
  const reader = new FileReader()
  reader.onload = (ev) => {
    pendingImages.value.push(ev.target.result) // data URL: "data:image/png;base64,..."
  }
  reader.readAsDataURL(blob)
}
```

**状态：**
- `pendingImages = ref([])` — 待发送的图片 data URL 数组

**UI：**
- 输入框上方显示图片缩略图预览（`<img>` 标签），右上角 `×` 按钮可移除单张
- 有待发图片时，发送按钮视觉上有图片角标提示

**发送逻辑：**
```js
async function send() {
  if (pendingImages.value.length > 0) {
    await SendMessageWithImages(inputText.value, pendingImages.value)
  } else {
    await SendMessage(inputText.value)
  }
  pendingImages.value = []
}
```

**textarea 绑定：** 在 `<textarea>` 上添加 `@paste="onPaste"`

---

#### 后端（app.go）

新增 Wails 绑定方法：

```go
// SendMessageWithImages streams an AI response for a user message that may
// include one or more inline images (data URLs: "data:image/png;base64,...").
func (a *App) SendMessageWithImages(userInput string, images []string) error
```

实现：
1. 解析每个 data URL，提取 MIME type 和 base64 字符串
2. 构造 `[]*schema.MessageInputPart`：先一个 text part，再若干 image parts
3. 构造 `*schema.Message{Role: schema.User, UserInputMultiContent: parts}`
4. 调用 `ag.ChatWithMessage(ctx, msg)` 替代 `ag.Chat(ctx, text)`
5. 其余 token 流式转发逻辑与 `SendMessage` 完全相同（复用 `drainRunnerMsg` helper）

---

#### 后端（agent.go）

新增方法：

```go
// ChatWithMessage sends a pre-built user Message to the agent, supporting
// multimodal content (text + images). Streams tokens via the returned channel.
func (a *Agent) ChatWithMessage(ctx context.Context, msg *schema.Message) <-chan StreamResult
```

实现与 `Chat()` 基本一致，区别：
- 不再用 `query string` 调用 `runner.Query`，改为直接调用 `runner.Run(ctx, []adk.Message{msg})`（eino Runner.Run 接受 []Message，Query 是其快捷方式）
- 短期记忆存储时，将 `UserInputMultiContent` 中图片 part 替换为 `[图片]` 占位文本，防止 base64 膨胀历史记录

---

## 工具注册（registry.go）

新增到 `All()`：
```go
&ReadClipboardTool{},
&WriteClipboardTool{},
&TakeScreenshotTool{},
&ListRunningAppsTool{},
```

新增到 `AllContextual()` 前的独立列表或直接加入 `All()`：
```go
&ControlAppTool{},
```

共 5 个工具，均注册在 `All()` 中（无运行时依赖）。

---

## 权限汇总

| 工具 | 级别 |
|------|------|
| `read_clipboard` | Protected |
| `write_clipboard` | Protected |
| `take_screenshot` | Protected |
| `list_running_apps` | Public |
| `control_app` | Protected |

---

## 不变的部分

- Go 后端所有现有工具和 Wails 绑定方法
- eino Agent 的 ReAct 逻辑、中间件、记忆迁移流程
- 现有 `SendMessage()` 路径（纯文本消息继续走原路径）
- CSS 变量和毛玻璃主题风格

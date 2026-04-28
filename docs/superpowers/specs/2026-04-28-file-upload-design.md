# 文件上传功能设计

**日期**: 2026-04-28  
**状态**: 待实现

## 背景

用户希望在聊天时附加文本文件（代码、日志、文档等）给 AI 处理。要求：
- 文件内容发送给 LLM，但记忆存储只保存文件名
- 前端显示文件名+图标
- 超过 200KB 或不可读文件（非文本类型）给出错误提示

## 约束

- eino 的 openai ACL 不支持原生 `file_url` 消息部件，文件内容必须以文本形式注入
- 现有 `images` 列存图片 data URL，新增独立 `files` 列存文件名，语义清晰
- DB 迁移通过 `patches` 数组的幂等 `ALTER TABLE ADD COLUMN` 模式完成

## 架构

```
前端 ChatPanel.vue
  pendingFiles ref([{name, mimeType, content}])
  文件选择按钮 → 读取文件 → 验证 → 加入 pendingFiles
  send() → SendMessageWithFiles(text, images, files)
        ↓
app.go  SendMessageWithFiles
  构建 schema.Message：文本 + 文件内容拼接 + 图片部件
  调用 ag.ChatWithMessage(ctx, msg)
        ↓
agent.go  ChatWithMessage / persistAndMigrate
  persistAndMigrate(userText, imageURLs, fileNames)  // fileNames 仅文件名
  shortMem.AddWithImagesAndFiles(role, content, images, files)
        ↓
memory/short.go  Message.Files + DB messages.files 列
```

## 数据结构

### Go 端（app.go）

```go
// FileAttachment 是前端传入的文件附件信息。
type FileAttachment struct {
    Name     string `json:"name"`
    MimeType string `json:"mimeType"`
    Content  string `json:"content"`
}
```

### memory.Message（internal/memory/short.go）

```go
type Message struct {
    ID        int64
    Role      string
    Content   string
    Images    []string // data URLs
    Files     []string // 文件名列表（不含内容）
    CreatedAt string
}
```

### DB 迁移（internal/db/sqlite.go）

在 `patches` 数组新增：
```go
`ALTER TABLE messages ADD COLUMN files TEXT NOT NULL DEFAULT ''`
```

## 各层实现细节

### 1. `internal/memory/short.go`

- `scanMessage`：新增扫描 `files` JSON 列
- `AddWithImagesAndFiles(role, content string, images []string, files []string) (int64, error)`：替换 `AddWithImages`（或新增，保留旧方法做兼容）
- `AddWithImages` 保持不变（调用 `AddWithImagesAndFiles` with `files=nil`）

### 2. `internal/db/sqlite.go`

`patches` 新增：
```go
`ALTER TABLE messages ADD COLUMN files TEXT NOT NULL DEFAULT ''`
```

### 3. `internal/agent/agent.go`

- `persistAndMigrate` 签名扩展：新增 `userFiles []string` 参数
- 调用 `shortMem.AddWithImagesAndFiles("user", userInput, userImages, userFiles)`
- `ChatWithMessage` 调用处新增提取文件名参数（通过 `msg` 的 `Extra` 字段传递）

### 4. `app.go`

新增方法：

```go
func (a *App) SendMessageWithFiles(userInput string, images []string, files []FileAttachment) error
```

文件内容拼接规则（追加到 `userInput` 文本末尾）：
```
\n\n[文件: report.txt (text/plain)]\n```\n<内容>\n```
```

多个文件依次追加。图片部件处理复用现有 `parseDataURL` 逻辑。

文件名列表提取后通过 `msg.Extra["_file_names"]` 传递给 agent，由 agent 在 persistAndMigrate 中读取。

### 5. 前端 `ChatPanel.vue`

**新增 ref**：
```js
const pendingFiles = ref([])  // [{name, mimeType, content}]
```

**可读 MIME 白名单**：
```
text/*
application/json
application/xml
application/javascript
application/typescript
application/x-sh
application/x-python
```

**验证逻辑**（`addFile(file)` 函数）：
- `file.size > 200 * 1024` → toast 提示"文件过大（最大 200KB）"，不加入
- MIME 不在白名单 → toast 提示"不支持此文件类型，仅支持文本文件"，不加入
- 通过校验 → `FileReader.readAsText` 读取内容，加入 `pendingFiles`

**UI 变更**：
- textarea 旁增加文件选择按钮（📎 图标，`<input type="file" multiple hidden>` 触发）
- pending 区域：现有图片卡片 + 新增文件卡片（文件图标 + 文件名 + × 按钮）
- 消息气泡：`m.files` 非空时在消息内容上方显示文件卡片（只读）

**send() 扩展**：
```js
const fileAttachments = pendingFiles.value.map(f => ({
    name: f.name, mimeType: f.mimeType, content: f.content
}))
pendingFiles.value = []
messages.value.push({ role: 'user', content: text, images: imgs, files: fileNames, time: new Date() })

if (imgs.length > 0 || fileAttachments.length > 0) {
    await SendMessageWithFiles(text, imgs, fileAttachments)
} else {
    await SendMessage(text)
}
```

**历史消息加载**：`GetMessages` 返回的 `m.Files` 映射到消息对象的 `files` 字段。

## 错误提示样式

使用现有的系统消息气泡样式（`role: 'system'`），与图片验证错误一致。

## 注意事项

- `msg.Extra["_file_names"]` 仅用于从 app.go 向 agent 传递文件名，不进入 LLM 上下文
- 文件内容拼接在发给 LLM 的 `sendMsg` 中，`app.go` 将原始 `userInput`（不含文件内容）和 `fileNames` 通过 `msg.Extra` 传入 agent；`persistAndMigrate` 存储原始 `userText`，保持记忆干净
- agent 的 `extractTextFromMessage` 读取的是含文件内容的拼接文本（已在 `sendMsg` 中），因此 `userMemory` 应改为从 `msg.Extra["_user_text"]` 读取原始文本，而非 `extractTextFromMessage(msg)`
- 前端 wailsjs 绑定在修改 Go 方法签名后需重新运行 `wails generate module`

## 测试要点

1. 选择 `.txt` / `.go` / `.json` 文件 → 正常加入 pending，发送后 LLM 收到内容
2. 选择 > 200KB 文件 → toast 错误，不加入 pending
3. 选择 `.png` / `.exe` 文件 → toast 错误，不加入 pending
4. 发送后刷新历史 → 消息气泡显示文件名图标，不显示文件内容
5. 同时附加图片+文件 → 两者均正常发送
6. 不附加文件 → 走原有 `SendMessage` / `SendMessageWithImages` 路径不受影响

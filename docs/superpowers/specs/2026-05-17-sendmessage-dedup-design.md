# 子项目 2：消除 SendMessage 系列方法中的重复 goroutine 逻辑

**日期：** 2026-05-17  
**范围：** `app.go`  
**目标：** 将 `SendMessage`、`SendMessageWithImages`、`SendMessageWithFiles` 三个方法中逐字相同的 goroutine 体提取为私有辅助方法，消除约 130 行重复代码。

---

## 背景

`app.go` 中三个公开 Wails 绑定方法共享完全相同的 goroutine 结构：

1. 取消上一个进行中的请求（持锁写 `chatCancel`、`chatGeneration`）
2. 读取 `a.petAgent`
3. 校验 agent 是否为 nil
4. 启动 goroutine，在其中：
   - `defer cancel()` / `defer` 清理 `chatCancel`
   - 调用 agent 获得 `<-chan StreamResult` channel
   - 用 `agent.NewEmotionParser()` 处理流
   - 将每种结果转为 `chat:token` / `chat:done` / `chat:error` / `chat:thinking` / `chat:image` / `chat:emotion` Wails 事件

三个方法唯一不同的是第 4 步中获取 channel 的方式：
- `SendMessage`：`ag.Chat(chatCtx, userInput)`
- `SendMessageWithImages`：`ag.ChatWithMessage(chatCtx, msg)`（msg 含图片 parts）
- `SendMessageWithFiles`：`ag.ChatWithMessage(chatCtx, msg)`（msg 含文件内容 + 图片 parts）

---

## 设计方案

### 提取私有辅助方法 `streamChat`

新增一个私有方法，负责：
1. 取消旧请求、设置新 `chatCancel`、递增 `chatGeneration`
2. 读取并校验 `a.petAgent`
3. 接受一个「如何获取 channel」的函数参数（`fetchCh func(*agent.Agent, context.Context) <-chan agent.StreamResult`）
4. 启动 goroutine 并驱动整个流

```go
// streamChat cancels any in-flight request, acquires the agent, and launches
// a goroutine that drains ch from fetchCh and emits Wails events.
func (a *App) streamChat(fetchCh func(*agent.Agent, context.Context) <-chan agent.StreamResult) error {
    a.mu.Lock()
    if a.chatCancel != nil {
        a.chatCancel()
        a.chatCancel = nil
    }
    chatCtx, cancel := context.WithCancel(a.ctx)
    a.chatCancel = cancel
    a.chatGeneration++
    myGen := a.chatGeneration
    a.mu.Unlock()

    a.mu.RLock()
    ag := a.petAgent
    a.mu.RUnlock()

    if ag == nil {
        a.mu.Lock()
        a.chatCancel = nil
        a.mu.Unlock()
        cancel()
        return fmt.Errorf("agent not initialized: complete settings first")
    }

    go func() {
        defer cancel()
        defer func() {
            a.mu.Lock()
            if a.chatGeneration == myGen {
                a.chatCancel = nil
            }
            a.mu.Unlock()
        }()
        ch := fetchCh(ag, chatCtx)
        ep := agent.NewEmotionParser()
        for result := range ch {
            if result.Err != nil {
                if errors.Is(result.Err, context.Canceled) {
                    return
                }
                wailsruntime.EventsEmit(a.ctx, "chat:error", a.formatChatError(result.Err))
                return
            }
            if result.Done {
                if tail := ep.Flush(); tail != "" {
                    wailsruntime.EventsEmit(a.ctx, "chat:token", tail)
                }
                wailsruntime.EventsEmit(a.ctx, "chat:done", "")
                return
            }
            if result.ThinkingToken != "" {
                wailsruntime.EventsEmit(a.ctx, "chat:thinking", result.ThinkingToken)
                continue
            }
            if len(result.Images) > 0 {
                wailsruntime.EventsEmit(a.ctx, "chat:image", result.Images)
            }
            text, emotion, intensity := ep.Feed(result.Token)
            if emotion != "" {
                wailsruntime.EventsEmit(a.ctx, "chat:emotion", map[string]any{
                    "emotion":   emotion,
                    "intensity": intensity,
                })
            }
            if text != "" {
                wailsruntime.EventsEmit(a.ctx, "chat:token", text)
            }
        }
        if tail := ep.Flush(); tail != "" {
            wailsruntime.EventsEmit(a.ctx, "chat:token", tail)
        }
        wailsruntime.EventsEmit(a.ctx, "chat:done", "")
    }()
    return nil
}
```

### 简化后的三个公开方法

**`SendMessage`**：

```go
func (a *App) SendMessage(userInput string) error {
    return a.streamChat(func(ag *agent.Agent, ctx context.Context) <-chan agent.StreamResult {
        return ag.Chat(ctx, userInput)
    })
}
```

**`SendMessageWithImages`**：

```go
func (a *App) SendMessageWithImages(userInput string, images []string) error {
    // Build UserInputMultiContent: text part first, then image parts.
    parts := make([]schema.MessageInputPart, 0, 1+len(images))
    if userInput != "" {
        parts = append(parts, schema.MessageInputPart{
            Type: schema.ChatMessagePartTypeText,
            Text: userInput,
        })
    }
    for _, dataURL := range images {
        mimeType, b64data, ok := parseDataURL(dataURL)
        if !ok {
            slog.Warn("SendMessageWithImages: invalid data URL, skipping")
            continue
        }
        parts = append(parts, schema.MessageInputPart{
            Type: schema.ChatMessagePartTypeImageURL,
            Image: &schema.MessageInputImage{
                MessagePartCommon: schema.MessagePartCommon{
                    Base64Data: &b64data,
                    MIMEType:   mimeType,
                },
            },
        })
    }
    msg := &schema.Message{
        Role:                  schema.User,
        UserInputMultiContent: parts,
    }
    return a.streamChat(func(ag *agent.Agent, ctx context.Context) <-chan agent.StreamResult {
        return ag.ChatWithMessage(ctx, msg)
    })
}
```

**`SendMessageWithFiles`**：

```go
func (a *App) SendMessageWithFiles(userInput string, images []string, files []FileAttachment) error {
    // Build LLM text: original input + file contents appended.
    var llmBuilder strings.Builder
    llmBuilder.WriteString(userInput)
    fileNames := make([]string, 0, len(files))
    for _, f := range files {
        fileNames = append(fileNames, f.Name)
        fmt.Fprintf(&llmBuilder, "\n\n[文件: %s (%s)]\n```\n%s\n```", f.Name, f.MimeType, f.Content)
    }
    llmText := llmBuilder.String()

    parts := make([]schema.MessageInputPart, 0, 1+len(images))
    parts = append(parts, schema.MessageInputPart{
        Type: schema.ChatMessagePartTypeText,
        Text: llmText,
    })
    for _, dataURL := range images {
        mimeType, b64data, ok := parseDataURL(dataURL)
        if !ok {
            slog.Warn("SendMessageWithFiles: invalid data URL, skipping")
            continue
        }
        parts = append(parts, schema.MessageInputPart{
            Type: schema.ChatMessagePartTypeImageURL,
            Image: &schema.MessageInputImage{
                MessagePartCommon: schema.MessagePartCommon{
                    Base64Data: &b64data,
                    MIMEType:   mimeType,
                },
            },
        })
    }
    msg := &schema.Message{
        Role:                  schema.User,
        UserInputMultiContent: parts,
        Extra: map[string]any{
            "_user_text":  userInput,
            "_file_names": fileNames,
        },
    }
    return a.streamChat(func(ag *agent.Agent, ctx context.Context) <-chan agent.StreamResult {
        return ag.ChatWithMessage(ctx, msg)
    })
}
```

---

## 约束

- `streamChat` 是私有方法（小写），不暴露给 Wails — 仅被三个公开方法调用
- 三个公开方法签名不变 — Wails 前端绑定无需重新生成
- `RegenerateLastReply` 调用 `a.SendMessage(...)` — 无需改动
- 不涉及任何业务逻辑变更，只是等价结构提取
- 消除的重复代码：约 130 行（三份 ~44 行相同的 goroutine 体）
- 新增代码：`streamChat` 约 48 行，三个方法体合计减少约 80 行净减少

---

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `app.go` | 修改 | 新增 `streamChat` 私有方法；简化 `SendMessage`、`SendMessageWithImages`、`SendMessageWithFiles` |

## 测试策略

- 无新增测试（纯等价重构，行为不变）
- 构建验证：`go build ./...` 和 `go build -race ./...`
- 人工行为验证：
  - 普通文本消息正常发送并流式显示
  - 粘贴图片后能正常发送多模态消息
  - 附件文件正常内联到消息
  - 多次快速发送时旧请求能被正确取消（generation counter 起作用）

# SendMessage Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the identical ~44-line goroutine body shared by `SendMessage`, `SendMessageWithImages`, and `SendMessageWithFiles` into a private `streamChat` helper, eliminating ~130 lines of duplication.

**Architecture:** One new private method `streamChat(fetchCh func(*agent.Agent, context.Context) <-chan agent.StreamResult) error` in `app.go`. The three public Wails-bound methods retain their signatures and message-building logic; they delegate goroutine management and stream driving to `streamChat`. No behavior changes.

**Tech Stack:** Go, Wails v2 (`github.com/wailsapp/wails/v2`), `internal/agent` package.

---

## File Map

| File | Changes |
|------|---------|
| `app.go` | Add `streamChat` private method (~48 lines); replace goroutine bodies in 3 public methods (~130 lines → ~15 lines combined) |

---

### Task 1: Add `streamChat` and refactor the three `SendMessage` methods

**Files:**
- Modify: `app.go` — `SendMessage` (line 1230), `SendMessageWithImages` (line 1335), `SendMessageWithFiles` (line 1446)

This is a single atomic task because all three methods and the new helper are tightly coupled — split commits would leave the file in an inconsistent state.

- [ ] **Step 1: Add the `streamChat` helper immediately before `SendMessage`**

  Open `app.go`. Find `SendMessage` at line 1230. Insert the new method immediately before it (before the blank line and doc comment, i.e. before line ~1228).

  ```go
  // streamChat cancels any in-flight request, acquires the agent, and launches
  // a goroutine that drains the channel returned by fetchCh and emits Wails events.
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

- [ ] **Step 2: Replace `SendMessage` body**

  Find the current `SendMessage` implementation (lines 1230–1307). Replace the entire function body with:

  ```go
  // SendMessage streams an AI response for a plain-text user message.
  func (a *App) SendMessage(userInput string) error {
  	return a.streamChat(func(ag *agent.Agent, ctx context.Context) <-chan agent.StreamResult {
  		return ag.Chat(ctx, userInput)
  	})
  }
  ```

- [ ] **Step 3: Replace `SendMessageWithImages` body**

  Find the current `SendMessageWithImages` implementation (now shifted due to Step 2). Replace the function body, keeping the message-building part but delegating to `streamChat`:

  ```go
  // SendMessageWithImages streams an AI response for a user message that includes
  // one or more inline images encoded as data URLs ("data:image/png;base64,...").
  // Falls back to a plain text message if no valid images are provided.
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

- [ ] **Step 4: Replace `SendMessageWithFiles` body**

  Find the current `SendMessageWithFiles` implementation. Replace the function body, keeping the message-building part:

  ```go
  // SendMessageWithFiles streams an AI response for a user message that may include
  // inline images (data URLs) and/or text file attachments.
  // File contents are appended to the user text before sending to the LLM.
  // Only file names are persisted in memory — not the content.
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

  	// Build UserInputMultiContent: text part first, then image parts.
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

- [ ] **Step 5: Verify build**

  ```bash
  go build ./...
  ```

  Expected: no errors.

- [ ] **Step 6: Verify race-free build**

  ```bash
  go build -race ./...
  ```

  Expected: exits 0, no data race warnings.

- [ ] **Step 7: Commit**

  ```bash
  git add app.go
  git commit -m "refactor: extract streamChat helper, eliminate SendMessage goroutine duplication"
  ```

---

### Task 2: Final verification

**Files:** none modified

- [ ] **Step 1: Verify no goroutine boilerplate remains in the three public methods**

  ```bash
  grep -n "chatCancel\|chatGeneration\|NewEmotionParser\|chat:token\|chat:done\|chat:error" app.go | grep -v "^app.go:1[1-3][0-9][0-9]:"
  ```

  This checks that lines referencing these identifiers outside the `streamChat` region are only the `StopGeneration` method and `chatGeneration`/`chatCancel` field declarations — not duplicate goroutine logic in the three public methods.

  Alternatively, count the occurrences of `NewEmotionParser`:

  ```bash
  grep -c "NewEmotionParser" app.go
  ```

  Expected: `1` (only in `streamChat`).

- [ ] **Step 2: Verify `RegenerateLastReply` still compiles and calls `SendMessage` unchanged**

  ```bash
  grep -n "RegenerateLastReply\|SendMessage" app.go
  ```

  Expected: `RegenerateLastReply` at its original location, still calling `a.SendMessage(userMsg.Content)`.

- [ ] **Step 3: Verify public method signatures unchanged**

  ```bash
  grep -n "^func (a \*App) Send" app.go
  ```

  Expected: same three method signatures as before.

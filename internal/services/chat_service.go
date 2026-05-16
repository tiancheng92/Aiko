//go:build darwin

package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	openai "github.com/meguminnnnnnnnn/go-openai"
	"github.com/cloudwego/eino/schema"
	"github.com/wailsapp/wails/v3/pkg/application"

	"aiko/internal/agent"
	"aiko/internal/llm"
	"aiko/internal/memory"
)

// FileAttachment carries a single text-file attachment from the frontend.
// Content is the full UTF-8 text of the file; Name and MimeType are metadata only.
type FileAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Content  string `json:"content"`
}

// ChatService handles AI conversation, streaming, and message history.
type ChatService struct{ s *sharedState }

// NewChatService creates a ChatService backed by the given shared state.
func NewChatService(s *sharedState) *ChatService { return &ChatService{s: s} }

// ServiceStartup implements the Wails v3 service lifecycle.
func (c *ChatService) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	return c.s.startup(ctx, opts)
}

// ServiceShutdown implements the Wails v3 service lifecycle.
func (c *ChatService) ServiceShutdown() error {
	return c.s.shutdown()
}

// ChatDirect streams a proactive AI response for the given prompt without saving to memory.
func (c *ChatService) ChatDirect(ctx context.Context, prompt string) error {
	c.s.mu.RLock()
	ag := c.s.petAgent
	c.s.mu.RUnlock()
	if ag == nil {
		return fmt.Errorf("agent not initialized")
	}
	ch := ag.ChatDirect(ctx, prompt)
	for r := range ch {
		if r.Err != nil {
			c.s.emitDirect("chat:error", c.formatChatError(r.Err))
			c.s.emitDirect("chat:done", nil)
			return r.Err
		}
		if len(r.Images) > 0 {
			c.s.emitDirect("chat:image", r.Images)
		}
		if r.Done {
			break
		}
		c.s.emitDirect("chat:token", r.Token)
	}
	c.s.emitDirect("chat:done", nil)
	return nil
}

// ChatDirectCollect runs a proactive AI generation and collects the full response text.
func (c *ChatService) ChatDirectCollect(ctx context.Context, prompt string) (string, error) {
	c.s.mu.RLock()
	ag := c.s.petAgent
	c.s.mu.RUnlock()
	if ag == nil {
		return "", fmt.Errorf("agent not initialized")
	}
	return ag.ChatDirectCollect(ctx, prompt)
}

// SendMessage sends a user message and streams response tokens as Wails events.
// Events emitted: "chat:token" (string), "chat:done" (""), "chat:error" (string).
// Any in-flight request is cancelled before starting the new one.
func (c *ChatService) SendMessage(userInput string) error {
	// Cancel any previous in-flight request.
	c.s.mu.Lock()
	if c.s.chatCancel != nil {
		c.s.chatCancel()
		c.s.chatCancel = nil
	}
	chatCtx, cancel := context.WithCancel(c.s.ctx)
	c.s.chatCancel = cancel
	c.s.chatGeneration++
	myGen := c.s.chatGeneration
	c.s.mu.Unlock()

	c.s.mu.RLock()
	ag := c.s.petAgent
	c.s.mu.RUnlock()

	if ag == nil {
		c.s.mu.Lock()
		c.s.chatCancel = nil
		c.s.mu.Unlock()
		cancel()
		slog.Error("SendMessage: petAgent is nil", "input", userInput)
		return fmt.Errorf("agent not initialized: complete settings first")
	}
	go func() {
		defer cancel() // ensure context is always released
		defer func() {
			c.s.mu.Lock()
			if c.s.chatGeneration == myGen {
				c.s.chatCancel = nil
			}
			c.s.mu.Unlock()
		}()
		ch := ag.Chat(chatCtx, userInput)
		ep := agent.NewEmotionParser()
		for result := range ch {
			if result.Err != nil {
				// Ignore context cancellation — user triggered StopGeneration; frontend handles UI.
				if errors.Is(result.Err, context.Canceled) {
					return
				}
				c.s.emitDirect("chat:error", c.formatChatError(result.Err))
				return
			}
			if result.Done {
				if tail := ep.Flush(); tail != "" {
					c.s.emitDirect("chat:token", tail)
				}
				c.s.emitDirect("chat:done", "")
				return
			}
			if result.ThinkingToken != "" {
				c.s.emitDirect("chat:thinking", result.ThinkingToken)
				continue
			}
			if len(result.Images) > 0 {
				c.s.emitDirect("chat:image", result.Images)
			}
			text, emotion, intensity := ep.Feed(result.Token)
			if emotion != "" {
				c.s.app.Event.Emit("chat:emotion", map[string]any{
					"emotion":   emotion,
					"intensity": intensity,
				})
			}
			if text != "" {
				c.s.emitDirect("chat:token", text)
			}
		}
		// Fallback: ensure frontend unblocks if channel closes without a terminal result.
		if tail := ep.Flush(); tail != "" {
			c.s.emitDirect("chat:token", tail)
		}
		c.s.emitDirect("chat:done", "")
	}()
	return nil
}

// RegenerateLastReply deletes the last assistant message from history and
// re-runs the agent with the last user message. The response is streamed
// through the usual chat:token / chat:done / chat:error events.
func (c *ChatService) RegenerateLastReply() error {
	if c.s.shortMem == nil {
		return fmt.Errorf("memory not initialized")
	}
	// Delete the stale assistant message.
	if _, err := c.s.shortMem.DeleteLastAssistantMessage(); err != nil {
		return fmt.Errorf("delete last assistant: %w", err)
	}
	// Find the last user message to re-send.
	userMsg, err := c.s.shortMem.LastUserMessage()
	if err != nil {
		return fmt.Errorf("find last user message: %w", err)
	}
	// Delete the user message from DB too — SendMessage will re-persist it.
	if err := c.s.shortMem.DeleteByIDs([]int64{userMsg.ID}); err != nil {
		return fmt.Errorf("delete last user message: %w", err)
	}
	return c.SendMessage(userMsg.Content)
}

// SendMessageWithImages streams an AI response for a user message that includes
// one or more inline images encoded as data URLs ("data:image/png;base64,...").
// Falls back to a plain text message if no valid images are provided.
func (c *ChatService) SendMessageWithImages(userInput string, images []string) error {
	// Cancel any previous in-flight request.
	c.s.mu.Lock()
	if c.s.chatCancel != nil {
		c.s.chatCancel()
		c.s.chatCancel = nil
	}
	chatCtx, cancel := context.WithCancel(c.s.ctx)
	c.s.chatCancel = cancel
	c.s.chatGeneration++
	myGen := c.s.chatGeneration
	c.s.mu.Unlock()

	c.s.mu.RLock()
	ag := c.s.petAgent
	c.s.mu.RUnlock()

	if ag == nil {
		c.s.mu.Lock()
		c.s.chatCancel = nil
		c.s.mu.Unlock()
		cancel()
		return fmt.Errorf("agent not initialized: complete settings first")
	}

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

	go func() {
		defer cancel()
		defer func() {
			c.s.mu.Lock()
			if c.s.chatGeneration == myGen {
				c.s.chatCancel = nil
			}
			c.s.mu.Unlock()
		}()
		ch := ag.ChatWithMessage(chatCtx, msg)
		ep := agent.NewEmotionParser()
		for result := range ch {
			if result.Err != nil {
				if errors.Is(result.Err, context.Canceled) {
					return
				}
				c.s.emitDirect("chat:error", c.formatChatError(result.Err))
				return
			}
			if result.Done {
				if tail := ep.Flush(); tail != "" {
					c.s.emitDirect("chat:token", tail)
				}
				c.s.emitDirect("chat:done", "")
				return
			}
			if result.ThinkingToken != "" {
				c.s.emitDirect("chat:thinking", result.ThinkingToken)
				continue
			}
			if len(result.Images) > 0 {
				c.s.emitDirect("chat:image", result.Images)
			}
			text, emotion, intensity := ep.Feed(result.Token)
			if emotion != "" {
				c.s.app.Event.Emit("chat:emotion", map[string]any{
					"emotion":   emotion,
					"intensity": intensity,
				})
			}
			if text != "" {
				c.s.emitDirect("chat:token", text)
			}
		}
		if tail := ep.Flush(); tail != "" {
			c.s.emitDirect("chat:token", tail)
		}
		c.s.emitDirect("chat:done", "")
	}()
	return nil
}

// SendMessageWithFiles streams an AI response for a user message that may include
// inline images (data URLs) and/or text file attachments.
// File contents are appended to the user text before sending to the LLM.
// Only file names are persisted in memory — not the content.
func (c *ChatService) SendMessageWithFiles(userInput string, images []string, files []FileAttachment) error {
	// Cancel any previous in-flight request.
	c.s.mu.Lock()
	if c.s.chatCancel != nil {
		c.s.chatCancel()
		c.s.chatCancel = nil
	}
	chatCtx, cancel := context.WithCancel(c.s.ctx)
	c.s.chatCancel = cancel
	c.s.chatGeneration++
	myGen := c.s.chatGeneration
	c.s.mu.Unlock()

	c.s.mu.RLock()
	ag := c.s.petAgent
	c.s.mu.RUnlock()

	if ag == nil {
		c.s.mu.Lock()
		c.s.chatCancel = nil
		c.s.mu.Unlock()
		cancel()
		return fmt.Errorf("agent not initialized: complete settings first")
	}

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

	go func() {
		defer cancel()
		defer func() {
			c.s.mu.Lock()
			if c.s.chatGeneration == myGen {
				c.s.chatCancel = nil
			}
			c.s.mu.Unlock()
		}()
		ch := ag.ChatWithMessage(chatCtx, msg)
		ep := agent.NewEmotionParser()
		for result := range ch {
			if result.Err != nil {
				if errors.Is(result.Err, context.Canceled) {
					return
				}
				c.s.emitDirect("chat:error", c.formatChatError(result.Err))
				return
			}
			if result.Done {
				if tail := ep.Flush(); tail != "" {
					c.s.emitDirect("chat:token", tail)
				}
				c.s.emitDirect("chat:done", "")
				return
			}
			if result.ThinkingToken != "" {
				c.s.emitDirect("chat:thinking", result.ThinkingToken)
				continue
			}
			if len(result.Images) > 0 {
				c.s.emitDirect("chat:image", result.Images)
			}
			text, emotion, intensity := ep.Feed(result.Token)
			if emotion != "" {
				c.s.app.Event.Emit("chat:emotion", map[string]any{
					"emotion":   emotion,
					"intensity": intensity,
				})
			}
			if text != "" {
				c.s.emitDirect("chat:token", text)
			}
		}
		if tail := ep.Flush(); tail != "" {
			c.s.emitDirect("chat:token", tail)
		}
		c.s.emitDirect("chat:done", "")
	}()
	return nil
}

// GetMessages returns recent chat history (up to limit messages).
func (c *ChatService) GetMessages(limit int) ([]memory.Message, error) {
	return c.s.shortMem.Recent(limit)
}

// GetMessagesBeforeID returns up to limit messages older than beforeID.
func (c *ChatService) GetMessagesBeforeID(beforeID int64, limit int) ([]memory.Message, error) {
	return c.s.shortMem.BeforeID(beforeID, limit)
}

// ClearChatHistory deletes all short-term messages from SQLite and all
// long-term memory vectors from the chromem collection.
func (c *ChatService) ClearChatHistory() error {
	if err := c.s.shortMem.DeleteAll(); err != nil {
		return fmt.Errorf("clear short-term memory: %w", err)
	}
	c.s.mu.RLock()
	longMem := c.s.longMem
	c.s.mu.RUnlock()
	if longMem != nil {
		embedder, err := llm.NewEmbedder(c.s.ctx, c.s.cfg)
		if err != nil {
			slog.Warn("ClearChatHistory: embedder init failed, skipping long-term memory clear", "err", err)
		} else if err := longMem.DeleteAll(c.s.vectorDB, embedder); err != nil {
			return fmt.Errorf("clear long-term memory: %w", err)
		}
	}
	slog.Info("ClearChatHistory: done")
	return nil
}

// ExportChatHistory opens a native save dialog and writes the recent 1000
// messages as plain text to the user-chosen file. Returns nil if the user
// cancels without choosing a file.
func (c *ChatService) ExportChatHistory() error {
	path, err := c.s.app.Dialog.SaveFile().
		SetMessage("导出聊天记录").
		SetFilename(fmt.Sprintf("chat-export-%s.txt", time.Now().Format("20060102-150405"))).
		AddFilter("文本文件", "*.txt").
		PromptForSingleSelection()
	if err != nil {
		return fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return nil // user cancelled
	}

	msgs, err := c.s.shortMem.Recent(1000)
	if err != nil {
		return fmt.Errorf("load messages: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "聊天记录导出 — %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	for _, m := range msgs {
		label := m.Role
		switch m.Role {
		case "user":
			label = "用户"
		case "assistant":
			label = "宠物"
		}
		fmt.Fprintf(&sb, "[%s] %s\n%s\n\n", m.CreatedAt, label, m.Content)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// StopGeneration cancels the current in-flight chat stream.
// The frontend is responsible for marking the interrupted messages as ghost bubbles.
func (c *ChatService) StopGeneration() {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	if c.s.chatCancel != nil {
		c.s.chatCancel()
		c.s.chatCancel = nil
	}
}

// formatChatError extracts the richest available error message from an LLM
// provider error. It appends APIError.Type/Code when present, and falls back
// to the raw HTTP response body captured by llmTransport (useful for OpenRouter
// errors that embed the upstream provider detail in error.metadata.raw).
func (c *ChatService) formatChatError(err error) string {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.Error()
		var extras []string
		if apiErr.Type != "" {
			extras = append(extras, "type: "+apiErr.Type)
		}
		if apiErr.Code != nil {
			extras = append(extras, fmt.Sprintf("code: %v", apiErr.Code))
		}
		// Append raw body from the transport — it may contain OpenRouter's
		// error.metadata.raw with the actual upstream provider error.
		c.s.mu.RLock()
		t := c.s.llmTransport
		c.s.mu.RUnlock()
		if t != nil {
			if raw := t.LastErrorBody(); len(raw) > 0 {
				extras = append(extras, "body: "+string(raw))
			}
		}
		if len(extras) > 0 {
			msg += "\n" + strings.Join(extras, "\n")
		}
		return msg
	}
	return err.Error()
}

// parseDataURL splits a data URL of the form "data:<mime>;base64,<data>" into
// its MIME type and base64-encoded data string. Returns ok=false if the input
// is not a valid base64 data URL.
func parseDataURL(dataURL string) (mimeType, b64data string, ok bool) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", "", false
	}
	rest := dataURL[len("data:"):]
	idx := strings.Index(rest, ";base64,")
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len(";base64,"):], true
}

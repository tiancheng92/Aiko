package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	openai "github.com/meguminnnnnnnnn/go-openai"
	"github.com/cloudwego/eino/schema"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

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

// ChatOptions carries per-message inference options from the frontend.
type ChatOptions struct {
	ThinkingLevel string `json:"thinkingLevel"` // "default"|"off"|"low"|"medium"|"high"
	UseKnowledge  bool   `json:"useKnowledge"`
	UseMemory     bool   `json:"useMemory"`
}

// ChatDirect streams a proactive AI response for the given prompt without saving to memory.
func (a *App) ChatDirect(ctx context.Context, prompt string) error {
	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()
	if ag == nil {
		return fmt.Errorf("agent not initialized")
	}
	ch := ag.ChatDirect(ctx, prompt)
	for r := range ch {
		if r.Err != nil {
			wailsruntime.EventsEmit(a.ctx, "chat:error", a.formatChatError(r.Err))
			wailsruntime.EventsEmit(a.ctx, "chat:done", nil)
			return r.Err
		}
		if len(r.Images) > 0 {
			wailsruntime.EventsEmit(a.ctx, "chat:image", r.Images)
		}
		if r.Done {
			break
		}
		wailsruntime.EventsEmit(a.ctx, "chat:token", r.Token)
	}
	wailsruntime.EventsEmit(a.ctx, "chat:done", nil)
	return nil
}

// ChatDirectCollect runs a proactive AI generation and collects the full response text.
func (a *App) ChatDirectCollect(ctx context.Context, prompt string) (string, error) {
	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()
	if ag == nil {
		return "", fmt.Errorf("agent not initialized")
	}
	return ag.ChatDirectCollect(ctx, prompt)
}

// streamChat cancels any in-flight chat, starts a new one via start, and emits
// chat:* Wails events as tokens arrive.
func (a *App) streamChat(start func(context.Context, *agent.Agent) <-chan agent.StreamResult) error {
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
		if a.chatGeneration == myGen {
			a.chatCancel = nil
		}
		a.mu.Unlock()
		cancel()
		return fmt.Errorf("agent not initialized: complete settings first")
	}

	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				wailsruntime.EventsEmit(a.ctx, "chat:error", fmt.Sprintf("agent panic: %v", r))
			}
		}()
		defer func() {
			a.mu.Lock()
			if a.chatGeneration == myGen {
				a.chatCancel = nil
			}
			a.mu.Unlock()
		}()
		ch := start(chatCtx, ag)
		bp := agent.NewBehaviorParser()
		for result := range ch {
			if result.Err != nil {
				// Ignore context cancellation — user triggered StopGeneration; frontend handles UI.
				if errors.Is(result.Err, context.Canceled) {
					return
				}
				wailsruntime.EventsEmit(a.ctx, "chat:error", a.formatChatError(result.Err))
				return
			}
			if result.Done {
				if tail := bp.Flush(); tail != "" {
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
			text, emotion, action := bp.Feed(result.Token)
			if emotion != "" {
				wailsruntime.EventsEmit(a.ctx, "chat:behavior", map[string]any{
					"emotion": emotion,
					"action":  action,
				})
			}
			if text != "" {
				wailsruntime.EventsEmit(a.ctx, "chat:token", text)
			}
		}
		if tail := bp.Flush(); tail != "" {
			wailsruntime.EventsEmit(a.ctx, "chat:token", tail)
		}
		wailsruntime.EventsEmit(a.ctx, "chat:done", "")
	}()
	return nil
}

// SendMessage streams an AI response for a plain-text user message.
func (a *App) SendMessage(userInput string, opts ChatOptions) error {
	agOpts := agent.ChatOptions{ThinkingLevel: opts.ThinkingLevel, UseKnowledge: opts.UseKnowledge, UseMemory: opts.UseMemory}
	return a.streamChat(func(ctx context.Context, ag *agent.Agent) <-chan agent.StreamResult {
		return ag.Chat(ctx, userInput, agOpts)
	})
}

// RegenerateLastReply deletes the last assistant message from history and
// re-runs the agent with the last user message. The response is streamed
// through the usual chat:token / chat:done / chat:error events.
func (a *App) RegenerateLastReply() error {
	if a.shortMem == nil {
		return fmt.Errorf("memory not initialized")
	}
	// Delete the stale assistant message.
	if _, err := a.shortMem.DeleteLastAssistantMessage(); err != nil {
		return fmt.Errorf("delete last assistant: %w", err)
	}
	// Find the last user message to re-send.
	userMsg, err := a.shortMem.LastUserMessage()
	if err != nil {
		return fmt.Errorf("find last user message: %w", err)
	}
	// Delete the user message from DB too — SendMessage will re-persist it.
	if err := a.shortMem.DeleteByIDs([]int64{userMsg.ID}); err != nil {
		return fmt.Errorf("delete last user message: %w", err)
	}
	return a.SendMessage(userMsg.Content, ChatOptions{UseKnowledge: true, UseMemory: true})
}

// SendMessageWithImages streams an AI response for a user message that includes
// one or more inline images encoded as data URLs ("data:image/png;base64,...").
// Falls back to a plain text message if no valid images are provided or if the
// active model does not support vision input.
func (a *App) SendMessageWithImages(userInput string, images []string, opts ChatOptions) error {
	a.mu.RLock()
	supportsVision := a.cfg.SupportsVision
	a.mu.RUnlock()

	text := userInput
	if text == "" {
		text = " "
	}

	// If the model doesn't support vision, fall back to text-only.
	if !supportsVision && len(images) > 0 {
		text = strings.TrimSpace(userInput)
		if text == "" {
			text = "[用户发送了图片，但当前模型不支持视觉输入]"
		} else {
			text += "\n\n[用户同时发送了图片，但当前模型不支持视觉输入]"
		}
		return a.SendMessage(text, opts)
	}

	// Build UserInputMultiContent: text part first, then image parts.
	// Always include a text part — some API providers reject messages
	// that only contain image_url parts.
	parts := make([]schema.MessageInputPart, 0, 1+len(images))
	parts = append(parts, schema.MessageInputPart{
		Type: schema.ChatMessagePartTypeText,
		Text: text,
	})
	for _, dataURL := range images {
		mimeType, b64data, ok := parseDataURL(dataURL)
		if !ok {
			log.Warn().Msg("SendMessageWithImages: invalid data URL, skipping")
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
		Content:               text,
		UserInputMultiContent: parts,
	}
	agOpts := agent.ChatOptions{ThinkingLevel: opts.ThinkingLevel, UseKnowledge: opts.UseKnowledge, UseMemory: opts.UseMemory}
	return a.streamChat(func(ctx context.Context, ag *agent.Agent) <-chan agent.StreamResult {
		return ag.ChatWithMessage(ctx, msg, agOpts)
	})
}

// SendMessageWithFiles streams an AI response for a user message that may include
// inline images (data URLs) and/or text file attachments.
// File contents are appended to the user text before sending to the LLM.
// Only file names are persisted in memory — not the content.
// Falls back to text-only if the active model does not support vision input.
func (a *App) SendMessageWithFiles(userInput string, images []string, files []FileAttachment, opts ChatOptions) error {
	// Build LLM text: original input + file contents appended.
	var llmBuilder strings.Builder
	llmBuilder.WriteString(userInput)
	fileNames := make([]string, 0, len(files))
	for _, f := range files {
		fileNames = append(fileNames, f.Name)
		fmt.Fprintf(&llmBuilder, "\n\n[文件: %s (%s)]\n```\n%s\n```", f.Name, f.MimeType, f.Content)
	}
	llmText := llmBuilder.String()

	a.mu.RLock()
	supportsVision := a.cfg.SupportsVision
	a.mu.RUnlock()

	// If the model doesn't support vision and there are images, strip them.
	effectiveImages := images
	if !supportsVision && len(images) > 0 {
		effectiveImages = nil
		if userInput == "" {
			llmText = "[用户发送了图片，但当前模型不支持视觉输入]\n\n" + llmText
		} else {
			llmText = strings.TrimSpace(userInput) + "\n\n[用户同时发送了图片，但当前模型不支持视觉输入]\n\n" + llmText
		}
	}

	// Build UserInputMultiContent: text part first, then image parts.
	parts := make([]schema.MessageInputPart, 0, 1+len(effectiveImages))
	parts = append(parts, schema.MessageInputPart{
		Type: schema.ChatMessagePartTypeText,
		Text: llmText,
	})
	for _, dataURL := range effectiveImages {
		mimeType, b64data, ok := parseDataURL(dataURL)
		if !ok {
			log.Warn().Msg("SendMessageWithFiles: invalid data URL, skipping")
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
	agOpts := agent.ChatOptions{ThinkingLevel: opts.ThinkingLevel, UseKnowledge: opts.UseKnowledge, UseMemory: opts.UseMemory}
	return a.streamChat(func(ctx context.Context, ag *agent.Agent) <-chan agent.StreamResult {
		return ag.ChatWithMessage(ctx, msg, agOpts)
	})
}

// parseDataURL splits a data URL of the form "data:<mime>;base64,<data>" into
// its MIME type and base64-encoded data string. Returns ok=false if the input
// is not a valid base64 data URL.
func parseDataURL(dataURL string) (mimeType, b64data string, ok bool) {
	rest, found := strings.CutPrefix(dataURL, "data:")
	if !found {
		return "", "", false
	}
	mimeType, b64data, ok = strings.Cut(rest, ";base64,")
	return
}

// GetMessages returns recent chat history (up to limit messages).
func (a *App) GetMessages(limit int) ([]memory.Message, error) {
	return a.shortMem.Recent(limit)
}

// GetMessagesBeforeID returns up to limit messages older than beforeID.
func (a *App) GetMessagesBeforeID(beforeID int64, limit int) ([]memory.Message, error) {
	return a.shortMem.BeforeID(beforeID, limit)
}

// SearchMessages returns all messages whose content matches the FTS5 query.
// Returns empty slice for empty query.
func (a *App) SearchMessages(query string) ([]memory.Message, error) {
	return a.shortMem.Search(query)
}

// GetMessagesFromNewestToID loads messages from newest backwards until the page
// containing targetID is reached. Used for jump-to-message after search.
func (a *App) GetMessagesFromNewestToID(targetID int64) ([]memory.Message, error) {
	return a.shortMem.GetNewestToID(targetID, 10)
}

// ClearChatHistory deletes all short-term messages from SQLite and all
// long-term memory vectors from the chromem collection.
func (a *App) ClearChatHistory() error {
	if err := a.shortMem.DeleteAll(); err != nil {
		return fmt.Errorf("clear short-term memory: %w", err)
	}
	a.mu.RLock()
	longMem := a.longMem
	cfgCopy := *a.cfg
	a.mu.RUnlock()
	if longMem != nil {
		embedder, err := llm.NewEmbedder(a.ctx, &cfgCopy)
		if err != nil {
			log.Warn().Err(err).Msg("ClearChatHistory: embedder init failed, skipping long-term memory clear")
		} else if err := longMem.DeleteAll(a.vectorDB, embedder); err != nil {
			return fmt.Errorf("clear long-term memory: %w", err)
		}
	}
	log.Info().Msg("ClearChatHistory: done")
	return nil
}

// StopGeneration cancels the current in-flight chat stream.
// The frontend is responsible for marking the interrupted messages as ghost bubbles.
func (a *App) StopGeneration() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
}

// ExportChatHistory opens a native save dialog and writes the recent 1000
// messages as plain text to the user-chosen file. Returns nil if the user
// cancels without choosing a file.
func (a *App) ExportChatHistory() error {
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出聊天记录",
		DefaultFilename: fmt.Sprintf("chat-export-%s.txt", time.Now().Format("20060102-150405")),
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "文本文件", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return nil // user cancelled
	}

	msgs, err := a.shortMem.Recent(1000)
	if err != nil {
		return fmt.Errorf("load messages: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "聊天记录导出 — %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	for i := range msgs {
		label := msgs[i].Role
		switch msgs[i].Role {
		case "user":
			label = "用户"
		case "assistant":
			label = "宠物"
		}
		fmt.Fprintf(&sb, "[%s] %s\n%s\n\n", msgs[i].CreatedAt, label, msgs[i].Content)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// formatChatError extracts the richest available error message from an LLM
// provider error. It appends APIError.Type/Code when present, and falls back
// to the raw HTTP response body captured by llmTransport (useful for OpenRouter
// errors that embed the upstream provider detail in error.metadata.raw).
func (a *App) formatChatError(err error) string {
	if apiErr, ok := errors.AsType[*openai.APIError](err); ok {
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
		a.mu.RLock()
		t := a.llmTransport
		a.mu.RUnlock()
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

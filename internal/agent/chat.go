package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/cloudwego/eino/schema"

	internaltools "aiko/internal/tools"
)

// SetSkillHint records that a skill was activated during this turn.
// Called by the skill middleware after injecting a skill into the prompt.
// The hint is consumed and cleared by shouldReflect after the reply.
func (a *Agent) SetSkillHint(skillName string) {
	a.skillHintMu.Lock()
	defer a.skillHintMu.Unlock()
	a.lastSkillHint = fmt.Sprintf("本次激活了 skill %q，回复结束后思考该 skill 是否需要更新。", skillName)
}

// shouldReflect decides whether to trigger a self-growth reflection after this turn.
// toolCallCount is the number of distinct tool names invoked during the turn, as
// counted by drainIter. Returns (trigger, hints). trigger is true under any of:
//   - periodic: turnCount % nudgeInterval == 0
//   - user correction keywords detected in userInput
//   - multi-tool chain (toolCallCount ≥ 3 distinct tools)
//   - explicit preference keywords detected in userInput
//   - hints is non-empty (skill activation signal from middleware)
func (a *Agent) shouldReflect(userInput string, toolCallCount int) (bool, []string) {
	var hints []string

	// Collect skill-activation hint (cleared here).
	a.skillHintMu.Lock()
	if a.lastSkillHint != "" {
		hints = append(hints, a.lastSkillHint)
		a.lastSkillHint = ""
	}
	a.skillHintMu.Unlock()

	// Periodic trigger.
	tc := a.turnCount.Load()
	if a.nudgeInterval > 0 && tc > 0 && tc%int64(a.nudgeInterval) == 0 {
		return true, hints
	}

	// User correction signal.
	correctionKW := []string{"不对", "你刚才说错了", "其实是", "纠正"}
	for _, kw := range correctionKW {
		if strings.Contains(userInput, kw) {
			hints = append(hints, "检测到用户纠错，检查相关 skill 是否需要更新。")
			break
		}
	}

	// Multi-tool chain signal (distinct tool names, not raw invocation count).
	if toolCallCount >= 3 {
		hints = append(hints, fmt.Sprintf("本次调用了 %d 种不同工具，考虑提炼为可复用技能。", toolCallCount))
	}

	// Explicit preference signal.
	prefKW := []string{"以后都", "我不喜欢", "记住", "每次都"}
	for _, kw := range prefKW {
		if strings.Contains(userInput, kw) {
			hints = append(hints, "检测到显式偏好表达，优先更新用户画像。")
			break
		}
	}

	return len(hints) > 0, hints
}

// buildReflectionPrompt constructs a structured fill-in-the-blank reflection prompt.
// hints is an optional list of targeted guidance lines appended after the checklist.
func buildReflectionPrompt(userInput, assistantReply string, hints []string) string {
	var sb strings.Builder
	sb.WriteString("[SELF-GROWTH REFLECTION]\n")
	sb.WriteString("决定是否 save_skill 前，请先调用 list_skills 查看已有技能，避免创建重复或名称冲突的技能。\n\n")
	sb.WriteString("本轮对话摘要（请填写）：\n")
	sb.WriteString("- 用户意图：<一句话>\n")
	sb.WriteString("- 解决方案：<一句话>\n")
	sb.WriteString("- 结果：成功 / 失败 / 部分完成\n\n")
	sb.WriteString("请选择（可不选）：\n")
	sb.WriteString("□ save_memory — 有新的具体事实或偏好值得记录\n")
	sb.WriteString("□ update_user_profile — 发现了用户新的习惯/背景\n")
	sb.WriteString("□ save_skill — 本次解决模式可复用\n")
	if len(hints) > 0 {
		sb.WriteString("\n重点提示：\n")
		for _, h := range hints {
			sb.WriteString("⚠️ ")
			sb.WriteString(h)
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("\n以下是本轮对话内容供参考：\n")
	sb.WriteString("用户：")
	sb.WriteString(userInput)
	sb.WriteString("\n助手：")
	sb.WriteString(assistantReply)
	return sb.String()
}

// reflect runs a background self-growth reflection turn after the main reply.
// It builds a structured prompt and calls ChatDirectCollect; errors are logged
// and discarded — reflection failures must never surface to the user.
func (a *Agent) reflect(ctx context.Context, userInput, assistantReply string, hints []string) {
	defer func() {
		if r := recover(); r != nil {
			log.Warn().Interface("err", r).Msg("reflect panic recovered")
		}
	}()
	prompt := buildReflectionPrompt(userInput, assistantReply, hints)
	if _, err := a.ChatDirectCollect(ctx, prompt); err != nil {
		log.Warn().Err(err).Msg("self-growth reflection failed")
	}
}

// Chat sends a user message to the agent and returns a channel that streams
// tokens. After the final Done=true result, user and assistant messages are
// persisted to short-term memory and excess messages are migrated to
// long-term memory asynchronously.
func (a *Agent) Chat(ctx context.Context, userInput string) <-chan StreamResult {
	ch := make(chan StreamResult, streamResultBufSize)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				ch <- StreamResult{Err: fmt.Errorf("agent panic: %v", r)}
			}
		}()

		ctxMsgs, err := a.buildContext(ctx, userInput)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		// Inject flush callback so check_and_update can persist before restart.
		flushFn := func(assistantSummary string) {
			if a.shortMem == nil {
				return
			}
			if _, _, stripped, ok := parseEmotionTag(assistantSummary); ok {
				assistantSummary = stripped
			}
			if _, err := a.shortMem.AddWithImagesAndFiles("user", userInput, nil, nil); err != nil {
				log.Warn().Err(err).Msg("short memory: add user message")
			}
			if _, err := a.shortMem.AddFull("assistant", assistantSummary, "", nil, nil); err != nil {
				log.Warn().Err(err).Msg("short memory: add assistant message")
			}
		}
		ctx = context.WithValue(ctx, internaltools.PersistBeforeRestartKey{}, flushFn)

		content := userInput

		msgs := append(ctxMsgs, &schema.Message{Role: schema.User, Content: content})
		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}

		ch <- StreamResult{Done: true}
		go a.persistAndMigrate(context.Background(), userInput, nil, nil, fullResponse, thinkingContent, toolImgs, toolCallCount)
	}()

	return ch
}

// ChatDirect sends a prompt to the agent and streams tokens without persisting
// the exchange to memory. Used by the scheduler to avoid polluting chat history.
func (a *Agent) ChatDirect(ctx context.Context, prompt string) <-chan StreamResult {
	ch := make(chan StreamResult, streamResultBufSize)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				ch <- StreamResult{Err: fmt.Errorf("agent panic: %v", r)}
			}
		}()
		_, _, _, _, ok := drainRunner(ctx, a.runner, prompt, ch, nil, nil, fmt.Sprintf("direct-%d", time.Now().UnixNano()))
		if !ok {
			return
		}
		ch <- StreamResult{Done: true}
		// NOTE: No persistAndMigrate call here — intentional.
	}()

	return ch
}

// ChatDirectCollect sends a prompt to the agent, collects the full response
// as a string, and returns it. Unlike ChatDirect, no Wails events are emitted.
// Used by the ProactiveEngine when the chat panel is closed.
func (a *Agent) ChatDirectCollect(ctx context.Context, prompt string) (string, error) {
	ch := a.ChatDirect(ctx, prompt)
	var sb strings.Builder
	for r := range ch {
		if r.Err != nil {
			return "", r.Err
		}
		if r.Done {
			break
		}
		sb.WriteString(r.Token)
	}
	return sb.String(), nil
}

// extractTextFromMessage returns the plain text from a Message's Content or
// the first text part in UserInputMultiContent. Used as memory key and query.
func extractTextFromMessage(msg *schema.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	for _, p := range msg.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeText && p.Text != "" {
			return p.Text
		}
	}
	return ""
}

// extractImagesFromMessage returns all base64 data URLs from image parts of msg.
func extractImagesFromMessage(msg *schema.Message) []string {
	var images []string
	for _, p := range msg.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeImageURL && p.Image != nil && p.Image.Base64Data != nil {
			images = append(images, "data:"+p.Image.MIMEType+";base64,"+*p.Image.Base64Data)
		}
	}
	return images
}

// ChatWithMessage sends a pre-built user Message (which may contain images via
// UserInputMultiContent) to the agent and streams tokens via the returned channel.
// After streaming, user input and assistant reply are persisted to short-term
// memory; images are stored as data URLs so history can re-render them.
func (a *Agent) ChatWithMessage(ctx context.Context, msg *schema.Message) <-chan StreamResult {
	ch := make(chan StreamResult, streamResultBufSize)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				ch <- StreamResult{Err: fmt.Errorf("agent panic: %v", r)}
			}
		}()

		userText := extractTextFromMessage(msg)
		ctxMsgs, err := a.buildContext(ctx, userText)
		if err != nil {
			ch <- StreamResult{Err: err}
			return
		}

		sendMsg := msg

		// Prefer the original user text stored in Extra (no file content) for memory.
		userMemory := extractTextFromMessage(msg)
		if orig, ok := msg.Extra["_user_text"].(string); ok && orig != "" {
			userMemory = orig
		}
		userImages := extractImagesFromMessage(msg)
		// Extract file names passed via Extra by app.go.
		var userFiles []string
		if raw, ok := msg.Extra["_file_names"]; ok {
			if names, ok := raw.([]string); ok {
				userFiles = names
			}
		}

		// Inject flush callback so check_and_update can persist before restart.
		flushFn := func(assistantSummary string) {
			if a.shortMem == nil {
				return
			}
			if _, err := a.shortMem.AddWithImagesAndFiles("user", userMemory, userImages, userFiles); err != nil {
				log.Warn().Err(err).Msg("short memory: add user message")
			}
			if _, _, stripped, ok := parseEmotionTag(assistantSummary); ok {
				assistantSummary = stripped
			}
			if _, err := a.shortMem.AddFull("assistant", assistantSummary, "", nil, nil); err != nil {
				log.Warn().Err(err).Msg("short memory: add assistant message")
			}
		}
		ctx = context.WithValue(ctx, internaltools.PersistBeforeRestartKey{}, flushFn)

		msgs := append(ctxMsgs, sendMsg)
		checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}

		ch <- StreamResult{Done: true}
		go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse, thinkingContent, toolImgs, toolCallCount)
	}()

	return ch
}

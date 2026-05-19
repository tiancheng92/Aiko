package agent

import (
	"context"
	"encoding/base64"

	"github.com/bytedance/sonic"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
	"aiko/internal/llm"
	internaltools "aiko/internal/tools"
)

// builderPool recycles strings.Builder instances to reuse their internal byte buffers
// across streaming responses, avoiding repeated buffer reallocations.
var builderPool = sync.Pool{New: func() any { return new(strings.Builder) }}

func getBuilder() *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	return b
}

func putBuilder(b *strings.Builder) {
	b.Reset()
	builderPool.Put(b)
}

// buildIndicator returns the indicator token for a tool/skill invocation.
// arguments (raw JSON) is base64-encoded into an `args` attribute so the
// frontend can display per-call parameters in a tooltip/popover.
// Skill calls use <skill-call> with the real skill name extracted from args;
// all other tools use <tool-call name="xxx">.
func buildIndicator(name, arguments string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(arguments))
	if name == "skill" {
		var a struct {
			Skill string `json:"skill"`
		}
		displayName := "skill"
		if err := sonic.Unmarshal([]byte(arguments), &a); err == nil && a.Skill != "" {
			displayName = a.Skill
		}
		return "\n\n<skill-call name=\"" + displayName + "\" args=\"" + b64 + "\"></skill-call>\n\n"
	}
	return "\n\n<tool-call name=\"" + name + "\" args=\"" + b64 + "\"></tool-call>\n\n"
}

// sendToken sends a token StreamResult to ch.
func sendToken(ch chan<- StreamResult, token string) { ch <- StreamResult{Token: token} }

// sendThinkingToken sends a thinking-token StreamResult to ch.
func sendThinkingToken(ch chan<- StreamResult, token string) {
	ch <- StreamResult{ThinkingToken: token}
}

// drainRunner consumes all events from runner.Query, forwards tokens to ch,
// and returns the accumulated response string, thinking content, tool images, and tool-call count.
// Returns (response, thinking, images, toolCalls, true) on success or ("", "", nil, 0, false) after sending an error to ch.
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID, thinkingLevel, provider string) (string, string, []string, int, bool) {
	runOpts := []adk.AgentRunOption{adk.WithCheckPointID(checkpointID)}
	if opt := llm.ReasoningOption(thinkingLevel, config.Provider(provider)); opt != nil {
		runOpts = append(runOpts, adk.WithChatModelOptions([]model.Option{*opt}))
	}
	iter := runner.Query(ctx, query, runOpts...)
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}

// drainRunnerMsg consumes all events from runner.Run with a pre-built message list,
// forwards tokens to ch, and returns the accumulated response string, thinking content,
// tool images, and tool-call count.
// Returns (response, thinking, images, toolCalls, true) on success or ("", "", nil, 0, false) after sending an error to ch.
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID, thinkingLevel, provider string) (string, string, []string, int, bool) {
	runOpts := []adk.AgentRunOption{adk.WithCheckPointID(checkpointID)}
	if opt := llm.ReasoningOption(thinkingLevel, config.Provider(provider)); opt != nil {
		runOpts = append(runOpts, adk.WithChatModelOptions([]model.Option{*opt}))
	}
	iter := runner.Run(ctx, msgs, runOpts...)
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}

// drainIter consumes all events from an AsyncIterator, forwards tokens to ch,
// handles interrupt events, and returns the accumulated response string, thinking content,
// collected tool images, and the number of distinct tool names invoked this turn.
func drainIter(ctx context.Context, runner *adk.Runner, iter *adk.AsyncIterator[*adk.AgentEvent],
	ch chan<- StreamResult, pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	uniqueTools := make(map[string]struct{})
	var imgs []string
	resp, thinking, ok := drainIterInner(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID, uniqueTools, &imgs)
	return resp, thinking, imgs, len(uniqueTools), ok
}

// iterResult carries a single event from iter.Next().
type iterResult struct {
	event *adk.AgentEvent
	ok    bool
}

// pumpIter starts a goroutine that calls iter.Next() in a loop and sends each
// result to the returned channel. Because iter.Next() blocks on sync.Cond.Wait()
// and has no context awareness, the goroutine cannot be interrupted mid-call;
// it exits as soon as iter.Next() returns after doneC is closed.
// doneC must be closed exactly once by the caller (e.g. via sync.Once + defer).
func pumpIter(iter *adk.AsyncIterator[*adk.AgentEvent], doneC <-chan struct{}) <-chan iterResult {
	ch := make(chan iterResult, 1)
	go func() {
		for {
			event, ok := iter.Next()
			select {
			case ch <- iterResult{event, ok}:
			case <-doneC:
				return
			}
			if !ok {
				return
			}
		}
	}()
	return ch
}

// processStreamingMessage drains a streaming MessageVariant, forwarding tokens,
// thinking content, images, and tool call names to ch and the accumulators.
// Returns false if a receive error occurs (which is already sent to ch).
func processStreamingMessage(mo *adk.MessageVariant, ch chan<- StreamResult,
	uniqueTools map[string]struct{}, imgsBuf *[]string,
	sb, thinkingSb *strings.Builder) bool {
	// callBufs accumulates per-call state keyed by ToolCall.Index.
	// Each streaming tool call arrives as multiple chunks: the first carries
	// Function.Name, subsequent ones carry Function.Arguments fragments.
	// We buffer until the JSON is complete (contains '}'), then emit once.
	type callBuf struct {
		name string
		args strings.Builder
		sent bool
	}
	callBufs := make(map[int]*callBuf)
	nextIdx := 0 // fallback counter when Index is nil
	for {
		m, recvErr := mo.MessageStream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				break
			}
			ch <- StreamResult{Err: recvErr}
			return false
		}
		if m == nil {
			continue
		}
		if m.Role == schema.Tool || len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				idx := nextIdx
				if tc.Index != nil {
					idx = *tc.Index
				}
				buf, ok := callBufs[idx]
				if !ok {
					buf = &callBuf{}
					callBufs[idx] = buf
					if tc.Index == nil {
						nextIdx++
					}
				}
				if tc.Function.Name != "" {
					buf.name = tc.Function.Name
				}
				buf.args.WriteString(tc.Function.Arguments)
				if !buf.sent && buf.name != "" && strings.Contains(buf.args.String(), "}") {
					buf.sent = true
					indicator := buildIndicator(buf.name, buf.args.String())
					sendToken(ch, indicator)
					sb.WriteString(indicator)
					uniqueTools[buf.name] = struct{}{}
				}
			}
			if m.Role == schema.Tool {
				if imgs := extractToolImages(m.Content); len(imgs) > 0 {
					ch <- StreamResult{Images: imgs}
					*imgsBuf = append(*imgsBuf, imgs...)
				}
			}
			continue
		}
		if imgs := extractImages(m.AssistantGenMultiContent); len(imgs) > 0 {
			ch <- StreamResult{Images: imgs}
			*imgsBuf = append(*imgsBuf, imgs...)
		}
		if m.ReasoningContent != "" {
			sendThinkingToken(ch, m.ReasoningContent)
			thinkingSb.WriteString(m.ReasoningContent)
		}
		if m.Content == "" {
			continue
		}
		sendToken(ch, m.Content)
		sb.WriteString(m.Content)
	}
	return true
}

// drainIterInner is the recursive core of drainIter; it accepts a shared uniqueTools
// map so that distinct tool names are accumulated correctly across interrupt/resume cycles.
// imgsBuf accumulates images produced by tool calls (e.g. read_image) across all
// recursive invocations so they can be persisted by the caller.
func drainIterInner(ctx context.Context, runner *adk.Runner, iter *adk.AsyncIterator[*adk.AgentEvent],
	ch chan<- StreamResult, pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string,
	uniqueTools map[string]struct{}, imgsBuf *[]string) (string, string, bool) {
	sb := getBuilder()
	thinkingSb := getBuilder()
	defer func() {
		putBuilder(sb)
		putBuilder(thinkingSb)
	}()

	doneC := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(doneC) }) }
	defer closeDone()
	eventCh := pumpIter(iter, doneC)

	for {
		var event *adk.AgentEvent
		var ok bool
		select {
		case <-ctx.Done():
			return "", "", false
		case r := <-eventCh:
			event, ok = r.event, r.ok
		}
		if !ok {
			break
		}
		if event.Err != nil {
			// Context cancellation means the user hit Stop — propagate silently.
			if errors.Is(event.Err, context.Canceled) {
				return "", "", false
			}
			// Interrupt signals must pass through for the checkpoint/resume flow.
			if _, ok := compose.IsInterruptRerunError(event.Err); ok {
				ch <- StreamResult{Err: event.Err}
				return "", "", false
			}
			// All other errors (tool failure, network glitch, etc.) are surfaced as
			// a token so the LLM can acknowledge the problem and continue.
			log.Warn().Err(event.Err).Msg("agent: non-fatal event error, forwarding to LLM")
			errToken := fmt.Sprintf("\n\n[工具调用出错: %v]", event.Err)
			ch <- StreamResult{Token: errToken}
			sb.WriteString(errToken)
			continue
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			resumeIter, err := handleInterrupt(ctx, runner, event, ch, pendingConfirms, emitEvent, checkpointID)
			if err != nil {
				return "", "", false
			}
			if resumeIter != nil {
				resp, thinkingResp, ok := drainIterInner(ctx, runner, resumeIter, ch, pendingConfirms, emitEvent, checkpointID, uniqueTools, imgsBuf)
				if !ok {
					return "", "", false
				}
				sb.WriteString(resp)
				thinkingSb.WriteString(thinkingResp)
			}
			continue
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mo := event.Output.MessageOutput
		if mo.IsStreaming {
			if !processStreamingMessage(mo, ch, uniqueTools, imgsBuf, sb, thinkingSb) {
				return "", "", false
			}
		} else if mo.Message != nil && mo.Message.Role == schema.Tool {
			// Tool result: forward image data URLs / HTTP image URLs to the frontend;
			// suppress raw text so tool internals stay out of the chat panel.
			if imgs := extractToolImages(mo.Message.Content); len(imgs) > 0 {
				ch <- StreamResult{Images: imgs}
				*imgsBuf = append(*imgsBuf, imgs...)
			}
		} else if mo.Message != nil && len(mo.Message.ToolCalls) == 0 {
			if imgs := extractImages(mo.Message.AssistantGenMultiContent); len(imgs) > 0 {
				ch <- StreamResult{Images: imgs}
				*imgsBuf = append(*imgsBuf, imgs...)
			}
			if mo.Message.ReasoningContent != "" {
				sendThinkingToken(ch, mo.Message.ReasoningContent)
				thinkingSb.WriteString(mo.Message.ReasoningContent)
			}
			if mo.Message.Content != "" {
				sendToken(ch, mo.Message.Content)
				sb.WriteString(mo.Message.Content)
			}
		} else if mo.Message != nil && len(mo.Message.ToolCalls) > 0 {
			for _, tc := range mo.Message.ToolCalls {
				if tc.Function.Name != "" {
					uniqueTools[tc.Function.Name] = struct{}{}
					indicator := buildIndicator(tc.Function.Name, tc.Function.Arguments)
					sendToken(ch, indicator)
					sb.WriteString(indicator)
				}
			}
		}
	}
	// Clone before returning: the builders go back to the pool immediately after,
	// so callers must not hold references to the internal buffer.
	return strings.Clone(sb.String()), strings.Clone(thinkingSb.String()), true
}

// extractImages returns data URLs or HTTP(S) URLs for any image parts in parts.
// Only assistant-generated images are passed here; tool returns are never included.
func extractImages(parts []schema.MessageOutputPart) []string {
	var imgs []string
	for _, p := range parts {
		if p.Type != "image" || p.Image == nil {
			continue
		}
		if p.Image.Base64Data != nil && *p.Image.Base64Data != "" {
			mime := p.Image.MIMEType
			if mime == "" {
				mime = "image/png"
			}
			imgs = append(imgs, "data:"+mime+";base64,"+*p.Image.Base64Data)
		} else if p.Image.URL != nil && *p.Image.URL != "" {
			imgs = append(imgs, *p.Image.URL)
		}
	}
	return imgs
}

// extractToolImages extracts image data URLs or HTTP(S) image URLs from a tool
// result string. Tool implementations that return a single data URL or image URL
// (e.g. read_image, generate_image) are detected here and forwarded to the
// frontend via StreamResult.Images so the chat panel can render them.
func extractToolImages(content string) []string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "data:image/") {
		return []string{content}
	}
	if (strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://")) &&
		isImageURL(content) {
		return []string{content}
	}
	return nil
}

// isImageURL reports whether url has a common image file extension or image-related path.
func isImageURL(url string) bool {
	lower := strings.ToLower(url)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

// handleInterrupt processes an eino interrupt event by notifying the frontend and
// blocking until the user confirms or rejects. Returns the resume iterator on success,
// or (nil, nil) if interrupt data is unrecognised, or (nil, err) on failure.
//
// The resume Targets key is the root-cause InterruptCtx.ID (the fully-qualified
// component address, e.g. "agent:aiko;node:ToolNode;tool:execute_shell:xxx").
// The interrupt payload is in InterruptCtx.Info, set by tool.Interrupt().
func handleInterrupt(
	ctx context.Context,
	runner *adk.Runner,
	event *adk.AgentEvent,
	ch chan<- StreamResult,
	pendingConfirms *sync.Map,
	emitEvent func(string, ...any),
	checkpointID string,
) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	if pendingConfirms == nil || emitEvent == nil {
		return nil, nil
	}
	if event.Action == nil || event.Action.Interrupted == nil ||
		len(event.Action.Interrupted.InterruptContexts) == 0 {
		return nil, nil
	}

	// Find the root-cause interrupt context — its ID is the Targets key for resume.
	ictx := event.Action.Interrupted.InterruptContexts[0]
	for _, c := range event.Action.Interrupted.InterruptContexts {
		if c.IsRootCause {
			ictx = c
			break
		}
	}

	var req ToolConfirmRequest
	switch info := ictx.Info.(type) {
	case internaltools.ShellConfirmInfo:
		req = ToolConfirmRequest{
			ID: info.ID, ToolType: "shell",
			Command: info.Command, WorkingDir: info.WorkingDir,
		}
	case internaltools.CodeConfirmInfo:
		req = ToolConfirmRequest{
			ID: info.ID, ToolType: "code",
			Language: info.Language, Code: info.Code, WorkingDir: info.WorkingDir,
		}
	case internaltools.UpdateConfirmInfo:
		req = ToolConfirmRequest{
			ID:             info.ID,
			ToolType:       "update",
			CurrentVersion: info.CurrentVersion,
			LatestVersion:  info.LatestVersion,
		}
	default:
		log.Warn().Str("type", fmt.Sprintf("%T", ictx.Info)).Interface("value", ictx.Info).Msg("handleInterrupt: unrecognized interrupt info type")
		return nil, nil
	}

	respCh := make(chan ToolConfirmResponse, 1)
	pendingConfirms.Store(req.ID, respCh)
	defer pendingConfirms.Delete(req.ID)

	emitEvent("tool:confirm", req)

	select {
	case resp := <-respCh:
		resumeIter, err := runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{
			Targets: map[string]any{
				ictx.ID: internaltools.ConfirmResult{
					Approved:      resp.Approved,
					EditedContent: resp.EditedContent,
				},
			},
		})
		if err != nil {
			ch <- StreamResult{Err: fmt.Errorf("resume failed: %w", err)}
			return nil, err
		}
		return resumeIter, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

# LLM Thinking Process Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stream and persist LLM reasoning/thinking content, display it as a collapsible block at the top of each assistant bubble, auto-collapse when thinking ends, and show it collapsed in historical messages.

**Architecture:** eino's OpenAI adapter already populates `m.ReasoningContent` from `choice.Delta.ReasoningContent` during streaming — we just emit it via a new `chat:thinking` Wails event. A new `thinking_content` column in SQLite stores it per-message. The frontend subscribes to `chat:thinking`, accumulates the text in each message object, and renders a collapsible block above the normal content.

**Tech Stack:** Go (eino agent, SQLite), Vue 3 `<script setup>`, Wails v2 events

---

## File Map

| File | Change |
|------|--------|
| `internal/agent/agent.go` | Add `ThinkingToken` to `StreamResult`; accumulate `thinkingSb` in `drainIterInner`; change return signature; propagate through `drainIter`, `drainRunnerMsg`, `Chat`, `ChatWithMessage`; pass to `persistAndMigrate` |
| `internal/db/sqlite.go` | Add v8 migration patch: `thinking_content TEXT NOT NULL DEFAULT ''` |
| `internal/memory/short.go` | Add `ThinkingContent` to `Message`; update `scanMessage`, all SELECTs, `AddWithImagesAndFiles` (new `thinkingContent` param via `AddFull`); add `AddFull` method |
| `app.go` | Emit `chat:thinking` in all three send methods |
| `frontend/src/components/ChatPanel.vue` | Subscribe `chat:thinking`; add `thinkingContent`/`thinkingExpanded` to message objects; update `mapMsg` for history; add ThinkingBlock UI + CSS |

---

## Task 1: DB migration — add `thinking_content` column

**Files:**
- Modify: `internal/db/sqlite.go:124-140`

- [ ] **Step 1: Add v8 patch to the patches slice**

In `internal/db/sqlite.go`, after the v7 patch line (`embedding_provider`), add:

```go
// v8: store LLM reasoning/thinking content alongside each assistant message.
`ALTER TABLE messages ADD COLUMN thinking_content TEXT NOT NULL DEFAULT ''`,
```

The full patches slice tail becomes:
```go
        // v7: embedding model may use a different provider (openai-compat vs openrouter).
        `ALTER TABLE model_profiles ADD COLUMN embedding_provider TEXT    NOT NULL DEFAULT 'openai'`,
        // v8: store LLM reasoning/thinking content alongside each assistant message.
        `ALTER TABLE messages ADD COLUMN thinking_content TEXT NOT NULL DEFAULT ''`,
    }
```

- [ ] **Step 2: Verify Go compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/db/sqlite.go
git commit -m "feat(db): add thinking_content column to messages table (v8 migration)"
```

---

## Task 2: Update `memory.Message` struct and all DB access

**Files:**
- Modify: `internal/memory/short.go`

- [ ] **Step 1: Add `ThinkingContent` field to `Message` struct**

In `internal/memory/short.go`, update the `Message` struct (currently at line 14):

```go
// Message is a single conversation turn stored in SQLite.
type Message struct {
	ID              int64
	Role            string   // "user" | "assistant"
	Content         string
	ThinkingContent string   // LLM reasoning/thinking process, empty for most messages
	Images          []string // data URLs, empty for most messages
	Files           []string // attached file names (no content), empty for most messages
	CreatedAt       string
}
```

- [ ] **Step 2: Update `scanMessage` to scan `thinking_content`**

`scanMessage` currently scans 6 columns: `id, role, content, images, files, created_at`. Update to scan 7 columns, inserting `thinking_content` after `content`:

```go
// scanMessage scans a row that selects id, role, content, thinking_content, images, files, created_at.
func scanMessage(scan func(...any) error) (Message, error) {
	var m Message
	var imagesJSON, filesJSON string
	if err := scan(&m.ID, &m.Role, &m.Content, &m.ThinkingContent, &imagesJSON, &filesJSON, &m.CreatedAt); err != nil {
		return m, err
	}
	if imagesJSON != "" {
		if err := json.Unmarshal([]byte(imagesJSON), &m.Images); err != nil {
			slog.Warn("short memory: images JSON unmarshal", "id", m.ID, "err", err)
		}
	}
	if filesJSON != "" {
		if err := json.Unmarshal([]byte(filesJSON), &m.Files); err != nil {
			slog.Warn("short memory: files JSON unmarshal", "id", m.ID, "err", err)
		}
	}
	return m, nil
}
```

- [ ] **Step 3: Update all SELECT queries to include `thinking_content`**

There are four query locations. Update each SELECT to add `thinking_content` after `content`:

**`Recent` (line ~51):**
```go
func (s *ShortStore) Recent(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		ORDER BY id DESC
		LIMIT ?`, n)
```

**`BeforeID` (line ~120):**
```go
func (s *ShortStore) BeforeID(beforeID int64, n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		WHERE id < ?
		ORDER BY id DESC
		LIMIT ?`, beforeID, n)
```

**`OldestN` (line ~156):**
```go
func (s *ShortStore) OldestN(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		ORDER BY id ASC
		LIMIT ?`, n)
```

**`LastUserMessage` (line ~248):**
```go
func (s *ShortStore) LastUserMessage() (Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		WHERE role = 'user'
		ORDER BY id DESC
		LIMIT 1`)
```

- [ ] **Step 4: Update `DeleteLastAssistantMessage` raw scan**

This method uses a raw `.Scan()` call instead of `scanMessage`. Update it to scan `thinking_content` (currently scans 6 columns with `new(string), new(string)` for images/files):

```go
func (s *ShortStore) DeleteLastAssistantMessage() (Message, error) {
	var m Message
	err := s.db.QueryRow(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		WHERE role = 'assistant'
		ORDER BY id DESC
		LIMIT 1`).Scan(
		&m.ID, &m.Role, &m.Content, &m.ThinkingContent,
		new(string), new(string), &m.CreatedAt,
	)
	if err != nil {
		return m, err
	}
	_, err = s.db.Exec(`DELETE FROM messages WHERE id = ?`, m.ID)
	return m, err
}
```

- [ ] **Step 5: Add `AddFull` and update `AddWithImagesAndFiles`**

Replace `AddWithImagesAndFiles` to delegate to a new `AddFull`:

```go
// AddFull inserts a new message with all optional fields and returns its ID.
func (s *ShortStore) AddFull(role, content, thinkingContent string, images []string, files []string) (int64, error) {
	imagesJSON := ""
	if len(images) > 0 {
		b, err := json.Marshal(images)
		if err != nil {
			slog.Warn("short memory: images JSON marshal", "err", err)
		} else {
			imagesJSON = string(b)
		}
	}
	filesJSON := ""
	if len(files) > 0 {
		b, err := json.Marshal(files)
		if err != nil {
			slog.Warn("short memory: files JSON marshal", "err", err)
		} else {
			filesJSON = string(b)
		}
	}
	res, err := s.db.Exec(
		`INSERT INTO messages(role, content, thinking_content, images, files) VALUES(?, ?, ?, ?, ?)`,
		role, content, thinkingContent, imagesJSON, filesJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AddWithImagesAndFiles inserts a new message with optional images and file names and returns its ID.
func (s *ShortStore) AddWithImagesAndFiles(role, content string, images []string, files []string) (int64, error) {
	return s.AddFull(role, content, "", images, files)
}
```

- [ ] **Step 6: Verify Go compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/memory/short.go
git commit -m "feat(memory): add ThinkingContent field and AddFull for thinking persistence"
```

---

## Task 3: Agent streaming — extract and emit `ReasoningContent`

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add `ThinkingToken` to `StreamResult`**

Update the `StreamResult` struct (currently at line 38):

```go
// StreamResult is a single streamed token or a terminal signal.
type StreamResult struct {
	Token         string
	ThinkingToken string   // reasoning/thinking chunk from ReasoningContent
	Images        []string // base64 data URLs or http(s) URLs of images output by the LLM
	Err           error
	Done          bool
}
```

- [ ] **Step 2: Update `drainIterInner` signature and body**

Change the return signature from `(string, bool)` to `(string, string, bool)` (content, thinkingContent, ok).

Add a `thinkingSb` builder alongside `sb`, and extract `m.ReasoningContent` for both the streaming and non-streaming branches.

Full updated function signature and body start:

```go
func drainIterInner(ctx context.Context, runner *adk.Runner, iter *adk.AsyncIterator[*adk.AgentEvent],
	ch chan<- StreamResult, pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string,
	uniqueTools map[string]struct{}, imgsBuf *[]string) (string, string, bool) {
	var sb strings.Builder
	var thinkingSb strings.Builder
```

In the streaming chunk loop, after the existing `m.Content` block (around line 540), add reasoning extraction:

```go
				if m.ReasoningContent != "" {
					ch <- StreamResult{ThinkingToken: m.ReasoningContent}
					thinkingSb.WriteString(m.ReasoningContent)
				}
				if m.Content == "" {
					continue
				}
				ch <- StreamResult{Token: m.Content}
				sb.WriteString(m.Content)
```

(Replace the existing `if m.Content == "" { continue }` + `ch <- StreamResult{Token: m.Content}` block with the above.)

In the non-streaming branch (`mo.Message != nil && len(mo.Message.ToolCalls) == 0`, around line 553):

```go
		} else if mo.Message != nil && len(mo.Message.ToolCalls) == 0 {
			if imgs := extractImages(mo.Message.AssistantGenMultiContent); len(imgs) > 0 {
				ch <- StreamResult{Images: imgs}
				*imgsBuf = append(*imgsBuf, imgs...)
			}
			if mo.Message.ReasoningContent != "" {
				ch <- StreamResult{ThinkingToken: mo.Message.ReasoningContent}
				thinkingSb.WriteString(mo.Message.ReasoningContent)
			}
			if mo.Message.Content != "" {
				ch <- StreamResult{Token: mo.Message.Content}
				sb.WriteString(mo.Message.Content)
			}
```

Update the return statement at the end:

```go
	return sb.String(), thinkingSb.String(), true
```

And the early returns (context cancel, interrupt):
```go
			return "", "", false   // context cancelled
			return "", "", false   // interrupt
```

- [ ] **Step 3: Update `drainIter` to propagate thinkingContent**

```go
func drainIter(ctx context.Context, runner *adk.Runner, iter *adk.AsyncIterator[*adk.AgentEvent],
	ch chan<- StreamResult, pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	uniqueTools := make(map[string]struct{})
	var imgs []string
	resp, thinking, ok := drainIterInner(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID, uniqueTools, &imgs)
	return resp, thinking, imgs, len(uniqueTools), ok
}
```

- [ ] **Step 4: Update `drainRunner` and `drainRunnerMsg` signatures**

```go
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	iter := runner.Query(ctx, query, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}

func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	iter := runner.Run(ctx, msgs, adk.WithCheckPointID(checkpointID))
	return drainIter(ctx, runner, iter, ch, pendingConfirms, emitEvent, checkpointID)
}
```

- [ ] **Step 5: Update all callers of `drainRunnerMsg` / `drainRunner`**

`Chat` method (line ~350) — add `thinkingContent` capture:
```go
		fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}
		ch <- StreamResult{Done: true}
		go a.persistAndMigrate(context.Background(), userInput, nil, nil, fullResponse, thinkingContent, toolImgs, toolCallCount)
```

`ChatDirect` method (line ~374) — `drainRunner` return now has 5 values; discard thinkingContent:
```go
		_, _, _, ok := drainRunner(ctx, a.runner, prompt, ch, nil, nil, fmt.Sprintf("direct-%d", time.Now().UnixNano()))
```

`ChatWithMessage` method (line ~747) — add `thinkingContent`:
```go
		fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
		if !ok {
			return
		}
		ch <- StreamResult{Done: true}
		// ...existing userMemory/userImages/userFiles extraction...
		go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse, thinkingContent, toolImgs, toolCallCount)
```

Also update the `handleInterrupt` recursive call inside `drainIterInner` (where `drainIterInner` is called recursively via `resumeIter`):
```go
			resp, thinkingResp, ok := drainIterInner(ctx, runner, resumeIter, ch, pendingConfirms, emitEvent, checkpointID, uniqueTools, imgsBuf)
			if !ok {
				return "", "", false
			}
			sb.WriteString(resp)
			thinkingSb.WriteString(thinkingResp)
```

- [ ] **Step 6: Update `persistAndMigrate` signature to accept thinkingContent**

```go
func (a *Agent) persistAndMigrate(ctx context.Context, userInput string, userImages []string, userFiles []string, assistantReply string, thinkingContent string, assistantImages []string, toolCallCount int) {
```

Inside the function, replace the `AddWithImages` call for assistant messages:
```go
	if _, err := a.shortMem.AddFull("assistant", assistantReply, thinkingContent, assistantImages, nil); err != nil {
		slog.Error("save assistant message failed", "err", err)
		return
	}
```

- [ ] **Step 7: Verify Go compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): extract ReasoningContent and emit ThinkingToken in stream results"
```

---

## Task 4: Wails app.go — emit `chat:thinking` event

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Add `chat:thinking` emit to all three send methods**

There are three send methods that have a stream consumption loop: `SendMessage`, `SendMessageWithImages`, `SendMessageWithFiles`. In each, find the stream loop `for result := range ch` (or equivalent) and add a branch for `ThinkingToken`.

The existing pattern in each loop looks like:
```go
if result.Err != nil { ... }
if result.Done { ... }
if len(result.Images) > 0 { ... }
// then: wailsruntime.EventsEmit(a.ctx, "chat:token", result.Token)
```

Add after `result.Done` check and before `result.Images` check in each method:
```go
			if result.ThinkingToken != "" {
				wailsruntime.EventsEmit(a.ctx, "chat:thinking", result.ThinkingToken)
				continue
			}
```

Do this for all three methods: `SendMessage` (~line 1200), `SendMessageWithImages` (~line where it loops), `SendMessageWithFiles` (~line where it loops).

- [ ] **Step 2: Verify Go compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add app.go
git commit -m "feat(app): emit chat:thinking event for LLM reasoning content"
```

---

## Task 5: Frontend — subscribe `chat:thinking`, update message objects and history

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Update `mapMsg` to include `thinkingContent` from history**

Currently at line 476:
```js
/** mapMsg converts a backend Message to the frontend shape. */
function mapMsg(m) {
  return {
    id: m.ID,
    role: m.Role,
    content: m.Content,
    thinkingContent: m.ThinkingContent ?? '',
    thinkingExpanded: false,
    time: m.CreatedAt,
    images: m.Images || [],
    files: m.Files || []
  }
}
```

- [ ] **Step 2: Add `offThinking` variable declaration alongside other `off*` variables**

Find the block where `offToken`, `offDone`, `offError` etc. are declared (they are `let` declarations). Add:
```js
let offThinking
```

- [ ] **Step 3: Subscribe to `chat:thinking` in `onMounted`**

Add alongside the existing `offToken = EventsOn('chat:token', ...)` subscription (around line 592):

```js
  offThinking = EventsOn('chat:thinking', (token) => {
    const last = messages.value[messages.value.length - 1]
    if (last && last.role === 'assistant') {
      if (last.thinkingContent === undefined) last.thinkingContent = ''
      last.thinkingContent += token
      last.thinkingExpanded = true
    }
  })
```

- [ ] **Step 4: Auto-collapse thinking on `chat:done`**

In the `offDone = EventsOn('chat:done', ...)` handler (around line 600), add collapsing after `typingScheduler.flush()`:

```js
  offDone = EventsOn('chat:done', () => {
    typingScheduler.flush()
    const idx = messages.value.length - 1
    if (idx >= 0) {
      const msg = messages.value[idx]
      if (msg.thinkingExpanded) {
        messages.value[idx] = { ...msg, thinkingExpanded: false }
      }
      messages.value[idx] = { ...messages.value[idx], streaming: false, time: new Date() }
    }
    // ...rest of existing handler unchanged...
```

- [ ] **Step 5: Unsubscribe `offThinking` in `onUnmounted`**

In the `onUnmounted` block (line 730), add `offThinking?.()` to the teardown line:
```js
  offToken?.(); offDone?.(); offError?.(); offClear?.(); offImage?.(); offThinking?.()
```

- [ ] **Step 6: Ensure new streaming assistant messages have `thinkingContent`**

There are several places that push a new assistant message object. Each needs `thinkingContent: ''` and `thinkingExpanded: false`. Update all `messages.value.push({ role: 'assistant', ...})` calls:

Line ~447 (`applyToken`):
```js
    messages.value.push({ role: 'assistant', content: token, streaming: true, isProactive: proactiveStarted, thinkingContent: '', thinkingExpanded: false })
```

Line ~569 (proactive start):
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, isProactive: true, thinkingContent: '', thinkingExpanded: false })
```

Line ~584 (cron start):
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, isCron: true, thinkingContent: '', thinkingExpanded: false })
```

Line ~865 (regenerate):
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, thinkingContent: '', thinkingExpanded: false })
```

Line ~1014 (send):
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, thinkingContent: '', thinkingExpanded: false })
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): subscribe chat:thinking, persist thinkingContent in message objects"
```

---

## Task 6: Frontend — ThinkingBlock UI and CSS

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Insert ThinkingBlock markup in the assistant bubble template**

In the template, find the `<template v-else>` that wraps the assistant bubble (line ~1170). Currently it renders either a `thinking-bubble` (dots) or the content bubble. Insert the ThinkingBlock **above** the content bubble, inside the `<template v-else>`:

```html
              <template v-else>
                <div v-if="m.thinking || (m.streaming && !renderMarkdown(m.content))" :class="['bubble', 'thinking-bubble', { proactive: m.isProactive }]">
                  <span class="dot" /><span class="dot" /><span class="dot" />
                </div>
                <!-- ThinkingBlock: shown when thinkingContent is non-empty -->
                <div v-if="m.thinkingContent" class="thinking-block">
                  <div class="thinking-block-header" @click="m.thinkingExpanded = !m.thinkingExpanded">
                    <svg class="thinking-chevron" :class="{ expanded: m.thinkingExpanded }" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                    <span>思考过程</span>
                  </div>
                  <div class="thinking-block-body" :class="{ expanded: m.thinkingExpanded }">
                    <div class="thinking-block-text">{{ m.thinkingContent }}<span v-if="m.streaming && m.thinkingExpanded" class="cursor">▋</span></div>
                  </div>
                </div>
                <div v-if="!m.thinking || m.thinkingContent" :class="['bubble', 'markdown', { proactive: m.isProactive }]" v-show="!m.thinking || m.content">
```

Note: the condition `v-if="!m.thinking || m.thinkingContent"` means: show the content bubble once thinking is done OR once we have thinking content (so content can stream alongside or after thinking). Adjust the existing `v-else` div to use `v-if` instead — replace the line:

```html
                <div v-else :class="['bubble', 'markdown', { proactive: m.isProactive }]">
```
with:
```html
                <div v-if="!m.thinking || m.content" :class="['bubble', 'markdown', { proactive: m.isProactive }]">
```

- [ ] **Step 2: Add ThinkingBlock CSS**

Add to the `<style>` section (near the end, before `</style>`):

```css
/* ThinkingBlock */
.thinking-block {
  width: 100%;
  max-width: 480px;
  margin-bottom: 6px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  overflow: hidden;
}

.thinking-block-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 10px;
  cursor: pointer;
  user-select: none;
  font-size: 0.78em;
  color: rgba(255, 255, 255, 0.5);
  transition: color 0.15s;
}

.thinking-block-header:hover {
  color: rgba(255, 255, 255, 0.75);
}

.thinking-chevron {
  transition: transform 0.2s ease;
  flex-shrink: 0;
}

.thinking-chevron.expanded {
  transform: rotate(180deg);
}

.thinking-block-body {
  max-height: 0;
  overflow: hidden;
  transition: max-height 0.25s ease;
}

.thinking-block-body.expanded {
  max-height: 400px;
  overflow-y: auto;
}

.thinking-block-text {
  padding: 0 10px 10px;
  font-size: 0.8em;
  color: rgba(255, 255, 255, 0.5);
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.5;
}
```

- [ ] **Step 3: Build frontend to verify no errors**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build
```
Expected: build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): add ThinkingBlock UI with collapsible thinking content display"
```

---

## Task 7: End-to-end smoke test

- [ ] **Step 1: Build and run the app**

```bash
cd /Users/xutiancheng/code/self/Aiko && make run
```

- [ ] **Step 2: Test with a reasoning model**

Configure a reasoning-capable model (e.g. DeepSeek-R1, QwQ, or any model that returns `reasoning_content` in the OpenAI-compatible stream). Send a question that triggers reasoning (e.g. "请解一道数学题：1+1=?").

Expected:
- While LLM thinks: thinking block appears at top of assistant bubble, text streams in, block is expanded
- When thinking ends and content starts streaming: thinking block auto-collapses
- After `chat:done`: content is complete, thinking block remains collapsed with "思考过程" header
- Clicking the header toggles expand/collapse

- [ ] **Step 3: Test history persistence**

Restart the app. The previous assistant message should show the thinking block collapsed (not expanded) with full thinking text accessible via click.

- [ ] **Step 4: Test with a non-reasoning model**

Configure a model that does NOT return `reasoning_content`. Send a message.

Expected: no thinking block appears in the assistant bubble; behavior is identical to before this feature.

- [ ] **Step 5: Final commit if any fixes were made**

```bash
git add -p
git commit -m "fix: address smoke test issues with thinking display"
```

---

## Self-Review

**Spec coverage check:**
- ✅ Stream `ReasoningContent` via new `chat:thinking` event — Tasks 3, 4
- ✅ DB `thinking_content` column — Tasks 1, 2
- ✅ Frontend streams thinking in real time — Task 5
- ✅ Auto-collapse on `chat:done` — Task 5 Step 4
- ✅ Collapsible block at top of assistant bubble — Task 6
- ✅ History messages show collapsed thinking block — Tasks 2, 5 Step 1
- ✅ Non-reasoning models unaffected — Task 7 Step 4

**Type consistency:**
- `ThinkingToken string` in `StreamResult` — defined Task 3 Step 1, consumed Task 4 Step 1
- `ThinkingContent string` in `memory.Message` — defined Task 2 Step 1, read in `mapMsg` Task 5 Step 1
- `AddFull(role, content, thinkingContent string, images, files []string)` — defined Task 2 Step 5, called in `persistAndMigrate` Task 3 Step 6
- `persistAndMigrate(..., thinkingContent string, ...)` — signature updated Task 3 Step 6, callers updated Task 3 Step 5
- `drainIter` return: `(string, string, []string, int, bool)` — defined Task 3 Step 3, all callers updated Task 3 Steps 4-5
- `thinkingContent`/`thinkingExpanded` on message objects — added Task 5 Steps 3-6, read in template Task 6 Step 1

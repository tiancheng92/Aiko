# LLM Thinking Process Display — Design Spec

**Date:** 2026-05-14  
**Status:** Approved

## Overview

Display the LLM's reasoning/thinking process in the chat panel. Thinking content streams in real time at the top of each assistant message bubble, auto-collapses when thinking ends, persists to the `messages` table, and is shown collapsed in historical messages.

## Background

eino's `schema.Message` already has a `ReasoningContent` field. The eino-ext OpenAI adapter (v0.1.17) populates this field from `choice.Delta.ReasoningContent` during streaming. Currently `drainIterInner` in `internal/agent/agent.go` ignores `m.ReasoningContent` entirely — thinking content is received from the LLM but silently discarded.

---

## Architecture

### Event Flow

```
LLM stream chunk
  m.ReasoningContent != "" → StreamResult{ThinkingToken: ...} → chat:thinking event → frontend
  m.Content != ""          → StreamResult{Token: ...}         → chat:token event   → frontend (unchanged)
chat:done                                                                           → auto-collapse thinking block
```

### Persistence Flow

```
drainIterInner → accumulates thinkingContent in thinkingSb
drainRunnerMsg → returns (content, thinkingContent, ...)
persistAndMigrate → ShortStore.AddFull(role, content, thinkingContent, images, files)
```

---

## Backend Changes

### 1. `internal/agent/agent.go`

**`StreamResult` struct** — add field:
```go
ThinkingToken string
```

**`drainIterInner`** — add `thinkingSb strings.Builder` alongside existing `sb`. In the streaming chunk loop, after the existing `m.Content` check, add:
```go
if m.ReasoningContent != "" {
    ch <- StreamResult{ThinkingToken: m.ReasoningContent}
    thinkingSb.WriteString(m.ReasoningContent)
}
```
Return signature: `(content string, thinkingContent string, ok bool)`.

Apply the same `ReasoningContent` extraction to the non-streaming (`mo.Message`) branch.

**`drainRunnerMsg`** — propagate `thinkingContent` return value up to callers.

**`persistAndMigrate`** — pass `thinkingContent` to `ShortStore.AddFull`.

### 2. `app.go`

In all three send methods (`SendMessage`, `SendMessageWithImages`, `SendMessageWithFiles`), add to the stream loop:
```go
case result.ThinkingToken != "":
    wailsruntime.EventsEmit(a.ctx, "chat:thinking", result.ThinkingToken)
```
No change to `chat:token`, `chat:done`, or `chat:error` handling.

---

## Database Changes

### 3. `internal/db/sqlite.go`

Add v8 patch to the `patches` slice:
```go
// v8: store LLM reasoning/thinking content alongside each assistant message.
`ALTER TABLE messages ADD COLUMN thinking_content TEXT NOT NULL DEFAULT ''`,
```

### 4. `internal/memory/short.go`

**`Message` struct** — add field:
```go
ThinkingContent string `json:"thinking_content"`
```

**`scanMessage`** — update SELECT column count to include `thinking_content` (7th column). All callers of `scanMessage` SELECT the same column list.

**All SELECT queries** — add `thinking_content` to column list:
- `Recent`
- `BeforeID`
- `OldestN`
- `LastUserMessage`
- `DeleteLastAssistantMessage` (raw scan, update manually)

**`AddFull`** — new lowest-level insert method:
```go
// AddFull inserts a new message with all optional fields and returns its ID.
func (s *ShortStore) AddFull(role, content, thinkingContent string, images []string, files []string) (int64, error)
```
`AddWithImagesAndFiles` delegates to `AddFull` with `thinkingContent: ""`.

---

## Frontend Changes (`frontend/src/components/ChatPanel.vue`)

### Message Object

Each assistant message gains two reactive fields:
- `thinkingContent: ''` — accumulated thinking text
- `thinkingExpanded: true` — true while streaming; `chat:done` sets to false; history initializes to false

### Event Handlers

```js
// In onMounted, alongside existing chat:token/chat:done subscriptions:
offThinking = EventsOn('chat:thinking', (token) => {
  const last = messages.value[messages.value.length - 1]
  if (last && last.role === 'assistant') {
    last.thinkingContent += token
    last.thinkingExpanded = true
  }
})
```
`offThinking?.()` called in `onUnmounted`.

On `chat:done`: set `last.thinkingExpanded = false`.

### History Loading

When `GetHistory` returns messages, for each assistant message:
```js
thinkingContent: m.ThinkingContent ?? '',
thinkingExpanded: false,
```

### ThinkingBlock UI Component (inline in ChatPanel or extracted)

Rendered at the **top** of each assistant bubble, only when `m.thinkingContent` is non-empty:

```
┌─────────────────────────────────────────┐
│ ▶ 思考过程                              │  ← clickable header
│ ─────────────────────────────────────── │
│  thinking text...  (when expanded)      │
└─────────────────────────────────────────┘
[normal message content below]
```

- Toggle via click on header row
- Collapse/expand with CSS `max-height` transition (e.g. `0` ↔ `300px`)
- During streaming: thinking text appends at bottom; cursor animation same as content streaming
- Style: `font-size: 0.85em`, `opacity: 0.75`, muted/secondary color to visually separate from main content
- Auto-collapse fires only once on `chat:done` (guard: only if currently expanded)

---

## Wails Event Table Addition

| Event | Direction | Meaning |
|-------|-----------|---------|
| `chat:thinking` | backend→frontend | Streaming thinking/reasoning token chunk |

---

## Out of Scope

- Sending thinking history back to LLM as context (eino handles `ReasoningContent` in subsequent turns internally)
- User preference to disable thinking display
- Per-message toggle to permanently hide thinking

---

## Key File Locations

| File | Change |
|------|--------|
| `internal/agent/agent.go` | `StreamResult`, `drainIterInner`, `drainRunnerMsg`, `persistAndMigrate` |
| `app.go` | `chat:thinking` emit in all 3 send methods |
| `internal/db/sqlite.go` | v8 migration patch |
| `internal/memory/short.go` | `Message.ThinkingContent`, `scanMessage`, all SELECTs, `AddFull` |
| `frontend/src/components/ChatPanel.vue` | `chat:thinking` event, message struct, ThinkingBlock UI, history load |

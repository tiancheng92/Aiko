# Memory Summary System — Design Spec

**Date:** 2026-05-29  
**Status:** Approved

## Overview

Add a LLM-powered conversation summary layer to Aiko's memory system. When the in-context short-term history grows too long (token limit) or hits the configured turn limit, older messages are compressed into a rolling summary. The summary is prepended to every LLM context so the agent retains continuity beyond the short-term window.

## Goals

1. Prevent context overflow when `MaxContextTokens` is exceeded — triggered **before** sending to LLM.
2. Maintain rolling summary across the full conversation lifetime.
3. Every summarisation also persists the compressed messages to long-term vector memory (existing `longMem.Store` path).
4. Summary is always prepended to the LLM context, alongside recent short-term messages.

## Non-Goals

- No UI for viewing/editing the summary.
- No per-session or per-profile summaries — one global summary per database.
- No change to the existing `ShortTermLimit`-based migration path other than also triggering summary there.

---

## Architecture

### New Files

- `internal/memory/summary.go` — `SummaryStore` struct + `Get`/`Set` methods
- (tests in `internal/memory/summary_test.go`)

### Modified Files

| File | Change |
|---|---|
| `internal/db/sqlite.go` | Add `summary` table; add v12 patch for `max_context_tokens` setting |
| `internal/config/config.go` | Add `MaxContextTokens int` field, default 10000 |
| `internal/agent/agent.go` | Add `summaryStore`, `chatModel` fields; extend `New()` signature |
| `internal/agent/context.go` | `buildContext`: call `checkAndSummarize`, fetch summary, insert into context |
| `app.go` | Pass `summaryStore` and `chatModel` to `agent.New()` |
| `frontend/src/components/SettingsWindow.vue` | Add `MaxContextTokens` input below short-term limit |

---

## Data Layer

### `summary` Table

```sql
CREATE TABLE IF NOT EXISTS summary (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    content    TEXT    NOT NULL DEFAULT '',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

Single-row table (enforced by `CHECK (id = 1)`). Always UPSERT on write.

### `SummaryStore` API

```go
type SummaryStore struct{ db *sql.DB }

func NewSummaryStore(db *sql.DB) *SummaryStore

// Get returns the current summary, or "" if none exists yet.
func (s *SummaryStore) Get() (string, error)

// Set overwrites the summary. Uses INSERT OR REPLACE to maintain the single row.
func (s *SummaryStore) Set(text string) error
```

---

## Config

```go
// MaxContextTokens is the estimated token threshold that triggers early summarisation
// before sending context to the LLM. 0 disables token-based triggering.
// Default: 10000.
MaxContextTokens int
```

Storage key: `max_context_tokens`. Loaded with `parseInt(m["max_context_tokens"], 10000)`.

---

## Summary Triggers

Two independent trigger paths:

### Path A — Token limit (pre-send, in `buildContext`)

1. `buildContext` calls `a.checkAndSummarize(ctx)` at the very start.
2. Load all unmigrated messages from `shortMem`.
3. Estimate tokens: `sum(len(msg.Content)) / 4`.
4. If `MaxContextTokens > 0` and estimated tokens > `MaxContextTokens`:
   - Take all messages **except** the most recent `ShortTermLimit/2` (keep those as live context).
   - Run `runSummary(ctx, oldMsgs)` — see below.
5. If `MaxContextTokens == 0`, skip.

### Path B — Turn limit (post-send, in `persistAndMigrate`)

Existing overflow logic already migrates `excess` messages to `longMem`. After the `longMem.Store` call (or instead of — they are now unified), also call `runSummary(ctx, oldest)` with the same `oldest` slice.

> **Note:** Path B runs asynchronously in a goroutine (same as current `persistAndMigrate` behaviour). Path A runs synchronously because the summary is needed before the LLM call.

---

## `runSummary` Function

```
func (a *Agent) runSummary(ctx context.Context, msgs []memory.Message) error
```

Steps (steps 1 and 2 run concurrently via errgroup):

1. **Long-term memory store**: `a.longMem.Store(ctx, memory.FormatBlock(msgs))` — identical to existing migration.
2. **LLM summarisation**:
   a. `prevSummary, _ = a.summaryStore.Get()`
   b. Build summarisation prompt (see below).
   c. Call `a.chatModel.Generate(ctx, promptMsgs)` — direct chat model call, no ReAct agent, no tool calls.
   d. `a.summaryStore.Set(newSummary)`
3. `a.shortMem.MarkMigrated(ids)` — after both concurrent steps succeed.

If `a.longMem == nil`, step 1 is skipped (not an error).

### Summarisation Prompt

```
System: You are a conversation memory manager. Compress the provided conversation history into a concise summary. Preserve key facts, decisions, and context. Write in third-person narrative. Be brief.

User:
[Previous summary]
{prevSummary}
[End of previous summary]

[New conversation to incorporate]
{FormatBlock(msgs)}
[End of new conversation]

Output only the updated summary, no preamble.
```

Uses `a.chatModel` (type `model.ToolCallingChatModel`, which satisfies `model.ChatModel`). Called via `chatModel.Generate(ctx, messages)` using the eino `schema.Message` slice directly.

---

## Context Assembly (`buildContext`)

Updated message order:

```
1. [User]      User Profile (static, cache-friendly)
2. [Assistant] "Understood."
3. [User]      Summary block            ← NEW (only if summaryText != "")
4. [Assistant] "Got it."                ← NEW pairing for summary block
5. [User/Asst] Short-term history messages (unmigrated, recent)
6. [User]      Dynamic context: long-term memories + knowledge base results
```

Summary block format:

```
[Conversation summary — compressed history from earlier turns:]
{summaryText}
[End of summary]
```

Implementation: `gatherContextSources` adds a concurrent `summaryStore.Get()` branch returning `summaryText string`. `buildContext` inserts the summary block after the User Profile exchange and before the history messages.

---

## Agent Struct Changes

```go
type Agent struct {
    // ... existing fields ...
    summaryStore *memory.SummaryStore  // NEW: nil-safe; nil means summary disabled
    chatModel    model.ToolCallingChatModel  // NEW: retained for summarisation LLM calls
}
```

`New()` gains two new parameters:
```go
func New(
    ctx context.Context,
    chatModel model.ToolCallingChatModel,
    shortMem *memory.ShortStore,
    longMem *memory.LongStore,
    summaryStore *memory.SummaryStore,  // NEW
    knowledgeSt *knowledge.Store,
    // ... rest unchanged
) (*Agent, error)
```

`chatModel` is also stored on the struct (currently passed only to `buildAgentRunner`, now retained).

---

## Frontend Changes

In `SettingsWindow.vue`, immediately below the **短期记忆轮数** row, add:

```
上下文 Token 上限
超过此 token 数时提前压缩对话历史（0 = 禁用）
[number input, default 10000, step 1000, min 0]
```

i18n keys:
- `settings.ai.maxContextTokens`
- `settings.ai.maxContextTokensDesc`

Bound to `form.MaxContextTokens` (same pattern as `ShortTermLimit`).

---

## Error Handling

- `checkAndSummarize` errors are **logged and non-fatal** — if summary fails, the request proceeds with the full (possibly over-limit) context rather than blocking the user.
- `runSummary` in Path B (post-send goroutine) logs errors; messages are only `MarkMigrated` if the full flow succeeds.
- `summaryStore.Get()` in `gatherContextSources` is non-fatal (log + return "").

---

## Token Estimation

```go
func estimateTokens(msgs []memory.Message) int {
    total := 0
    for i := range msgs {
        total += len(msgs[i].Content) / 4
    }
    return total
}
```

`len / 4` is a conservative approximation (safe for both English and CJK text — CJK characters are ~1 token each but counted as ~3 bytes / 4 = <1, so the estimate slightly under-counts CJK, meaning the threshold triggers a little late but never dangerously early).

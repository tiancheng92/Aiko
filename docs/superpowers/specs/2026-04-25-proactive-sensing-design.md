# Proactive Sensing Design

**Goal:** Aiko proactively sends contextually relevant messages at the right moment — morning/evening greetings on a schedule, and follow-up reminders derived from prior conversation — without polluting the memory system.

**Date:** 2026-04-25

---

## Overview

Aiko currently only responds when the user speaks first. This feature adds a `ProactiveEngine` that lets the pet initiate contact at the right moment: scheduled greetings (morning, evening) and conversation-derived follow-ups scheduled by the Agent itself via a `schedule_followup` tool.

All proactive messages are generated via `ChatDirect` — streamed, not saved to memory — preserving chat history integrity.

---

## Architecture

```
internal/proactive/
  engine.go       # ProactiveEngine: owns cron registration + DB + delivery
  store.go        # SQLite CRUD for proactive_items table
  tool.go         # schedule_followup internaltools.Tool implementation
```

`app.go` instantiates `ProactiveEngine` after the agent is initialized, injects it into the tool registry via `AllContextual()`, and calls `engine.Start()`.

---

## Data Model

New SQLite table managed by `internal/db/` migration:

```sql
CREATE TABLE IF NOT EXISTS proactive_items (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  trigger_at  DATETIME NOT NULL,
  prompt      TEXT NOT NULL,       -- system instruction for ChatDirect
  fired       BOOLEAN DEFAULT FALSE,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

`trigger_at` is UTC. Engine polls every minute (cron `* * * * *`) and fires any unfired rows with `trigger_at <= now`.

---

## ProactiveEngine (`engine.go`)

```go
type ProactiveEngine struct {
    app    AppInterface   // subset interface — ChatDirect + isChatVisible + emitEvent
    store  *ProactiveStore
    cron   *cron.Cron
    ctx    context.Context
}
```

**AppInterface** (defined in `engine.go`) exposes only what the engine needs:

```go
type AppInterface interface {
    ChatDirect(ctx context.Context, prompt string) error
    ChatDirectCollect(ctx context.Context, prompt string) (string, error)
    IsChatVisible() bool
    EmitEvent(name string, data any)
}
```

**Start()** registers two fixed jobs + one poll job:

| Job | Cron | Prompt key |
|-----|------|------------|
| Morning greeting | `0 9 * * *` | `greeting_morning` |
| Evening check-in | `0 21 * * *` | `greeting_evening` |
| Follow-up poll | `* * * * *` | (row-specific prompt) |

**fire(prompt string)** — shared delivery logic:

1. Call `app.ChatDirect(ctx, prompt)` — generates text, streams tokens
2. If `app.IsChatVisible()`: tokens arrive via existing `chat:token` / `chat:done` events → rendered in chat panel with a special `proactive: true` flag so it's visually distinguishable (subtle left-border accent, no user bubble)
3. If chat closed: collect full text from `ChatDirect` result, emit `notification:show`

**Greeting prompts** are Go string constants in `engine.go`:

```go
const greetingMorningPrompt = `你是桌面宠物 Aiko，现在是早上，主动向用户发一句温暖简短的早安问候（1-2句话，自然随意，不要过于正式）。不要提及你是AI。`
const greetingEveningPrompt = `你是桌面宠物 Aiko，现在是晚上，主动向用户发一句轻松的晚间问候（1-2句话）。可以关心今天过得怎样。不要提及你是AI。`
```

---

## schedule_followup Tool (`tool.go`)

Registered in `AllContextual()`. Agent calls this when it detects a follow-up worth scheduling.

**Input schema:**

```json
{
  "when": "2026-04-26T09:00:00",   // ISO 8601, local time
  "message": "帮用户跟进一下昨天提到的面试准备情况"
}
```

**Behavior:**
1. Parse `when` → convert to UTC
2. Validate: must be in the future, max 30 days out
3. Insert row into `proactive_items`
4. Return: `"已安排：将在 <human-readable time> 提醒你"`

The Agent can call this at any point during a conversation when it detects a future commitment, a task the user mentioned, or a natural follow-up opportunity.

---

## ChatDirect Streaming vs Notification

**When chat is open** (`isChatVisible == true`):
- `ChatDirect` streams tokens via `chat:token` events as usual
- Frontend adds a proactive message bubble — same rendering path, no new code needed
- A small visual cue distinguishes it: left border accent color (CSS class `proactive`)

**When chat is closed** (`isChatVisible == false`):
- Engine checks `IsChatVisible()` before calling `ChatDirect`
- If closed: use a variant `ChatDirectCollect(ctx, prompt) (string, error)` that buffers all tokens internally and returns the full text (no Wails events emitted)
- Truncate to 80 chars + "…" if longer, emit `notification:show`
- `ChatDirectCollect` is a new method on `agent.go` — same logic as `ChatDirect` but writes tokens to a `strings.Builder` instead of emitting Wails events

**isChatVisible tracking** — `app.go` already handles `bubble:toggle` event; add `IsChatVisible() bool` method exposing the existing `isChatVisible` field (protected by `a.mu`).

---

## Frontend Changes

Minimal. ChatPanel needs to handle proactive messages arriving when chat is open:

- `chat:token` / `chat:done` already work — proactive messages arrive on the same events
- Add CSS class `.proactive` on assistant bubbles that have `proactive: true` metadata
- Engine sets a flag before streaming: emit `chat:proactive:start` → frontend sets `currentBubbleIsProactive = true` → clears on `chat:done`

No new Vue composables needed.

---

## DB Migration

Add to `internal/db/migrations.go` (or equivalent migration file):

```go
{
    Version: <next_version>,
    SQL: `CREATE TABLE IF NOT EXISTS proactive_items (
        id         INTEGER PRIMARY KEY AUTOINCREMENT,
        trigger_at DATETIME NOT NULL,
        prompt     TEXT NOT NULL,
        fired      BOOLEAN DEFAULT FALSE,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );`,
},
```

---

## App Integration (`app.go`)

```go
// New field
proactiveEngine *proactive.ProactiveEngine

// In initLLMComponents, after agent is built:
a.proactiveEngine = proactive.New(a, a.store)
a.proactiveEngine.Start()

// IsChatVisible — new method
func (a *App) IsChatVisible() bool {
    a.mu.RLock()
    defer a.mu.RUnlock()
    return a.isChatVisible
}

// EmitEvent — thin wrapper
func (a *App) EmitEvent(name string, data any) {
    runtime.EventsEmit(a.ctx, name, data)
}
```

`isChatVisible` is already tracked in `app.go` via `bubble:toggle` listener — only need to expose it via the interface.

---

## Out of Scope

- Calendar / email monitoring (separate feature, not this spec)
- User-facing UI to view/delete scheduled follow-ups (can be added later)
- Sound effects for proactive messages (existing sound system already handles `chat:token` / `chat:done`)
- Multiple concurrent proactive messages (engine fires one at a time; if another fires while streaming, it waits)

---

## Testing

- `ProactiveStore`: unit test insert/query/mark-fired
- `schedule_followup` tool: test validation (past time rejected, >30 days rejected, valid inserts row)
- `ProactiveEngine.fire()`: mock `AppInterface`, verify `ChatDirect` called with correct prompt; verify `notification:show` emitted when `IsChatVisible()` returns false

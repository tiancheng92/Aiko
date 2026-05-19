# Chat Message Options Design

**Date**: 2026-05-19  
**Feature**: Per-message thinking level, knowledge base, and long-term memory toggles in ChatPanel

---

## Overview

Add three persistent user-preference controls to the ChatPanel input toolbar (left side of the toolbar row):

1. **Thinking level** — controls LLM reasoning effort per message
2. **Use knowledge base** — whether to include knowledge base search results in context
3. **Use long-term memory** — whether to include long-term memory retrieval in context

Settings are persisted to SQLite and restored on next session. They are **not reset after send** — they stay at whatever the user last set.

---

## UI Design

### Layout

The three chips occupy the currently-empty left side of the input toolbar:

```
[ 🧠 默认 ]  [ 📚 知识库 ]  [ 💾 记忆 ]            🔗  📤
```

Knowledge base and memory chips are **only shown when `VectorEnabled()` is true** (embedding model configured).

### Thinking Level Chip

Cycles through available levels on click. Available levels depend on current provider:

- **OpenAI**: 默认 → 低 → 中 → 高 → 默认（4 levels, no "关闭"）
- **OpenRouter**: 默认 → 关闭 → 低 → 中 → 高 → 默认（5 levels）

Color coding: 默认 gray, 关闭 gray, 低 green, 中 yellow, 高 orange.

### Knowledge Base / Memory Chips

Simple toggle. Active = highlighted, inactive = gray. Clicking toggles and immediately persists.

---

## Config Changes

### `internal/config/config.go`

Add three fields to `Config` struct:

```go
ThinkingLevel string // "default" | "off" | "low" | "medium" | "high", default "default"
UseKnowledge  bool   // default true
UseMemory     bool   // default true
```

Persisted via existing `settings` key-value table:
- `thinking_level`
- `use_knowledge`  
- `use_memory`

---

## Backend Changes

### `app_chat.go` — New `ChatOptions` struct

```go
type ChatOptions struct {
    ThinkingLevel string // "default" | "off" | "low" | "medium" | "high"
    UseKnowledge  bool
    UseMemory     bool
}
```

All three `SendMessage*` methods gain a final `opts ChatOptions` parameter:
- `SendMessage(userInput string, opts ChatOptions)`
- `SendMessageWithImages(userInput string, images []string, opts ChatOptions)`
- `SendMessageWithFiles(userInput string, images []string, files []FileAttachment, opts ChatOptions)`

Each method passes `opts` through to `streamChat` → `agent.Chat`.

### `internal/agent/chat.go`

`Chat(ctx, userInput, opts ChatOptions)` — opts threaded through to:
1. `buildContext` (for knowledge/memory flags)
2. `runner.Query` / `runner.Run` (for thinking level option)

### `internal/agent/context.go`

`gatherContextSources(ctx, userInput string, useKnowledge, useMemory bool)`:
- `useKnowledge=false` → skip `knowledgeSt` semantic search
- `useMemory=false` → skip `longMem.SearchSplit`

No other changes to context building.

### `internal/llm/` — New `ReasoningOption` helper

New function `ReasoningOption(level, provider string) model.Option`:

| Level | OpenAI | OpenRouter |
|---|---|---|
| `"default"` | nil (don't pass) | nil (don't pass) |
| `"off"` | nil (not applicable) | `WithReasoning(&Reasoning{Effort: EffortOfNone})` |
| `"low"` | `WithReasoningEffort(ReasoningEffortLevelLow)` | `WithReasoning(&Reasoning{Effort: EffortOfLow})` |
| `"medium"` | `WithReasoningEffort(ReasoningEffortLevelMedium)` | `WithReasoning(&Reasoning{Effort: EffortOfMedium})` |
| `"high"` | `WithReasoningEffort(ReasoningEffortLevelHigh)` | `WithReasoning(&Reasoning{Effort: EffortOfHigh})` |

Provider detection: same logic already used in `NewChatModel` (BaseURL pattern matching OpenRouter vs OpenAI-compatible).

The returned option (if non-nil) is passed to `runner.Query/Run` via `compose.WithChatModelOption(opt)`.

**Note**: For OpenAI, `"default"` (not passing the parameter) means the model uses its own default. For reasoning-capable models (o-series), that default is typically `medium`. This is an API-level limitation — there is no way to explicitly disable reasoning for OpenAI o-series models via this API.

---

## Frontend Changes

### `ChatPanel.vue`

**State** (loaded from `GetConfig()` on mount):
```js
const thinkingLevel = ref('default')  // from cfg.ThinkingLevel
const useKnowledge = ref(true)         // from cfg.UseKnowledge
const useMemory = ref(true)            // from cfg.UseMemory
const isOpenRouter = computed(() => /* detect from cfg.LLMBaseURL */)
const availableLevels = computed(() =>
  isOpenRouter.value
    ? ['default', 'off', 'low', 'medium', 'high']
    : ['default', 'low', 'medium', 'high']
)
```

**Chip click handlers** — update ref and call `SaveConfig` with updated values immediately.

**Send** — pass current chip state as `ChatOptions` to whichever `SendMessage*` variant is used.

**Chip visibility** — knowledge and memory chips wrapped in `v-if="cfg.VectorEnabled"` (expose `VectorEnabled` via `GetConfig` or a dedicated binding).

---

## Data Flow Summary

```
User clicks chip
  → update ref
  → SaveConfig({ ThinkingLevel, UseKnowledge, UseMemory, ...rest })

User sends message
  → SendMessageWithFiles(text, images, files, { ThinkingLevel, UseKnowledge, UseMemory })
  → streamChat(text, images, files, opts)
  → agent.Chat(ctx, userInput, opts)
       ├─ buildContext(ctx, userInput, opts.UseKnowledge, opts.UseMemory)
       │    └─ gatherContextSources skips KB/memory as requested
       └─ runner.Query(ctx, query,
              compose.WithChatModelOption(ReasoningOption(opts.ThinkingLevel, provider)),
              adk.WithCheckPointID(checkpointID))
```

---

## Constraints & Notes

- `VectorEnabled()` check: knowledge/memory chips hidden (not just disabled) when embeddings not configured
- OpenRouter provider detection reuses the same BaseURL pattern-matching logic already in `internal/llm/client.go`
- After `wails generate module`, the updated `SendMessage*` signatures produce new TypeScript bindings in `wailsjs/` — do not edit those manually
- The "off" thinking level is silently treated as "default" for OpenAI (no error, no warning needed — it simply won't be reachable from the UI for OpenAI users)

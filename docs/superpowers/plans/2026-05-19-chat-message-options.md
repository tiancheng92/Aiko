# Chat Message Options Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-message thinking level, knowledge base toggle, and long-term memory toggle chips to the ChatPanel input toolbar, persisted to SQLite.

**Architecture:** Three new `Config` fields (`ThinkingLevel`, `UseKnowledge`, `UseMemory`) flow from SQLite → Go config → `ChatOptions` struct → agent layer, where thinking level maps to eino provider-specific reasoning options and knowledge/memory flags gate context gathering. The frontend loads initial state from `GetConfig`, updates chips with immediate `SaveConfig` calls, and passes options on every send.

**Tech Stack:** Go (eino adk/openai/openrouter ACL), Vue 3 `<script setup>`, Wails v2 bindings, SQLite settings table.

---

## File Map

| File | Change |
|---|---|
| `internal/config/config.go` | Add 3 fields to `Config`, update `Load` and `Save` |
| `internal/llm/reasoning.go` | **New** — `ReasoningOption(level, provider string) model.Option` |
| `internal/agent/chat.go` | Add `ChatOptions` struct; update `Chat` + `ChatWithMessage` signatures |
| `internal/agent/context.go` | Add `useKnowledge`, `useMemory` params to `gatherContextSources` + `buildContext` |
| `internal/agent/drain.go` | Thread reasoning option through `drainRunner` / `drainRunnerMsg` / `runner.Query` / `runner.Run` |
| `app_chat.go` | Add `ChatOptions`; update `SendMessage`, `SendMessageWithImages`, `SendMessageWithFiles` |
| `frontend/src/components/ChatPanel.vue` | Add chip state, chip UI, send integration |

---

## Task 1: Add Config Fields

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add fields to the `Config` struct** (after `UserAvatar` on line 52)

```go
// ThinkingLevel controls the LLM reasoning effort per message.
// Values: "default" | "off" | "low" | "medium" | "high". Default "default".
ThinkingLevel string
// UseKnowledge controls whether knowledge base results are included in context.
UseKnowledge bool
// UseMemory controls whether long-term memory results are included in context.
UseMemory bool
```

- [ ] **Step 2: Update `Load` to read the three new settings** (after line 122 `cfg.UserAvatar = m["user_avatar"]`)

```go
cfg.ThinkingLevel = orDefault(m["thinking_level"], "default")
cfg.UseKnowledge = m["use_knowledge"] != "false" // default true
cfg.UseMemory = m["use_memory"] != "false"        // default true
```

- [ ] **Step 3: Update `Save` to persist the three new settings** (add to the `pairs` map, after `"user_avatar"` entry on line 163)

```go
"thinking_level": cfg.ThinkingLevel,
"use_knowledge":  strconv.FormatBool(cfg.UseKnowledge),
"use_memory":     strconv.FormatBool(cfg.UseMemory),
```

- [ ] **Step 4: Verify Go compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add ThinkingLevel, UseKnowledge, UseMemory settings"
```

---

## Task 2: Add ReasoningOption Helper

**Files:**
- Create: `internal/llm/reasoning.go`

- [ ] **Step 1: Create the file**

```go
package llm

import (
	einoopenai "github.com/cloudwego/eino-ext/libs/acl/openai"
	einoopenrouter "github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino/components/model"

	"aiko/internal/config"
)

// ReasoningOption translates a UI thinking level ("default"|"off"|"low"|"medium"|"high")
// into a provider-specific eino model.Option. Returns nil when no option should be passed
// (level is "default", or level is "off" for OpenAI which has no disable mechanism).
func ReasoningOption(level string, provider config.Provider) model.Option {
	switch provider {
	case config.ProviderOpenRouter:
		switch level {
		case "off":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfNone,
			})
		case "low":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfLow,
			})
		case "medium":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfMedium,
			})
		case "high":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfHigh,
			})
		}
	default: // openai-compatible
		switch level {
		case "low":
			return einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelLow)
		case "medium":
			return einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelMedium)
		case "high":
			return einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelHigh)
		}
	}
	// "default" or "off" for OpenAI: don't pass any option.
	return nil
}
```

- [ ] **Step 2: Verify Go compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/llm/reasoning.go
git commit -m "feat(llm): add ReasoningOption helper for per-message thinking level"
```

---

## Task 3: Update Agent Context Gathering

**Files:**
- Modify: `internal/agent/context.go`

- [ ] **Step 1: Update `gatherContextSources` signature** (line 51) to accept `useKnowledge`, `useMemory bool`

Change:
```go
func (a *Agent) gatherContextSources(ctx context.Context, userInput string) (
```
To:
```go
func (a *Agent) gatherContextSources(ctx context.Context, userInput string, useKnowledge, useMemory bool) (
```

- [ ] **Step 2: Gate long-term memory search with `useMemory`** (the goroutine starting at line 65)

Change:
```go
	g.Go(func() error {
		if a.longMem == nil {
			return nil
		}
		res, err := a.longMem.SearchSplit(gctx, userInput, 5)
```
To:
```go
	g.Go(func() error {
		if a.longMem == nil || !useMemory {
			return nil
		}
		res, err := a.longMem.SearchSplit(gctx, userInput, 5)
```

- [ ] **Step 3: Gate knowledge base search with `useKnowledge`**

Find the goroutine in `gatherContextSources` that calls `knowledgeSt` (if present). Add `!useKnowledge` guard in the same pattern. If knowledge search is done inside `buildContext` instead, apply the guard there.

> Check: run `grep -n "knowledgeSt\|knowledge" internal/agent/context.go` to locate the exact line, then add `|| !useKnowledge` guard.

- [ ] **Step 4: Update `buildContext` signature** (line 108) to accept and thread through the new params

Change:
```go
func (a *Agent) buildContext(ctx context.Context, userInput string) ([]adk.Message, error) {
	profile, memResult, recentMsgs, location, err := a.gatherContextSources(ctx, userInput)
```
To:
```go
func (a *Agent) buildContext(ctx context.Context, userInput string, useKnowledge, useMemory bool) ([]adk.Message, error) {
	profile, memResult, recentMsgs, location, err := a.gatherContextSources(ctx, userInput, useKnowledge, useMemory)
```

- [ ] **Step 5: Verify Go compiles** (will fail until chat.go callers are updated — that's expected)

```bash
go build ./... 2>&1 | grep -v "too many arguments\|not enough arguments" | head -20
```

---

## Task 4: Add ChatOptions and Update Agent Chat Methods

**Files:**
- Modify: `internal/agent/chat.go`

- [ ] **Step 1: Add `ChatOptions` struct** (before the `SetSkillHint` function, at the top of chat.go after the imports)

```go
// ChatOptions carries per-message overrides for the agent turn.
type ChatOptions struct {
	ThinkingLevel string // "default" | "off" | "low" | "medium" | "high"
	UseKnowledge  bool
	UseMemory     bool
}
```

- [ ] **Step 2: Update `Chat` signature** (line 125) and thread options through

Change:
```go
func (a *Agent) Chat(ctx context.Context, userInput string) <-chan StreamResult {
```
To:
```go
func (a *Agent) Chat(ctx context.Context, userInput string, opts ChatOptions) <-chan StreamResult {
```

Inside `Chat`, update the `buildContext` call (line 136):
```go
ctxMsgs, err := a.buildContext(ctx, userInput, opts.UseKnowledge, opts.UseMemory)
```

Update the `drainRunnerMsg` call (line 165) to pass opts:
```go
fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID, opts.ThinkingLevel, a.cfg.LLMProvider)
```

- [ ] **Step 3: Find and update `ChatWithMessage`** — locate it, update signature and calls the same way

```bash
grep -n "func (a \*Agent) ChatWithMessage" internal/agent/chat.go
```

Add `opts ChatOptions` parameter, pass `opts.UseKnowledge, opts.UseMemory` to `buildContext`, and `opts.ThinkingLevel, a.cfg.LLMProvider` to `drainRunnerMsg`.

- [ ] **Step 4: Verify Go compiles** (drain.go still needs updating)

```bash
go build ./... 2>&1 | head -20
```

---

## Task 5: Thread Reasoning Option Through drain.go

**Files:**
- Modify: `internal/agent/drain.go`

- [ ] **Step 1: Update `drainRunnerMsg` signature** to accept `thinkingLevel, provider string`

Change:
```go
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	iter := runner.Run(ctx, msgs, adk.WithCheckPointID(checkpointID))
```
To:
```go
func drainRunnerMsg(ctx context.Context, runner *adk.Runner, msgs []adk.Message, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID, thinkingLevel, provider string) (string, string, []string, int, bool) {
	var runOpts []adk.AgentRunOption
	runOpts = append(runOpts, adk.WithCheckPointID(checkpointID))
	if opt := llm.ReasoningOption(thinkingLevel, config.Provider(provider)); opt != nil {
		runOpts = append(runOpts, compose.WithChatModelOption(opt))
	}
	iter := runner.Run(ctx, msgs, runOpts...)
```

- [ ] **Step 2: Add missing imports to drain.go**

Add to the import block:
```go
"github.com/cloudwego/eino/compose"
"aiko/internal/config"
"aiko/internal/llm"
```

- [ ] **Step 3: Update `drainRunner` signature** the same way (for `runner.Query` path used by `ChatDirect`)

Change:
```go
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID string) (string, string, []string, int, bool) {
	iter := runner.Query(ctx, query, adk.WithCheckPointID(checkpointID))
```
To:
```go
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
	pendingConfirms *sync.Map, emitEvent func(string, ...any), checkpointID, thinkingLevel, provider string) (string, string, []string, int, bool) {
	var runOpts []adk.AgentRunOption
	runOpts = append(runOpts, adk.WithCheckPointID(checkpointID))
	if opt := llm.ReasoningOption(thinkingLevel, config.Provider(provider)); opt != nil {
		runOpts = append(runOpts, compose.WithChatModelOption(opt))
	}
	iter := runner.Query(ctx, query, runOpts...)
```

- [ ] **Step 4: Update all internal callers of `drainRunner`** in drain.go and chat.go

Find all calls:
```bash
grep -n "drainRunner\b" internal/agent/
```

For `ChatDirect` and `ChatDirectCollect` (which don't have opts), pass `"default"` and `a.cfg.LLMProvider` as the last two args — these paths never need to override thinking level.

- [ ] **Step 5: Verify Go compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/chat.go internal/agent/context.go internal/agent/drain.go internal/llm/reasoning.go
git commit -m "feat(agent): thread ChatOptions (thinking level, knowledge, memory) through chat pipeline"
```

---

## Task 6: Update Wails Bindings in app_chat.go

**Files:**
- Modify: `app_chat.go`

- [ ] **Step 1: Add `ChatOptions` struct** to app_chat.go (alongside `FileAttachment`, after line 28)

```go
// ChatOptions carries per-message inference options from the frontend.
type ChatOptions struct {
	ThinkingLevel string `json:"thinkingLevel"` // "default"|"off"|"low"|"medium"|"high"
	UseKnowledge  bool   `json:"useKnowledge"`
	UseMemory     bool   `json:"useMemory"`
}
```

- [ ] **Step 2: Update `SendMessage`** (line 148)

Change:
```go
func (a *App) SendMessage(userInput string) error {
	return a.streamChat(func(ctx context.Context, ag *agent.Agent) <-chan agent.StreamResult {
		return ag.Chat(ctx, userInput)
	})
}
```
To:
```go
func (a *App) SendMessage(userInput string, opts ChatOptions) error {
	agOpts := agent.ChatOptions{
		ThinkingLevel: opts.ThinkingLevel,
		UseKnowledge:  opts.UseKnowledge,
		UseMemory:     opts.UseMemory,
	}
	return a.streamChat(func(ctx context.Context, ag *agent.Agent) <-chan agent.StreamResult {
		return ag.Chat(ctx, userInput, agOpts)
	})
}
```

- [ ] **Step 3: Update `SendMessageWithImages`** — find the function and add `opts ChatOptions` as last parameter, convert and pass `agent.ChatOptions` to `ag.ChatWithMessage`

```bash
grep -n "func (a \*App) SendMessageWithImages" app_chat.go
```

Add `opts ChatOptions` parameter. Inside, update the `ag.ChatWithMessage(ctx, msg, userInput)` call to `ag.ChatWithMessage(ctx, msg, userInput, agent.ChatOptions{ThinkingLevel: opts.ThinkingLevel, UseKnowledge: opts.UseKnowledge, UseMemory: opts.UseMemory})`.

- [ ] **Step 4: Update `SendMessageWithFiles`** — same pattern as `SendMessageWithImages`

```bash
grep -n "func (a \*App) SendMessageWithFiles" app_chat.go
```

- [ ] **Step 5: Verify Go compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Regenerate Wails bindings**

```bash
wails generate module
```

Expected: `frontend/src/wailsjs/go/main/App.js` updated with new signatures for `SendMessage`, `SendMessageWithImages`, `SendMessageWithFiles`.

- [ ] **Step 7: Commit**

```bash
git add app_chat.go frontend/src/wailsjs/
git commit -m "feat(app): add ChatOptions to SendMessage* Wails bindings"
```

---

## Task 7: Frontend — Chip State and Logic

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Add chip state refs** in the `<script setup>` section (near other reactive state at the top)

```js
// Per-message inference options (persisted to config)
const thinkingLevel = ref('default')
const useKnowledge = ref(true)
const useMemory = ref(true)
```

- [ ] **Step 2: Load initial state from config** in the existing config loading section (wherever `cfg` is loaded from `GetConfig`)

```js
// Inside the onMounted or wherever GetConfig() result is handled:
thinkingLevel.value = cfg.ThinkingLevel || 'default'
useKnowledge.value = cfg.UseKnowledge !== false
useMemory.value = cfg.UseMemory !== false
```

- [ ] **Step 3: Add computed for available thinking levels and current provider**

```js
const isOpenRouter = computed(() => {
  return cfg?.LLMProvider === 'openrouter'
})

const thinkingLevels = computed(() =>
  isOpenRouter.value
    ? ['default', 'off', 'low', 'medium', 'high']
    : ['default', 'low', 'medium', 'high']
)

const thinkingLevelLabel = computed(() => {
  const labels = { default: '默认', off: '关闭', low: '低', medium: '中', high: '高' }
  return labels[thinkingLevel.value] || '默认'
})

const thinkingLevelColor = computed(() => {
  const colors = { default: '', off: '', low: 'green', medium: 'yellow', high: 'orange' }
  return colors[thinkingLevel.value] || ''
})
```

- [ ] **Step 4: Add chip toggle handlers**

```js
/** Cycles thinking level through available levels and persists. */
async function cycleThinkingLevel() {
  const levels = thinkingLevels.value
  const idx = levels.indexOf(thinkingLevel.value)
  thinkingLevel.value = levels[(idx + 1) % levels.length]
  await persistChatOptions()
}

/** Toggles knowledge base flag and persists. */
async function toggleKnowledge() {
  useKnowledge.value = !useKnowledge.value
  await persistChatOptions()
}

/** Toggles long-term memory flag and persists. */
async function toggleMemory() {
  useMemory.value = !useMemory.value
  await persistChatOptions()
}

/** Saves the three chat option fields to config. */
async function persistChatOptions() {
  try {
    await SaveConfig({ ...cfg, ThinkingLevel: thinkingLevel.value, UseKnowledge: useKnowledge.value, UseMemory: useMemory.value })
  } catch (e) {
    console.error('persistChatOptions failed', e)
  }
}
```

- [ ] **Step 5: Update the `send()` function** to pass `ChatOptions` to each `SendMessage*` call

Find the existing `send()` function. Wherever it calls `SendMessage(...)`, `SendMessageWithImages(...)`, or `SendMessageWithFiles(...)`, add the options object as the last argument:

```js
const chatOpts = { thinkingLevel: thinkingLevel.value, useKnowledge: useKnowledge.value, useMemory: useMemory.value }

// e.g.:
await SendMessage(input, chatOpts)
// or:
await SendMessageWithImages(input, images, chatOpts)
// or:
await SendMessageWithFiles(input, images, files, chatOpts)
```

- [ ] **Step 6: Verify no JS errors**

```bash
cd frontend && yarn build 2>&1 | tail -20
```

Expected: build succeeds, no type errors about SendMessage signatures.

---

## Task 8: Frontend — Chip UI

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Add chip HTML to the input toolbar** — in the `.input-toolbar` div, replace the existing `<div style="flex:1"/>` spacer with:

```html
<div class="chat-opts-chips">
  <button
    class="chat-opt-chip"
    :class="['thinking-' + thinkingLevel]"
    @click="cycleThinkingLevel"
    title="思考等级"
  >🧠 {{ thinkingLevelLabel }}</button>
  <button
    v-if="cfg?.EmbeddingModel"
    class="chat-opt-chip"
    :class="{ active: useKnowledge }"
    @click="toggleKnowledge"
    title="本次是否检索知识库"
  >📚 知识库</button>
  <button
    v-if="cfg?.EmbeddingModel"
    class="chat-opt-chip"
    :class="{ active: useMemory }"
    @click="toggleMemory"
    title="本次是否检索长期记忆"
  >💾 记忆</button>
</div>
```

- [ ] **Step 2: Add CSS** for the chips in the `<style scoped>` section

```css
.chat-opts-chips {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 1;
}

.chat-opt-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 8px;
  border-radius: 12px;
  font-size: 11px;
  cursor: pointer;
  border: 1px solid rgba(255,255,255,0.12);
  background: rgba(255,255,255,0.06);
  color: rgba(255,255,255,0.45);
  transition: background 0.15s, color 0.15s, border-color 0.15s;
  user-select: none;
  white-space: nowrap;
}

.chat-opt-chip:hover {
  background: rgba(255,255,255,0.1);
  color: rgba(255,255,255,0.7);
}

.chat-opt-chip.active {
  background: rgba(255,255,255,0.12);
  color: rgba(255,255,255,0.85);
  border-color: rgba(255,255,255,0.25);
}

.chat-opt-chip.thinking-low  { color: #4ade80; border-color: rgba(74,222,128,0.3); }
.chat-opt-chip.thinking-medium { color: #facc15; border-color: rgba(250,204,21,0.3); }
.chat-opt-chip.thinking-high { color: #fb923c; border-color: rgba(251,146,60,0.3); }
```

- [ ] **Step 3: Test the UI** — run `make run`, open the chat panel, verify:
  - Three chips appear in the left toolbar area
  - Clicking 🧠 cycles through levels (4 for OpenAI, 5 for OpenRouter)
  - Clicking 📚/💾 toggles active state
  - Colors change as expected
  - Knowledge/memory chips are hidden when no embedding model is set

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): add thinking level and context chips to ChatPanel toolbar"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Covered by task |
|---|---|
| Three chip UI in left toolbar | Task 8 |
| Chips cycle/toggle, persist immediately | Task 7 |
| ThinkingLevel, UseKnowledge, UseMemory in Config | Task 1 |
| Default: ThinkingLevel="default", both bool=true | Task 1 |
| 4 levels for OpenAI, 5 for OpenRouter | Task 7 (thinkingLevels computed) |
| ReasoningOption maps level to eino option | Task 2 |
| gatherContextSources respects useKnowledge/useMemory | Task 3 |
| ChatOptions threaded through agent chain | Tasks 4+5 |
| Wails bindings updated, regenerated | Task 6 |
| Knowledge/memory chips hidden without embeddings | Task 8 (v-if cfg.EmbeddingModel) |
| "off" silently treated as "default" for OpenAI | Task 2 (ReasoningOption returns nil) |

All spec requirements covered. No TBD/TODO placeholders present.

# Streaming Token Animation Design

## Goal

Add per-token pop animation (A2) and a rainbow pulse cursor (C) to the AI chat bubble during LLM streaming, without breaking markdown rendering.

## Architecture

### Problem

The current assistant bubble renders content as `v-html="renderMarkdown(m.content)"` — a single HTML replacement on every token. Individual tokens are not addressable DOM nodes, so per-token CSS animation is impossible with this approach.

### Solution: Two-Lane Rendering

Split the assistant bubble content into two rendering layers:

| Lane | Field | Description |
|------|-------|-------------|
| Settled | `m.displayHtml` | Markdown-rendered HTML; updated every 500 ms or every 40 tokens |
| Pending | `m.pendingTokens` | `Array<{ text: string, key: number }>` — tokens since last settle; each rendered as an animated `<span>` |

```
LLM stream → applyToken() → append to m.content (source of truth)
                          → push { text, key } to m.pendingTokens
                          → if pendingTokens.length >= 40: settle()
settle() → m.displayHtml = renderMarkdown(m.content)
         → m.pendingTokens = []
setInterval 500ms → settle() on last streaming message
chat:done → flush tokens → settle() immediately
```

Because `pendingTokens` entries have ever-increasing unique `key` values, each new entry is a fresh DOM node — Vue assigns it a new element and the CSS animation triggers on mount.

### Settle Behaviour

- **Interval**: Every 500 ms a `setInterval` settles the last streaming message (if any pending tokens exist).
- **Threshold**: Also settle immediately when `pendingTokens.length >= 40`, to bound DOM node count.
- **On done**: `chat:done` handler calls `settleMessage(idx)` after `typingScheduler.flush()`.
- **Transient markdown artifacts**: Tokens that span a markdown boundary (e.g. `**bo` then `ld**`) render as raw text in the pending lane and correctly render as bold after the next settle. This is a minor, brief visual artefact acceptable at 500 ms cadence.

## Message Object Changes

Add three fields to every new assistant message:

```js
{
  role: 'assistant',
  content: '',             // existing — source of truth full text
  displayHtml: '',         // NEW — settled rendered HTML
  pendingTokens: [],       // NEW — Array<{ text: string, key: number }>
  tokenKeyCounter: 0,      // NEW — monotonic key for pendingTokens entries
  streaming: true,
  // ... all existing fields unchanged
}
```

These fields are only meaningful on assistant messages; user messages and system messages are unaffected.

## Component Changes — ChatPanel.vue

### Script

**`applyToken`** — append new tokens to both `content` and `pendingTokens`:
```js
function applyToken(token) {
  // existing thinking-block transition logic unchanged ...

  const idx = messages.value.length - 1
  const last = messages.value[idx]
  if (last && last.role === 'assistant' && last.streaming) {
    last.content += token
    last.pendingTokens.push({ text: token, key: last.tokenKeyCounter++ })
    if (last.pendingTokens.length >= 40) settleMessage(idx)
  } else {
    messages.value.push({
      role: 'assistant', content: token, streaming: true,
      displayHtml: '', pendingTokens: [{ text: token, key: 0 }], tokenKeyCounter: 1,
      isProactive: proactiveStarted, thinkingContent: '', thinkingExpanded: false,
    })
    EventsEmit('pet:state:change', 'speaking')
  }
  scrollToBottom()
}
```

**`settleMessage(idx)`** — new helper:
```js
function settleMessage(idx) {
  const msg = messages.value[idx]
  if (!msg || !msg.pendingTokens?.length) return
  msg.displayHtml = renderMarkdown(msg.content)
  msg.pendingTokens = []
}
```

**Settle interval** — registered in `onMounted`, cleared in `onUnmounted`:
```js
let settleIntervalId = null
onMounted(() => {
  settleIntervalId = setInterval(() => {
    const idx = messages.value.findLastIndex(m => m.streaming)
    if (idx >= 0) settleMessage(idx)
  }, 500)
})
onUnmounted(() => clearInterval(settleIntervalId))
```

**`chat:done` handler** — add immediate settle after flush:
```js
offDone = EventsOn('chat:done', () => {
  typingScheduler.flush()
  const idx = messages.value.length - 1
  if (idx >= 0) {
    settleMessage(idx)                          // NEW — settle pending tokens
    const fadingKey = msgKey(messages.value[idx], idx)
    streamingFading.add(fadingKey)
    setTimeout(() => streamingFading.delete(fadingKey), 700)
    messages.value[idx] = { ...messages.value[idx], streaming: false, thinkingExpanded: false, time: new Date() }
  }
  // ... rest unchanged
})
```

### Template

Replace the existing assistant content `v-html` with two-lane rendering. The cursor moves out of the `v-html` string and into a dedicated `<span>`.

**Before:**
```html
<div v-if="m.content" v-html="renderMarkdown(m.content) + (m.streaming ? '<span class=\'cursor\'>▋</span>' : '')"></div>
```

**After (inside the assistant `.bubble.markdown`):**
```html
<div v-if="m.displayHtml || (!m.streaming && m.content)"
     v-html="m.displayHtml || renderMarkdown(m.content)"></div>
<template v-if="m.pendingTokens && m.pendingTokens.length">
  <span v-for="tok in m.pendingTokens" :key="tok.key" class="token-word">{{ tok.text }}</span>
</template>
<span v-if="m.streaming" class="stream-cursor">▋</span>
```

Note: The fallback `renderMarkdown(m.content)` in the `v-html` binding handles historical messages loaded from SQLite that have no `displayHtml`.

### CSS (scoped)

```css
@keyframes token-word-appear {
  from { opacity: 0; transform: scale(0.88) translateY(2px); }
  to   { opacity: 1; transform: scale(1)    translateY(0);   }
}
.token-word {
  display: inline;
  animation: token-word-appear 0.12s cubic-bezier(0.34, 1.4, 0.64, 1) both;
}

@keyframes cursor-color {
  0%   { color: rgba(80,  200, 255, 0.95); filter: drop-shadow(0 0 5px rgba(80,  200, 255, 0.65)); }
  25%  { color: rgba(160, 100, 255, 0.95); filter: drop-shadow(0 0 5px rgba(160, 100, 255, 0.60)); }
  50%  { color: rgba(240, 80,  200, 0.95); filter: drop-shadow(0 0 5px rgba(240, 80,  200, 0.60)); }
  75%  { color: rgba(255, 160, 60,  0.95); filter: drop-shadow(0 0 5px rgba(255, 160, 60,  0.50)); }
  100% { color: rgba(80,  200, 255, 0.95); filter: drop-shadow(0 0 5px rgba(80,  200, 255, 0.65)); }
}
@keyframes cursor-pulse {
  0%, 100% { transform: scaleY(1);    opacity: 0.9; }
  50%      { transform: scaleY(0.65); opacity: 0.5; }
}
.stream-cursor {
  display: inline-block;
  vertical-align: middle;
  margin-left: 1px;
  animation:
    cursor-color 2s linear infinite,
    cursor-pulse 0.8s ease-in-out infinite;
}
@media (prefers-reduced-motion: reduce) {
  .token-word   { animation: none; }
  .stream-cursor { animation: none; color: rgba(255,255,255,0.7); }
}
```

Remove the existing `.cursor` rule (now unused).

## What Is Not Changed

- `useTypingScheduler.js` — unchanged; punctuation pauses and batching remain as-is
- User messages, system messages — unchanged
- Historical messages (loaded from DB) — `displayHtml` will be `''`, fallback to `renderMarkdown(m.content)` renders them correctly with no animation
- The shimmer border / fade-out effects — unaffected

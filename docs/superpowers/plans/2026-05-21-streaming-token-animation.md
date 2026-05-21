# Streaming Token Animation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-token pop animation (A2) and a rainbow pulse cursor (C) to the AI chat bubble during LLM streaming.

**Architecture:** Introduce two-lane rendering on assistant messages: a `displayHtml` field (settled markdown HTML, updated every 500 ms or every 40 tokens) and a `pendingTokens` array (raw token spans rendered with a CSS pop animation). A `settleMessage()` helper flushes pending tokens into `displayHtml`. The old `▋` cursor injected via `v-html` is replaced with a standalone `<span class="stream-cursor">` carrying a rainbow keyframe animation.

**Tech Stack:** Vue 3 `<script setup>`, CSS `@keyframes`, `setInterval` / `clearInterval`.

---

## File Map

| File | Change |
|---|---|
| `frontend/src/components/ChatPanel.vue` | All changes — script logic, template, CSS |

---

### Task 1: Add `settleMessage` helper and settle interval

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue` (script section)

**Context:**  
`onMounted` is at line 544. `onUnmounted` is at line 845. There is already a module-level `let` block for event off-handles. Add a `let settleIntervalId = null` alongside them, then wire up the interval in `onMounted` / `onUnmounted`.

- [ ] **Step 1: Add `settleIntervalId` declaration**

Find the line:
```js
const streamingFading = reactive(new Set())
```
Add immediately after it:
```js
let settleIntervalId = null
```

- [ ] **Step 2: Add `settleMessage` helper**

Find the line:
```js
/** applyToken appends a token to the last streaming assistant message. */
```
Add the helper immediately before it:
```js
/** settleMessage flushes pendingTokens into displayHtml for the message at idx. */
function settleMessage(idx) {
  const msg = messages.value[idx]
  if (!msg || !msg.pendingTokens?.length) return
  msg.displayHtml = renderMarkdown(msg.content)
  msg.pendingTokens = []
}
```

- [ ] **Step 3: Start settle interval in `onMounted`**

Find the end of `onMounted` setup (around line 563, after the sentinel observer block). Add before `onMounted`'s closing `}`... actually find the last statement inside `onMounted` and add after it:
```js
  settleIntervalId = setInterval(() => {
    const idx = messages.value.findLastIndex(m => m.streaming)
    if (idx >= 0) settleMessage(idx)
  }, 500)
```

- [ ] **Step 4: Clear interval in `onUnmounted`**

Find in `onUnmounted`:
```js
  springCancels.forEach(cancel => cancel())
  springCancels.clear()
```
Add immediately after:
```js
  clearInterval(settleIntervalId)
```

- [ ] **Step 5: Verify no runtime error**

Run: `cd frontend && yarn build 2>&1 | tail -20`
Expected: build succeeds, no errors about `settleMessage` or `settleIntervalId`.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(chat): add settleMessage helper and 500ms settle interval"
```

---

### Task 2: Update `applyToken` for two-lane rendering

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue` (script — `applyToken` function, lines ~412–437)

**Context:**  
Current `applyToken` (line 413–437) does `last.content += token`. We need to also push to `pendingTokens` and trigger an immediate settle at the 40-token threshold. New assistant messages also need `displayHtml`, `pendingTokens`, and `tokenKeyCounter` fields.

The thinking-block transition path (lines 418–426) creates a new message object via spread — it needs the new fields too. The `chat:proactive:start` handler at line 596 and `chat:cron:start` at line 611 also push new assistant message objects — those need the new fields too.

- [ ] **Step 1: Replace `applyToken` body**

Replace the entire function (lines 412–437) with:
```js
/** applyToken appends a token to the last streaming assistant message. */
function applyToken(token) {
  // Transition the thinking placeholder on first real token.
  const thinkIdx = messages.value.findLastIndex(m => m.thinking)
  if (thinkIdx >= 0) {
    if (messages.value[thinkIdx].thinkingContent) {
      messages.value[thinkIdx] = {
        ...messages.value[thinkIdx],
        thinking: false,
        content: token,
        displayHtml: '',
        pendingTokens: [{ text: token, key: 0 }],
        tokenKeyCounter: 1,
      }
      scrollToBottom()
      return
    }
    messages.value.splice(thinkIdx, 1)
  }

  const idx = messages.value.length - 1
  const last = messages.value[idx]
  if (last && last.role === 'assistant' && last.streaming) {
    last.content += token
    last.pendingTokens.push({ text: token, key: last.tokenKeyCounter++ })
    if (last.pendingTokens.length >= 40) settleMessage(idx)
  } else {
    messages.value.push({
      role: 'assistant',
      content: token,
      streaming: true,
      isProactive: proactiveStarted,
      thinkingContent: '',
      thinkingExpanded: false,
      displayHtml: '',
      pendingTokens: [{ text: token, key: 0 }],
      tokenKeyCounter: 1,
    })
    EventsEmit('pet:state:change', 'speaking')
  }
  scrollToBottom()
}
```

- [ ] **Step 2: Add new fields to `chat:proactive:start` push**

Find (line ~596):
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, isProactive: true, thinkingContent: '', thinkingExpanded: false })
```
Replace with:
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, isProactive: true, thinkingContent: '', thinkingExpanded: false, displayHtml: '', pendingTokens: [], tokenKeyCounter: 0 })
```

- [ ] **Step 3: Add new fields to `chat:cron:start` push**

Find (line ~611):
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, isCron: true, thinkingContent: '', thinkingExpanded: false })
```
Replace with:
```js
    messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, isCron: true, thinkingContent: '', thinkingExpanded: false, displayHtml: '', pendingTokens: [], tokenKeyCounter: 0 })
```

- [ ] **Step 4: Build check**

Run: `cd frontend && yarn build 2>&1 | tail -20`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(chat): two-lane rendering in applyToken with pendingTokens"
```

---

### Task 3: Settle before `chat:done` clears streaming

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue` (script — `chat:done` handler, lines ~638–646)

**Context:**  
The `chat:done` handler (line 638) currently calls `typingScheduler.flush()` then immediately spreads the message with `streaming: false`. We must call `settleMessage(idx)` between flush and the spread so `displayHtml` is fully populated and `pendingTokens` is empty before the `streaming` flag drops (otherwise the two-lane template will briefly show stale `pendingTokens` spans on a non-streaming message).

- [ ] **Step 1: Add `settleMessage` call in `chat:done`**

Find:
```js
  offDone = EventsOn('chat:done', () => {
    typingScheduler.flush()
    const idx = messages.value.length - 1
    const lastMsg = messages.value[idx]
    if (idx >= 0) {
      const fadingKey = msgKey(messages.value[idx], idx)
```
Replace with:
```js
  offDone = EventsOn('chat:done', () => {
    typingScheduler.flush()
    const idx = messages.value.length - 1
    const lastMsg = messages.value[idx]
    if (idx >= 0) {
      settleMessage(idx)
      const fadingKey = msgKey(messages.value[idx], idx)
```

- [ ] **Step 2: Build check**

Run: `cd frontend && yarn build 2>&1 | tail -20`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(chat): settle pending tokens on chat:done before clearing streaming flag"
```

---

### Task 4: Update template to two-lane rendering + stream-cursor

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue` (template — line ~1562)

**Context:**  
The current line rendering AI content is (line ~1562):
```html
<div v-if="m.content || m.streaming" v-html="renderMarkdown(m.content) + (m.streaming ? '<span class=\'cursor\'>▋</span>' : '')" />
```

This needs to become three elements:
1. A `<div v-html>` for settled HTML (with fallback to `renderMarkdown` for historical messages that have no `displayHtml`)
2. A `<template v-for>` for animated pending token spans
3. A standalone `<span class="stream-cursor">` for the cursor

- [ ] **Step 1: Replace the content rendering line**

Find exactly:
```html
                  <div v-if="m.content || m.streaming" v-html="renderMarkdown(m.content) + (m.streaming ? '<span class=\'cursor\'>▋</span>' : '')" />
```
Replace with:
```html
                  <div v-if="m.displayHtml || (!m.streaming && m.content)" v-html="m.displayHtml || renderMarkdown(m.content)" />
                  <template v-if="m.pendingTokens && m.pendingTokens.length">
                    <span v-for="tok in m.pendingTokens" :key="tok.key" class="token-word">{{ tok.text }}</span>
                  </template>
                  <span v-if="m.streaming" class="stream-cursor">▋</span>
```

- [ ] **Step 2: Build check**

Run: `cd frontend && yarn build 2>&1 | tail -20`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(chat): two-lane template with token-word spans and stream-cursor"
```

---

### Task 5: Add CSS — token-word animation and stream-cursor

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue` (scoped `<style scoped>`)

**Context:**  
The existing `.cursor` rule is at line ~2420:
```css
.cursor { animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }
```
This class is no longer used in the template (replaced by `.stream-cursor`). Remove it and replace with the new rules. Add `@keyframes token-word-appear` as a global keyframe in the non-scoped `<style>` block (line ~3746) alongside the existing `shimmer-spin` and `shimmer-fadeout` keyframes — scoped keyframes can be unreliable with `animation: ... both` fill mode across browsers.

- [ ] **Step 1: Add `token-word-appear` keyframe to the global `<style>` block**

Find in the non-scoped `<style>` block:
```css
@keyframes shimmer-fadeout {
```
Add immediately before it:
```css
@keyframes token-word-appear {
  from { opacity: 0; transform: scale(0.88) translateY(2px); }
  to   { opacity: 1; transform: scale(1)    translateY(0);   }
}

```

- [ ] **Step 2: Replace `.cursor` rule with new rules in scoped `<style scoped>`**

Find:
```css
/* Cursor blink */
.cursor { animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }
```
Replace with:
```css
/* Per-token pop animation */
.token-word {
  display: inline;
  animation: token-word-appear 0.12s cubic-bezier(0.34, 1.4, 0.64, 1) both;
}

/* Rainbow pulse cursor — replaces old .cursor */
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
  .token-word    { animation: none; }
  .stream-cursor { animation: none; color: rgba(255, 255, 255, 0.7); }
}
```

- [ ] **Step 3: Build check**

Run: `cd frontend && yarn build 2>&1 | tail -20`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(chat): add token-word pop animation and rainbow stream-cursor CSS"
```

---

### Task 6: Manual verification

**Context:**  
There are no automated tests for CSS animations. Verify by running the app and observing the streaming behavior live.

- [ ] **Step 1: Build and run**

```bash
make run
```

- [ ] **Step 2: Send a message and observe streaming**

Send any message. While the AI is responding, verify:
- Each new token appears with a brief pop (scale + fade-in from below)
- The cursor `▋` at the end cycles through: blue → purple → pink → orange → back to blue, with a vertical pulse
- Every ~500 ms the pending token spans disappear and are replaced by settled markdown HTML (the text itself doesn't visually change, only the DOM structure settles)

- [ ] **Step 3: Verify completion**

After the AI finishes responding:
- The shimmer border fade-out plays (from previous feature)
- No dangling `▋` cursor remains
- The text is fully readable markdown (code blocks, bold, etc. render correctly)

- [ ] **Step 4: Verify historical messages**

Close and reopen the chat panel. Old messages should display correctly — no broken layout, no missing text. (Historical messages have no `displayHtml`, so they fall back to `renderMarkdown(m.content)`.)

- [ ] **Step 5: Commit (if any last-minute fixes were needed)**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "fix(chat): streaming token animation post-verification fixes"
```
(Skip this commit if no fixes were needed.)

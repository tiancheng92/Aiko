# LLM Latency Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a real-time `● Nms` latency badge in the ChatBubble title bar, pinging the active model's Base URL every 5 seconds via a Go backend method.

**Architecture:** A new `PingLLM() int64` Wails binding does an HTTP HEAD to `cfg.LLMBaseURL` (4s timeout) and returns elapsed ms or -1 on failure. ChatBubble polls it every 5s with `setInterval`, renders a colored dot + ms value between the title and the fullscreen button, and resets immediately when the active model profile changes.

**Tech Stack:** Go `net/http`, Wails v2 bindings, Vue 3 Composition API (`ref`, `onMounted`, `onUnmounted`)

---

## File Map

| File | Change |
|------|--------|
| `app.go` | Add `PingLLM() int64` method |
| `frontend/wailsjs/go/main/App.js` | Add `PingLLM` binding (auto-generated, manual update until next `wails generate module`) |
| `frontend/wailsjs/go/main/App.d.ts` | Add `PingLLM` type declaration |
| `frontend/src/components/ChatBubble.vue` | Add latency badge to title bar + polling logic |

---

## Task 1: Backend — `PingLLM()` method

**Files:**
- Modify: `app.go` (after `LarkRunCommand`, line ~1732)

- [ ] **Step 1: Add `PingLLM` method to `app.go`**

Insert after the closing brace of `LarkRunCommand` (line 1732):

```go
// PingLLM measures the round-trip latency to the active model provider's
// Base URL by issuing an HTTP HEAD request with a 4-second timeout.
// Returns elapsed milliseconds, or -1 on any error (empty URL, timeout, etc.).
func (a *App) PingLLM() int64 {
	a.mu.RLock()
	baseURL := a.cfg.LLMBaseURL
	a.mu.RUnlock()

	if baseURL == "" {
		return -1
	}

	client := &http.Client{Timeout: 4 * time.Second}
	start := time.Now()
	resp, err := client.Head(baseURL)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return -1
	}
	resp.Body.Close()
	return elapsed
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add app.go
git commit -m "feat: add PingLLM() Wails binding for latency measurement"
```

---

## Task 2: Wails Bindings — expose `PingLLM` to frontend

**Files:**
- Modify: `frontend/wailsjs/go/main/App.js`
- Modify: `frontend/wailsjs/go/main/App.d.ts`

- [ ] **Step 1: Add to `App.js`**

Open `frontend/wailsjs/go/main/App.js`. Add the following export alongside the other exported functions (keep alphabetical order near the `P` section):

```js
export function PingLLM() {
  return window['go']['main']['App']['PingLLM']();
}
```

- [ ] **Step 2: Add to `App.d.ts`**

Open `frontend/wailsjs/go/main/App.d.ts`. Add alongside other declarations:

```ts
export function PingLLM(): Promise<number>;
```

- [ ] **Step 3: Commit**

```bash
git add frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts
git commit -m "chore: add PingLLM Wails binding stub"
```

---

## Task 3: Frontend — latency badge in ChatBubble title bar

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue`

### 3a: Script — polling logic

- [ ] **Step 1: Add `PingLLM` import**

In the `<script setup>` section, find the existing import from `../../wailsjs/go/main/App`:

```js
import { ExportChatHistory, GetChatSize, SaveChatSize } from '../../wailsjs/go/main/App'
```

Replace with:

```js
import { ExportChatHistory, GetChatSize, PingLLM, SaveChatSize } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: Add latency state and helper**

After the existing `const emit = defineEmits(...)` line, add:

```js
const latencyMs = ref(null)  // null = not yet measured, -1 = error, ≥0 = ms

/** latencyColor returns the dot/text color for the current latency value. */
function latencyColor(ms) {
  if (ms === null || ms < 0) return 'rgba(255,255,255,0.25)'
  if (ms < 300) return '#4ade80'
  if (ms <= 800) return '#facc15'
  return '#f87171'
}

/** latencyLabel returns the display text for the current latency value. */
function latencyLabel(ms) {
  if (ms === null || ms < 0) return '—'
  return ms + 'ms'
}
```

- [ ] **Step 3: Add polling in lifecycle hooks**

Find the `onMounted` block. At the top of `onMounted`, before the existing `try { const [w, h] = await GetChatSize(...)` line, add:

```js
let latencyTimer = null
let offModelChangedLatency = null
```

These must be declared **outside** `onMounted` (at the top of `<script setup>`, after `latencyMs`):

```js
let latencyTimer = null
let offModelChangedLatency = null
```

Inside `onMounted`, after the existing `offSizeChange = EventsOn(...)` lines, add:

```js
  const pingOnce = () => PingLLM().then(ms => { latencyMs.value = ms }).catch(() => { latencyMs.value = -1 })
  pingOnce()
  latencyTimer = setInterval(pingOnce, 5000)
  offModelChangedLatency = EventsOn('config:model:changed', pingOnce)
```

- [ ] **Step 4: Clean up in `onUnmounted`**

Find the `onUnmounted` block. Add cleanup alongside the existing `offSizeChange?.()` calls:

```js
  clearInterval(latencyTimer)
  offModelChangedLatency?.()
```

### 3b: Template — badge markup

- [ ] **Step 5: Insert badge in title bar**

Find the title bar template (around line 141):

```html
<div class="title-bar">
  <span class="title">聊天</span>
  <button class="icon-btn" @click="toggleFullscreen" ...>
```

Replace with:

```html
<div class="title-bar">
  <span class="title">聊天</span>
  <div class="latency-badge" :style="{ color: latencyColor(latencyMs) }">
    <span class="latency-dot">●</span>
    <span class="latency-value">{{ latencyLabel(latencyMs) }}</span>
  </div>
  <button class="icon-btn" @click="toggleFullscreen" ...>
```

### 3c: Style — badge CSS

- [ ] **Step 6: Add badge styles**

In the `<style scoped>` block, after the `.title` rule, add:

```css
.latency-badge {
  display: flex;
  align-items: center;
  gap: 3px;
  margin-left: auto;
  font-size: 11px;
  font-weight: 500;
  opacity: 0.85;
  transition: color 0.4s;
  white-space: nowrap;
  user-select: none;
}
.latency-dot {
  font-size: 8px;
  line-height: 1;
}
.latency-value {
  line-height: 1;
  min-width: 28px;
}
```

Also update `.title` to remove `flex: 1` (the badge now acts as the spacer via `margin-left: auto`):

```css
.title {
  color: rgba(255, 255, 255, 0.85);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
```

- [ ] **Step 7: Build frontend to verify no errors**

```bash
cd frontend && yarn build
```

Expected: build succeeds with no errors.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/ChatBubble.vue
git commit -m "feat: add LLM latency indicator to ChatBubble title bar"
```

---

## Task 4: Smoke test

- [ ] **Step 1: Run the app**

```bash
make run
```

- [ ] **Step 2: Verify badge appears**

Open the chat bubble. The title bar should show:
```
聊天  ● 42ms   [fullscreen] [✕]
```
Dot and text color should be green (< 300ms), yellow (300–800ms), or red (> 800ms / error).

- [ ] **Step 3: Verify profile switch resets badge**

In Settings → Model Profiles, switch to a different profile. The badge should update within ~1 second (immediate `pingOnce` triggered by `config:model:changed`).

- [ ] **Step 4: Verify error state**

Temporarily set Base URL to an invalid value in Settings, save. Badge should show `● —` in red within 5 seconds.

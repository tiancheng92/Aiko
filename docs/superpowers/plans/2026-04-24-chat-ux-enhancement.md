# Chat UX Enhancement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three independent chat UX improvements: D1 streaming interrupt with ghost bubbles, D3 cute sound effects, D4 typing rhythm with punctuation pauses and speed jitter.

**Architecture:** D1 adds a per-request cancel context in `app.go` and a `StopGeneration()` Wails binding; ghost bubbles are pure UI state never persisted. D3 is a `useSounds.js` composable using Web Audio API oscillator synthesis (no files needed). D4 is a `useTypingScheduler.js` composable that queues tokens with variable delays before applying them to the message list. All three modules are independent — each can be disabled without touching the others.

**Tech Stack:** Go `context.WithCancel`, Wails v2 events, Vue 3 `<script setup>`, Web Audio API (`AudioContext`, `OscillatorNode`, `GainNode`), `setTimeout` queue.

---

## File Structure

| File | Change |
|---|---|
| `app.go` | Add `chatCancel context.CancelFunc` to `App` struct; modify `SendMessage` to use per-request cancel ctx; add `StopGeneration()`; add `SoundsEnabled` guard in `SaveConfig` |
| `internal/config/config.go` | Add `SoundsEnabled bool` field; Load/Save `sounds_enabled` key |
| `frontend/src/composables/useSounds.js` | New: Web Audio API tone synthesis for send/receive/error |
| `frontend/src/composables/useTypingScheduler.js` | New: token queue with punctuation pauses + speed jitter |
| `frontend/src/components/ChatPanel.vue` | Add `isStreaming` ref; Stop button; ghost bubble rendering; integrate `useSounds` and `useTypingScheduler`; import `StopGeneration` |
| `frontend/src/components/SettingsWindow.vue` | Add sounds toggle (same pattern as `toggleVoiceAutoSend`) |

---

### Task 1: Go backend — chatCancel + StopGeneration + SoundsEnabled config

**Files:**
- Modify: `app.go` (App struct ~line 39, SendMessage ~line 521, SaveConfig ~line 292, after IsSMSWatcherRunning ~line 917)
- Modify: `internal/config/config.go` (Config struct ~line 10, Load ~line 56, Save ~line 82)

- [ ] **Step 1: Add `SoundsEnabled` to Config struct**

In `internal/config/config.go`, after the `VoiceAutoSend` line (~line 23):

```go
SoundsEnabled       bool   // 是否启用聊天音效
```

- [ ] **Step 2: Add Load for SoundsEnabled**

In the `Load()` function, after `cfg.VoiceAutoSend = m["voice_auto_send"] == "true"` (~line 77):

```go
cfg.SoundsEnabled = m["sounds_enabled"] == "true"
```

- [ ] **Step 3: Add Save for SoundsEnabled**

In the `Save()` function `pairs` map, after `"voice_auto_send": strconv.FormatBool(cfg.VoiceAutoSend),` (~line 104):

```go
"sounds_enabled": strconv.FormatBool(cfg.SoundsEnabled),
```

- [ ] **Step 4: Add `chatCancel` to App struct**

In `app.go`, in the `App` struct after `smsWatcher *sms.Watcher` (~line 57):

```go
chatCancel   context.CancelFunc // cancels the current in-flight SendMessage; guarded by mu
```

- [ ] **Step 5: Modify SendMessage to use per-request cancel context**

Replace the existing `SendMessage` function (~lines 519–547) with:

```go
// SendMessage sends a user message and streams response tokens as Wails events.
// Events emitted: "chat:token" (string), "chat:done" (""), "chat:error" (string).
// Any in-flight request is cancelled before starting the new one.
func (a *App) SendMessage(userInput string) error {
	// Cancel any previous in-flight request.
	a.mu.Lock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
	chatCtx, cancel := context.WithCancel(a.ctx)
	a.chatCancel = cancel
	a.mu.Unlock()

	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()

	if ag == nil {
		a.mu.Lock()
		a.chatCancel = nil
		a.mu.Unlock()
		cancel()
		slog.Error("SendMessage: petAgent is nil", "input", userInput)
		return fmt.Errorf("agent not initialized: complete settings first")
	}
	go func() {
		defer func() {
			a.mu.Lock()
			// Only clear chatCancel if it's still ours (a newer call may have replaced it).
			if a.chatCancel != nil {
				a.chatCancel = nil
			}
			a.mu.Unlock()
		}()
		ch := ag.Chat(chatCtx, userInput)
		for result := range ch {
			if result.Err != nil {
				wailsruntime.EventsEmit(a.ctx, "chat:error", result.Err.Error())
				return
			}
			if result.Done {
				wailsruntime.EventsEmit(a.ctx, "chat:done", "")
				return
			}
			wailsruntime.EventsEmit(a.ctx, "chat:token", result.Token)
		}
		// Fallback: ensure frontend unblocks if channel closes without a terminal result.
		wailsruntime.EventsEmit(a.ctx, "chat:done", "")
	}()
	return nil
}
```

- [ ] **Step 6: Add StopGeneration method**

After `IsSMSWatcherRunning` (~line 916), add:

```go
// StopGeneration cancels the current in-flight chat stream.
// The frontend is responsible for marking the interrupted messages as ghost bubbles.
func (a *App) StopGeneration() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
}
```

- [ ] **Step 7: Guard SoundsEnabled in SaveConfig**

In `SaveConfig` (~line 292), after `cfg.VoiceAutoSend = a.cfg.VoiceAutoSend`:

```go
cfg.SoundsEnabled = a.cfg.SoundsEnabled
```

- [ ] **Step 8: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 9: Regenerate Wails bindings**

```bash
wails generate module
```

Expected: `frontend/wailsjs/go/main/App.js` and `App.d.ts` now include `StopGeneration`.

- [ ] **Step 10: Commit**

```bash
git add app.go internal/config/config.go frontend/wailsjs/
git commit -m "feat(backend): add StopGeneration, chatCancel context, SoundsEnabled config"
```

---

### Task 2: Frontend D1 — Stop button and ghost bubbles

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

The current `send()` button is `<button @click="send" :disabled="loading">发送</button>`. We replace the input-row with a conditional Stop/Send button, add `isStreaming` state, and render ghost bubbles at reduced opacity.

- [ ] **Step 1: Add StopGeneration import**

In `ChatPanel.vue`, find the imports line (~line 3):

```js
import { SendMessage, GetMessages, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend } from '../../wailsjs/go/main/App'
```

Replace with:

```js
import { SendMessage, GetMessages, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend, StopGeneration } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: Add isStreaming ref**

After `const voiceAutoSend = ref(false)` (~line 94), add:

```js
const isStreaming = ref(false)
```

- [ ] **Step 3: Set isStreaming in send()**

In `send()` (~line 247), after `loading.value = true`, add:

```js
isStreaming.value = true
```

In the `catch` block of `send()`, after `loading.value = false`, add:

```js
isStreaming.value = false
```

- [ ] **Step 4: Clear isStreaming on chat:done and chat:error**

In the `offDone` handler (~line 154), after `loading.value = false`:

```js
isStreaming.value = false
```

In the `offError` handler (~line 161), after `loading.value = false`:

```js
isStreaming.value = false
```

- [ ] **Step 5: Add stopGeneration function**

After the `send()` function (~line 265), add:

```js
/** stopGeneration cancels the current in-flight AI response and marks the interrupted
 *  messages as ghost bubbles (visual only — not persisted, not sent to LLM context). */
async function stopGeneration() {
  try {
    await StopGeneration()
  } catch (e) {
    console.warn('StopGeneration failed:', e)
  }
  isStreaming.value = false
  loading.value = false

  // Mark the last user message and last assistant message (thinking or streaming) as ghost.
  const lastUser = messages.value.findLastIndex(m => m.role === 'user' && !m.ghost)
  if (lastUser >= 0) messages.value[lastUser] = { ...messages.value[lastUser], ghost: true }

  const lastAssistant = messages.value.findLastIndex(m => m.role === 'assistant' && !m.ghost)
  if (lastAssistant >= 0) {
    messages.value[lastAssistant] = {
      ...messages.value[lastAssistant],
      ghost: true,
      streaming: false,
      thinking: false,
    }
  }
  EventsEmit('pet:state:change', 'idle')
}
```

- [ ] **Step 6: Replace send button with conditional Stop/Send**

In the template, find the input-row button (~line 346):

```html
<button @click="send" :disabled="loading">发送</button>
```

Replace with:

```html
<button v-if="isStreaming" class="stop-btn" @click="stopGeneration">⏹ 停止</button>
<button v-else @click="send" :disabled="loading">发送</button>
```

- [ ] **Step 7: Add ghost bubble rendering**

In the template, find the bubble-wrap div (~line 293):

```html
<div class="bubble-wrap">
```

Replace with:

```html
<div class="bubble-wrap" :class="{ ghost: m.ghost }">
```

- [ ] **Step 8: Add ghost styles**

In the `<style scoped>` section, after `.system .bubble { ... }` (~line 415), add:

```css
/* Ghost bubbles: interrupted messages, visual only */
.ghost .bubble {
  opacity: 0.35;
  font-style: italic;
}
.ghost .bubble::after {
  content: ' ⊘';
  font-size: 11px;
  opacity: 0.6;
  font-style: normal;
}
```

- [ ] **Step 9: Verify dev build**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): D1 stop button and ghost bubbles for interrupted messages"
```

---

### Task 3: D3 — Sound effects composable + settings toggle

**Files:**
- Create: `frontend/src/composables/useSounds.js`
- Modify: `frontend/src/components/SettingsWindow.vue`

Sounds are synthesized via Web Audio API oscillators — no mp3 files needed. AudioContext is lazy-init on first user gesture to comply with browser autoplay policy.

- [ ] **Step 1: Create useSounds.js**

Create `frontend/src/composables/useSounds.js`:

```js
/** useSounds provides cute synthesized sound effects for chat interactions.
 *  Sounds are generated via Web Audio API — no external files required.
 *  AudioContext is lazy-initialized on first play() call (requires prior user gesture). */
export function useSounds() {
  let ctx = null

  /** ensureCtx lazily creates the AudioContext on first use. */
  function ensureCtx() {
    if (!ctx) ctx = new (window.AudioContext || window.webkitAudioContext)()
    if (ctx.state === 'suspended') ctx.resume()
    return ctx
  }

  /** playTone synthesizes a short tone with the given parameters.
   *  @param {number} freq - frequency in Hz
   *  @param {number} duration - duration in seconds
   *  @param {string} type - oscillator type: 'sine' | 'triangle' | 'square'
   *  @param {number} volume - gain 0–1
   *  @param {number} [freqEnd] - optional end frequency for a glide effect */
  function playTone(freq, duration, type, volume, freqEnd) {
    try {
      const ac = ensureCtx()
      const osc = ac.createOscillator()
      const gain = ac.createGain()
      osc.connect(gain)
      gain.connect(ac.destination)
      osc.type = type
      osc.frequency.setValueAtTime(freq, ac.currentTime)
      if (freqEnd !== undefined) {
        osc.frequency.linearRampToValueAtTime(freqEnd, ac.currentTime + duration)
      }
      gain.gain.setValueAtTime(volume, ac.currentTime)
      gain.gain.exponentialRampToValueAtTime(0.001, ac.currentTime + duration)
      osc.start(ac.currentTime)
      osc.stop(ac.currentTime + duration)
    } catch (e) {
      // Silently ignore audio errors (e.g. tab not focused, AudioContext suspended)
      console.debug('useSounds playTone error:', e)
    }
  }

  /** playSend plays a short upward "tik" for message send. */
  function playSend() {
    playTone(880, 0.08, 'sine', 0.15, 1200)
  }

  /** playReceive plays a soft descending "ding" for first AI token. */
  function playReceive() {
    playTone(660, 0.15, 'triangle', 0.12, 520)
  }

  /** playError plays a gentle low "bump" for errors. */
  function playError() {
    playTone(220, 0.25, 'triangle', 0.1, 180)
  }

  return { playSend, playReceive, playError }
}
```

- [ ] **Step 2: Add GetSoundsEnabled / SetSoundsEnabled to app.go**

In `app.go`, after `SetVoiceAutoSend` (~line 930), add:

```go
// GetSoundsEnabled returns whether chat sound effects are enabled.
func (a *App) GetSoundsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.SoundsEnabled
}

// SetSoundsEnabled sets the sounds enabled flag and persists it.
func (a *App) SetSoundsEnabled(enabled bool) error {
	a.mu.Lock()
	a.cfg.SoundsEnabled = enabled
	a.mu.Unlock()
	return a.configStore.Save(a.cfg)
}
```

- [ ] **Step 3: Verify Go compilation and regenerate bindings**

```bash
go build ./... && wails generate module
```

Expected: no errors; `GetSoundsEnabled` and `SetSoundsEnabled` appear in `frontend/wailsjs/go/main/App.js`.

- [ ] **Step 4: Add sounds toggle to SettingsWindow.vue**

Find the `toggleVoiceAutoSend` function (~line 565). After its closing `}`, add:

```js
/** toggleSoundsEnabled updates sound effects setting immediately. */
async function toggleSoundsEnabled() {
  try {
    await SetSoundsEnabled(cfg.value.SoundsEnabled)
  } catch (e) {
    console.warn('toggleSoundsEnabled failed:', e)
  }
}
```

- [ ] **Step 5: Import SetSoundsEnabled and GetSoundsEnabled in SettingsWindow**

Find the existing import line (near top of `<script setup>`). It currently imports `GetVoiceAutoSend, SetVoiceAutoSend`. Add `GetSoundsEnabled, SetSoundsEnabled` to the same import:

```js
import { ..., GetVoiceAutoSend, SetVoiceAutoSend, GetSoundsEnabled, SetSoundsEnabled } from '../../wailsjs/go/main/App'
```

- [ ] **Step 6: Add SoundsEnabled default to cfg ref**

Find the `const cfg = ref({...})` initialization. Add `SoundsEnabled: false` to the default object (alongside `VoiceAutoSend: false`).

- [ ] **Step 7: Add toggle UI after the voice auto-send toggle**

Find the voice auto-send section in SettingsWindow template (~line 1034):

```html
<p class="sms-desc" style="margin-top:4px">释放 Option 键后，等待转录完成并自动发送消息</p>
```

After that `<p>`, add:

```html
<!-- Sounds toggle -->
<div class="sms-toggle-row" style="margin-top:16px">
  <span class="sms-status-label" style="flex:1">聊天音效</span>
  <label class="voice-auto-send-switch">
    <input type="checkbox" v-model="cfg.SoundsEnabled" @change="toggleSoundsEnabled" />
    <span class="voice-auto-send-slider"></span>
  </label>
</div>
<p class="sms-desc" style="margin-top:4px">发送、收到消息和出错时播放轻柔提示音</p>
```

- [ ] **Step 8: Verify dev build**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/composables/useSounds.js frontend/src/components/SettingsWindow.vue app.go frontend/wailsjs/
git commit -m "feat(sounds): D3 useSounds composable and settings toggle"
```

---

### Task 4: D4 — Typing scheduler composable + ChatPanel integration

**Files:**
- Create: `frontend/src/composables/useTypingScheduler.js`
- Modify: `frontend/src/components/ChatPanel.vue`

The scheduler sits between `chat:token` events and DOM updates. Tokens are queued; a recursive `drain()` loop applies each token after a computed delay. D3 sounds are also wired in here (first-token receive sound) and in the `send()` function (send sound).

- [ ] **Step 1: Create useTypingScheduler.js**

Create `frontend/src/composables/useTypingScheduler.js`:

```js
/** useTypingScheduler queues chat tokens and drains them with variable timing to create
 *  a natural typing rhythm: punctuation pauses + subtle speed jitter. */
export function useTypingScheduler(applyToken) {
  const PUNCT = new Set(['。', '！', '？', '\n', '，', '、', '…', '；', '!', '?', ';'])
  const BASE_DELAY_MS = 16
  const JITTER_MS = 8
  const PUNCT_MIN_MS = 120
  const PUNCT_MAX_MS = 200

  const queue = []
  let draining = false

  /** computeDelay returns the ms to wait before rendering this token. */
  function computeDelay(token) {
    // Check last char of token for punctuation
    const last = token[token.length - 1]
    if (PUNCT.has(last)) {
      return PUNCT_MIN_MS + Math.random() * (PUNCT_MAX_MS - PUNCT_MIN_MS)
    }
    return BASE_DELAY_MS + (Math.random() * 2 - 1) * JITTER_MS
  }

  /** drain processes the next token from the queue, then schedules itself again. */
  function drain() {
    if (queue.length === 0) {
      draining = false
      return
    }
    const token = queue.shift()
    applyToken(token)
    setTimeout(drain, computeDelay(token))
  }

  /** enqueue adds a token to the queue and starts draining if not already running. */
  function enqueue(token) {
    queue.push(token)
    if (!draining) {
      draining = true
      drain()
    }
  }

  /** flush drains all remaining queued tokens immediately (no delay). */
  function flush() {
    while (queue.length > 0) {
      applyToken(queue.shift())
    }
    draining = false
  }

  /** clear discards all queued tokens without applying them. */
  function clear() {
    queue.length = 0
    draining = false
  }

  return { enqueue, flush, clear }
}
```

- [ ] **Step 2: Import composables in ChatPanel.vue**

At the top of `<script setup>` in `ChatPanel.vue`, after the existing imports, add:

```js
import { useSounds } from '../composables/useSounds'
import { useTypingScheduler } from '../composables/useTypingScheduler'
```

- [ ] **Step 3: Initialize composables and wire applyToken**

After `const isStreaming = ref(false)` (added in Task 2), add:

```js
const { playSend, playReceive, playError } = useSounds()
let soundsEnabled = false
// Load initial value — non-blocking, best-effort
import { GetSoundsEnabled } from '../../wailsjs/go/main/App'

/** applyToken appends a token to the last streaming assistant message. */
function applyToken(token) {
  // Remove thinking placeholder on first real token.
  const thinkIdx = messages.value.findLastIndex(m => m.thinking)
  if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)

  const idx = messages.value.length - 1
  const last = messages.value[idx]
  if (last && last.role === 'assistant' && last.streaming) {
    messages.value[idx] = { ...last, content: last.content + token }
  } else {
    messages.value.push({ role: 'assistant', content: token, streaming: true })
    EventsEmit('pet:state:change', 'speaking')
  }
  scrollToBottom()
}

const typingScheduler = useTypingScheduler(applyToken)
```

- [ ] **Step 4: Load soundsEnabled in onMounted**

Inside `onMounted`, after `try { voiceAutoSend.value = await GetVoiceAutoSend() } catch {}`, add:

```js
try { soundsEnabled = await GetSoundsEnabled() } catch {}
```

- [ ] **Step 5: Replace chat:token handler with scheduler**

Find the existing `offToken = EventsOn('chat:token', ...)` handler (~line 138). Replace the entire block with:

```js
let firstTokenThisTurn = true

offToken = EventsOn('chat:token', (token) => {
  if (firstTokenThisTurn) {
    firstTokenThisTurn = false
    if (soundsEnabled) playReceive()
  }
  typingScheduler.enqueue(token)
})
```

- [ ] **Step 6: Update chat:done to flush scheduler**

Find `offDone = EventsOn('chat:done', ...)` (~line 154). Replace with:

```js
offDone = EventsOn('chat:done', () => {
  typingScheduler.flush()
  const idx = messages.value.length - 1
  if (idx >= 0) messages.value[idx] = { ...messages.value[idx], streaming: false, time: new Date() }
  loading.value = false
  isStreaming.value = false
  EventsEmit('pet:state:change', 'idle')
})
```

- [ ] **Step 7: Update chat:error to clear scheduler**

Find `offError = EventsOn('chat:error', ...)` (~line 161). Replace with:

```js
offError = EventsOn('chat:error', (err) => {
  typingScheduler.clear()
  const thinkIdx = messages.value.findLastIndex(m => m.thinking)
  if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)
  messages.value.push({ role: 'system', content: '错误: ' + err })
  loading.value = false
  isStreaming.value = false
  if (soundsEnabled) playError()
  EventsEmit('pet:state:change', 'error')
})
```

- [ ] **Step 8: Play send sound in send() and reset firstTokenThisTurn**

In `send()`, after `loading.value = true`:

```js
isStreaming.value = true
firstTokenThisTurn = true
if (soundsEnabled) playSend()
```

- [ ] **Step 9: Clear scheduler in stopGeneration()**

In `stopGeneration()` (added in Task 2), after `await StopGeneration()`, add:

```js
typingScheduler.clear()
```

- [ ] **Step 10: Verify dev build**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 11: Commit**

```bash
git add frontend/src/composables/useTypingScheduler.js frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): D4 typing scheduler + D3 sounds wired into ChatPanel"
```

---

### Task 5: Manual end-to-end verification

No automated test harness for Wails UI. Verify manually with `wails dev`.

- [ ] **Step 1: Start dev mode**

```bash
wails dev
```

- [ ] **Step 2: Verify D1 — interrupt**

Send a long prompt (e.g. "请写一首500字的诗"). While AI is responding:
1. Confirm Send button becomes "⏹ 停止"
2. Click Stop — confirm streaming stops immediately
3. Confirm last user + assistant bubbles are ghosted (40% opacity, italic, "⊘" suffix)
4. Confirm input re-enables and a new message can be sent
5. Send a new message — confirm new response is clean with no corruption

- [ ] **Step 3: Verify D1 — ghost bubbles not in LLM context**

After interrupting, send: "你刚才说了什么？" — confirm the AI responds as if no prior exchange happened (not referencing the interrupted response).

- [ ] **Step 4: Verify D3 — sounds**

Open Settings → 语音设置 → enable 聊天音效. Send a message. Confirm:
1. Subtle "tik" on send
2. Soft "ding" when first token arrives
3. No sound on subsequent tokens of the same response

Trigger an error (disconnect network, send message). Confirm gentle "bump" plays.

Toggle sounds OFF. Send message. Confirm silence.

- [ ] **Step 5: Verify D4 — typing rhythm**

Send a message and observe the response streaming. Confirm:
1. Tokens render with slight variation in speed (not perfectly uniform)
2. After a `。` or `？`, there is a noticeable short pause before the next token renders
3. On `chat:done`, any remaining queued tokens are applied immediately (no lingering delay)

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "chore: chat UX enhancement feature complete (D1/D3/D4)"
```

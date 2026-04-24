# Chat UX Enhancement Design

## Goal

Make the chat experience feel more alive and responsive through three independent improvements:

- **D1** — Interrupt streaming generation mid-reply
- **D3** — Cute sound effects (send / receive / error)
- **D4** — Typing rhythm: punctuation pauses + speed jitter

## Architecture

Three independent modules — each can be enabled/disabled without touching the others.

```
D1: StopGeneration() Wails method  →  cancel context  →  agent.Chat stops streaming
                                   →  frontend marks ghost bubbles (visual only)

D3: useSounds composable           →  Web Audio API (3 mp3 files in public/sounds/)
                                   →  triggered by send() / first chat:token / chat:error

D4: useTypingScheduler composable  →  token queue + setTimeout drain
                                   →  inserted between chat:token event and DOM update
```

---

## D1 — Streaming Interrupt

### Data Flow

```
User clicks Stop
  → frontend: StopGeneration() Wails call
      → app.go: chatCancelFunc()
          → agent.Chat(ctx) context cancelled
              → drainRunner exits (ctx.Done())
              → persistAndMigrate NOT called (only called after Done=true)
  → frontend: marks current user + assistant bubbles ghost: true
              clears streaming state
              emits pet:state:change 'idle'
```

### Why this works cleanly

`agent.Chat` passes the cancel context all the way to `drainRunner`, which propagates it to the eino runner. `persistAndMibrate` is only called after `StreamResult{Done: true}` — a cancelled stream never reaches Done, so nothing is written to DB or LLM context. No orphaned user messages. No residual broken history.

### Ghost bubbles

Ghost bubbles are pure UI state (`ghost: true` flag on the message object). They:
- Render at 40% opacity with a strikethrough-style muted colour
- Are **not** included in the `GetMessages()` call results (they're never persisted)
- Disappear on page reload or chat clear

### Backend changes (`app.go`)

Add to `App` struct (protected by `a.mu`):
```go
chatCancel context.CancelFunc // cancels the current in-flight SendMessage
```

Modify `SendMessage`:
```go
func (a *App) SendMessage(userInput string) error {
    // Cancel any previous in-flight request
    a.mu.Lock()
    if a.chatCancel != nil {
        a.chatCancel()
    }
    chatCtx, cancel := context.WithCancel(a.ctx)
    a.chatCancel = cancel
    a.mu.Unlock()

    a.mu.RLock()
    ag := a.petAgent
    a.mu.RUnlock()
    ...
    go func() {
        defer func() {
            a.mu.Lock()
            a.chatCancel = nil
            a.mu.Unlock()
        }()
        ch := ag.Chat(chatCtx, userInput)
        ...
    }()
}
```

Add new exported method:
```go
// StopGeneration cancels the current in-flight chat stream.
func (a *App) StopGeneration() {
    a.mu.Lock()
    defer a.mu.Unlock()
    if a.chatCancel != nil {
        a.chatCancel()
        a.chatCancel = nil
    }
}
```

### Frontend changes (`ChatPanel.vue`)

- Add `isStreaming` ref (true between send → chat:done/chat:error)
- Show Stop button (replaces send button) when `isStreaming`
- On Stop click: call `StopGeneration()`, mark last user + assistant message `ghost: true`, set `isStreaming = false`, emit `pet:state:change idle`
- Ghost bubble style: `opacity: 0.4`, italic text, small "⊘ 已中断" label

---

## D3 — Sound Effects

### Composable: `frontend/src/composables/useSounds.js`

```js
/** useSounds provides cute sound effect playback for chat interactions. */
export function useSounds() {
  const ctx = new (window.AudioContext || window.webkitAudioContext)()
  const buffers = {}

  async function load(name, url) { ... } // fetch + decodeAudioData
  function play(name) { ... }           // createBufferSource + connect + start

  return { play, load }
}
```

Three sound files in `frontend/public/sounds/`:
- `send.mp3` — short upward "tik" (send message)
- `receive.mp3` — soft "ding" (first token of each AI response)
- `error.mp3` — gentle low "bump" (chat:error)

Sound files are royalty-free short clips (~100–300ms). They are loaded lazily on first user interaction (AudioContext requires gesture).

### Trigger points in `ChatPanel.vue`

| Event | Sound |
|---|---|
| `send()` called | `play('send')` |
| First `chat:token` per turn | `play('receive')` (guarded by `firstTokenPlayed` flag, reset on each send) |
| `chat:error` event | `play('error')` |
| `StopGeneration` click | no sound (silent cancel feels more natural) |

### Settings toggle

Add `SoundsEnabled bool` to `Config` (same pattern as `VoiceAutoSend`). Toggle in SettingsWindow under the voice section. `useSounds` checks the config value before playing.

---

## D4 — Typing Rhythm

### Composable: `frontend/src/composables/useTypingScheduler.js`

Sits between the raw `chat:token` event and the `messages` reactive state. Instead of applying each token immediately, it queues tokens and drains the queue with variable timing.

```
chat:token arrives → push to tokenQueue
                   → if not draining: startDrain()

startDrain():
  token = queue.shift()
  apply token to messages
  delay = baseDelay + jitter(token)  // see below
  setTimeout(startDrain, delay)
```

### Timing rules

```
Punctuation set: 。！？\n，、…；
  → pause: random(120, 200) ms

Other tokens:
  → base: 16ms
  → jitter: ±8ms (uniform random)
  → effective range: 8–24ms
```

### Integration

`useTypingScheduler` exposes:
```js
{ enqueue(token), flush(), clear() }
```

- `enqueue(token)` — called from `chat:token` handler instead of directly mutating `messages`
- `flush()` — called on `chat:done` to drain remaining queue immediately (no delay)
- `clear()` — called on `StopGeneration` or `chat:error` to discard pending queue

The composable holds a ref to the last assistant message and appends tokens to it, matching the current `last.content + token` pattern.

---

## Files Changed

| File | Change |
|---|---|
| `app.go` | Add `chatCancel` field; modify `SendMessage` to use per-request cancel ctx; add `StopGeneration()` |
| `frontend/src/components/ChatPanel.vue` | Add `isStreaming` ref; Stop button; ghost bubble rendering; integrate `useSounds` and `useTypingScheduler` |
| `frontend/src/composables/useSounds.js` | New: Web Audio playback of 3 sound effects |
| `frontend/src/composables/useTypingScheduler.js` | New: token queue with punctuation pause + jitter |
| `frontend/public/sounds/send.mp3` | New: send sound file |
| `frontend/public/sounds/receive.mp3` | New: receive sound file |
| `frontend/public/sounds/error.mp3` | New: error sound file |
| `internal/config/config.go` | Add `SoundsEnabled bool` field |
| `frontend/src/components/SettingsWindow.vue` | Add sounds toggle |

---

## Error / Edge Cases

- **AudioContext blocked before gesture**: Web Audio requires a user gesture. Lazy-init the context on first `send()` click, not on component mount.
- **Stop clicked before first token**: `chatCancel` exists, `StopGeneration` works. Ghost bubbles: user message already appended, assistant bubble may be the `thinking: true` placeholder — mark both ghost.
- **Rapid successive sends**: Each `SendMessage` cancels the previous one before creating a new context. The previous goroutine exits cleanly via ctx cancellation.
- **Scheduler queue on interrupt**: `clear()` discards queued tokens immediately; no stale tokens bleed into the next response.
- **D4 flush on fast response**: If a response arrives fully before the queue drains, `flush()` on `chat:done` applies all remaining tokens at once.

---

## Out of Scope

- No per-sound volume control (global mute/unmute only)
- No custom sound file upload
- D4 rhythm is not configurable (fixed timing constants)
- Ghost bubbles do not survive page reload (intentional)

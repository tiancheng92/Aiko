# LLM Latency Indicator — Design Spec

**Date:** 2026-04-30  
**Status:** Approved

## Overview

Display a real-time latency indicator (colored dot + ms value) in the ChatBubble title bar, showing the round-trip time to the active model provider's Base URL. Refreshes every 5 seconds via a Go backend ping.

## Architecture

```
ChatBubble.vue
  setInterval(5s) ──► PingLLM() [Wails binding]
                           └─► HTTP HEAD → cfg.LLMBaseURL (timeout 4s)
                           └─► returns int64 ms, or -1 on failure
  ◄─── ms value ── render LatencyBadge
```

## Backend

**New method in `app.go`:** `PingLLM() int64`

- Reads `a.cfg.LLMBaseURL` under `a.mu.RLock()`
- Issues `http.Head(baseURL)` with a 4-second timeout via a dedicated `http.Client`
- Records wall-clock duration from just before the request to response received
- Returns elapsed milliseconds as `int64`
- Returns `-1` on any error (empty URL, network error, timeout, non-2xx or 3xx ignored — only connection time matters)
- The HTTP client is created per-call (no persistent state needed; latency is the goal, not throughput)

## Frontend

**File:** `frontend/src/components/ChatBubble.vue`

### Title bar layout change

Current:
```
[聊天]          [fullscreen] [✕]
```

After:
```
[聊天]  [● 42ms]  [fullscreen] [✕]
```

The latency badge sits between the title and the fullscreen button, right-aligned via `margin-left: auto` on the badge container (title loses `flex:1`, badge takes its place as the spacer).

### Latency badge component (inline in ChatBubble)

- `latencyMs` ref: `null` (not yet measured) | `number` (last result, -1 = error)
- Dot color logic:
  - `null` → gray `rgba(255,255,255,0.2)`, text `—`
  - `< 0` (error) → red `#f87171`, text `—`
  - `< 300` → green `#4ade80`
  - `300–800` → yellow `#facc15`
  - `> 800` → red `#f87171`
- Font size: 11px, color same as dot, opacity 0.85
- No tooltip needed (number is already shown)

### Lifecycle

```js
onMounted:
  pingOnce()                         // immediate first ping
  timer = setInterval(pingOnce, 5000)
  offModelChanged = EventsOn('config:model:changed', pingOnce)  // reset on profile switch

onUnmounted:
  clearInterval(timer)
  offModelChanged?.()
```

`pingOnce` calls `PingLLM()` and writes result to `latencyMs`.

## Error handling

| Scenario | Backend returns | Badge shows |
|---|---|---|
| BaseURL empty | `-1` | `● —` (red) |
| Network unreachable | `-1` | `● —` (red) |
| Timeout (>4s) | `-1` | `● —` (red) |
| Success | `ms ≥ 0` | `● Nms` (color by threshold) |

## Out of scope

- Horizontal scrollbar ping / per-endpoint latency history
- Latency sparkline/graph
- Configurable thresholds or interval

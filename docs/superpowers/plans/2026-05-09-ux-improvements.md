# UX Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement four user-facing UX improvements: (1) macOS launch-at-login toggle in settings, (2) right-click "regenerate last reply" on assistant messages in ChatPanel, (3) message send/receive slide-in animation, (4) chat bubble drag-to-resize from edges/corner.

**Architecture:**
- Launch-at-login: LaunchAgent plist written to `~/Library/LaunchAgents/` by a new Go binding `SetAutoLaunch`/`GetAutoLaunch`, toggled from the Appearance tab in SettingsWindow.
- Regenerate: new `RegenerateLastReply` Go binding deletes the last assistant message from DB and re-runs with the last user message; ChatPanel adds a right-click context menu on assistant bubbles using the existing ContextMenu component.
- Message animation: CSS `@keyframes` + Vue `<TransitionGroup>` wrapping the messages list; slide-up + fade-in for new messages.
- Drag resize: mouse event listeners on four resize handles (right edge, bottom edge, bottom-right corner, left edge) in ChatBubble.vue; updates `bubbleW`/`bubbleH` reactively, persists on mouseup via existing `SaveChatSize`.

**Tech Stack:** Go (macos.go CGO / os/exec for plist), Vue 3 Composition API, CSS transitions, existing Wails event/binding patterns.

---

## File Map

| File | Change |
|------|--------|
| `macos.go` | Add `SetAutoLaunchEnabled` / `GetAutoLaunchEnabled` C functions + Go wrappers |
| `app.go` | Expose `SetAutoLaunch(bool) error` and `GetAutoLaunch() bool` Wails bindings |
| `internal/memory/short.go` | Add `DeleteLastAssistantMessage() (Message, error)` |
| `frontend/src/components/SettingsWindow.vue` | Import + call `SetAutoLaunch`/`GetAutoLaunch`; add toggle in Appearance tab |
| `frontend/src/components/ChatPanel.vue` | Add right-click menu on assistant bubbles; add `regenLastReply()`; add `<TransitionGroup>` with slide-in animation |
| `frontend/src/components/ChatBubble.vue` | Add resize handles + drag logic; persist on mouseup |
| `frontend/wailsjs/go/main/App.js` | Auto-regenerated — run `wails generate module` after Go changes |

---

## Task 1: Launch-at-login — Go backend

**Files:**
- Modify: `macos.go`
- Modify: `app.go`

macOS launch-at-login is implemented by writing/removing a LaunchAgent plist in `~/Library/LaunchAgents/`. The bundle identifier is `com.xutiancheng.aiko`; the plist label uses the same string.

- [ ] **Step 1: Add C helpers and Go bindings to `macos.go`**

Add before the `import "C"` line, inside the CGO block:

```objc
// autoLaunchPlistPath returns the full path for Aiko's LaunchAgent plist.
static NSString *autoLaunchPlistPath(void) {
    NSString *home = NSHomeDirectory();
    return [home stringByAppendingPathComponent:
        @"Library/LaunchAgents/com.xutiancheng.aiko.plist"];
}

// setAutoLaunchEnabled installs or removes the LaunchAgent plist.
static void setAutoLaunchEnabled(BOOL enabled) {
    NSString *path = autoLaunchPlistPath();
    if (!enabled) {
        [[NSFileManager defaultManager] removeItemAtPath:path error:nil];
        return;
    }
    // Build path to the running app bundle's executable.
    NSString *execPath = [[NSBundle mainBundle] executablePath];
    if (!execPath) {
        // Fallback: use the process path (works in dev / wails dev mode).
        execPath = [[[NSProcessInfo processInfo] arguments] firstObject];
    }
    NSDictionary *plist = @{
        @"Label":            @"com.xutiancheng.aiko",
        @"ProgramArguments": @[execPath],
        @"RunAtLoad":        @YES,
        @"KeepAlive":        @NO,
    };
    [plist writeToFile:path atomically:YES];
}

// getAutoLaunchEnabled returns YES when the LaunchAgent plist exists.
static BOOL getAutoLaunchEnabled(void) {
    return [[NSFileManager defaultManager]
        fileExistsAtPath:autoLaunchPlistPath()];
}
```

- [ ] **Step 2: Add Go wrapper functions to `macos.go`** (after the `import "C"` block, alongside other exported Go funcs)

```go
// SetAutoLaunchEnabled installs or removes the Aiko LaunchAgent plist so
// the app starts automatically at login. macOS-only.
func SetAutoLaunchEnabled(enabled bool) {
	if enabled {
		C.setAutoLaunchEnabled(C.BOOL(1))
	} else {
		C.setAutoLaunchEnabled(C.BOOL(0))
	}
}

// GetAutoLaunchEnabled reports whether the Aiko LaunchAgent plist exists.
func GetAutoLaunchEnabled() bool {
	return C.getAutoLaunchEnabled() == C.BOOL(1)
}
```

- [ ] **Step 3: Add Wails bindings to `app.go`**

Add near the other simple toggle bindings (e.g. near `GetVoiceAutoSend`):

```go
// GetAutoLaunch reports whether Aiko is configured to launch at login.
func (a *App) GetAutoLaunch() bool {
	return GetAutoLaunchEnabled()
}

// SetAutoLaunch enables or disables launch-at-login for Aiko.
func (a *App) SetAutoLaunch(enabled bool) {
	SetAutoLaunchEnabled(enabled)
}
```

- [ ] **Step 4: Regenerate Wails bindings**

```bash
cd /Users/xutiancheng/code/self/Aiko
wails generate module
```

Expected: `frontend/wailsjs/go/main/App.js` and `App.d.ts` updated with `GetAutoLaunch` and `SetAutoLaunch`.

- [ ] **Step 5: Verify Go build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add macos.go app.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts
git commit -m "feat: add launch-at-login Go bindings (LaunchAgent plist)"
```

---

## Task 2: Launch-at-login — Settings UI toggle

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

The toggle lives in the **外观** (Appearance) tab, near the top.

- [ ] **Step 1: Import the new bindings in SettingsWindow.vue**

In the import block at the top of `<script setup>`, add `GetAutoLaunch, SetAutoLaunch` to the existing import from `'../../wailsjs/go/main/App'`.

- [ ] **Step 2: Add reactive state and load/save logic**

Add near other boolean refs (e.g. near `voiceAutoSend`):

```js
const autoLaunch = ref(false)
```

In `onMounted`, after existing `try`/`catch` blocks for loading config, add:

```js
try { autoLaunch.value = await GetAutoLaunch() } catch (e) {
  console.warn('GetAutoLaunch failed:', e)
}
```

Add a toggle handler:

```js
/** toggleAutoLaunch enables or disables launch-at-login immediately. */
async function toggleAutoLaunch(val) {
  try {
    await SetAutoLaunch(val)
    autoLaunch.value = val
  } catch (e) {
    console.warn('SetAutoLaunch failed:', e)
  }
}
```

- [ ] **Step 3: Add UI toggle in Appearance tab**

In the template, find the Appearance tab section (search for `外观` or `tab === 'appearance'`). Add a settings row for auto-launch:

```html
<div class="setting-row">
  <div class="setting-label">
    <span class="setting-name">开机自启</span>
    <span class="setting-desc">登录 macOS 时自动启动 Aiko</span>
  </div>
  <label class="toggle">
    <input type="checkbox" :checked="autoLaunch" @change="toggleAutoLaunch($event.target.checked)" />
    <span class="toggle-track"><span class="toggle-thumb" /></span>
  </label>
</div>
```

(The `.toggle`, `.toggle-track`, `.toggle-thumb` classes already exist in SettingsWindow.vue CSS — reuse them.)

- [ ] **Step 4: Build frontend**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build
```

Expected: Done with no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat: add launch-at-login toggle to appearance settings"
```

---

## Task 3: Regenerate last reply — Go backend

**Files:**
- Modify: `internal/memory/short.go`
- Modify: `app.go`

"Regenerate" means: delete the last assistant message from DB, then re-send the last user message through the agent. The frontend is responsible for updating the UI (removing the stale bubble and adding a new streaming one).

- [ ] **Step 1: Add `DeleteLastAssistantMessage` to `short.go`**

Add after `DeleteByIDs`:

```go
// DeleteLastAssistantMessage removes the most recent assistant message from
// the store and returns it so the caller can re-use its preceding user message.
// Returns sql.ErrNoRows if no assistant message exists.
func (s *ShortStore) DeleteLastAssistantMessage() (Message, error) {
	var m Message
	err := s.db.QueryRow(`
		SELECT id, role, content, images, files, created_at
		FROM messages
		WHERE role = 'assistant'
		ORDER BY id DESC
		LIMIT 1`).Scan(
		&m.ID, &m.Role, &m.Content,
		new(string), new(string), &m.CreatedAt,
	)
	if err != nil {
		return m, err
	}
	_, err = s.db.Exec(`DELETE FROM messages WHERE id = ?`, m.ID)
	return m, err
}

// LastUserMessage returns the most recent user message.
// Returns sql.ErrNoRows if none exists.
func (s *ShortStore) LastUserMessage() (Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, images, files, created_at
		FROM messages
		WHERE role = 'user'
		ORDER BY id DESC
		LIMIT 1`)
	if err != nil {
		return Message{}, err
	}
	defer rows.Close()
	if rows.Next() {
		return scanMessage(rows.Scan)
	}
	return Message{}, sql.ErrNoRows
}
```

- [ ] **Step 2: Add `RegenerateLastReply` Wails binding to `app.go`**

Add near `SendMessage`:

```go
// RegenerateLastReply deletes the last assistant message from history and
// re-runs the agent with the last user message. The response is streamed
// through the usual chat:token / chat:done / chat:error events.
func (a *App) RegenerateLastReply() error {
	if a.shortMem == nil {
		return fmt.Errorf("memory not initialized")
	}
	// Delete the stale assistant message.
	if _, err := a.shortMem.DeleteLastAssistantMessage(); err != nil {
		return fmt.Errorf("delete last assistant: %w", err)
	}
	// Find the last user message to re-send.
	userMsg, err := a.shortMem.LastUserMessage()
	if err != nil {
		return fmt.Errorf("find last user message: %w", err)
	}
	// Delete the user message from DB too — SendMessage will re-persist it.
	if err := a.shortMem.DeleteByIDs([]int64{userMsg.ID}); err != nil {
		return fmt.Errorf("delete last user message: %w", err)
	}
	return a.SendMessage(userMsg.Content)
}
```

- [ ] **Step 3: Regenerate Wails bindings**

```bash
cd /Users/xutiancheng/code/self/Aiko && wails generate module
```

- [ ] **Step 4: Verify Go build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/short.go app.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts
git commit -m "feat: add RegenerateLastReply binding — deletes last assistant msg and re-sends"
```

---

## Task 4: Regenerate last reply — ChatPanel UI

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

Show a right-click context menu on assistant bubbles using the existing `ContextMenu` component. The menu has a single item: "重新生成". The regenerate flow: (1) remove the last assistant bubble from `messages`, (2) remove the last user bubble from `messages`, (3) call `RegenerateLastReply()` on the backend, (4) let the normal `chat:token`/`chat:done` flow add a new streaming bubble.

- [ ] **Step 1: Import `ContextMenu` and `RegenerateLastReply` in ChatPanel.vue**

At the top of `<script setup>`, add `RegenerateLastReply` to the existing import from `'../../wailsjs/go/main/App'`.

Add a component import:

```js
import ContextMenu from './ContextMenu.vue'
```

- [ ] **Step 2: Add context menu state**

```js
const msgMenuRef = ref(null)
const msgMenuItems = ref([])
```

- [ ] **Step 3: Add right-click handler**

```js
/** onAssistantBubbleContextMenu shows the per-message right-click menu. */
function onAssistantBubbleContextMenu(e, i) {
  const m = messages.value[i]
  if (!m || m.role !== 'assistant' || m.streaming || m.thinking) return
  // Only allow regen on the last assistant message.
  const lastAssistantIdx = messages.value.reduce((last, msg, idx) =>
    msg.role === 'assistant' && !msg.streaming && !msg.thinking ? idx : last, -1)
  if (i !== lastAssistantIdx) return
  e.preventDefault()
  msgMenuItems.value = [
    {
      iconSvg: '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 .49-3.5"/></svg>',
      label: '重新生成',
      action: () => regenLastReply(i),
    },
  ]
  msgMenuRef.value?.show(e.clientX, e.clientY)
}
```

- [ ] **Step 4: Add `regenLastReply` function**

```js
/** regenLastReply removes the last assistant + user bubble and re-requests. */
async function regenLastReply(assistantIdx) {
  if (loading.value) return
  // Remove the assistant bubble at assistantIdx.
  messages.value.splice(assistantIdx, 1)
  // Remove the preceding user bubble (last user before assistantIdx).
  const userIdx = messages.value.slice(0, assistantIdx).reduce(
    (last, m, idx) => m.role === 'user' ? idx : last, -1)
  if (userIdx >= 0) messages.value.splice(userIdx, 1)

  loading.value = true
  isStreaming.value = true
  firstTokenThisTurn = true
  messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  try {
    await RegenerateLastReply()
  } catch (e) {
    const thinkIdx = messages.value.findLastIndex(m => m.thinking)
    if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)
    messages.value.push({ role: 'system', content: '重新生成失败: ' + e })
    loading.value = false
    isStreaming.value = false
    EventsEmit('pet:state:change', 'error')
  }
}
```

- [ ] **Step 5: Add ContextMenu to template and wire up right-click**

In the `<template>`, find the assistant bubble `<div v-else :class="['bubble', 'markdown', ...]">` and add `@contextmenu` to its parent `.bubble-row` div. Also add `<ContextMenu ref="msgMenuRef" :items="msgMenuItems" />` inside `.chat-panel`.

Change:
```html
<div class="bubble-row" :class="{ 'is-collapsed': isCollapsed(m, i) }">
```
to:
```html
<div class="bubble-row" :class="{ 'is-collapsed': isCollapsed(m, i) }"
  @contextmenu="m.role === 'assistant' ? onAssistantBubbleContextMenu($event, i) : undefined">
```

Add inside `.chat-panel` (alongside other `ContextMenu` / `Transition` elements):
```html
<ContextMenu ref="msgMenuRef" :items="msgMenuItems" />
```

- [ ] **Step 6: Build frontend and verify**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build
```

Expected: Done with no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat: add right-click regenerate on last assistant message"
```

---

## Task 5: Message send/receive slide-in animation

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

Wrap the `v-for` messages list in a `<TransitionGroup>` so new bubbles slide up + fade in. History loads (lazy-load older messages) must NOT animate — only newly appended messages get the transition. Use a CSS class flag to disable animation during history loads.

- [ ] **Step 1: Add `<TransitionGroup>` around the message list**

In `<template>`, find:
```html
<div v-for="(m, i) in messages" :key="i" :class="['msg', m.role]">
```

Wrap it:
```html
<TransitionGroup name="msg-slide" tag="div" class="messages-inner">
  <div v-for="(m, i) in messages" :key="msgKey(m, i)" :class="['msg', m.role]">
  ...
  </div>
</TransitionGroup>
```

Remove the old `ref="messagesEl"` from the outer `.messages` div — keep it on the outer div. The `TransitionGroup` renders a wrapping `div.messages-inner` that fills the `.messages` scroll container.

Add CSS for `.messages-inner`:
```css
.messages-inner {
  display: flex;
  flex-direction: column;
  gap: 12px; /* move gap here from .messages */
  padding: 12px 16px 8px;
}
```

- [ ] **Step 2: Add animation CSS**

```css
.msg-slide-enter-active {
  transition: opacity 0.22s ease, transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.msg-slide-enter-from {
  opacity: 0;
  transform: translateY(10px);
}
/* Disable move animation on TransitionGroup to avoid jitter when prepending history */
.msg-slide-move { transition: none; }
```

- [ ] **Step 3: Suppress animation during history load**

Add a reactive flag:

```js
const suppressAnimation = ref(false)
```

In `loadOlderMessages`, wrap the prepend:
```js
suppressAnimation.value = true
messages.value = olderMapped.concat(messages.value)
await nextTick()
suppressAnimation.value = false
```

Bind `suppress-anim` class on `<TransitionGroup>`:
```html
<TransitionGroup name="msg-slide" tag="div" class="messages-inner" :class="{ 'suppress-anim': suppressAnimation }">
```

Add CSS:
```css
.suppress-anim .msg-slide-enter-active { transition: none; }
```

- [ ] **Step 4: Build and verify**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build
```

Expected: Done with no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat: add slide-in animation for new chat messages"
```

---

## Task 6: Chat bubble drag-to-resize

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue`

Add resize handles on the right edge, bottom edge, left edge, and bottom-right corner of `.chat-bubble`. On mousedown on a handle, track mousemove to update `bubbleW`/`bubbleH`, then persist on mouseup via `SaveChatSize`.

- [ ] **Step 1: Add resize handle elements to template**

Inside `.chat-bubble` (as last children, after `<ContextMenu>`):
```html
<!-- Resize handles — invisible 6px drag strips -->
<div class="resize-handle resize-e"  @mousedown.stop="startResize($event, 'e')" />
<div class="resize-handle resize-s"  @mousedown.stop="startResize($event, 's')" />
<div class="resize-handle resize-w"  @mousedown.stop="startResize($event, 'w')" />
<div class="resize-handle resize-se" @mousedown.stop="startResize($event, 'se')" />
```

- [ ] **Step 2: Add resize CSS**

```css
.resize-handle {
  position: absolute;
  z-index: 10;
  user-select: none;
}
.resize-e  { right: 0;  top: 6px; bottom: 6px; width: 6px; cursor: ew-resize; }
.resize-w  { left: 0;   top: 6px; bottom: 6px; width: 6px; cursor: ew-resize; }
.resize-s  { bottom: 0; left: 6px; right: 6px; height: 6px; cursor: ns-resize; }
.resize-se { right: 0;  bottom: 0; width: 14px; height: 14px; cursor: nwse-resize; }
```

- [ ] **Step 3: Add resize logic to `<script setup>`**

```js
const MIN_W = 300
const MIN_H = 320

let resizeDrag = null

/** startResize begins a drag-resize operation. */
function startResize(e, edge) {
  if (isFullscreen.value) return
  resizeDrag = {
    edge,
    startX: e.clientX,
    startY: e.clientY,
    startW: bubbleW.value,
    startH: bubbleH.value,
    startLeft: pos.value?.x ?? 0,
  }
  window.addEventListener('mousemove', onResizeMove)
  window.addEventListener('mouseup', onResizeUp)
  window.addEventListener('blur', onResizeUp)
}

/** onResizeMove updates bubble dimensions during drag. */
function onResizeMove(e) {
  if (!resizeDrag) return
  const dx = e.clientX - resizeDrag.startX
  const dy = e.clientY - resizeDrag.startY
  const { edge } = resizeDrag
  if (edge === 'e' || edge === 'se') {
    bubbleW.value = Math.max(MIN_W, resizeDrag.startW + dx)
  }
  if (edge === 'w') {
    const newW = Math.max(MIN_W, resizeDrag.startW - dx)
    bubbleW.value = newW
  }
  if (edge === 's' || edge === 'se') {
    bubbleH.value = Math.max(MIN_H, resizeDrag.startH + dy)
  }
}

/** onResizeUp finalizes resize and persists via SaveChatSize. */
function onResizeUp() {
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeUp)
  window.removeEventListener('blur', onResizeUp)
  if (!resizeDrag) return
  resizeDrag = null
  const sw = props.activeScreen.width
  const sh = props.activeScreen.height
  if (sw > 0 && sh > 0) {
    SaveChatSize(bubbleW.value, bubbleH.value, sw, sh).catch(e =>
      console.warn('SaveChatSize on resize failed', e)
    )
  }
}
```

- [ ] **Step 4: Clean up resize listeners on unmount**

In `onUnmounted`, add:
```js
window.removeEventListener('mousemove', onResizeMove)
window.removeEventListener('mouseup', onResizeUp)
window.removeEventListener('blur', onResizeUp)
```

Also import `SaveChatSize` at the top:
```js
import { ..., SaveChatSize } from '../../wailsjs/go/main/App'
```

- [ ] **Step 5: Add `.resize-handle` to macOS hitTest selector in `macos.go`**

In `macos.go`, find the hitTest JS selector string and add `.resize-handle`:
```
'.live2d-pet,.vrm-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.execution-progress,.resize-handle'
```

- [ ] **Step 6: Build frontend and Go**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build && cd .. && go build ./...
```

Expected: both succeed with no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/ChatBubble.vue macos.go
git commit -m "feat: drag-to-resize chat bubble from edges and corner"
```

---

## Self-Review

**Spec coverage:**
1. Launch-at-login with user toggle ✅ Tasks 1–2
2. Regenerate last reply right-click ✅ Tasks 3–4
3. Message send animation ✅ Task 5
4. Drag-to-resize chat bubble ✅ Task 6

**Placeholder scan:** No TBD/TODO found. All code blocks complete.

**Type consistency:**
- `DeleteLastAssistantMessage()` defined in Task 3 Step 1, called in Task 3 Step 2 ✅
- `LastUserMessage()` defined in Task 3 Step 1, called in Task 3 Step 2 ✅
- `RegenerateLastReply()` Wails binding defined in Task 3, imported and called in Task 4 ✅
- `startResize` / `onResizeMove` / `onResizeUp` consistent across Task 6 ✅
- `SetAutoLaunch` / `GetAutoLaunch` defined in Task 1, used in Task 2 ✅

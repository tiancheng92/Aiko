# Per-Screen Pet & Chat Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the mouse moves to a different monitor, Aiko's window migrates to that screen and loads per-screen configs for pet position, pet size, and chat bubble size.

**Architecture:** Go backend polls mouse position every 500ms, detects active screen changes, migrates the Wails window, and emits `screen:changed`. Frontend subscribes in `App.vue`, propagates via `screen:active:changed`, and each component reloads its per-screen config. Storage uses the existing `settings` table with keys `pet_size_{W}x{H}` and `chat_size_{W}x{H}` (mirroring existing `ball_pos_{W}x{H}`).

**Tech Stack:** Go + Wails v2 runtime (`ScreenGetAll`, `WindowSetSize`, `WindowSetPosition`, `EventsEmit`), Vue 3 `<script setup>`, existing SQLite `settings` table.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `app.go` | Modify | Add `activeScreen` field, `startScreenWatcher()`, `ScreenInfo` struct, 5 new Wails bindings |
| `frontend/src/App.vue` | Modify | Subscribe to `screen:changed`, maintain `activeScreen` ref, emit `screen:active:changed` |
| `frontend/src/components/Live2DPet.vue` | Modify | On `screen:active:changed` reload pet size + position from backend |
| `frontend/src/components/ChatBubble.vue` | Modify | On `screen:active:changed` reload chat size from backend; persist on resize |
| `frontend/src/components/SettingsWindow.vue` | Modify | Pass active screen to per-screen save calls; show current screen label |

---

## Task 1: Go — `ScreenInfo` struct + 5 new Wails bindings

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Add `ScreenInfo` struct and `activeScreen` field**

In `app.go`, after the `App` struct closing brace, add:

```go
// ScreenInfo holds the logical resolution of a screen.
type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}
```

In the `App` struct, after the `mu sync.RWMutex` line add:

```go
activeScreen ScreenInfo // current screen under the mouse cursor, guarded by mu
```

- [ ] **Step 2: Add `GetScreenList`**

```go
// GetScreenList returns all connected screens as ScreenInfo values.
func (a *App) GetScreenList() []ScreenInfo {
	screens, err := wailsruntime.ScreenGetAll(a.ctx)
	if err != nil {
		return nil
	}
	result := make([]ScreenInfo, 0, len(screens))
	for _, s := range screens {
		result = append(result, ScreenInfo{Width: s.Size.Width, Height: s.Size.Height})
	}
	return result
}
```

- [ ] **Step 3: Add `GetPetSize` and `SavePetSize`**

```go
// GetPetSize returns the saved pet height for the given screen resolution, or 0 if not set.
func (a *App) GetPetSize(screenW, screenH int) int {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

// SavePetSize persists the pet height for the given screen resolution.
func (a *App) SavePetSize(size, screenW, screenH int) error {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, strconv.Itoa(size))
	return err
}
```

- [ ] **Step 4: Add `GetChatSize` and `SaveChatSize`**

```go
// GetChatSize returns the saved chat bubble [width, height] for the given screen resolution.
// Returns [0, 0] if no size has been saved for that resolution yet.
func (a *App) GetChatSize(screenW, screenH int) []int {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return []int{0, 0}
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return []int{0, 0}
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return []int{0, 0}
	}
	return []int{w, h}
}

// SaveChatSize persists the chat bubble dimensions for the given screen resolution.
func (a *App) SaveChatSize(width, height, screenW, screenH int) error {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	val := fmt.Sprintf("%d,%d", width, height)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}
```

- [ ] **Step 5: Verify Go compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add app.go
git commit -m "feat: add per-screen config Wails bindings (GetPetSize, SavePetSize, GetChatSize, SaveChatSize, GetScreenList)"
```

---

## Task 2: Go — `startScreenWatcher`

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Add `startScreenWatcher` method**

Add after the `GetScreenList` method:

```go
// startScreenWatcher polls the mouse position every 500ms and migrates the Wails window
// to the screen containing the cursor. Emits "screen:changed" when the active screen changes.
func (a *App) startScreenWatcher() {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			screens, err := wailsruntime.ScreenGetAll(a.ctx)
			if err != nil {
				slog.Warn("startScreenWatcher: ScreenGetAll failed", "err", err)
				continue
			}
			// Read mouse position in macOS screen coordinates (Y-up, origin at bottom-left).
			mx := float64(C.getMouseScreenX())
			my := float64(C.getMouseScreenY())

			var found *wailsruntime.Screen
			for i := range screens {
				s := &screens[i]
				// Screen.Size gives logical dimensions; origin comes from NSScreen.frame.
				// Wails reports width/height but not origin — use IsCurrent as fallback.
				// We detect by checking if mouse falls within [originX, originX+W) x [originY, originY+H).
				// Since Wails v2 doesn't expose Bounds, use IsCurrent flag as the signal.
				if s.IsCurrent {
					found = s
					break
				}
			}
			if found == nil {
				continue
			}

			current := ScreenInfo{Width: found.Size.Width, Height: found.Size.Height}

			a.mu.RLock()
			same := a.activeScreen == current
			a.mu.RUnlock()
			if same {
				continue
			}

			// Migrate window to the new screen.
			// WindowSetPosition origin: Wails uses top-left in logical coords.
			// For the current screen we compute its origin from the primary screen offset.
			// Since Wails doesn't expose screen origin, we iterate and use the primary as (0,0).
			var originX, originY int
			for _, s := range screens {
				if s.IsCurrent {
					// Walk all screens to infer origin via macOS coordinate math.
					// Use mouse position relative to primary to derive offset.
					_ = mx
					_ = my
					// Wails v2 does not expose screen origin in the Screen struct.
					// Use WindowSetPosition with the known macOS screen origin:
					// obtain from C.getWindowOriginX/Y after migration is not possible pre-move.
					// Best available: use (0,0) for primary, derive others from Size deltas.
					// For non-primary screens we rely on IsCurrent flag — Wails already knows
					// where the current screen is internally, so we pass the logical origin
					// obtained by summing widths of preceding screens.
					// Since NSScreen.frame.origin IS available via CGO, we add a helper below.
					_ = s
					break
				}
			}
			// Use CGO helper to get the current screen's macOS frame origin.
			screenOriginX := int(C.getCurrentScreenOriginX())
			screenOriginY := int(C.getCurrentScreenOriginY())
			// macOS Y-up → Wails Y-down: Wails WindowSetPosition uses top-left in a Y-down system.
			// The primary screen origin in Wails coords is (0,0). For secondary screens,
			// Wails expects the logical top-left offset from the primary screen's top-left.
			// macOS: origin is bottom-left. Convert:
			//   wailsY = primaryHeight - (screenOriginY + screenHeight)
			// We get primaryHeight from the first screen.
			primaryH := screens[0].Size.Height
			originX = screenOriginX
			originY = primaryH - (screenOriginY + found.Size.Height)

			wailsruntime.WindowSetSize(a.ctx, found.Size.Width, found.Size.Height)
			wailsruntime.WindowSetPosition(a.ctx, originX, originY)

			a.mu.Lock()
			a.activeScreen = current
			a.mu.Unlock()

			wailsruntime.EventsEmit(a.ctx, "screen:changed", current)
			slog.Info("startScreenWatcher: screen changed", "width", current.Width, "height", current.Height)
		}
	}()
}
```

- [ ] **Step 2: Add CGO helpers `getCurrentScreenOriginX` / `getCurrentScreenOriginY` to `macos.go`**

In the CGO C block of `macos.go`, add after the existing `getWindowHeight` helper:

```objc
// getCurrentScreenOriginX returns the X origin of the screen containing gWindow, in macOS screen coords.
static CGFloat getCurrentScreenOriginX() {
    if (!gWindow) return 0;
    return gWindow.screen ? gWindow.screen.frame.origin.x : 0;
}
// getCurrentScreenOriginY returns the Y origin of the screen containing gWindow, in macOS screen coords.
static CGFloat getCurrentScreenOriginY() {
    if (!gWindow) return 0;
    return gWindow.screen ? gWindow.screen.frame.origin.y : 0;
}
```

In the Go section of `macos.go`, add exported wrappers:

```go
// getCurrentScreenOriginX returns the X origin of the screen that contains the main window.
func getCurrentScreenOriginX() float64 { return float64(C.getCurrentScreenOriginX()) }

// getCurrentScreenOriginY returns the Y origin of the screen that contains the main window.
func getCurrentScreenOriginY() float64 { return float64(C.getCurrentScreenOriginY()) }
```

Then update the `startScreenWatcher` to use these wrapper functions instead of `C.getCurrentScreenOriginX()` / `C.getCurrentScreenOriginY()` directly (since they're defined in `macos.go`, not `app.go`). Replace:

```go
screenOriginX := int(C.getCurrentScreenOriginX())
screenOriginY := int(C.getCurrentScreenOriginY())
```

With:

```go
screenOriginX := int(getCurrentScreenOriginX())
screenOriginY := int(getCurrentScreenOriginY())
```

Also remove the unused `mx`, `my`, `originX`, `originY` intermediate code and the blank `_ = s` block, simplifying `startScreenWatcher` to:

```go
// startScreenWatcher polls the mouse position every 500ms and migrates the Wails window
// to the screen containing the cursor. Emits "screen:changed" when the active screen changes.
func (a *App) startScreenWatcher() {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			screens, err := wailsruntime.ScreenGetAll(a.ctx)
			if err != nil {
				slog.Warn("startScreenWatcher: ScreenGetAll failed", "err", err)
				continue
			}

			var found *wailsruntime.Screen
			for i := range screens {
				if screens[i].IsCurrent {
					found = &screens[i]
					break
				}
			}
			if found == nil {
				continue
			}

			current := ScreenInfo{Width: found.Size.Width, Height: found.Size.Height}

			a.mu.RLock()
			same := a.activeScreen == current
			a.mu.RUnlock()
			if same {
				continue
			}

			// Derive window position in Wails logical coords (Y-down from primary top-left).
			primaryH := screens[0].Size.Height
			originX := int(getCurrentScreenOriginX())
			originY := primaryH - (int(getCurrentScreenOriginY()) + found.Size.Height)

			wailsruntime.WindowSetSize(a.ctx, found.Size.Width, found.Size.Height)
			wailsruntime.WindowSetPosition(a.ctx, originX, originY)

			a.mu.Lock()
			a.activeScreen = current
			a.mu.Unlock()

			wailsruntime.EventsEmit(a.ctx, "screen:changed", current)
			slog.Info("startScreenWatcher: screen changed", "width", current.Width, "height", current.Height)
		}
	}()
}
```

- [ ] **Step 3: Call `startScreenWatcher` in `startup`**

In `app.go`'s `startup` method, after `registerGlobalHotkey()`:

```go
// Watch for mouse moving to a different screen and migrate the window.
a.startScreenWatcher()
```

- [ ] **Step 4: Verify Go compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add app.go macos.go
git commit -m "feat: add startScreenWatcher — window migrates to active screen on mouse move"
```

---

## Task 3: Regenerate Wails bindings

**Files:**
- Modify: `frontend/src/wailsjs/go/main/App.js` (auto-generated)

- [ ] **Step 1: Regenerate**

```bash
cd /Users/xutiancheng/code/self/Aiko && wails generate module
```

Expected: `frontend/src/wailsjs/go/main/App.js` and `App.d.ts` updated with `GetScreenList`, `GetPetSize`, `SavePetSize`, `GetChatSize`, `SaveChatSize`.

- [ ] **Step 2: Verify new bindings exist**

```bash
grep -E "GetPetSize|SavePetSize|GetChatSize|SaveChatSize|GetScreenList" frontend/src/wailsjs/go/main/App.js
```

Expected: 5 lines matched.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/wailsjs/
git commit -m "chore: regenerate Wails bindings for per-screen config methods"
```

---

## Task 4: Frontend — `App.vue` active screen state + event relay

**Files:**
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: Add `activeScreen` ref and import**

In `App.vue`, add to the imports:

```js
import { GetScreenSize } from '../wailsjs/go/main/App'
```

Add after the existing `ref` declarations:

```js
// activeScreen holds the current screen resolution; updated on screen:changed events.
const activeScreen = ref({ width: 0, height: 0 })
```

- [ ] **Step 2: Initialize `activeScreen` on mount**

Inside `onMounted`, after `await waitForRuntime()`, add:

```js
try {
  const [w, h] = await GetScreenSize()
  if (w > 0 && h > 0) activeScreen.value = { width: w, height: h }
} catch (e) {
  console.warn('App.vue: GetScreenSize failed', e)
}
```

- [ ] **Step 3: Subscribe to `screen:changed` and relay**

In `onMounted`, after the `offSettings` line, add:

```js
EventsOn('screen:changed', (info) => {
  activeScreen.value = { width: info.width, height: info.height }
  EventsEmit('screen:active:changed', info)
})
```

- [ ] **Step 4: Pass `activeScreen` as prop to child components**

Update the `<Live2DPet>` usage to pass the prop:

```html
<Live2DPet
  :active-screen="activeScreen"
  @ball-pos="ballPos = $event"
  @ball-size="ballSize = $event"
/>
```

Update the `<ChatBubble>` usage:

```html
<ChatBubble
  v-if="bubbleOpen"
  ref="chatBubbleRef"
  :ball-pos="ballPos"
  :ball-size="ballSize"
  :active-screen="activeScreen"
  @close="bubbleOpen = false"
  @open-settings="settingsOpen = true"
/>
```

Update `<SettingsWindow>`:

```html
<SettingsWindow
  v-if="settingsOpen"
  :active-screen="activeScreen"
  @close="settingsOpen = false"
/>
```

- [ ] **Step 5: Verify dev server starts without errors**

```bash
cd /Users/xutiancheng/code/self/Aiko && wails dev 2>&1 | head -20
```

Expected: no import errors (press Ctrl+C after confirming).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/App.vue
git commit -m "feat: App.vue tracks activeScreen, relays screen:active:changed to children"
```

---

## Task 5: Frontend — `Live2DPet.vue` per-screen config

**Files:**
- Modify: `frontend/src/components/Live2DPet.vue`

- [ ] **Step 1: Add `activeScreen` prop and new imports**

In `Live2DPet.vue`, add `activeScreen` to `defineProps`:

```js
const props = defineProps({
  // ... existing props ...
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
})
```

Add to the existing Go imports line:

```js
import { GetBallPosition, SaveBallPosition, GetScreenSize, GetConfig, SaveConfig, GetPetSize } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: Replace `GetConfig` PetSize load with `GetPetSize`**

Find the block:

```js
let configuredSize = 0
try {
  const loadedCfg = await GetConfig()
  if (loadedCfg?.PetSize > 0) configuredSize = loadedCfg.PetSize
} catch (e) {
  console.warn('GetConfig for PetSize failed', e)
}
petSize.value = configuredSize > 0
  ? configuredSize
  : 350
```

Replace with:

```js
let configuredSize = 0
try {
  configuredSize = await GetPetSize(sw.value, sh.value)
} catch (e) {
  console.warn('GetPetSize failed', e)
}
petSize.value = configuredSize > 0 ? configuredSize : 350
```

- [ ] **Step 3: Handle `screen:active:changed` to reload position and size**

Add after the existing `offSizeChange = EventsOn(...)` block in `onMounted`:

```js
EventsOn('screen:active:changed', async (info) => {
  const w = info.width
  const h = info.height
  sw.value = w
  sh.value = h

  // Reload per-screen pet size.
  try {
    const size = await GetPetSize(w, h)
    if (size > 0) applySize(size)
  } catch (e) {
    console.warn('screen:active:changed: GetPetSize failed', e)
  }

  // Reload per-screen position.
  try {
    const [bx, by] = await GetBallPosition(w, h)
    if (bx >= 0 && by >= 0) {
      pos.value = { x: bx, y: by }
    } else {
      pos.value = { x: w - petSize.value - 40, y: h - petSize.value - 40 }
    }
  } catch (e) {
    console.warn('screen:active:changed: GetBallPosition failed', e)
  }
})
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/Live2DPet.vue
git commit -m "feat: Live2DPet reloads position and size on screen:active:changed"
```

---

## Task 6: Frontend — `ChatBubble.vue` per-screen config

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue`

- [ ] **Step 1: Add `activeScreen` prop and import `GetChatSize` / `SaveChatSize`**

Add prop:

```js
const props = defineProps({
  ballPos:      { type: Object, default: () => ({ x: -1, y: -1 }) },
  ballSize:     { type: Number, default: 64 },
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
})
```

Update import:

```js
import { ExportChatHistory, GetConfig, GetChatSize, SaveChatSize } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: Replace `GetConfig` size load with `GetChatSize`**

Find in `onMounted`:

```js
try {
  const cfg = await GetConfig()
  applySize({ width: cfg.ChatWidth, height: cfg.ChatHeight })
} catch (e) {
  console.error('load chat size failed:', e)
}
```

Replace with:

```js
try {
  const [w, h] = await GetChatSize(props.activeScreen.width, props.activeScreen.height)
  applySize({ width: w, height: h })
} catch (e) {
  console.error('load chat size failed:', e)
}
```

- [ ] **Step 3: Subscribe to `screen:active:changed` to reload size**

In `onMounted`, after the `offSizeChange` line:

```js
EventsOn('screen:active:changed', async (info) => {
  try {
    const [w, h] = await GetChatSize(info.width, info.height)
    applySize({ width: w, height: h })
  } catch (e) {
    console.warn('screen:active:changed: GetChatSize failed', e)
  }
})
```

- [ ] **Step 4: Persist chat size on resize using `SaveChatSize`**

Find the existing resize/drag logic that saves chat size. In the resize handler (look for where `bubbleW.value` and `bubbleH.value` are updated after user interaction), add a save call. If there is no existing save-on-resize, add it to the `applySize` function after the component is mounted (skip during initial mount):

```js
let mounted = false

onMounted(async () => {
  // ... existing code ...
  mounted = true
})

/** applySize updates bubble dimensions; 0 means revert to default. */
function applySize({ width, height }) {
  bubbleW.value = width  >= 300 ? width  : DEFAULT_W
  bubbleH.value = height >= 320 ? height : DEFAULT_H
  // Persist whenever the user or settings changes size after mount.
  if (mounted) {
    const sw = props.activeScreen.width
    const sh = props.activeScreen.height
    if (sw > 0 && sh > 0) {
      SaveChatSize(bubbleW.value, bubbleH.value, sw, sh).catch(e =>
        console.warn('SaveChatSize failed', e)
      )
    }
  }
}
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ChatBubble.vue
git commit -m "feat: ChatBubble loads and saves chat size per active screen"
```

---

## Task 7: Frontend — `SettingsWindow.vue` per-screen save + current screen label

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: Add `activeScreen` prop**

```js
const props = defineProps({
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
})
```

- [ ] **Step 2: Add `SavePetSize` / `SaveChatSize` imports**

Update the import line that already imports `SaveConfig`:

```js
import {
  GetConfig, SaveConfig,
  SavePetSize, SaveChatSize,
  // ... other existing imports ...
} from '../../wailsjs/go/main/App'
```

- [ ] **Step 3: Update `previewPetSize` to also save per-screen**

Find:

```js
function previewPetSize(e) {
  const size = Number(e.target.value)
  cfg.value.PetSize = size
  EventsEmit('config:pet:size:changed', size)
}
```

Replace with:

```js
/** previewPetSize emits a real-time size change and persists for the active screen. */
function previewPetSize(e) {
  const size = Number(e.target.value)
  cfg.value.PetSize = size
  EventsEmit('config:pet:size:changed', size)
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0) {
    SavePetSize(size, sw, sh).catch(err => console.warn('SavePetSize failed', err))
  }
}
```

- [ ] **Step 4: Update `previewChatSize` / `resetChatSize` to also save per-screen**

Find:

```js
function previewChatSize(field, e) {
  const val = Number(e.target.value)
  cfg.value[field] = val
  EventsEmit('config:chat:size:changed', { width: cfg.value.ChatWidth, height: cfg.value.ChatHeight })
}

function resetChatSize() {
  cfg.value.ChatWidth  = 0
  cfg.value.ChatHeight = 0
  EventsEmit('config:chat:size:changed', { width: 0, height: 0 })
}
```

Replace with:

```js
/** previewChatSize emits a real-time resize event and persists for the active screen. */
function previewChatSize(field, e) {
  const val = Number(e.target.value)
  cfg.value[field] = val
  EventsEmit('config:chat:size:changed', { width: cfg.value.ChatWidth, height: cfg.value.ChatHeight })
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0 && cfg.value.ChatWidth > 0 && cfg.value.ChatHeight > 0) {
    SaveChatSize(cfg.value.ChatWidth, cfg.value.ChatHeight, sw, sh)
      .catch(err => console.warn('SaveChatSize failed', err))
  }
}

/** resetChatSize restores default chat bubble dimensions for the active screen. */
function resetChatSize() {
  cfg.value.ChatWidth  = 0
  cfg.value.ChatHeight = 0
  EventsEmit('config:chat:size:changed', { width: 0, height: 0 })
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0) {
    SaveChatSize(0, 0, sw, sh).catch(err => console.warn('SaveChatSize failed', err))
  }
}
```

- [ ] **Step 5: Add current screen label in the UI**

Find the chat size section in the template (near `@input="previewChatSize('ChatWidth', $event)"`). Add a label just above the inputs:

```html
<div class="screen-label" v-if="activeScreen.width > 0">
  当前屏幕：{{ activeScreen.width }}×{{ activeScreen.height }}
</div>
```

Add CSS:

```css
.screen-label {
  font-size: 11px;
  color: rgba(255,255,255,0.45);
  margin-bottom: 6px;
}
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat: SettingsWindow saves pet/chat size per active screen and shows screen label"
```

---

## Task 8: Manual smoke test

- [ ] **Step 1: Build and run in dev mode**

```bash
cd /Users/xutiancheng/code/self/Aiko && wails dev
```

- [ ] **Step 2: Single monitor — verify no regression**
  - Pet appears in saved position
  - Chat bubble opens at correct size
  - Settings shows current screen resolution label

- [ ] **Step 3: Multi-monitor — verify screen migration**
  - Move mouse to secondary screen; within 1s Aiko window should migrate
  - Pet repositions to secondary screen's saved coords (or default)
  - Chat bubble resizes to secondary screen's saved size (or default)
  - Move mouse back to primary; window migrates back

- [ ] **Step 4: Persist test**
  - On secondary screen, drag pet to new position, resize chat bubble
  - Quit and relaunch
  - Move mouse to secondary screen — pet and chat bubble should restore saved config

- [ ] **Step 5: Final commit if any fixups were needed**

```bash
git add -p
git commit -m "fix: per-screen config smoke test fixups"
```

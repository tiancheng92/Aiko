# System Resource Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a system resource panel showing CPU, memory, and disk usage positioned to the left of the pet, with a show/hide toggle in the pet's right-click context menu and configurable refresh interval (default 2s).

**Architecture:** Go backend polls system stats via shell commands at a configurable interval and pushes data to the frontend via a Wails event. The Vue frontend renders a card-style panel with progress bars, positioned left of the pet using the same pattern as PomodoroPanel.

**Tech Stack:** Go 1.26 (Wails binding, shell commands, ticker), Vue 3 (Composition API, Teleport, spring animations)

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/tools/system/system_tools.go` | Modify | Export `GetCPUUsage`, `GetMemoryUsage`, `GetDiskUsage` |
| `internal/config/config.go` | Modify | Add `SystemStatsInterval` field + persistence |
| `app_system.go` | Modify | Add `GetSystemStats()` binding, stats ticker goroutine |
| `app.go` | Modify | Start/stop stats ticker in startup/shutdown |
| `frontend/src/components/SystemResourcePanel.vue` | **Create** | Panel component with CPU/memory/disk progress bars |
| `frontend/src/App.vue` | Modify | Mount panel, manage `systemPanelOpen` state |
| `frontend/src/components/Live2DPet.vue` | Modify | Add context menu item, accept `systemPanelOpen` prop |
| `frontend/src/components/VRMPet.vue` | Modify | Add context menu item, accept `systemPanelOpen` prop |
| `frontend/src/components/SettingsWindow.vue` | Modify | Add refresh interval config input |
| `frontend/src/utils/icons.js` | Modify | Add `ICON_CPU` |
| `macos.go` | Modify | Add `.system-panel` to hitTest selector |
| `frontend/src/locales/zh-CN.json` | Modify | Add translation strings |
| `frontend/src/locales/en.json` | Modify | Add translation strings |
| `frontend/src/locales/ja.json` | Modify | Add translation strings |
| `frontend/src/locales/ko.json` | Modify | Add translation strings |

---

### Task 1: Export system stats helpers from tools package

**Files:**
- Modify: `internal/tools/system/system_tools.go:267-376`

- [ ] **Step 1: Rename unexported helpers to exported names**

Rename three functions to be exported:

```go
// GetCPUUsage returns the current CPU usage percentage (0–100).
func GetCPUUsage() (float64, error) {
```

```go
// GetMemoryUsage returns used and total memory in bytes.
func GetMemoryUsage() (used, total uint64, err error) {
```

```go
// GetDiskUsage returns used and total disk bytes for the given path.
func GetDiskUsage(path string) (used, total uint64, err error) {
```

Update the call sites within the same file:
- `getCPUUsage()` → `GetCPUUsage()` at line 115 (in `GetSystemStatsTool.InvokableRun`)
- `getMemoryUsage()` → `GetMemoryUsage()` at line 119
- `getDiskUsage("/")` → `GetDiskUsage("/")` at line 124

- [ ] **Step 2: Verify compilation**

```bash
go build ./...
```

Expected: clean build, no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/system/system_tools.go
git commit -m "refactor(system): export CPU/memory/disk usage helpers for reuse"
```

---

### Task 2: Add SystemStatsInterval to config

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add field to Config struct**

After the `PomodoroRoundsBeforeLongBreak` field (line 43), add:

```go
	SystemStatsInterval int // seconds between system stats polls; default 2
```

- [ ] **Step 2: Load from DB in Load()**

After the pomodoro settings load block (around line 134), add:

```go
	cfg.SystemStatsInterval = parseInt(m["system_stats_interval"], 2)
	if cfg.SystemStatsInterval < 1 {
		cfg.SystemStatsInterval = 2
	}
```

- [ ] **Step 3: Save to DB in Save()**

In the `pairs` map within `Save()`, after the pomodoro entries (around line 199), add:

```go
		"system_stats_interval":           strconv.Itoa(cfg.SystemStatsInterval),
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add SystemStatsInterval setting"
```

---

### Task 3: Add Wails binding and stats ticker

**Files:**
- Modify: `app_system.go` (append to end of file)

- [ ] **Step 1: Add SystemStats response type and GetSystemStats binding**

Append to `app_system.go`:

```go
// SystemStats holds real-time CPU, memory, and disk usage data.
type SystemStats struct {
	CPU    float64       `json:"cpu"`
	Memory MemoryStats   `json:"memory"`
	Disk   DiskStats     `json:"disk"`
}

// MemoryStats holds memory usage data.
type MemoryStats struct {
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

// DiskStats holds disk usage data.
type DiskStats struct {
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

// GetSystemStats returns current CPU, memory, and disk usage.
func (a *App) GetSystemStats() SystemStats {
	var stats SystemStats

	cpu, err := toolsystem.GetCPUUsage()
	if err != nil {
		log.Warn().Err(err).Msg("GetSystemStats: CPU")
	} else {
		stats.CPU = cpu
	}

	memUsed, memTotal, err := toolsystem.GetMemoryUsage()
	if err != nil {
		log.Warn().Err(err).Msg("GetSystemStats: memory")
	} else {
		stats.Memory.Used = memUsed
		stats.Memory.Total = memTotal
		if memTotal > 0 {
			stats.Memory.Percent = float64(memUsed) / float64(memTotal) * 100
		}
	}

	diskUsed, diskTotal, err := toolsystem.GetDiskUsage("/")
	if err != nil {
		log.Warn().Err(err).Msg("GetSystemStats: disk")
	} else {
		stats.Disk.Used = diskUsed
		stats.Disk.Total = diskTotal
		if diskTotal > 0 {
			stats.Disk.Percent = float64(diskUsed) / float64(diskTotal) * 100
		}
	}

	return stats
}
```

Add the import for `toolsystem` at the top of the file:

```go
	toolsystem "aiko/internal/tools/system"
```

Note: `app_system.go` already imports `"aiko/internal/tools"` as `internaltools`. Check the existing import block and add the new import alongside it.

- [ ] **Step 2: Add ticker management fields to App struct**

In `app.go`, find the `App` struct definition and add these fields alongside the other ticker-related fields (e.g., near `cancelWatcher` and `watcherWG`):

```go
	cancelStats     context.CancelFunc
	statsWG         sync.WaitGroup
```

- [ ] **Step 3: Add startStatsTicker and stopStatsTicker methods**

Append to `app_system.go`:

```go
// startStatsTicker begins a goroutine that polls system stats at the configured
// interval and emits "stats:update" Wails events. Call from startup() after cfg is loaded.
func (a *App) startStatsTicker() {
	a.mu.RLock()
	interval := a.cfg.SystemStatsInterval
	a.mu.RUnlock()

	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.cancelStats = cancel
	a.mu.Unlock()

	a.statsWG.Go(func() {
		defer cancel()
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := a.GetSystemStats()
				wailsruntime.EventsEmit(a.ctx, "stats:update", stats)
			}
		}
	})
}

// stopStatsTicker cancels the stats polling goroutine and waits for it to exit.
func (a *App) stopStatsTicker() {
	a.mu.Lock()
	if a.cancelStats != nil {
		a.cancelStats()
	}
	a.mu.Unlock()
	a.statsWG.Wait()
}

// RestartStatsTicker restarts the stats polling goroutine with a new interval.
// Called after the user changes the interval in settings.
func (a *App) RestartStatsTicker() {
	a.stopStatsTicker()
	a.startStatsTicker()
}
```

Need to add `"context"`, `"sync"`, `"time"`, and `wailsruntime` imports if not already present in `app_system.go`. Check existing imports — `app_system.go` already imports `"context"`, `"time"`, and `wailsruntime` via existing usage. `"sync"` needs to be added for `sync.WaitGroup` — but `a.statsWG` is a field on `App`, not a local. The `sync.WaitGroup` type is used in the field definition in `app.go`. So just verify `app_system.go` has the needed imports.

Actually, looking at the code more carefully: `startStatsTicker` and `stopStatsTicker` are methods on `*App`. The `statsWG` field needs `sync.WaitGroup` type which is already available since `app.go` imports `"sync"`. The go1.26 `wg.Go()` syntax is:

```go
import "sync"

// in App struct:
statsWG sync.WaitGroup

// usage:
a.statsWG.Go(func() {
    // ...
})
```

Wait, actually Go 1.25+ added `WaitGroup.Go()` method. This is a new feature. Let me verify whether the project uses this pattern already.

Looking at app_layout.go line 78-149, the screenWatcher uses a traditional pattern: `a.watcherWG.Add(1); go func() { defer a.watcherWG.Done(); ... }()`. So the project uses the traditional pattern.

Let me follow the same pattern to be consistent.

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add app_system.go app.go
git commit -m "feat(system): add GetSystemStats binding and stats polling ticker"
```

---

### Task 4: Start/stop stats ticker in app startup/shutdown

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Start ticker in startup()**

In `startup()`, after the proactive engine start (line 256), add:

```go
	// Start system stats polling ticker.
	a.startStatsTicker()
```

- [ ] **Step 2: Stop ticker in shutdown()**

In the `shutdown()` method, alongside the other cleanup (find where `stopScreenWatcher` and similar are called), add:

```go
	// Stop system stats polling.
	a.stopStatsTicker()
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add app.go
git commit -m "feat(app): start/stop stats ticker on startup/shutdown"
```

---

### Task 5: Add ICON_CPU to icons.js

**Files:**
- Modify: `frontend/src/utils/icons.js`

- [ ] **Step 1: Add ICON_CPU icon**

After the `ICON_POMODORO` line (line 40), add:

```js
// System resource panel
export const ICON_CPU     = SVG('<rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 2v2M15 2v2M9 20v2M15 20v2M20 9h2M20 15h2M2 9h2M2 15h2"/>')
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/utils/icons.js
git commit -m "feat(icons): add ICON_CPU for system resource panel"
```

---

### Task 6: Create SystemResourcePanel.vue

**Files:**
- Create: `frontend/src/components/SystemResourcePanel.vue`

- [ ] **Step 1: Create the component**

```vue
<template>
  <Teleport to="body">
    <Transition
      :css="false"
      @enter="onEnter"
      @leave="onLeave"
    >
      <div
        v-if="visible"
        class="system-panel"
        :style="panelStyle"
      >
        <!-- CPU -->
        <div class="stat-row">
          <div class="stat-icon" v-html="ICON_CPU" />
          <div class="stat-info">
            <div class="stat-label">CPU</div>
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{ width: cpu + '%', background: barColor(cpu) }"
              />
            </div>
          </div>
          <div class="stat-value" :style="{ color: barColor(cpu) }">{{ cpu.toFixed(0) }}%</div>
        </div>

        <!-- Memory -->
        <div class="stat-row">
          <div class="stat-label mem-label">MEM</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{ width: memory.percent + '%', background: barColor(memory.percent) }"
              />
            </div>
          </div>
          <div class="stat-value" :style="{ color: barColor(memory.percent) }">{{ memory.percent.toFixed(0) }}%</div>
        </div>

        <!-- Disk -->
        <div class="stat-row">
          <div class="stat-label disk-label">DSK</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{ width: disk.percent + '%', background: barColor(disk.percent) }"
              />
            </div>
          </div>
          <div class="stat-value" :style="{ color: barColor(disk.percent) }">{{ disk.percent.toFixed(0) }}%</div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { GetSystemStats } from '../../wailsjs/go/main/App'
import { springAnimate } from '../composables/useSpring'
import { ICON_CPU } from '../utils/icons'

const props = defineProps({
  petPos: { type: Object, required: true },
  petSize: { type: Number, default: 160 },
})

const visible = ref(false)
const cpu = ref(0)
const memory = ref({ used: 0, total: 0, percent: 0 })
const disk = ref({ used: 0, total: 0, percent: 0 })

let offStatsUpdate = null
let cancelAnim = null

const panelWidth = 170
const panelHeight = 108

const panelStyle = computed(() => {
  const x = props.petPos.x - panelWidth - 12
  const y = props.petPos.y + props.petSize / 2
  const clampedX = Math.min(Math.max(x, 8), window.innerWidth - panelWidth - 8)
  const clampedY = Math.min(Math.max(y - panelHeight / 2, 38), window.innerHeight - panelHeight - 8)
  return {
    left: `${clampedX}px`,
    top: `${clampedY}px`,
  }
})

function barColor(pct) {
  if (pct >= 90) return '#EF4444'
  if (pct >= 70) return '#F59E0B'
  return '#10B981'
}

function show() {
  visible.value = true
  GetSystemStats().then((st) => {
    cpu.value = st.cpu
    memory.value = st.memory
    disk.value = st.disk
  }).catch(() => {})
}

const prefersReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches

function onEnter(el, done) {
  if (prefersReduced) { el.style.opacity = '1'; done(); return }
  cancelAnim = springAnimate({
    from: 0, to: 1, stiffness: 320, damping: 22,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { done() },
  })
}

function onLeave(el, done) {
  if (prefersReduced) { el.style.opacity = '0'; done(); return }
  cancelAnim = springAnimate({
    from: 1, to: 0, stiffness: 400, damping: 36,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { done() },
  })
}

onMounted(() => {
  offStatsUpdate = EventsOn('stats:update', (st) => {
    cpu.value = st.cpu
    memory.value = st.memory
    disk.value = st.disk
  })
})

onUnmounted(() => {
  offStatsUpdate?.()
  cancelAnim?.()
})

defineExpose({ show })
</script>

<style scoped>
.system-panel {
  position: fixed;
  z-index: 2001;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  background: rgba(15, 23, 42, 0.92);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border-radius: 14px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
  user-select: none;
}

.stat-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.stat-icon {
  width: 16px;
  height: 16px;
  color: rgba(255, 255, 255, 0.5);
  flex-shrink: 0;
}

.stat-icon :deep(svg) {
  width: 16px;
  height: 16px;
}

.stat-label {
  width: 28px;
  font-size: 10px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.5);
  flex-shrink: 0;
  text-align: right;
}

.stat-info {
  flex: 1;
  min-width: 0;
}

.stat-bar-track {
  width: 100%;
  height: 4px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 2px;
  overflow: hidden;
}

.stat-bar-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.4s ease, background 0.3s ease;
}

.stat-value {
  width: 34px;
  font-size: 11px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  text-align: right;
  flex-shrink: 0;
}
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/SystemResourcePanel.vue
git commit -m "feat(frontend): add SystemResourcePanel component"
```

---

### Task 7: Update App.vue for system resource panel

**Files:**
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: Import SystemResourcePanel**

Add import alongside the PomodoroPanel import (line 8):

```js
import SystemResourcePanel from './components/SystemResourcePanel.vue'
```

- [ ] **Step 2: Add state variables**

After `pomodoroPanelWasOpen` (line 22), add:

```js
const systemPanelOpen = ref(true)
const systemPanelRef = ref(null)
const systemPanelWasOpen = ref(false)
```

- [ ] **Step 3: Add toggle and close handlers**

After `closePomodoro()` (line 451), add:

```js
/** toggleSystemPanel toggles the system resource panel visibility. */
function toggleSystemPanel() {
  if (systemPanelOpen.value) {
    systemPanelOpen.value = false
    return
  }
  systemPanelOpen.value = true
  systemPanelWasOpen.value = false
  nextTick(() => {
    systemPanelRef.value?.show()
  })
}

function closeSystemPanel() {
  systemPanelOpen.value = false
}
```

- [ ] **Step 4: Update toggleBubble to also handle system panel**

In `toggleBubble()`, update the chat-open block (line 414) to also hide the system panel:

```js
function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
  if (bubbleOpen.value) {
    if (pomodoroPanelOpen.value) {
      pomodoroPanelWasOpen.value = true
      pomodoroPanelOpen.value = false
    }
    if (systemPanelOpen.value) {
      systemPanelWasOpen.value = true
      systemPanelOpen.value = false
    }
    pendingTokens = ''
    nextTick(() => {
      chatBubbleRef.value?.focusInput()
      chatBubbleRef.value?.scrollToBottom()
    })
  } else {
    if (pomodoroPanelWasOpen.value) {
      pomodoroPanelWasOpen.value = false
      pomodoroPanelOpen.value = true
      nextTick(() => { pomodoroPanelRef.value?.show() })
    }
    if (systemPanelWasOpen.value) {
      systemPanelWasOpen.value = false
      systemPanelOpen.value = true
      nextTick(() => { systemPanelRef.value?.show() })
    }
  }
}
```

- [ ] **Step 5: Add props and event handlers to pet component tags**

Update both `<Live2DPet>` and `<VRMPet>` tags — add `:system-panel-open` prop and `@toggle-system-panel` event:

For Live2DPet (around line 561):
```html
  <Live2DPet
    v-if="renderBackend === 'live2d'"
    :active-screen="activeScreen"
    :pomodoro-panel-open="pomodoroPanelOpen"
    :system-panel-open="systemPanelOpen"
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
    @open-settings="openSettings"
    @open-pomodoro="openPomodoro"
    @toggle-system-panel="toggleSystemPanel"
  />
```

For VRMPet (around line 572):
```html
  <VRMPet
    v-else-if="renderBackend === 'vrm'"
    :active-screen="activeScreen"
    :pomodoro-panel-open="pomodoroPanelOpen"
    :system-panel-open="systemPanelOpen"
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
    @open-settings="openSettings"
    @open-pomodoro="openPomodoro"
    @toggle-system-panel="toggleSystemPanel"
  />
```

- [ ] **Step 6: Mount SystemResourcePanel in template**

After the PomodoroPanel block (around line 610), add:

```html
  <SystemResourcePanel
    v-if="systemPanelOpen"
    ref="systemPanelRef"
    :pet-pos="ballPos"
    :pet-size="ballSize"
    @close="closeSystemPanel"
  />
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/App.vue
git commit -m "feat(frontend): integrate system resource panel into App"
```

---

### Task 8: Update pet components for context menu and props

**Files:**
- Modify: `frontend/src/components/Live2DPet.vue`
- Modify: `frontend/src/components/VRMPet.vue`

- [ ] **Step 1: Update Live2DPet.vue**

**a) Add `systemPanelOpen` prop** — alongside the existing `pomodoroPanelOpen` prop (around line 18):

```js
const props = defineProps({
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
  pomodoroPanelOpen: { type: Boolean, default: false },
  systemPanelOpen: { type: Boolean, default: true },
})
```

**b) Add `ICON_CPU` import** — in the import from `../utils/icons` (find existing import at top of `<script setup>`):

```js
import { ICON_FACE, ICON_SHIRT, ICON_SETTING, ICON_POWER, ICON_POMODORO, ICON_CPU } from '../utils/icons'
```

**c) Add menu item** — in `petMenuItems` computed, after the pomodoro item (around line 71):

```js
const petMenuItems = computed(() => [
  { iconSvg: ICON_FACE,    label: t('petMenu.switchExpression'), action: cycleExpression },
  { iconSvg: ICON_SHIRT,   label: t('petMenu.switchModel'), action: switchToNextModel },
  { iconSvg: ICON_POMODORO, label: props.pomodoroPanelOpen ? t('pomodoro.menuLabelHide') : t('pomodoro.menuLabelShow'), action: () => emit('open-pomodoro') },
  { iconSvg: ICON_CPU,     label: props.systemPanelOpen ? t('system.hidePanel') : t('system.showPanel'), action: () => emit('toggle-system-panel') },
  { divider: true },
  { iconSvg: ICON_SETTING, label: t('petMenu.openSettings'), action: () => emit('open-settings') },
  { divider: true },
  { iconSvg: ICON_POWER,   label: t('petMenu.quitApp'), action: () => Quit(), danger: true },
])
```

**d) Add emit** — update the `defineEmits` call:

```js
const emit = defineEmits(['position', 'ball-size', 'click', 'open-settings', 'open-pomodoro', 'toggle-system-panel'])
```

- [ ] **Step 2: Update VRMPet.vue**

Same four changes as Live2DPet:

**a) Add `systemPanelOpen` prop** — alongside existing props (around line 40):

```js
const props = defineProps({
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
  pomodoroPanelOpen: { type: Boolean, default: false },
  systemPanelOpen: { type: Boolean, default: true },
})
```

**b) Add `ICON_CPU` import** — find the import from `../utils/icons`:

```js
import { ICON_SHIRT, ICON_SETTING, ICON_POWER, ICON_POMODORO, ICON_CPU } from '../utils/icons'
```

**c) Add menu item** — in `petMenuItems` computed (around line 479):

```js
const petMenuItems = computed(() => [
  { iconSvg: ICON_SHIRT, label: t('petMenu.switchModel'), action: switchToNextVRMModel },
  { iconSvg: ICON_POMODORO, label: props.pomodoroPanelOpen ? t('pomodoro.menuLabelHide') : t('pomodoro.menuLabelShow'), action: () => emit("open-pomodoro") },
  { iconSvg: ICON_CPU,   label: props.systemPanelOpen ? t('system.hidePanel') : t('system.showPanel'), action: () => emit('toggle-system-panel') },
  { divider: true },
  { iconSvg: ICON_SETTING, label: t('petMenu.openSettings'), action: () => emit("open-settings") },
  { divider: true },
  { iconSvg: ICON_POWER,  label: t('petMenu.quitApp'), action: () => Quit(), danger: true },
])
```

**d) Add emit** — update the `defineEmits` call:

```js
const emit = defineEmits(["position", "ball-size", "click", "open-settings", "open-pomodoro", "toggle-system-panel"])
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/Live2DPet.vue frontend/src/components/VRMPet.vue
git commit -m "feat(frontend): add system panel toggle to pet context menus"
```

---

### Task 9: Update SettingsWindow.vue with refresh interval config

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: Add config row in the general tab pane**

Insert after line 1619 (after the shortcut `</div>` closing tag, before the general tab pane closing `</div>` at line 1620):

```html
          <!-- 系统资源 -->
          <div class="group-label">{{ $t('system.settings.title') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <span class="row-title">{{ $t('system.settings.interval') }}</span>
                <span class="row-desc">{{ $t('system.settings.intervalDesc') }}</span>
              </div>
              <div class="row-ctrl">
                <input
                  v-model.number="cfg.SystemStatsInterval"
                  type="number"
                  min="1"
                  max="60"
                  class="input-int"
                />
                <span class="row-unit">s</span>
              </div>
            </div>
          </div>
```

Note: `cfg` is the reactive object holding all config fields, synced from `GetConfig()` on mount. The `deep` watcher on `cfg` triggers `debouncedSave`, which calls `SaveConfig()` with the full config payload — `SystemStatsInterval` will be included automatically.

- [ ] **Step 2: Add RestartStatsTicker call after config save**

**a) Add import** — at line 7, alongside `SaveConfig`:

```
GetConfig, SaveConfig, RestartStatsTicker,
```

**b) Call RestartStatsTicker after save** — in the `save()` function, after `await SaveConfig(payload)` (around line 748), add:

```js
    await RestartStatsTicker()
```

This restarts the Go backend ticker with the new interval so the change takes effect immediately.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(frontend): add system stats refresh interval to settings"
```

---

### Task 10: Update macos.go hitTest selector

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: Add `.system-panel` to hitTest selector**

At line 356, change:

```
"return !!(e.closest('.live2d-pet,.vrm-pet,.chat-bubble,.pomodoro-panel,.settings-win,.ctx-menu,.notif-bubble,.execution-progress,.resize-handle,.lightbox-fullscreen'));"
```

To:

```
"return !!(e.closest('.live2d-pet,.vrm-pet,.chat-bubble,.pomodoro-panel,.system-panel,.settings-win,.ctx-menu,.notif-bubble,.execution-progress,.resize-handle,.lightbox-fullscreen'));"
```

- [ ] **Step 2: Commit**

```bash
git add macos.go
git commit -m "fix(macos): add system-panel to hitTest selector"
```

---

### Task 11: Add i18n translations

**Files:**
- Modify: `frontend/src/locales/zh-CN.json`
- Modify: `frontend/src/locales/en.json`
- Modify: `frontend/src/locales/ja.json`
- Modify: `frontend/src/locales/ko.json`

- [ ] **Step 1: Add translations to zh-CN.json**

Find the pomodoro section and add a new `system` section. Insert before the closing `}` of the top-level object:

```json
  "system": {
    "showPanel": "显示系统资源",
    "hidePanel": "隐藏系统资源",
    "settings": {
      "title": "系统资源",
      "interval": "刷新间隔",
      "intervalDesc": "CPU、内存、磁盘使用率的数据刷新频率"
    }
  }
```

- [ ] **Step 2: Add translations to en.json**

```json
  "system": {
    "showPanel": "Show System Panel",
    "hidePanel": "Hide System Panel",
    "settings": {
      "title": "System Resource",
      "interval": "Refresh Interval",
      "intervalDesc": "How often to refresh CPU, memory, and disk usage data"
    }
  }
```

- [ ] **Step 3: Add translations to ja.json**

```json
  "system": {
    "showPanel": "システムリソースを表示",
    "hidePanel": "システムリソースを非表示",
    "settings": {
      "title": "システムリソース",
      "interval": "更新間隔",
      "intervalDesc": "CPU、メモリ、ディスク使用率のデータ更新頻度"
    }
  }
```

- [ ] **Step 4: Add translations to ko.json**

```json
  "system": {
    "showPanel": "시스템 리소스 표시",
    "hidePanel": "시스템 리소스 숨기기",
    "settings": {
      "title": "시스템 리소스",
      "interval": "새로고침 간격",
      "intervalDesc": "CPU, 메모리, 디스크 사용량 데이터 새로고침 빈도"
    }
  }
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/locales/zh-CN.json frontend/src/locales/en.json frontend/src/locales/ja.json frontend/src/locales/ko.json
git commit -m "feat(i18n): add system resource panel translations"
```

---

### Task 12: Regenerate Wails bindings and verify build

**Files:**
- Auto-generated: `frontend/src/wailsjs/` (do not manually edit)

- [ ] **Step 1: Regenerate Wails bindings**

```bash
wails generate module
```

- [ ] **Step 2: Build the full project**

```bash
make build
```

Expected: clean build, no errors. If the build fails, read the error and fix any import/type issues in previous tasks.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/wailsjs/
git commit -m "chore: regenerate Wails bindings for system stats"
```

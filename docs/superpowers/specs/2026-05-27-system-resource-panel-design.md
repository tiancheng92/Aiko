# System Resource Panel Design

## Overview

Add a system resource panel that displays real-time CPU, memory, and disk usage. The panel is positioned to the left of the pet, shown by default, with a show/hide toggle in the pet's right-click context menu. The refresh interval is configurable in settings (default 2s).

## Backend

### New Binding: `GetSystemStats`

- **File**: `app_system.go` (add method to existing file)
- Returns structured JSON: `{cpu: float64, memory: {used: uint64, total: uint64, percent: float64}, disk: {used: uint64, total: uint64, percent: float64}}`
- Reuses existing shell-command helpers from `internal/tools/system/system_tools.go` (`getCPUUsage`, `getMemoryUsage`, `getDiskUsage`)
- Since those helpers are unexported (lowercase), extract them into a shared location or add new exported wrappers in the system tools package

### Stats Polling Ticker

- **File**: `app_system.go` (or new `app_stats.go`)
- A goroutine that polls system stats at the configured interval, emitting `stats:update` Wails event
- Managed via `a.ctx` cancellation and a WaitGroup (same pattern as `startScreenWatcher`)
- On config change (interval updated), restart the ticker with the new interval

### Config: `system_stats_interval`

- **File**: `internal/config/config.go`
- New field `SystemStatsInterval int` (seconds, default `2`)
- Stored in `settings` table with key `system_stats_interval`

## Frontend

### New Component: `SystemResourcePanel.vue`

- **Positioning**: `position: fixed`, left of the pet, vertically centered on pet. Clamped to viewport bounds.
  - `left = petPos.x - panelWidth - 12` (12px gap)
  - `top = petPos.y + petSize / 2` (vertically centered), then adjusted via `transform: translateY(-50%)`
- **Props**: `petPos: {x, y}`, `petSize: number`
- **Content**: three rows (CPU / Memory / Disk), each with:
  - Icon + label
  - Percentage number
  - Progress bar (colored by threshold: green < 70%, yellow < 90%, red >= 90%)
- **Animation**: spring-based enter/leave via `useSpring`, same pattern as PomodoroPanel
- **Data**: listens to `stats:update` Wails event, updates reactive refs
- Rendered via `<Teleport to="body">`

### App.vue Changes

- New state: `systemPanelOpen` (ref, default `true`)
- Conditional render `<SystemResourcePanel>` when `systemPanelOpen` is true
- Same pattern as PomodoroPanel: save/restore state when chat bubble opens/closes

### Context Menu Changes

- Both `Live2DPet.vue` and `VRMPet.vue`: add menu item for system resource panel toggle
- Label flips between "Hide System Panel" / "Show System Panel" based on `systemPanelOpen` prop
- Emit `@toggle-system-panel` event upward to App.vue

### SettingsWindow.vue Changes

- New section "System Resource" with a number input for refresh interval (seconds)
- Range: 1–60 seconds
- Calls `SaveConfig` to persist

### Icons

- New `ICON_CPU` in `icons.js` for the context menu item and panel header

## Data Flow

```
Settings change → SaveConfig → restartGoTicker(newInterval)
                                  ↓
Go ticker (every N seconds) → exec shell commands → stats:update event
                                  ↓
SystemResourcePanel.vue listens → update cpu/mem/disk refs → reactive re-render
```

## Events

| Event | Direction | Payload |
|---|---|---|
| `stats:update` | backend→frontend | `{cpu, memory: {used, total, percent}, disk: {used, total, percent}}` |

## Files Changed

| File | Change |
|---|---|
| `internal/tools/system/system_tools.go` | Export `GetCPUUsage`, `GetMemoryUsage`, `GetDiskUsage` (or add public wrappers) |
| `app_system.go` | Add `GetSystemStats()` binding, ticker goroutine with configurable interval |
| `internal/config/config.go` | Add `SystemStatsInterval` field + persistence |
| `frontend/src/components/SystemResourcePanel.vue` | **New** — panel component |
| `frontend/src/App.vue` | Mount panel, manage `systemPanelOpen` state |
| `frontend/src/components/Live2DPet.vue` | Add context menu item |
| `frontend/src/components/VRMPet.vue` | Add context menu item |
| `frontend/src/components/SettingsWindow.vue` | Add interval config |
| `frontend/src/utils/icons.js` | Add `ICON_CPU` |
| `frontend/src/i18n/` | Add translation strings |
| `macos.go` | Add `.system-panel` to hitTest selector |

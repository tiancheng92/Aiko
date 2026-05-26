# Pomodoro Timer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a floating Pomodoro timer panel to Aiko desktop pet with customizable intervals, pet state integration, and system notifications.

**Architecture:** Backend `pomodoro.Engine` manages countdown lifecycle, emits Wails events to frontend. Frontend `PomodoroPanel.vue` renders glassmorphism panel above pet. Engine is independent of LLM components, initialized in `startup()`.

**Tech Stack:** Go 1.26, Vue 3 Composition API, Wails v2 runtime events, SQLite settings table

---

## File Structure

| Action | File | Purpose |
|--------|------|---------|
| Create | `internal/pomodoro/engine.go` | Countdown engine + state machine |
| Create | `internal/pomodoro/engine_test.go` | Unit tests for engine |
| Create | `app_pomodoro.go` | Wails bindings |
| Create | `frontend/src/components/PomodoroPanel.vue` | Floating timer panel |
| Modify | `internal/config/config.go` | Add 4 pomodoro config fields |
| Modify | `app.go` | Add field, init in startup, stop in shutdown |
| Modify | `frontend/src/composables/usePetState.js` | Add focusing/resting states |
| Modify | `frontend/src/components/Live2DPet.vue` | Add pomodoro menu item |
| Modify | `frontend/src/components/VRMPet.vue` | Add pomodoro menu item |
| Modify | `frontend/src/App.vue` | Coordinate panel visibility + mutual exclusion |
| Modify | `frontend/src/components/SettingsWindow.vue` | Add pomodoro settings tab |

---

### Task 1: Add pomodoro config fields to Config, Load, Save

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add fields to Config struct**

Add after `ChatHeight` field (line 39):

```go
// Pomodoro timer settings (minutes).
PomodoroFocusDuration        int // default 25
PomodoroShortBreakDuration   int // default 5
PomodoroLongBreakDuration    int // default 15
PomodoroRoundsBeforeLongBreak int // default 4
```

- [ ] **Step 2: Add defaults in Load()**

Add after `cfg.ChatHeight = parseInt(m["chat_height"], 0)` (line 125):

```go
cfg.PomodoroFocusDuration = parseInt(m["pomodoro_focus_duration"], 25)
cfg.PomodoroShortBreakDuration = parseInt(m["pomodoro_short_break_duration"], 5)
cfg.PomodoroLongBreakDuration = parseInt(m["pomodoro_long_break_duration"], 15)
cfg.PomodoroRoundsBeforeLongBreak = parseInt(m["pomodoro_rounds_before_long_break"], 4)
```

- [ ] **Step 3: Add key-value pairs in Save()**

Add inside the `pairs` map literal (before `"language": cfg.Language` on line 187):

```go
"pomodoro_focus_duration":           strconv.Itoa(cfg.PomodoroFocusDuration),
"pomodoro_short_break_duration":     strconv.Itoa(cfg.PomodoroShortBreakDuration),
"pomodoro_long_break_duration":      strconv.Itoa(cfg.PomodoroLongBreakDuration),
"pomodoro_rounds_before_long_break": strconv.Itoa(cfg.PomodoroRoundsBeforeLongBreak),
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add pomodoro timer settings fields"
```

---

### Task 2: Create pomodoro engine

**Files:**
- Create: `internal/pomodoro/engine.go`
- Create: `internal/pomodoro/engine_test.go`

- [ ] **Step 1: Write engine test**

Create `internal/pomodoro/engine_test.go`:

```go
package pomodoro

import (
	"testing"
	"time"
)

func TestEngineLifecycle(t *testing.T) {
	cfg := Config{
		FocusDuration:          25,
		ShortBreakDuration:     5,
		LongBreakDuration:      15,
		RoundsBeforeLongBreak:  4,
	}
	e := New(cfg)

	if e.Status().State != StateIdle {
		t.Fatalf("expected idle, got %s", e.Status().State)
	}

	e.Start()
	if s := e.Status().State; s != StateRunning {
		t.Fatalf("expected running after start, got %s", s)
	}
	if s := e.Status().Phase; s != PhaseFocus {
		t.Fatalf("expected focus phase, got %s", s)
	}

	e.Pause()
	if s := e.Status().State; s != StatePaused {
		t.Fatalf("expected paused, got %s", s)
	}

	e.Resume()
	if s := e.Status().State; s != StateRunning {
		t.Fatalf("expected running after resume, got %s", s)
	}

	e.Stop()
	if s := e.Status().State; s != StateIdle {
		t.Fatalf("expected idle after stop, got %s", s)
	}
}

func TestTickCallback(t *testing.T) {
	cfg := Config{
		FocusDuration:          25,
		ShortBreakDuration:     5,
		LongBreakDuration:      15,
		RoundsBeforeLongBreak:  4,
	}
	e := New(cfg)

	var ticks []TickPayload
	e.OnTick = func(p TickPayload) { ticks = append(ticks, p) }

	e.Start()
	time.Sleep(2100 * time.Millisecond) // allow 2 ticks
	e.Stop()

	if len(ticks) < 2 {
		t.Fatalf("expected at least 2 ticks, got %d", len(ticks))
	}
	if ticks[0].Phase != "focus" {
		t.Fatalf("expected focus phase in tick, got %s", ticks[0].Phase)
	}
	if ticks[0].Round != 1 {
		t.Fatalf("expected round 1, got %d", ticks[0].Round)
	}
	// remaining should decrease
	if ticks[0].Remaining <= ticks[1].Remaining {
		t.Fatal("expected remaining to decrease across ticks")
	}
}

func TestPhaseTransition(t *testing.T) {
	cfg := Config{
		FocusDuration:          0, // 0 minutes = immediate transition for testing
		ShortBreakDuration:     5,
		LongBreakDuration:      15,
		RoundsBeforeLongBreak:  4,
	}
	e := New(cfg)

	var phases []Phase
	e.OnPhaseChange = func(p PhasePayload) { phases = append(phases, p.Phase) }
	e.OnStateChange = func(p StatePayload) {}

	e.Start()
	time.Sleep(500 * time.Millisecond) // allow transition to happen

	e.mu.Lock()
	phase := e.phase
	e.mu.Unlock()

	if phase != PhaseShortBreak {
		t.Fatalf("expected short_break after focus ends, got %s", phase)
	}
	if len(phases) < 1 {
		t.Fatal("expected at least one phase change")
	}
	e.Stop()
}

func TestStatusImmutable(t *testing.T) {
	cfg := Config{
		FocusDuration:          25,
		ShortBreakDuration:     5,
		LongBreakDuration:      15,
		RoundsBeforeLongBreak:  4,
	}
	e := New(cfg)
	e.Start()
	st := e.Status()
	// mutate the returned struct
	st.State = "hacked"
	// engine state must be unaffected
	if e.Status().State != StateRunning {
		t.Fatal("Status() must return a copy, not a reference")
	}
	e.Stop()
}
```

- [ ] **Step 2: Run tests and confirm they fail**

Run: `go test ./internal/pomodoro/... -v -count=1`
Expected: compilation error — package pomodoro does not exist

- [ ] **Step 3: Write engine implementation**

Create `internal/pomodoro/engine.go`:

```go
// Package pomodoro implements a Pomodoro Technique countdown timer engine.
//
// The engine manages a state machine (idle → running ⇄ paused) across focus
// and break phases, emitting callbacks for ticks, phase changes, and state
// transitions. It uses wall-clock time (endTime) rather than a monotonic
// ticker, so system sleep is handled correctly: on wake the remaining time
// is recalculated and overdue phases auto-advance.
package pomodoro

import (
	"sync"
	"time"
)

// State represents the timer's run state.
type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StatePaused  State = "paused"
)

// Phase represents the current Pomodoro phase.
type Phase string

const (
	PhaseFocus      Phase = "focus"
	PhaseShortBreak Phase = "short_break"
	PhaseLongBreak  Phase = "long_break"
)

// Config holds the customizable Pomodoro durations (all in minutes).
type Config struct {
	FocusDuration           int
	ShortBreakDuration      int
	LongBreakDuration       int
	RoundsBeforeLongBreak   int
}

// TickPayload is sent on every tick (1 Hz) while running.
type TickPayload struct {
	Remaining int    `json:"remaining"`
	Phase     string `json:"phase"`
	Round     int    `json:"round"`
}

// PhasePayload is sent when a phase changes (focus → break, etc.).
type PhasePayload struct {
	Phase   string `json:"phase"`
	Message string `json:"message"` // pet speech text
}

// StatePayload is sent when the run state changes.
type StatePayload struct {
	State string `json:"state"`
}

// StatusPayload is the full engine status returned by Status().
type StatusPayload struct {
	State        string `json:"state"`
	Phase        string `json:"phase"`
	Remaining    int    `json:"remaining"`
	CurrentRound int    `json:"currentRound"`
	TotalRounds  int    `json:"totalRounds"`
	Config       Config `json:"config"`
}

// Engine drives the Pomodoro countdown lifecycle.
type Engine struct {
	mu           sync.Mutex
	cfg          Config
	state        State
	phase        Phase
	currentRound int
	endTime      time.Time // wall-clock time when current phase ends

	ticker *time.Ticker
	done   chan struct{}

	OnTick        func(TickPayload)
	OnPhaseChange func(PhasePayload)
	OnStateChange func(StatePayload)
}

// New creates a new Engine with the given config. Callbacks are nil
// initially — set them before calling Start.
func New(cfg Config) *Engine {
	return &Engine{
		cfg:          cfg,
		state:        StateIdle,
		phase:        PhaseFocus,
		currentRound: 1,
	}
}

// Start begins or resumes countdown from the current phase. If the engine
// is paused it resumes; if idle it starts a focus phase.
func (e *Engine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()

	switch e.state {
	case StateRunning:
		return // already running
	case StatePaused:
		// Resume with remaining duration from endTime.
		remaining := time.Until(e.endTime)
		if remaining <= 0 {
			// Timer expired while paused (edge case from system sleep).
			remaining = 0
		}
		e.endTime = time.Now().Add(remaining)
	default:
		// Idle — start fresh focus phase.
		e.phase = PhaseFocus
		e.currentRound = 1
		e.endTime = time.Now().Add(time.Duration(e.cfg.FocusDuration) * time.Minute)
	}

	e.state = StateRunning
	e.startTicker()

	if e.OnStateChange != nil {
		e.OnStateChange(StatePayload{State: string(StateRunning)})
	}
}

// Pause suspends the countdown without resetting.
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != StateRunning {
		return
	}
	e.state = StatePaused
	e.stopTicker()

	if e.OnStateChange != nil {
		e.OnStateChange(StatePayload{State: string(StatePaused)})
	}
}

// Resume continues a paused countdown.
func (e *Engine) Resume() {
	e.Start() // Start handles Paused → Running transition.
}

// Stop ends the current session and resets to idle.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stopTicker()
	e.state = StateIdle
	e.phase = PhaseFocus
	e.currentRound = 1

	if e.OnStateChange != nil {
		e.OnStateChange(StatePayload{State: string(StateIdle)})
	}
}

// Status returns a snapshot of current engine state.
func (e *Engine) Status() StatusPayload {
	e.mu.Lock()
	defer e.mu.Unlock()

	remaining := int(time.Until(e.endTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	return StatusPayload{
		State:        string(e.state),
		Phase:        string(e.phase),
		Remaining:    remaining,
		CurrentRound: e.currentRound,
		TotalRounds:  e.cfg.RoundsBeforeLongBreak,
		Config:       e.cfg,
	}
}

// startTicker launches the 1 Hz tick loop. Must be called under mu.
func (e *Engine) startTicker() {
	e.done = make(chan struct{})
	e.ticker = time.NewTicker(time.Second)

	go func() {
		for {
			select {
			case <-e.ticker.C:
				e.tick()
			case <-e.done:
				return
			}
		}
	}()
}

// stopTicker stops the tick loop. Must be called under mu.
func (e *Engine) stopTicker() {
	if e.ticker != nil {
		e.ticker.Stop()
		e.ticker = nil
	}
	if e.done != nil {
		close(e.done)
		e.done = nil
	}
}

// tick handles one tick: emit tick event, check for phase expiry.
func (e *Engine) tick() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != StateRunning {
		return
	}

	remaining := int(time.Until(e.endTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	if e.OnTick != nil {
		e.OnTick(TickPayload{
			Remaining: remaining,
			Phase:     string(e.phase),
			Round:     e.currentRound,
		})
	}

	if remaining <= 0 {
		e.advancePhase()
	}
}

// advancePhase transitions to the next phase. Must be called under mu.
func (e *Engine) advancePhase() {
	switch e.phase {
	case PhaseFocus:
		// Focus → Break. Determine short vs long break.
		if e.currentRound >= e.cfg.RoundsBeforeLongBreak {
			e.phase = PhaseLongBreak
			e.endTime = time.Now().Add(time.Duration(e.cfg.LongBreakDuration) * time.Minute)
		} else {
			e.phase = PhaseShortBreak
			e.endTime = time.Now().Add(time.Duration(e.cfg.ShortBreakDuration) * time.Minute)
		}
		if e.OnPhaseChange != nil {
			e.OnPhaseChange(PhasePayload{
				Phase:   string(e.phase),
				Message: phaseMessage(e.phase, e.currentRound, e.cfg.RoundsBeforeLongBreak),
			})
		}

	case PhaseShortBreak:
		// Short break → next Focus round.
		e.currentRound++
		e.phase = PhaseFocus
		e.endTime = time.Now().Add(time.Duration(e.cfg.FocusDuration) * time.Minute)
		if e.OnPhaseChange != nil {
			e.OnPhaseChange(PhasePayload{
				Phase:   string(e.phase),
				Message: phaseMessage(e.phase, e.currentRound, e.cfg.RoundsBeforeLongBreak),
			})
		}

	case PhaseLongBreak:
		// Long break → all done.
		e.stopTicker()
		e.state = StateIdle
		e.phase = PhaseFocus
		e.currentRound = 1
		if e.OnPhaseChange != nil {
			e.OnPhaseChange(PhasePayload{
				Phase:   "done",
				Message: "全部完成！今天效率很高！",
			})
		}
		if e.OnStateChange != nil {
			e.OnStateChange(StatePayload{State: string(StateIdle)})
		}
	}
}

// phaseMessage returns the pet speech text for a phase transition.
func phaseMessage(p Phase, round, total int) string {
	switch p {
	case PhaseShortBreak:
		return "休息一下！5 分钟后继续。"
	case PhaseLongBreak:
		return "已经完成多轮了，好好休息 15 分钟吧!"
	case PhaseFocus:
		if round == 1 {
			return "开始专注！25 分钟后休息~"
		}
		return "继续加油！"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/pomodoro/... -v -count=1`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pomodoro/
git commit -m "feat(pomodoro): add timer engine with state machine"
```

---

### Task 3: Create Wails bindings

**Files:**
- Create: `app_pomodoro.go`

- [ ] **Step 1: Create app_pomodoro.go**

```go
package main

import (
	"aiko/internal/pomodoro"
)

// StartPomodoro begins the pomodoro countdown.
func (a *App) StartPomodoro() {
	a.mu.RLock()
	engine := a.pomodoroEngine
	a.mu.RUnlock()
	if engine != nil {
		engine.Start()
	}
}

// PausePomodoro suspends the countdown.
func (a *App) PausePomodoro() {
	a.mu.RLock()
	engine := a.pomodoroEngine
	a.mu.RUnlock()
	if engine != nil {
		engine.Pause()
	}
}

// ResumePomodoro continues a paused countdown.
func (a *App) ResumePomodoro() {
	a.mu.RLock()
	engine := a.pomodoroEngine
	a.mu.RUnlock()
	if engine != nil {
		engine.Resume()
	}
}

// StopPomodoro ends the current pomodoro session.
func (a *App) StopPomodoro() {
	a.mu.RLock()
	engine := a.pomodoroEngine
	a.mu.RUnlock()
	if engine != nil {
		engine.Stop()
	}
}

// GetPomodoroStatus returns the current pomodoro engine status.
func (a *App) GetPomodoroStatus() pomodoro.StatusPayload {
	a.mu.RLock()
	engine := a.pomodoroEngine
	a.mu.RUnlock()
	if engine == nil {
		return pomodoro.StatusPayload{State: "idle"}
	}
	return engine.Status()
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: clean build (may warn about unused field until Task 4)

- [ ] **Step 3: Commit**

```bash
git add app_pomodoro.go
git commit -m "feat(pomodoro): add Wails bindings for timer control"
```

---

### Task 4: Integrate engine into App lifecycle

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Add import**

Add to imports block:

```go
"aiko/internal/pomodoro"
```

- [ ] **Step 2: Add field to App struct**

After `proactiveEngine` field (around line 65):

```go
pomodoroEngine      *pomodoro.Engine
```

- [ ] **Step 3: Initialize engine in startup()**

After `a.proactiveEngine = proactiveEngine` (in `initLLMComponents`):

No — the pomodoro engine is independent of LLM. Add in `startup()`, after `a.configStore = config.NewStore(a.sqlDB)` and config is loaded (after line ~110 where `cfg` is first available):

After the `if len(missing) == 0 { go a.initLLMComponents(ctx) }` block (around line ~180), add:

```go
// Initialize pomodoro engine (independent of LLM).
pomoCfg := pomodoro.Config{
	FocusDuration:          cfg.PomodoroFocusDuration,
	ShortBreakDuration:     cfg.PomodoroShortBreakDuration,
	LongBreakDuration:      cfg.PomodoroLongBreakDuration,
	RoundsBeforeLongBreak:  cfg.PomodoroRoundsBeforeLongBreak,
}
a.mu.Lock()
a.pomodoroEngine = pomodoro.New(pomoCfg)
// Wire callbacks to Wails events.
engine := a.pomodoroEngine
a.mu.Unlock()

engine.OnTick = func(p pomodoro.TickPayload) {
	a.EmitEvent("pomodoro:tick", p)
}
engine.OnPhaseChange = func(p pomodoro.PhasePayload) {
	a.EmitEvent("pomodoro:phase:changed", p)
	// Show in-app notification bubble (chat is closed during running).
	a.EmitEvent("notification:show", map[string]any{
		"title":   "番茄钟",
		"message": p.Message,
	})
}
engine.OnStateChange = func(p pomodoro.StatePayload) {
	a.EmitEvent("pomodoro:state:changed", p)
	// Update pet state based on pomodoro state.
	switch {
	case p.State == "running" && a.pomodoroEngine != nil && a.pomodoroEngine.Status().Phase == "focus":
		a.EmitEvent("pet:state:change", "focusing")
	case p.State == "running" && a.pomodoroEngine != nil && a.pomodoroEngine.Status().Phase != "focus":
		a.EmitEvent("pet:state:change", "resting")
	case p.State == "idle" || p.State == "paused":
		a.EmitEvent("pet:state:change", "idle")
	}
}
```

- [ ] **Step 4: Add cleanup in shutdown()**

In the `shutdown` method, after `proactiveEngine.Stop()` and `scheduler.Stop()` block, add:

```go
a.mu.RLock()
if a.pomodoroEngine != nil {
	a.pomodoroEngine.Stop()
}
a.mu.RUnlock()
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 6: Run engine tests**

Run: `go test ./internal/pomodoro/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add app.go
git commit -m "feat(pomodoro): integrate engine into app lifecycle"
```

---

### Task 5: Add focusing/resting pet states

**Files:**
- Modify: `frontend/src/composables/usePetState.js`

- [ ] **Step 1: Update usePetState.js**

Read the file at `frontend/src/composables/usePetState.js` and update the state handling:

The state values are currently: `'idle' | 'thinking' | 'speaking' | 'listening' | 'error'`

No code change is needed in this composable — it reacts to the `pet:state:change` event generically. The new `focusing` and `resting` states are emitted from the Go backend and flow through to `petState.value` automatically.

However, verify that the error auto-reset logic won't interfere:

```js
// In usePetState.js, the error reset timer only triggers for state === 'error'.
// 'focusing' and 'resting' pass through without side effects. No change needed.
```

This task is a no-op — the composable already handles arbitrary state strings. Confirm by reading the file.

- [ ] **Step 2: Verify no changes needed**

Run: `cd frontend && yarn build`
Expected: clean build

- [ ] **Step 3: Commit** — skip (no changes)

---

### Task 6: Create PomodoroPanel.vue

**Files:**
- Create: `frontend/src/components/PomodoroPanel.vue`

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
        class="pomodoro-panel"
        :style="panelStyle"
        @mousedown.stop
      >
        <!-- Circular progress ring -->
        <div class="timer-ring" :style="ringStyle">
          <div class="timer-inner">
            <div class="timer-time">{{ formattedTime }}</div>
            <div class="timer-label" :style="{ color: phaseColor }">{{ phaseLabel }}</div>
          </div>
        </div>

        <!-- Right side: info + buttons -->
        <div class="timer-info">
          <div class="round-info">
            <span class="round-text">第 {{ round }} / {{ totalRounds }} 轮</span>
            <div class="round-dots">
              <span
                v-for="i in totalRounds"
                :key="i"
                class="dot"
                :class="{ done: i < round, current: i === round && state === 'running' }"
              />
            </div>
          </div>

          <div class="timer-actions">
            <button
              v-if="state === 'idle'"
              class="pomo-btn start"
              @click="onStart"
            >开始</button>
            <button
              v-if="state === 'running'"
              class="pomo-btn pause"
              @click="onPause"
            >暂停</button>
            <button
              v-if="state === 'paused'"
              class="pomo-btn resume"
              @click="onResume"
            >继续</button>
            <button
              v-if="state !== 'idle'"
              class="pomo-btn stop"
              @click="onStop"
            >结束</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } form 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  StartPomodoro,
  PausePomodoro,
  ResumePomodoro,
  StopPomodoro,
  GetPomodoroStatus,
} from '../../wailsjs/go/main/App'
import { springAnimate } from '../composables/useSpring'

const props = defineProps({
  petPos: { type: Object, required: true },
  petSize: { type: Number, default: 160 },
})

const emit = defineEmits(['close'])

const visible = ref(false)
const animating = ref(false)
const state = ref('idle')    // idle | running | paused
const phase = ref('focus')   // focus | short_break | long_break
const remaining = ref(0)     // seconds
const round = ref(1)
const totalRounds = ref(4)
const totalDuration = ref(25 * 60) // seconds for current phase

let offTick = null
let offPhaseChanged = null
let offStateChanged = null
let cancelAnim = null

// ── computed ──

const formattedTime = computed(() => {
  const m = Math.floor(remaining.value / 60)
  const s = remaining.value % 60
  return `${m}:${String(s).padStart(2, '0')}`
})

const phaseLabel = computed(() => {
  switch (phase.value) {
    case 'focus': return '专注中'
    case 'short_break': return '短休息'
    case 'long_break': return '长休息'
    case 'done': return '完成 ✓'
    default: return ''
  }
})

const phaseColor = computed(() => {
  switch (phase.value) {
    case 'focus': return '#ff6b6b'
    case 'short_break': return '#51cf66'
    case 'long_break': return '#339af0'
    default: return '#ff6b6b'
  }
})

const progress = computed(() => {
  if (totalDuration.value <= 0) return 0
  return 1 - (remaining.value / totalDuration.value)
})

const ringStyle = computed(() => ({
  background: `conic-gradient(${phaseColor.value} ${progress.value * 360}deg, #333 ${progress.value * 360}deg)`,
}))

const panelStyle = computed(() => {
  const x = props.petPos.x + props.petSize / 2
  const y = props.petPos.y - 160
  return {
    left: `${x}px`,
    top: `${Math.max(40, y)}px`,
    transform: 'translate(-50%, 0)',
  }
})

// ── methods ──

async function onStart() {
  await StartPomodoro()
}

async function onPause() {
  await PausePomodoro()
}

async function onResume() {
  await ResumePomodoro()
}

async function onStop() {
  await StopPomodoro()
  visible.value = false
  emit('close')
}

function show() {
  visible.value = true
  // Load current status from backend.
  GetPomodoroStatus().then(st => {
    if (st.state === 'running' || st.state === 'paused') {
      state.value = st.state
      phase.value = st.phase
      remaining.value = st.remaining
      round.value = st.currentRound
      totalRounds.value = st.totalRounds
      totalDuration.value = phaseDuration(st.phase, st.config)
    }
  })
}

function hide() {
  // Only hide if not running (stop button calls hide after StopPomodoro).
  if (state.value !== 'running') {
    visible.value = false
  }
}

function phaseDuration(p, cfg) {
  switch (p) {
    case 'focus': return cfg.FocusDuration * 60
    case 'short_break': return cfg.ShortBreakDuration * 60
    case 'long_break': return cfg.LongBreakDuration * 60
    default: return cfg.FocusDuration * 60
  }
}

// ── animation ──

function onEnter(el, done) {
  animating.value = true
  cancelAnim = springAnimate({
    from: 0,
    to: 1,
    stiffness: 320,
    damping: 22,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { animating.value = false; done() },
  })
}

function onLeave(el, done) {
  animating.value = true
  cancelAnim = springAnimate({
    from: 1,
    to: 0,
    stiffness: 400,
    damping: 36,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { animating.value = false; done() },
  })
}

// ── events ──

onMounted(() => {
  offTick = EventsOn('pomodoro:tick', (p) => {
    remaining.value = p.remaining
    phase.value = p.phase
    round.value = p.round
  })
  offStateChanged = EventsOn('pomodoro:state:changed', (p) => {
    state.value = p.state
  })
  offPhaseChanged = EventsOn('pomodoro:phase:changed', (p) => {
    phase.value = p.phase
    if (p.phase === 'focus') {
      // Set totalDuration for progress ring.
      GetPomodoroStatus().then(st => {
        totalDuration.value = phaseDuration(p.phase, st.config)
      })
    }
  })
})

onUnmounted(() => {
  offTick?.()
  offStateChanged?.()
  offPhaseChanged?.()
  cancelAnim?.()
})
</script>

<style scoped>
.pomodoro-panel {
  position: fixed;
  z-index: 2001;
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 18px 22px;
  background: rgba(26, 26, 46, 0.88);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-radius: 18px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
  user-select: none;
}

.timer-ring {
  width: 100px;
  height: 100px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.timer-inner {
  width: 86px;
  height: 86px;
  border-radius: 50%;
  background: #1a1a2e;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.timer-time {
  font-size: 22px;
  font-weight: 700;
  color: #fff;
  font-variant-numeric: tabular-nums;
  line-height: 1;
}

.timer-label {
  font-size: 10px;
  margin-top: 2px;
}

.timer-info {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.round-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.round-text {
  font-size: 12px;
  color: #aaa;
}

.round-dots {
  display: flex;
  gap: 4px;
}

.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #444;
}

.dot.done {
  background: var(--phase-color, #ff6b6b);
}

.dot.current {
  background: var(--phase-color, #ff6b6b);
  box-shadow: 0 0 6px var(--phase-color, #ff6b6b);
}

.timer-actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.pomo-btn {
  width: 72px;
  padding: 6px 12px;
  border: none;
  border-radius: 8px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  color: #fff;
  transition: opacity 0.15s;
}

.pomo-btn:hover {
  opacity: 0.85;
}

.pomo-btn.start {
  background: #ff6b6b;
}

.pomo-btn.pause {
  background: rgba(255, 255, 255, 0.12);
}

.pomo-btn.resume {
  background: #51cf66;
}

.pomo-btn.stop {
  background: rgba(255, 255, 255, 0.08);
}
</style>
```

- [ ] **Step 2: Update macos.go hitTest selector**

In `macos.go`, find the `hitTest` JS selector string. It currently contains `.live2d-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.lightbox,.tool-confirm-modal,.execution-progress`. Add `,.pomodoro-panel` to the list so mouse events on the timer panel don't pass through to the desktop.

Search: `grep -n "live2d-pet.*chat-bubble" macos.go`
Expected: one line with the selector string. Insert `,.pomodoro-panel` after `.chat-bubble`.

- [ ] **Step 3: Verify frontend build**

Run: `cd frontend && yarn build`
Expected: clean build

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/PomodoroPanel.vue macos.go
git commit -m "feat(pomodoro): add floating timer panel component"
```

---

### Task 7: Add pomodoro menu item to pet context menus

**Files:**
- Modify: `frontend/src/components/Live2DPet.vue`
- Modify: `frontend/src/components/VRMPet.vue`

- [ ] **Step 1: Add menu item to Live2DPet.vue**

In `Live2DPet.vue`, the `petMenuItems` array starts at line 64. Add a new item after "切换表情" and before the first divider.

But wait — the pomodoro menu item needs access to a shared state (whether the panel is open). The cleanest approach: emit an event to App.vue which manages the state. The pet comp already emits `open-settings`. Add `open-pomodoro`.

Add after the `cycleExpression` item (line 66):

```js
{ iconSvg: ICON_POMODORO, label: '番茄钟', action: () => emit('open-pomodoro'), disabled: props.pomodoroPanelOpen },
```

But Live2DPet doesn't currently receive a `pomodoroPanelOpen` prop. We need to add it.

Actually, looking at the design more carefully: the `disabled` state should be driven by whether the panel is currently visible. The most straightforward approach: add a `pomodoroPanelOpen` prop to both pet components, pass it from App.vue.

In Live2DPet.vue, add to `defineProps`:

```js
const props = defineProps({
  modelPath: { type: String, required: true },
  pos: { type: Object, default: () => ({ x: -1, y: -1 }) },
  pomodoroPanelOpen: { type: Boolean, default: false },   // <-- add this
})
```

In `petMenuItems`, add the pomodoro item before the first divider:

```js
const petMenuItems = [
  { iconSvg: ICON_FACE,    label: '切换表情', action: cycleExpression },
  { iconSvg: ICON_SHIRT,   label: '更换模型', action: switchToNextModel },
  { divider: true },
  { iconSvg: ICON_POMODORO, label: '番茄钟', action: () => emit('open-pomodoro'), disabled: props.pomodoroPanelOpen },
  { iconSvg: ICON_SETTING, label: '打开设置', action: () => emit('open-settings') },
  { divider: true },
  { iconSvg: ICON_POWER,   label: '退出程序', action: () => Quit(), danger: true },
]
```

Add an emits entry (or update existing one):

```js
const emit = defineEmits(['position', 'ball-size', 'click', 'open-settings', 'open-pomodoro'])
```

- [ ] **Step 2: Add menu item to VRMPet.vue**

Same changes in VRMPet.vue:
- Add `pomodoroPanelOpen` prop
- Add menu item
- Add `open-pomodoro` to emits

The VRMPet.vue `petMenuItems` (line 475):

```js
const petMenuItems = [
  { iconSvg: ICON_SHIRT, label: "更换模型", action: switchToNextVRMModel },
  { divider: true },
  { iconSvg: ICON_POMODORO, label: "番茄钟", action: () => emit('open-pomodoro'), disabled: props.pomodoroPanelOpen },
  { iconSvg: ICON_SETTING, label: "打开设置", action: () => emit("open-settings") },
  { divider: true },
  { iconSvg: ICON_POWER, label: "退出程序", action: () => Quit(), danger: true },
]
```

- [ ] **Step 3: Verify frontend build**

Run: `cd frontend && yarn build`
Expected: clean build (may warn about ICON_POMODORO not defined if we haven't added the SVG yet)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/Live2DPet.vue frontend/src/components/VRMPet.vue
git commit -m "feat(pomodoro): add pomodoro menu item to pet context menus"
```

---

### Task 8: Coordinate panel visibility in App.vue

**Files:**
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: Add state variables**

Add near the other ref declarations:

```js
const pomodoroPanelOpen = ref(false)
const pomodoroRunning = ref(false)
const pomodoroPanelRef = ref(null)
```

- [ ] **Step 2: Add open/close handlers**

Add functions:

```js
function openPomodoro() {
  pomodoroPanelOpen.value = true
  // If pomodoro is running, close chat (mutual exclusion).
  if (pomodoroRunning.value && bubbleOpen.value) {
    bubbleOpen.value = false
  }
}

function closePomodoro() {
  pomodoroPanelOpen.value = false
}

function onPomodoroStateChanged(payload) {
  pomodoroRunning.value = payload.state === 'running'
  if (payload.state === 'running' && bubbleOpen.value) {
    bubbleOpen.value = false
  }
}
```

- [ ] **Step 3: Update toggleBubble to respect mutual exclusion**

Modify `toggleBubble()`:

```js
function toggleBubble() {
  // If pomodoro is running, don't open chat (mutual exclusion).
  if (!bubbleOpen.value && pomodoroRunning.value) {
    return
  }
  bubbleOpen.value = !bubbleOpen.value
  if (bubbleOpen.value) {
    pendingTokens = ''
    nextTick(() => {
      const panel = document.querySelector('.chat-panel')
      if (panel) panel.__vue_app__?.config?.globalProperties
    })
  }
}
```

- [ ] **Step 4: Register pomodoro events in onMounted**

Add inside `onMounted()`:

```js
offPomodoroState = EventsOn('pomodoro:state:changed', onPomodoroStateChanged)
```

Add declaration at module level:

```js
let offPomodoroState = null
```

- [ ] **Step 5: Clean up in onUnmounted**

Add:

```js
offPomodoroState?.()
```

- [ ] **Step 6: Add PomodoroPanel to template**

Import the component:

```js
import PomodoroPanel from './components/PomodoroPanel.vue'
```

Add in template (after NotificationBubble, before the Transition for ChatBubble):

```html
<PomodoroPanel
  ref="pomodoroPanelRef"
  :pet-pos="ballPos"
  :pet-size="ballSize"
  @close="closePomodoro"
/>
```

Wait — the panel has its own `visible` ref. We control show/hide via method calls. Update to use `v-if`:

```html
<PomodoroPanel
  v-if="pomodoroPanelOpen"
  ref="pomodoroPanelRef"
  :pet-pos="ballPos"
  :pet-size="ballSize"
  @close="closePomodoro"
/>
```

And in `openPomodoro()`, call `pomodoroPanelRef.value?.show()` after nextTick. Actually, since we use `v-if` with `pomodoroPanelOpen`, the component will mount with `visible = false`. We need to call `show()` after mount.

Simpler: just use `v-if` and have PomodoroPanel auto-show on mount:

In PomodoroPanel.vue, add `onMounted(() => { show() })` — no, that won't work because show loads status.

Better approach: pass a `visible` prop:

```html
<PomodoroPanel
  v-if="pomodoroPanelOpen"
  :visible="pomodoroPanelOpen"
  :pet-pos="ballPos"
  :pet-size="ballSize"
  @close="closePomodoro"
/>
```

Actually, the cleanest approach: PomodoroPanel manages its own `visible` ref internally and exposes `show()/hide()`. App.vue calls:

```js
async function openPomodoro() {
  pomodoroPanelOpen.value = true
  await nextTick()
  pomodoroPanelRef.value?.show()
}
```

Let me keep it simple. The panel already has internal `visible` ref. Use `v-if="pomodoroPanelOpen"` for DOM lifecycle, and call `show()` from App.vue via exposed ref.

- [ ] **Step 7: Pass pomodoroPanelOpen to pet components**

Add prop to Live2DPet/VRMPet in template:

```html
<Live2DPet
  v-if="renderBackend === 'live2d'"
  :pomodoro-panel-open="pomodoroPanelOpen"
  @open-pomodoro="openPomodoro"
  ...existing props...
/>
```

Same for VRMPet.

- [ ] **Step 8: Verify frontend build**

Run: `cd frontend && yarn build`
Expected: clean build

- [ ] **Step 9: Commit**

```bash
git add frontend/src/App.vue
git commit -m "feat(pomodoro): coordinate panel visibility and mutual exclusion"
```

---

### Task 9: Add pomodoro settings tab

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: Add tab to tabMeta array**

Insert before the `about` tab entry (or after `automation`):

```js
{ id: 'pomodoro', label: '番茄钟', iconSvg: ICON_TAB_POMODORO, iconBg: 'var(--cat-pomodoro)',
  keywords: 'pomodoro timer focus break 番茄 计时 专注 休息 时长 轮数' },
```

- [ ] **Step 2: Add tab pane content**

Add before the `about` tab-pane div. Pattern from existing tab panes:

```html
<div v-if="activeTab === 'pomodoro'" class="tab-pane">
  <div class="group-label">计时设置</div>
  <div class="settings-group">
    <div class="settings-row">
      <div class="row-body">
        <span class="row-title">专注时长（分钟）</span>
        <span class="row-desc">单次专注会话的时长，默认 25</span>
      </div>
      <input
        v-model.number="cfg.PomodoroFocusDuration"
        type="number"
        min="1"
        max="120"
        class="field-input number"
      />
    </div>

    <div class="settings-row">
      <div class="row-body">
        <span class="row-title">短休息时长（分钟）</span>
        <span class="row-desc">专注之间的短暂休息，默认 5</span>
      </div>
      <input
        v-model.number="cfg.PomodoroShortBreakDuration"
        type="number"
        min="1"
        max="30"
        class="field-input number"
      />
    </div>

    <div class="settings-row">
      <div class="row-body">
        <span class="row-title">长休息时长（分钟）</span>
        <span class="row-desc">完成 N 轮后的较长休息，默认 15</span>
      </div>
      <input
        v-model.number="cfg.PomodoroLongBreakDuration"
        type="number"
        min="1"
        max="60"
        class="field-input number"
      />
    </div>

    <div class="settings-row">
      <div class="row-body">
        <span class="row-title">长休息间隔（轮）</span>
        <span class="row-desc">几轮专注后触发长休息，默认 4</span>
      </div>
      <input
        v-model.number="cfg.PomodoroRoundsBeforeLongBreak"
        type="number"
        min="1"
        max="10"
        class="field-input number"
      />
    </div>
  </div>
  <p class="hint-text">修改即时生效，不影响正在进行的番茄钟。</p>
</div>
```

- [ ] **Step 3: Verify v-model binds to correct Config fields**

Check that `cfg` in SettingsWindow corresponds to the Config struct. The SettingsWindow loads config via `GetConfig()` and has a `cfg` ref. We need to ensure `PomodoroFocusDuration`, `PomodoroShortBreakDuration`, `PomodoroLongBreakDuration`, `PomodoroRoundsBeforeLongBreak` exist on the object.

Since `wails generate module` creates the TypeScript bindings, these fields will be auto-generated once `go build` succeeds (after Task 1). The frontend `GetConfig()` will return them with zero values until the config is saved.

- [ ] **Step 4: Verify frontend build**

Run: `cd frontend && yarn build`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(pomodoro): add pomodoro settings tab to settings window"
```

---

### Task 10: Add pomodoro SVG icons

**Files:**
- Modify: `frontend/src/utils/icons.js`

- [ ] **Step 1: Add ICON_POMODORO and ICON_TAB_POMODORO**

In `frontend/src/utils/icons.js`, add two new exports:

```js
// Context menu icon: simple clock outline
export const ICON_POMODORO = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>`

// Settings tab icon: tomato
export const ICON_TAB_POMODORO = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2c-3 0-8 2-8 8 0 5 3 10 8 12 5-2 8-7 8-12 0-6-5-8-8-8z"/><path d="M12 6v2"/></svg>`
```

Import ICON_TAB_POMODORO in SettingsWindow.vue (Task 9) and ICON_POMODORO in Live2DPet.vue and VRMPet.vue (Task 7).

- [ ] **Step 2: Verify frontend build**

Run: `cd frontend && yarn build`
Expected: clean build

- [ ] **Step 3: Commit**

```bash
git add frontend/src/utils/icons.js
git commit -m "feat(pomodoro): add pomodoro SVG icons"
```

---

### Task 11: Regenerate Wails bindings and integration test

**Files:**
- (auto-generated) `frontend/src/wailsjs/`

- [ ] **Step 1: Regenerate Wails bindings**

Run: `wails generate module`
Expected: new methods appear in `frontend/src/wailsjs/go/main/App.js`

- [ ] **Step 2: Full build**

Run: `make build`
Expected: clean build

- [ ] **Step 3: Manual smoke test**

Run: `make run`

1. Right-click pet → "番茄钟" appears in context menu
2. Click "番茄钟" → panel opens above pet
3. Right-click again → "番茄钟" is disabled
4. Click "开始" → timer counts down, pet state changes
5. Click "暂停" → timer pauses, can open chat
6. Click "继续" → timer resumes
7. Click "结束" → panel closes, menu item re-enabled
8. Open Settings → 番茄钟 tab → change durations → save

- [ ] **Step 4: Commit**

```bash
git add frontend/src/wailsjs/
git commit -m "chore: regenerate Wails bindings for pomodoro"
```

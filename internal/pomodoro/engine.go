// Package pomodoro implements a Pomodoro Technique countdown timer engine.
//
// The engine manages a state machine (idle → running ⇄ paused) across focus
// and break phases, emitting callbacks for ticks, phase changes, and state
// transitions. It uses wall-clock time (endTime) rather than a monotonic
// ticker, so system sleep is handled correctly: on wake the remaining time
// is recalculated and overdue phases auto-advance.
//
// Callbacks are always invoked without holding the internal mutex, so they
// may safely call exported methods like Status() without deadlocking.
package pomodoro

import (
	"fmt"
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
	FocusDuration         int
	ShortBreakDuration    int
	LongBreakDuration     int
	RoundsBeforeLongBreak int
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
	Message string `json:"message"`
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
	mu              sync.Mutex
	cfg             Config
	state           State
	phase           Phase
	currentRound    int
	endTime         time.Time // wall-clock time when current phase ends
	phaseStartedAt  time.Time // wall-clock time when current phase was started

	ticker *time.Ticker
	done   chan struct{}

	OnTick        func(TickPayload)
	OnPhaseChange func(PhasePayload)
	OnStateChange func(StatePayload)
}

// New creates a new Engine with the given config.
func New(cfg Config) *Engine {
	return &Engine{
		cfg:          cfg,
		state:        StateIdle,
		phase:        PhaseFocus,
		currentRound: 1,
	}
}

// Start begins or resumes countdown from the current phase.
func (e *Engine) Start() {
	e.mu.Lock()

	switch e.state {
	case StateRunning:
		e.mu.Unlock()
		return
	case StatePaused:
		remaining := time.Until(e.endTime)
		if remaining <= 0 {
			remaining = 0
		}
		// Compute elapsed so phaseStartedAt accounts for pause gap.
		elapsed := e.phaseDuration() - remaining
		if elapsed < 0 {
			elapsed = 0
		}
		e.phaseStartedAt = time.Now().Add(-elapsed)
		e.endTime = time.Now().Add(remaining)
	default:
		e.phase = PhaseFocus
		e.currentRound = 1
		e.phaseStartedAt = time.Now()
		e.endTime = e.phaseStartedAt.Add(time.Duration(e.cfg.FocusDuration) * time.Minute)
	}

	e.state = StateRunning
	e.startTicker()
	onStateChange := e.OnStateChange
	e.mu.Unlock()

	if onStateChange != nil {
		onStateChange(StatePayload{State: string(StateRunning)})
	}
}

// Pause suspends the countdown without resetting.
func (e *Engine) Pause() {
	e.mu.Lock()
	if e.state != StateRunning {
		e.mu.Unlock()
		return
	}
	e.state = StatePaused
	e.stopTicker()
	onStateChange := e.OnStateChange
	e.mu.Unlock()

	if onStateChange != nil {
		onStateChange(StatePayload{State: string(StatePaused)})
	}
}

// Resume continues a paused countdown.
func (e *Engine) Resume() {
	e.Start()
}

// Stop ends the current session and resets to idle.
func (e *Engine) Stop() {
	e.mu.Lock()
	e.stopTicker()
	e.state = StateIdle
	e.phase = PhaseFocus
	e.currentRound = 1
	onStateChange := e.OnStateChange
	e.mu.Unlock()

	if onStateChange != nil {
		onStateChange(StatePayload{State: string(StateIdle)})
	}
}

// phaseDuration returns the configured duration for the current phase in nanoseconds.
func (e *Engine) phaseDuration() time.Duration {
	switch e.phase {
	case PhaseFocus:
		return time.Duration(e.cfg.FocusDuration) * time.Minute
	case PhaseShortBreak:
		return time.Duration(e.cfg.ShortBreakDuration) * time.Minute
	case PhaseLongBreak:
		return time.Duration(e.cfg.LongBreakDuration) * time.Minute
	default:
		return 25 * time.Minute
	}
}

// UpdateConfig applies new config settings. If the timer is running or paused,
// the current phase end time is recalculated so the new duration takes effect
// immediately for the current phase. Elapsed time is preserved.
func (e *Engine) UpdateConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == StateRunning || e.state == StatePaused {
		elapsed := e.phaseDuration() - time.Until(e.endTime)
		if elapsed < 0 {
			elapsed = 0
		}
		e.cfg = cfg
		newDur := e.phaseDuration()
		e.endTime = time.Now().Add(newDur - elapsed)
		if time.Until(e.endTime) < 0 {
			e.endTime = time.Now() // phase expired, next tick will advance
		}
	} else {
		e.cfg = cfg
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
	tickerC := e.ticker.C
	done := e.done

	go func() {
		for {
			select {
			case <-tickerC:
				e.tick()
			case <-done:
				return
			}
		}
	}()
}

// stopTicker stops the tick loop. Must be called under mu.
func (e *Engine) stopTicker() {
	if e.done != nil {
		close(e.done)
		e.done = nil
	}
	if e.ticker != nil {
		e.ticker.Stop()
		e.ticker = nil
	}
}

// tick handles one tick: emit tick event, check for phase expiry.
// Callbacks are invoked without holding mu to avoid reentrant deadlocks.
func (e *Engine) tick() {
	e.mu.Lock()
	if e.state != StateRunning {
		e.mu.Unlock()
		return
	}

	remaining := int(time.Until(e.endTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	onTick := e.OnTick
	phase := e.phase
	round := e.currentRound
	needAdvance := remaining <= 0
	e.mu.Unlock()

	if onTick != nil {
		onTick(TickPayload{
			Remaining: remaining,
			Phase:     string(phase),
			Round:     round,
		})
	}

	if needAdvance {
		e.advancePhase()
	}
}

// advancePhase transitions to the next phase. Callbacks are invoked without
// holding mu so they can safely call exported methods.
func (e *Engine) advancePhase() {
	e.mu.Lock()

	var onPhaseChange func(PhasePayload)
	var onStateChange func(StatePayload)

	switch e.phase {
	case PhaseFocus:
		if e.currentRound >= e.cfg.RoundsBeforeLongBreak {
			e.phase = PhaseLongBreak
			e.phaseStartedAt = time.Now()
			e.endTime = e.phaseStartedAt.Add(time.Duration(e.cfg.LongBreakDuration) * time.Minute)
		} else {
			e.phase = PhaseShortBreak
			e.phaseStartedAt = time.Now()
			e.endTime = e.phaseStartedAt.Add(time.Duration(e.cfg.ShortBreakDuration) * time.Minute)
		}
		onPhaseChange = e.OnPhaseChange
		msg := phaseMessage(e.phase, e.currentRound, e.cfg)
		e.mu.Unlock()

		if onPhaseChange != nil {
			onPhaseChange(PhasePayload{Phase: string(e.phase), Message: msg})
		}

	case PhaseShortBreak:
		e.currentRound++
		e.phase = PhaseFocus
		e.phaseStartedAt = time.Now()
			e.endTime = e.phaseStartedAt.Add(time.Duration(e.cfg.FocusDuration) * time.Minute)
		onPhaseChange = e.OnPhaseChange
		msg := phaseMessage(e.phase, e.currentRound, e.cfg)
		e.mu.Unlock()

		if onPhaseChange != nil {
			onPhaseChange(PhasePayload{Phase: string(e.phase), Message: msg})
		}

	case PhaseLongBreak:
		e.stopTicker()
		e.state = StateIdle
		e.phase = PhaseFocus
		e.currentRound = 1
		onPhaseChange = e.OnPhaseChange
		onStateChange = e.OnStateChange
		e.mu.Unlock()

		if onPhaseChange != nil {
			onPhaseChange(PhasePayload{Phase: "done", Message: "全部完成！今天效率很高！"})
		}
		if onStateChange != nil {
			onStateChange(StatePayload{State: string(StateIdle)})
		}
	}
}

// phaseMessage returns the pet speech text for a phase transition.
func phaseMessage(p Phase, round int, cfg Config) string {
	switch p {
	case PhaseShortBreak:
		return fmt.Sprintf("休息一下！%d 分钟后继续。", cfg.ShortBreakDuration)
	case PhaseLongBreak:
		return fmt.Sprintf("已经完成多轮了，好好休息 %d 分钟吧！", cfg.LongBreakDuration)
	case PhaseFocus:
		if round == 1 {
			return fmt.Sprintf("开始专注！%d 分钟后休息~", cfg.FocusDuration)
		}
		return "继续加油！"
	default:
		return ""
	}
}

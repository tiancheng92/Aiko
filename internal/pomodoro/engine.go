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
	defer e.mu.Unlock()

	switch e.state {
	case StateRunning:
		return
	case StatePaused:
		remaining := time.Until(e.endTime)
		if remaining <= 0 {
			remaining = 0
		}
		e.endTime = time.Now().Add(remaining)
	default:
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
	e.Start()
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
	// Capture channel references locally so the goroutine is safe even if
	// stopTicker nils e.ticker before the goroutine notices the closed done.
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
	// Close done first to signal the goroutine to exit before we nil the ticker.
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
		return "已经完成多轮了，好好休息 15 分钟吧！"
	case PhaseFocus:
		if round == 1 {
			return "开始专注！25 分钟后休息~"
		}
		return "继续加油！"
	default:
		return ""
	}
}

package pomodoro

import (
	"testing"
	"time"
)

func TestEngineLifecycle(t *testing.T) {
	cfg := Config{
		FocusDuration:         25,
		ShortBreakDuration:    5,
		LongBreakDuration:     15,
		RoundsBeforeLongBreak: 4,
	}
	e := New(cfg)

	if State(e.Status().State) != StateIdle {
		t.Fatalf("expected idle, got %s", e.Status().State)
	}

	e.Start()
	if s := State(e.Status().State); s != StateRunning {
		t.Fatalf("expected running after start, got %s", s)
	}
	if s := Phase(e.Status().Phase); s != PhaseFocus {
		t.Fatalf("expected focus phase, got %s", s)
	}

	e.Pause()
	if s := State(e.Status().State); s != StatePaused {
		t.Fatalf("expected paused, got %s", s)
	}

	e.Resume()
	if s := State(e.Status().State); s != StateRunning {
		t.Fatalf("expected running after resume, got %s", s)
	}

	e.Stop()
	if s := State(e.Status().State); s != StateIdle {
		t.Fatalf("expected idle after stop, got %s", s)
	}
}

func TestTickCallback(t *testing.T) {
	cfg := Config{
		FocusDuration:         25,
		ShortBreakDuration:    5,
		LongBreakDuration:     15,
		RoundsBeforeLongBreak: 4,
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
		FocusDuration:         0, // 0 minutes = immediate transition for testing
		ShortBreakDuration:    5,
		LongBreakDuration:     15,
		RoundsBeforeLongBreak: 4,
	}
	e := New(cfg)

	var phases []Phase
	e.OnPhaseChange = func(p PhasePayload) { phases = append(phases, Phase(p.Phase)) }

	e.Start()
	time.Sleep(1500 * time.Millisecond) // allow transition to happen

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
		FocusDuration:         25,
		ShortBreakDuration:    5,
		LongBreakDuration:     15,
		RoundsBeforeLongBreak: 4,
	}
	e := New(cfg)
	e.Start()
	st := e.Status()
	// mutate the returned struct
	st.State = "hacked"
	// engine state must be unaffected
	if State(e.Status().State) != StateRunning {
		t.Fatal("Status() must return a copy, not a reference")
	}
	e.Stop()
}

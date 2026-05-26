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

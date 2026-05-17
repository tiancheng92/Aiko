package main

import (
	"context"
	"fmt"

	"aiko/internal/proactive"
	"aiko/internal/scheduler"
)

// ListProactiveItems returns all pending proactive reminders ordered by trigger time.
func (a *App) ListProactiveItems() ([]proactive.Item, error) {
	a.mu.RLock()
	pe := a.proactiveEngine
	a.mu.RUnlock()
	if pe == nil {
		return nil, nil
	}
	return pe.Store().List(context.Background())
}

// DeleteProactiveItem cancels a pending proactive reminder by ID.
func (a *App) DeleteProactiveItem(id int64) error {
	a.mu.RLock()
	pe := a.proactiveEngine
	a.mu.RUnlock()
	if pe == nil {
		return nil
	}
	return pe.Store().Delete(context.Background(), id)
}

// ListCronJobs returns all scheduled jobs.
func (a *App) ListCronJobs() ([]scheduler.Job, error) {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return []scheduler.Job{}, nil
	}
	return sched.ListJobs(a.ctx)
}

// CreateCronJob creates a new scheduled job.
func (a *App) CreateCronJob(name, description, schedule, prompt string, saveToMemory, notify bool) (scheduler.Job, error) {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return scheduler.Job{}, fmt.Errorf("scheduler not ready")
	}
	return sched.CreateJob(a.ctx, name, description, schedule, prompt, saveToMemory, notify)
}

// UpdateCronJob updates an existing scheduled job.
func (a *App) UpdateCronJob(id int64, name, description, schedule, prompt string, saveToMemory, notify bool) (scheduler.Job, error) {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return scheduler.Job{}, fmt.Errorf("scheduler not ready")
	}
	return sched.UpdateJob(a.ctx, id, name, description, schedule, prompt, saveToMemory, notify)
}

// DeleteCronJob removes a scheduled job by ID.
func (a *App) DeleteCronJob(id int64) error {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.DeleteJob(a.ctx, id)
}

// SetCronJobEnabled enables or disables a scheduled job.
func (a *App) SetCronJobEnabled(id int64, enabled bool) error {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.SetJobEnabled(a.ctx, id, enabled)
}

// RunCronJobNow fires a scheduled job immediately regardless of its schedule.
func (a *App) RunCronJobNow(id int64) error {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.RunJobNow(id)
}

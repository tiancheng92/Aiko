//go:build darwin

package services

import (
	"context"
	"fmt"

	"aiko/internal/proactive"
	"aiko/internal/scheduler"
)

// SchedulerService manages cron jobs and proactive message items.
type SchedulerService struct{ s *sharedState }

// NewSchedulerService creates a SchedulerService backed by the given shared state.
func NewSchedulerService(s *sharedState) *SchedulerService { return &SchedulerService{s: s} }

// ListCronJobs returns all scheduled jobs.
func (sc *SchedulerService) ListCronJobs() ([]scheduler.Job, error) {
	sc.s.mu.RLock()
	sched := sc.s.scheduler
	sc.s.mu.RUnlock()
	if sched == nil {
		return []scheduler.Job{}, nil
	}
	return sched.ListJobs(sc.s.ctx)
}

// CreateCronJob creates a new scheduled job.
func (sc *SchedulerService) CreateCronJob(name, description, schedule, prompt string, saveToMemory, notify bool) (scheduler.Job, error) {
	sc.s.mu.RLock()
	sched := sc.s.scheduler
	sc.s.mu.RUnlock()
	if sched == nil {
		return scheduler.Job{}, fmt.Errorf("scheduler not ready")
	}
	return sched.CreateJob(sc.s.ctx, name, description, schedule, prompt, saveToMemory, notify)
}

// UpdateCronJob updates an existing scheduled job.
func (sc *SchedulerService) UpdateCronJob(id int64, name, description, schedule, prompt string, saveToMemory, notify bool) (scheduler.Job, error) {
	sc.s.mu.RLock()
	sched := sc.s.scheduler
	sc.s.mu.RUnlock()
	if sched == nil {
		return scheduler.Job{}, fmt.Errorf("scheduler not ready")
	}
	return sched.UpdateJob(sc.s.ctx, id, name, description, schedule, prompt, saveToMemory, notify)
}

// DeleteCronJob removes a scheduled job by ID.
func (sc *SchedulerService) DeleteCronJob(id int64) error {
	sc.s.mu.RLock()
	sched := sc.s.scheduler
	sc.s.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.DeleteJob(sc.s.ctx, id)
}

// SetCronJobEnabled enables or disables a scheduled job.
func (sc *SchedulerService) SetCronJobEnabled(id int64, enabled bool) error {
	sc.s.mu.RLock()
	sched := sc.s.scheduler
	sc.s.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.SetJobEnabled(sc.s.ctx, id, enabled)
}

// RunCronJobNow fires a scheduled job immediately regardless of its schedule.
func (sc *SchedulerService) RunCronJobNow(id int64) error {
	sc.s.mu.RLock()
	sched := sc.s.scheduler
	sc.s.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.RunJobNow(id)
}

// ListProactiveItems returns all pending proactive reminders ordered by trigger time.
func (sc *SchedulerService) ListProactiveItems() ([]proactive.Item, error) {
	sc.s.mu.RLock()
	pe := sc.s.proactiveEngine
	sc.s.mu.RUnlock()
	if pe == nil {
		return nil, nil
	}
	return pe.Store().List(context.Background())
}

// DeleteProactiveItem cancels a pending proactive reminder by ID.
func (sc *SchedulerService) DeleteProactiveItem(id int64) error {
	sc.s.mu.RLock()
	pe := sc.s.proactiveEngine
	sc.s.mu.RUnlock()
	if pe == nil {
		return nil
	}
	return pe.Store().Delete(context.Background(), id)
}

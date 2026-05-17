// internal/scheduler/scheduler.go
package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/robfig/cron/v3"
)

// Job represents a single scheduled task persisted in SQLite.
type Job struct {
	ID           int64
	Name         string
	Description  string
	Schedule     string  // cron expression e.g. "0 8 * * *"
	Prompt       string  // the message to send to the agent
	Enabled      bool
	SaveToMemory bool
	Notify       bool
	LastRun      *string // RFC3339 or nil
	NextRunAt    *string // RFC3339 or nil
	CreatedAt    string  // RFC3339
}

// ResultFunc is called when a job fires, with the job and the agent's response.
type ResultFunc func(job Job, result string, err error)

// jobTimeout is the maximum duration allowed for a single cron job execution.
const jobTimeout = 10 * time.Minute

// pollInterval is how often the scheduler checks for due jobs.
// After system sleep/wake the next poll fires within one interval because we
// compare against wall-clock time stored in the DB, not an internal timer.
const pollInterval = time.Minute

// Scheduler manages cron jobs backed by SQLite using a poll-based design.
// Instead of relying on cron engine timers (which use the monotonic clock and
// therefore pause during system sleep), each job stores its next_run_at wall-
// clock timestamp in the DB. A background goroutine wakes every minute, reads
// due rows (next_run_at <= now), advances next_run_at, and executes them.
// This mirrors the approach used by Hermes Agent's cron/scheduler.py.
type Scheduler struct {
	mu       sync.Mutex
	db       *sql.DB
	chatFn   func(ctx context.Context, job Job) (string, error)
	onResult ResultFunc
	done     chan struct{}
	wg       sync.WaitGroup
	// pollNow is a channel that triggers an immediate poll tick (used by
	// TriggerPoll so the wake observer can bypass the 1-minute wait).
	pollNow  chan struct{}
}

// New creates a Scheduler. chatFn is called to execute each job's prompt.
func New(db *sql.DB, chatFn func(ctx context.Context, job Job) (string, error), onResult ResultFunc) *Scheduler {
	return &Scheduler{
		db:       db,
		chatFn:   chatFn,
		onResult: onResult,
		pollNow:  make(chan struct{}, 1),
	}
}

// Start initialises next_run_at for enabled jobs that lack one (or have a
// non-UTC legacy value written before the UTC-normalisation fix), then
// launches the background poll loop.
func (s *Scheduler) Start(ctx context.Context) error {
	jobs, err := s.ListJobs(ctx)
	if err != nil {
		return fmt.Errorf("load jobs: %w", err)
	}
	for i := range jobs {
		if !jobs[i].Enabled {
			continue
		}
		// Re-initialise if next_run_at is absent or stored in a non-UTC format
		// (legacy rows written with a local-timezone offset like +08:00).
		needsInit := jobs[i].NextRunAt == nil ||
			(len(*jobs[i].NextRunAt) > 0 && (*jobs[i].NextRunAt)[len(*jobs[i].NextRunAt)-1] != 'Z')
		if needsInit {
			if err := s.initNextRun(ctx, jobs[i]); err != nil {
				log.Warn().Str("job", jobs[i].Name).Err(err).Msg("scheduler: init next_run_at")
			}
		}
	}

	s.mu.Lock()
	s.done = make(chan struct{})
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop(ctx)
	return nil
}

// Stop halts the poll loop and waits for in-flight executions to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// TriggerPoll requests an immediate poll without waiting for the next tick.
// Non-blocking: if a poll is already queued the call is a no-op.
func (s *Scheduler) TriggerPoll() {
	select {
	case s.pollNow <- struct{}{}:
	default:
	}
}

// loop is the background goroutine that drives periodic polling.
func (s *Scheduler) loop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	s.mu.Lock()
	done := s.done
	s.mu.Unlock()

	// Poll once immediately on start so jobs due at startup are not skipped.
	s.poll(ctx)

	for {
		select {
		case <-ticker.C:
			s.poll(ctx)
		case <-s.pollNow:
			s.poll(ctx)
		case <-done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// poll queries all enabled jobs whose next_run_at is in the past, advances
// their next_run_at before executing (at-most-once semantics, matching
// Hermes' "advance first, then run" pattern), and fires each one.
func (s *Scheduler) poll(ctx context.Context) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, schedule, prompt, enabled,
		       save_to_memory, notify, last_run, next_run_at, created_at
		FROM cron_jobs
		WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
	`, now.UTC().Format(time.RFC3339))
	if err != nil {
		log.Warn().Err(err).Msg("scheduler poll: query")
		return
	}
	defer rows.Close()

	var due []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			log.Warn().Err(err).Msg("scheduler poll: scan")
			continue
		}
		due = append(due, j)
	}
	if err := rows.Err(); err != nil {
		log.Warn().Err(err).Msg("scheduler poll: rows")
		return
	}

	for i := range due {
		// Advance next_run_at before execution — ensures at-most-once delivery
		// even if the process crashes mid-run (same pattern as Hermes Agent).
		next, err := computeNextRun(due[i].Schedule, now)
		if err != nil {
			log.Warn().Str("job", due[i].Name).Err(err).Msg("scheduler poll: compute next")
			continue
		}
		nextStr := next.UTC().Format(time.RFC3339)
		if _, err := s.db.ExecContext(ctx,
			`UPDATE cron_jobs SET next_run_at = ? WHERE id = ?`,
			nextStr, due[i].ID,
		); err != nil {
			log.Warn().Str("job", due[i].Name).Err(err).Msg("scheduler poll: advance next_run_at")
			continue
		}
		s.fireJob(due[i])
	}
}

// fireJob runs a job in a goroutine.
func (s *Scheduler) fireJob(j Job) {
	s.wg.Add(1)
	go func(job Job) {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		log.Info().Str("job", job.Name).Msg("cron job fired")
		result, err := s.chatFn(ctx, job)
		if _, dbErr := s.db.ExecContext(context.Background(),
			`UPDATE cron_jobs SET last_run = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339), job.ID,
		); dbErr != nil {
			log.Warn().Str("job", job.Name).Err(dbErr).Msg("scheduler: update last_run")
		}
		if s.onResult != nil {
			s.onResult(job, result, err)
		}
	}(j)
}

// CreateJob persists a new job and sets its initial next_run_at.
func (s *Scheduler) CreateJob(ctx context.Context, name, description, schedule, prompt string, saveToMemory, notify bool) (Job, error) {
	if _, err := cron.ParseStandard(schedule); err != nil {
		return Job{}, fmt.Errorf("invalid cron expression %q: %w", schedule, err)
	}
	next, err := computeNextRun(schedule, time.Now())
	if err != nil {
		return Job{}, err
	}
	nextStr := next.UTC().Format(time.RFC3339)
	stm, ntf := boolToInt(saveToMemory), boolToInt(notify)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO cron_jobs(name, description, schedule, prompt, enabled, save_to_memory, notify, next_run_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?)
	`, name, description, schedule, prompt, stm, ntf, nextStr)
	if err != nil {
		return Job{}, fmt.Errorf("insert job: %w", err)
	}
	id, _ := res.LastInsertId()
	j := Job{
		ID: id, Name: name, Description: description,
		Schedule: schedule, Prompt: prompt, Enabled: true,
		SaveToMemory: saveToMemory, Notify: notify, NextRunAt: &nextStr,
	}
	return j, nil
}

// DeleteJob removes a job from the DB.
func (s *Scheduler) DeleteJob(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
	return err
}

// UpdateJob persists changes and recomputes next_run_at from now.
func (s *Scheduler) UpdateJob(ctx context.Context, id int64, name, description, schedule, prompt string, saveToMemory, notify bool) (Job, error) {
	if _, err := cron.ParseStandard(schedule); err != nil {
		return Job{}, fmt.Errorf("invalid cron expression %q: %w", schedule, err)
	}
	next, err := computeNextRun(schedule, time.Now())
	if err != nil {
		return Job{}, err
	}
	nextStr := next.UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE cron_jobs SET name=?, description=?, schedule=?, prompt=?,
		        save_to_memory=?, notify=?, next_run_at=? WHERE id=?`,
		name, description, schedule, prompt,
		boolToInt(saveToMemory), boolToInt(notify), nextStr, id)
	if err != nil {
		return Job{}, fmt.Errorf("update job: %w", err)
	}
	jobs, err := s.ListJobs(ctx)
	if err != nil {
		return Job{}, err
	}
	for i := range jobs {
		if jobs[i].ID == id {
			return jobs[i], nil
		}
	}
	return Job{}, fmt.Errorf("job %d not found after update", id)
}

// SetJobEnabled enables or disables a job. Enabling recomputes next_run_at.
func (s *Scheduler) SetJobEnabled(ctx context.Context, id int64, enabled bool) error {
	v := boolToInt(enabled)
	if _, err := s.db.ExecContext(ctx, `UPDATE cron_jobs SET enabled=? WHERE id=?`, v, id); err != nil {
		return fmt.Errorf("set enabled: %w", err)
	}
	if enabled {
		jobs, err := s.ListJobs(ctx)
		if err != nil {
			return err
		}
		for i := range jobs {
			if jobs[i].ID == id {
				return s.initNextRun(ctx, jobs[i])
			}
		}
	}
	return nil
}

// RunJobNow fires a job immediately regardless of its schedule.
func (s *Scheduler) RunJobNow(id int64) error {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT id, name, description, schedule, prompt, enabled,
		       save_to_memory, notify, last_run, next_run_at, created_at
		FROM cron_jobs WHERE id = ? LIMIT 1`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return fmt.Errorf("job %d not found", id)
	}
	j, err := scanJob(rows)
	if err != nil {
		return err
	}
	s.fireJob(j)
	return nil
}

// ListJobs returns all jobs ordered by created_at.
func (s *Scheduler) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, schedule, prompt, enabled,
		       save_to_memory, notify, last_run, next_run_at, created_at
		FROM cron_jobs ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// initNextRun sets next_run_at for a job that doesn't have one yet.
func (s *Scheduler) initNextRun(ctx context.Context, j Job) error {
	next, err := computeNextRun(j.Schedule, time.Now())
	if err != nil {
		return err
	}
	nextStr := next.UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE cron_jobs SET next_run_at = ? WHERE id = ?`, nextStr, j.ID)
	return err
}

// computeNextRun returns the next wall-clock fire time after `after`.
func computeNextRun(schedule string, after time.Time) (time.Time, error) {
	p, err := cron.ParseStandard(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse schedule %q: %w", schedule, err)
	}
	return p.Next(after), nil
}

// scanJob scans one row from a cron_jobs query into a Job.
// All DATETIME columns are stored as RFC3339 text; scan them as NullString
// to avoid driver-level time parsing which modernc.org/sqlite does not do.
func scanJob(rows *sql.Rows) (Job, error) {
	var j Job
	var enabled, saveToMemory, notify int
	var lastRun, nextRunAt, createdAt sql.NullString
	if err := rows.Scan(
		&j.ID, &j.Name, &j.Description, &j.Schedule, &j.Prompt,
		&enabled, &saveToMemory, &notify,
		&lastRun, &nextRunAt, &createdAt,
	); err != nil {
		return Job{}, err
	}
	j.Enabled = enabled == 1
	j.SaveToMemory = saveToMemory == 1
	j.Notify = notify == 1
	if lastRun.Valid && lastRun.String != "" {
		j.LastRun = new(lastRun.String)
	}
	if nextRunAt.Valid && nextRunAt.String != "" {
		j.NextRunAt = new(nextRunAt.String)
	}
	if createdAt.Valid {
		j.CreatedAt = createdAt.String
	}
	return j, nil
}

// boolToInt converts a bool to 0/1 for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// internal/scheduler/scheduler.go
package scheduler

import (
    "context"
    "database/sql"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/robfig/cron/v3"
)

// Job represents a single scheduled task persisted in SQLite.
type Job struct {
    ID          int64
    Name        string
    Description string
    Schedule    string // cron expression e.g. "0 8 * * *"
    Prompt      string // the message to send to the agent
    Enabled     bool
    LastRun     *time.Time
    CreatedAt   time.Time
}

// ResultFunc is called when a job fires, with the job and the agent's response.
type ResultFunc func(job Job, result string, err error)

// Scheduler manages cron jobs backed by SQLite.
type Scheduler struct {
    mu       sync.Mutex
    db       *sql.DB
    cr       *cron.Cron
    entryIDs map[int64]cron.EntryID // job.ID -> cron entry ID
    chatFn   func(ctx context.Context, prompt string) (string, error)
    onResult ResultFunc
}

// New creates a Scheduler. chatFn is called to execute each job's prompt.
// onResult is called with the job output after each execution.
func New(db *sql.DB, chatFn func(ctx context.Context, prompt string) (string, error), onResult ResultFunc) *Scheduler {
    s := &Scheduler{
        db:       db,
        cr:       cron.New(),
        entryIDs: make(map[int64]cron.EntryID),
        chatFn:   chatFn,
        onResult: onResult,
    }
    return s
}

// Start loads all enabled jobs from DB and starts the cron engine.
func (s *Scheduler) Start(ctx context.Context) error {
    jobs, err := s.ListJobs(ctx)
    if err != nil {
        return fmt.Errorf("load jobs: %w", err)
    }
    for _, j := range jobs {
        if j.Enabled {
            if err := s.scheduleJob(j); err != nil {
                slog.Warn("failed to schedule job", "job", j.Name, "err", err)
            }
        }
    }
    s.cr.Start()
    return nil
}

// Stop halts the cron engine.
func (s *Scheduler) Stop() {
    s.cr.Stop()
}

// CreateJob persists a new job and schedules it immediately.
func (s *Scheduler) CreateJob(ctx context.Context, name, description, schedule, prompt string) (Job, error) {
    // Validate the cron expression before persisting.
    if _, err := cron.ParseStandard(schedule); err != nil {
        return Job{}, fmt.Errorf("invalid cron expression %q: %w", schedule, err)
    }
    res, err := s.db.ExecContext(ctx, `
        INSERT INTO cron_jobs(name, description, schedule, prompt, enabled)
        VALUES (?, ?, ?, ?, 1)
    `, name, description, schedule, prompt)
    if err != nil {
        return Job{}, fmt.Errorf("insert job: %w", err)
    }
    id, _ := res.LastInsertId()
    j := Job{ID: id, Name: name, Description: description, Schedule: schedule, Prompt: prompt, Enabled: true}
    if err := s.scheduleJob(j); err != nil {
        return j, fmt.Errorf("schedule job: %w", err)
    }
    return j, nil
}

// RunJobNow fires a job immediately regardless of its schedule.
func (s *Scheduler) RunJobNow(id int64) error {
	jobs, err := s.ListJobs(context.Background())
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.ID == id {
			go func(job Job) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				_, _ = s.db.ExecContext(ctx, `UPDATE cron_jobs SET last_run=? WHERE id=?`, time.Now(), job.ID)
				slog.Info("cron job fired manually", "job", job.Name)
				result, err := s.chatFn(ctx, job.Prompt)
				if s.onResult != nil {
					s.onResult(job, result, err)
				}
			}(j)
			return nil
		}
	}
	return fmt.Errorf("job %d not found", id)
}

// DeleteJob removes a job from cron and from the DB.
func (s *Scheduler) DeleteJob(ctx context.Context, id int64) error {
    s.mu.Lock()
    if eid, ok := s.entryIDs[id]; ok {
        s.cr.Remove(eid)
        delete(s.entryIDs, id)
    }
    s.mu.Unlock()
    _, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
    return err
}

// UpdateJob persists name/description/schedule/prompt changes for an existing job
// and reschedules it in the cron engine.
func (s *Scheduler) UpdateJob(ctx context.Context, id int64, name, description, schedule, prompt string) (Job, error) {
    if _, err := cron.ParseStandard(schedule); err != nil {
        return Job{}, fmt.Errorf("invalid cron expression %q: %w", schedule, err)
    }
    _, err := s.db.ExecContext(ctx,
        `UPDATE cron_jobs SET name=?, description=?, schedule=?, prompt=? WHERE id=?`,
        name, description, schedule, prompt, id)
    if err != nil {
        return Job{}, fmt.Errorf("update job: %w", err)
    }
    jobs, err := s.ListJobs(ctx)
    if err != nil {
        return Job{}, err
    }
    var updated Job
    for _, j := range jobs {
        if j.ID == id {
            updated = j
            break
        }
    }
    // Reschedule: remove old entry then re-add if enabled.
    s.mu.Lock()
    if eid, ok := s.entryIDs[id]; ok {
        s.cr.Remove(eid)
        delete(s.entryIDs, id)
    }
    s.mu.Unlock()
    if updated.Enabled {
        if err := s.scheduleJob(updated); err != nil {
            return updated, fmt.Errorf("reschedule job: %w", err)
        }
    }
    return updated, nil
}

// SetJobEnabled enables or disables a scheduled job.
func (s *Scheduler) SetJobEnabled(ctx context.Context, id int64, enabled bool) error {
    v := 0
    if enabled {
        v = 1
    }
    if _, err := s.db.ExecContext(ctx, `UPDATE cron_jobs SET enabled=? WHERE id=?`, v, id); err != nil {
        return fmt.Errorf("set job enabled: %w", err)
    }
    s.mu.Lock()
    if eid, ok := s.entryIDs[id]; ok {
        s.cr.Remove(eid)
        delete(s.entryIDs, id)
    }
    s.mu.Unlock()
    if enabled {
        jobs, err := s.ListJobs(ctx)
        if err != nil {
            return err
        }
        for _, j := range jobs {
            if j.ID == id {
                return s.scheduleJob(j)
            }
        }
    }
    return nil
}

// ListJobs returns all jobs ordered by created_at.
func (s *Scheduler) ListJobs(ctx context.Context) ([]Job, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT id, name, description, schedule, prompt, enabled, last_run, created_at
        FROM cron_jobs ORDER BY created_at ASC
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var jobs []Job
    for rows.Next() {
        var j Job
        var enabled int
        var lastRun sql.NullTime
        var createdAt string
        if err := rows.Scan(&j.ID, &j.Name, &j.Description, &j.Schedule, &j.Prompt, &enabled, &lastRun, &createdAt); err != nil {
            return nil, err
        }
        j.Enabled = enabled == 1
        if lastRun.Valid {
            j.LastRun = &lastRun.Time
        }
        jobs = append(jobs, j)
    }
    return jobs, rows.Err()
}

// scheduleJob registers a job with the cron engine.
func (s *Scheduler) scheduleJob(j Job) error {
    eid, err := s.cr.AddFunc(j.Schedule, func() {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
        defer cancel()
        // Update last_run.
        _, _ = s.db.ExecContext(ctx, `UPDATE cron_jobs SET last_run=? WHERE id=?`, time.Now(), j.ID)
        slog.Info("cron job fired", "job", j.Name)
        result, err := s.chatFn(ctx, j.Prompt)
        if s.onResult != nil {
            s.onResult(j, result, err)
        }
    })
    if err != nil {
        return err
    }
    s.mu.Lock()
    s.entryIDs[j.ID] = eid
    s.mu.Unlock()
    return nil
}


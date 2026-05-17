package scheduler_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"aiko/internal/db"
	"aiko/internal/scheduler"
)

// newTestDB opens a real SQLite DB in a temp directory and runs the full
// production migrations, ensuring schema parity with the deployed database.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

// noopChatFn is a stub chatFn that immediately returns a fixed response.
func noopChatFn(_ context.Context, _ scheduler.Job) (string, error) {
	return "stub response", nil
}

// TestNewScheduler_EmptyList verifies that a fresh scheduler has no jobs.
func TestNewScheduler_EmptyList(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(db, noopChatFn, nil)
	jobs, err := s.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs, got %d", len(jobs))
	}
}

// TestCreateAndListJobs verifies that a created job appears in ListJobs.
func TestCreateAndListJobs(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(db, noopChatFn, nil)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, "morning", "daily greeting", "0 8 * * *", "say good morning", false, false)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.ID == 0 {
		t.Fatal("expected non-zero job ID")
	}

	jobs, err := s.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "morning" {
		t.Errorf("expected name %q, got %q", "morning", jobs[0].Name)
	}
	if jobs[0].Schedule != "0 8 * * *" {
		t.Errorf("expected schedule %q, got %q", "0 8 * * *", jobs[0].Schedule)
	}
}

// TestUpdateJob verifies that UpdateJob persists all changed fields.
func TestUpdateJob(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(db, noopChatFn, nil)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, "original", "desc", "0 8 * * *", "hello", false, false)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	updated, err := s.UpdateJob(ctx, job.ID, "updated", "new desc", "0 9 * * *", "good morning", true, true)
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if updated.Name != "updated" {
		t.Errorf("expected name %q, got %q", "updated", updated.Name)
	}
	if updated.Schedule != "0 9 * * *" {
		t.Errorf("expected schedule %q, got %q", "0 9 * * *", updated.Schedule)
	}
	if !updated.SaveToMemory {
		t.Error("expected SaveToMemory true")
	}
}

// TestDeleteJob verifies that a deleted job no longer appears in ListJobs.
func TestDeleteJob(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(db, noopChatFn, nil)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, "temp", "desc", "0 8 * * *", "hi", false, false)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := s.DeleteJob(ctx, job.ID); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}

	jobs, err := s.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs after delete, got %d", len(jobs))
	}
}

// TestSetJobEnabled verifies that enable/disable toggling is persisted correctly.
func TestSetJobEnabled(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(db, noopChatFn, nil)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, "toggle", "desc", "0 8 * * *", "hi", false, false)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if !job.Enabled {
		t.Fatal("expected job to be enabled after creation")
	}

	if err := s.SetJobEnabled(ctx, job.ID, false); err != nil {
		t.Fatalf("SetJobEnabled false: %v", err)
	}
	jobs, err := s.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if jobs[0].Enabled {
		t.Error("expected job to be disabled")
	}

	if err := s.SetJobEnabled(ctx, job.ID, true); err != nil {
		t.Fatalf("SetJobEnabled true: %v", err)
	}
	jobs, err = s.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if !jobs[0].Enabled {
		t.Error("expected job to be enabled again")
	}
}

// TestRunJobNow verifies that RunJobNow fires the chatFn and calls the onResult callback.
func TestRunJobNow(t *testing.T) {
	db := newTestDB(t)

	var mu sync.Mutex
	var called []string
	chatFn := func(_ context.Context, job scheduler.Job) (string, error) {
		mu.Lock()
		called = append(called, job.Prompt)
		mu.Unlock()
		return "response", nil
	}

	resultCh := make(chan scheduler.Job, 1)
	onResult := func(job scheduler.Job, result string, err error) {
		resultCh <- job
	}

	s := scheduler.New(db, chatFn, onResult)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, "manual", "desc", "0 8 * * *", "do something", false, false)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := s.RunJobNow(job.ID); err != nil {
		t.Fatalf("RunJobNow: %v", err)
	}

	select {
	case result := <-resultCh:
		if result.ID != job.ID {
			t.Errorf("expected job ID %d, got %d", job.ID, result.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for onResult callback")
	}

	mu.Lock()
	if len(called) != 1 || called[0] != "do something" {
		t.Errorf("chatFn called with wrong args: %v", called)
	}
	mu.Unlock()
}

// TestCreateJob_InvalidCron verifies that an invalid cron expression is rejected at creation time.
func TestCreateJob_InvalidCron(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(db, noopChatFn, nil)

	_, err := s.CreateJob(context.Background(), "bad", "desc", "not-a-cron", "hi", false, false)
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

// TestConcurrentJobCreation verifies that concurrent CreateJob calls all succeed without races.
func TestConcurrentJobCreation(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(db, noopChatFn, nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.CreateJob(ctx, "concurrent", "desc", "0 8 * * *", "hi", false, false)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("goroutine %d: %w", n, err))
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		t.Error(err)
	}

	jobs, err := s.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 10 {
		t.Errorf("expected 10 jobs, got %d", len(jobs))
	}
}

package proactive_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"aiko/internal/db"
	"aiko/internal/proactive"
)

// TestScheduleFollowupValidation verifies that past times and far-future times are rejected.
func TestScheduleFollowupValidation(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := proactive.NewStore(database)
	tool := proactive.NewScheduleFollowupTool(store)

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "past time rejected",
			input:   `{"when":"2020-01-01T09:00:00","message":"ping"}`,
			wantErr: "过去",
		},
		{
			name:    "too far future rejected",
			input:   `{"when":"2099-12-31T09:00:00","message":"ping"}`,
			wantErr: "30天",
		},
		{
			name:    "missing message rejected",
			input:   `{"when":"` + time.Now().Add(time.Hour).Format("2006-01-02T15:04:05") + `","message":""}`,
			wantErr: "message",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.InvokableRun(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result, tc.wantErr) {
				t.Errorf("expected result containing %q, got %q", tc.wantErr, result)
			}
		})
	}
}

// TestScheduleFollowupValid verifies that a valid call inserts a row.
func TestScheduleFollowupValid(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := proactive.NewStore(database)
	tool := proactive.NewScheduleFollowupTool(store)

	when := time.Now().Add(time.Hour).Format("2006-01-02T15:04:05")
	result, err := tool.InvokableRun(context.Background(),
		`{"when":"`+when+`","message":"check in on interview prep"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "已安排") {
		t.Errorf("expected success message, got %q", result)
	}

	// Confirm row was inserted by querying the store for any future item.
	// Use a time far in the future to include our item.
	items, err := store.DueItems(context.Background(), time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("due items: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

// Package proactive implements the ProactiveEngine, which sends time-driven and
// context-derived messages to the user without polluting the memory system.
package proactive

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Item is a single scheduled proactive message.
type Item struct {
	ID        int64
	TriggerAt time.Time
	Prompt    string
	Fired     bool
	CreatedAt time.Time
}

// Store is the interface for managing scheduled proactive items.
type Store interface {
	Insert(ctx context.Context, triggerAt time.Time, prompt string) error
	DueItems(ctx context.Context, now time.Time) ([]Item, error)
	MarkFired(ctx context.Context, id int64) error
}

// ProactiveStore manages the proactive_items SQLite table.
type ProactiveStore struct {
	db *sql.DB
}

// NewStore returns a ProactiveStore backed by the given database.
func NewStore(db *sql.DB) *ProactiveStore {
	return &ProactiveStore{db: db}
}

// Insert creates a new pending proactive item.
func (s *ProactiveStore) Insert(ctx context.Context, triggerAt time.Time, prompt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO proactive_items (trigger_at, prompt) VALUES (?, ?)`,
		triggerAt.UTC().Format("2006-01-02 15:04:05"), prompt,
	)
	if err != nil {
		return fmt.Errorf("insert proactive item: %w", err)
	}
	return nil
}

// DueItems returns all unfired items with trigger_at <= now.
func (s *ProactiveStore) DueItems(ctx context.Context, now time.Time) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_at, prompt, fired, created_at
		   FROM proactive_items
		  WHERE fired = FALSE AND trigger_at <= ?
		  ORDER BY trigger_at ASC`,
		now.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("query due items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		var trigStr, createdStr string
		if err := rows.Scan(&it.ID, &trigStr, &it.Prompt, &it.Fired, &createdStr); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		it.TriggerAt, _ = time.Parse("2006-01-02 15:04:05", trigStr)
		it.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		items = append(items, it)
	}
	return items, rows.Err()
}

// MarkFired marks the item with the given id as fired.
func (s *ProactiveStore) MarkFired(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE proactive_items SET fired = TRUE WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("mark fired %d: %w", id, err)
	}
	return nil
}

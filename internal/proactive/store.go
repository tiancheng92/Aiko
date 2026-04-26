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
	TriggerAt string // RFC3339; string so Wails can generate TS bindings
	Prompt    string
	CreatedAt string // RFC3339; string so Wails can generate TS bindings
}

// Store is the interface for managing scheduled proactive items.
type Store interface {
	Insert(ctx context.Context, triggerAt time.Time, prompt string) error
	DueItems(ctx context.Context, now time.Time) ([]Item, error)
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]Item, error)
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

// DueItems returns all items with trigger_at <= now, ordered by trigger_at ascending.
func (s *ProactiveStore) DueItems(ctx context.Context, now time.Time) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_at, prompt, created_at
		   FROM proactive_items
		  WHERE trigger_at <= ?
		  ORDER BY trigger_at ASC`,
		now.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("query due items: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// Delete removes the item with the given id. Returns nil if the id does not exist.
func (s *ProactiveStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proactive_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete proactive item %d: %w", id, err)
	}
	return nil
}

// List returns all pending items ordered by trigger_at ascending.
func (s *ProactiveStore) List(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_at, prompt, created_at
		   FROM proactive_items
		  ORDER BY trigger_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list proactive items: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// scanItems scans a *sql.Rows into a slice of Item.
func scanItems(rows *sql.Rows) ([]Item, error) {
	var items []Item
	for rows.Next() {
		var it Item
		var trigStr, createdStr string
		if err := rows.Scan(&it.ID, &trigStr, &it.Prompt, &createdStr); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		it.TriggerAt = parseDBTime(trigStr).UTC().Format(time.RFC3339)
		it.CreatedAt = parseDBTime(createdStr).UTC().Format(time.RFC3339)
		items = append(items, it)
	}
	return items, rows.Err()
}

// parseDBTime parses a SQLite DATETIME string (UTC) into time.Time.
// Tries common SQLite formats; returns zero time on failure.
func parseDBTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999",
	} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

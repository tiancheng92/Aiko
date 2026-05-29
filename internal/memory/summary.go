package memory

import (
	"database/sql"
	"fmt"
)

// SummaryStore persists the single rolling conversation summary to SQLite.
type SummaryStore struct{ db *sql.DB }

// NewSummaryStore returns a SummaryStore backed by db.
func NewSummaryStore(db *sql.DB) *SummaryStore { return &SummaryStore{db: db} }

// Get returns the current summary text, or "" if no summary has been written yet.
func (s *SummaryStore) Get() (string, error) {
	var content string
	err := s.db.QueryRow(`SELECT content FROM summary WHERE id = 1`).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("summary get: %w", err)
	}
	return content, nil
}

// Set overwrites the summary with text. Uses UPSERT to maintain the single row.
func (s *SummaryStore) Set(text string) error {
	_, err := s.db.Exec(
		`INSERT INTO summary(id, content, updated_at) VALUES(1, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		text,
	)
	if err != nil {
		return fmt.Errorf("summary set: %w", err)
	}
	return nil
}

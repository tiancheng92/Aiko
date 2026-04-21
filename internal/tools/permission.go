// internal/tools/permission.go
package tools

import (
	"context"
	"database/sql"
	"time"
)

// PermissionStore persists and queries tool permission grants in SQLite.
type PermissionStore struct {
	db *sql.DB
}

// NewPermissionStore creates a PermissionStore backed by db.
func NewPermissionStore(db *sql.DB) *PermissionStore {
	return &PermissionStore{db: db}
}

// IsGranted reports whether the given tool has been granted by the user.
// Public tools always return true without hitting the DB.
func (s *PermissionStore) IsGranted(ctx context.Context, t Tool) (bool, error) {
	if t.Permission() == PermPublic {
		return true, nil
	}
	var granted int
	err := s.db.QueryRowContext(ctx,
		`SELECT granted FROM tool_permissions WHERE tool_name = ?`, t.Name(),
	).Scan(&granted)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return granted == 1, nil
}

// Grant records user approval for the given tool.
func (s *PermissionStore) Grant(ctx context.Context, toolName string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_permissions(tool_name, granted, granted_at)
		VALUES(?, 1, ?)
		ON CONFLICT(tool_name) DO UPDATE SET granted=1, granted_at=excluded.granted_at
	`, toolName, time.Now().UTC())
	return err
}

// Revoke removes user approval for the given tool.
func (s *PermissionStore) Revoke(ctx context.Context, toolName string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_permissions(tool_name, granted)
		VALUES(?, 0)
		ON CONFLICT(tool_name) DO UPDATE SET granted=0, granted_at=NULL
	`, toolName)
	return err
}

// PermissionRow represents a row in the tool_permissions table for the UI.
type PermissionRow struct {
	ToolName string `json:"ToolName"`
	Level    string `json:"Level"`
	Granted  bool   `json:"Granted"`
}

// ListAll returns all known tool permissions from the DB.
func (s *PermissionStore) ListAll(ctx context.Context) ([]PermissionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, permission_level, granted FROM tool_permissions ORDER BY tool_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PermissionRow
	for rows.Next() {
		var r PermissionRow
		var granted int
		if err := rows.Scan(&r.ToolName, &r.Level, &granted); err != nil {
			return nil, err
		}
		r.Granted = granted == 1
		result = append(result, r)
	}
	return result, rows.Err()
}

// EnsureRow inserts a row for t if one does not already exist, recording its
// permission level. This is called at startup so ListAll returns complete data.
func (s *PermissionStore) EnsureRow(ctx context.Context, t Tool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO tool_permissions(tool_name, permission_level, granted)
		VALUES(?, ?, ?)
	`, t.Name(), string(t.Permission()), boolToInt(t.Permission() == PermPublic))
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

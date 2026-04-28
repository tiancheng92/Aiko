// internal/mcp/store.go
package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	json "github.com/bytedance/sonic"
)

// ServerConfig holds the persisted configuration for one MCP server.
type ServerConfig struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "stdio" | "sse" | "http"
	Command   string            `json:"command"`   // stdio only
	Args      []string          `json:"args"`      // stdio only
	URL       string            `json:"url"`       // sse / http
	Headers   map[string]string `json:"headers"`   // sse / http optional request headers
	Enabled   bool              `json:"enabled"`
	CreatedAt string            `json:"created_at"` // RFC3339; string so Wails can generate TS bindings
}

// ServerStore manages MCP server configurations in SQLite.
type ServerStore struct {
	db *sql.DB
}

// NewServerStore creates a ServerStore backed by the given SQLite database.
func NewServerStore(db *sql.DB) *ServerStore {
	return &ServerStore{db: db}
}

// List returns all configured MCP servers ordered by creation time.
func (s *ServerStore) List(ctx context.Context) ([]ServerConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, transport, COALESCE(command,''), COALESCE(args,'[]'),
		        COALESCE(url,''), COALESCE(headers,'{}'), enabled, created_at
		 FROM mcp_servers ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list mcp_servers: %w", err)
	}
	defer rows.Close()

	var cfgs []ServerConfig
	for rows.Next() {
		var c ServerConfig
		var argsJSON, headersJSON string
		var createdAt time.Time
		if err := rows.Scan(&c.ID, &c.Name, &c.Transport, &c.Command,
			&argsJSON, &c.URL, &headersJSON, &c.Enabled, &createdAt); err != nil {
			return nil, fmt.Errorf("scan mcp_server row: %w", err)
		}
		if err := json.Unmarshal([]byte(argsJSON), &c.Args); err != nil {
			slog.Warn("mcp store: args JSON unmarshal", "id", c.ID, "err", err)
		}
		if err := json.Unmarshal([]byte(headersJSON), &c.Headers); err != nil {
			slog.Warn("mcp store: headers JSON unmarshal", "id", c.ID, "err", err)
		}
		c.CreatedAt = createdAt.Format(time.RFC3339)
		cfgs = append(cfgs, c)
	}
	return cfgs, rows.Err()
}

// Add inserts a new MCP server configuration.
func (s *ServerStore) Add(ctx context.Context, c ServerConfig) (ServerConfig, error) {
	argsJSON, err := json.Marshal(c.Args)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("marshal args: %w", err)
	}
	headersJSON, err := json.Marshal(c.Headers)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("marshal headers: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_servers(name, transport, command, args, url, headers, enabled) VALUES(?,?,?,?,?,?,?)`,
		c.Name, c.Transport, c.Command, string(argsJSON), c.URL, string(headersJSON), c.Enabled)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("insert mcp_server: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ServerConfig{}, fmt.Errorf("mcp_server last insert id: %w", err)
	}
	c.ID = id
	return c, nil
}

// Update modifies an existing MCP server configuration by ID.
func (s *ServerStore) Update(ctx context.Context, c ServerConfig) error {
	argsJSON, err := json.Marshal(c.Args)
	if err != nil {
		return fmt.Errorf("marshal args: %w", err)
	}
	headersJSON, err := json.Marshal(c.Headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE mcp_servers SET name=?, transport=?, command=?, args=?, url=?, headers=?, enabled=? WHERE id=?`,
		c.Name, c.Transport, c.Command, string(argsJSON), c.URL, string(headersJSON), c.Enabled, c.ID); err != nil {
		return fmt.Errorf("update mcp_server: %w", err)
	}
	return nil
}

// Delete removes an MCP server configuration by ID.
func (s *ServerStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete mcp_server: %w", err)
	}
	return nil
}
//go:build darwin

package services

import (
	"log/slog"

	"aiko/internal/mcp"
)

// MCPService manages MCP server configuration with hot-reload.
type MCPService struct{ s *sharedState }

// NewMCPService creates an MCPService backed by the given shared state.
func NewMCPService(s *sharedState) *MCPService { return &MCPService{s: s} }

// ListMCPServers returns all configured MCP server entries.
func (m *MCPService) ListMCPServers() ([]mcp.ServerConfig, error) {
	return m.s.mcpStore.List(m.s.ctx)
}

// AddMCPServer adds a new MCP server configuration and reloads tools.
func (m *MCPService) AddMCPServer(cfg mcp.ServerConfig) (mcp.ServerConfig, error) {
	result, err := m.s.mcpStore.Add(m.s.ctx, cfg)
	if err != nil {
		return result, err
	}
	if err := m.s.initLLMComponents(m.s.ctx); err != nil {
		slog.Warn("AddMCPServer: LLM reinit skipped", "err", err)
	}
	return result, nil
}

// UpdateMCPServer updates an existing MCP server configuration and reloads tools.
func (m *MCPService) UpdateMCPServer(cfg mcp.ServerConfig) error {
	if err := m.s.mcpStore.Update(m.s.ctx, cfg); err != nil {
		return err
	}
	if err := m.s.initLLMComponents(m.s.ctx); err != nil {
		slog.Warn("UpdateMCPServer: LLM reinit skipped", "err", err)
	}
	return nil
}

// DeleteMCPServer removes an MCP server configuration by ID and reloads tools.
func (m *MCPService) DeleteMCPServer(id int64) error {
	if err := m.s.mcpStore.Delete(m.s.ctx, id); err != nil {
		return err
	}
	if err := m.s.initLLMComponents(m.s.ctx); err != nil {
		slog.Warn("DeleteMCPServer: LLM reinit skipped", "err", err)
	}
	return nil
}

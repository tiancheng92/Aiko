package main

import (
	"log/slog"

	"aiko/internal/mcp"
)

// ListMCPServers returns all configured MCP server entries.
func (a *App) ListMCPServers() ([]mcp.ServerConfig, error) {
	return a.mcpStore.List(a.ctx)
}

// AddMCPServer adds a new MCP server configuration and reloads tools.
func (a *App) AddMCPServer(cfg mcp.ServerConfig) (mcp.ServerConfig, error) {
	result, err := a.mcpStore.Add(a.ctx, cfg)
	if err != nil {
		return result, err
	}
	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("AddMCPServer: LLM reinit skipped", "err", err)
	}
	return result, nil
}

// UpdateMCPServer updates an existing MCP server configuration and reloads tools.
func (a *App) UpdateMCPServer(cfg mcp.ServerConfig) error {
	if err := a.mcpStore.Update(a.ctx, cfg); err != nil {
		return err
	}
	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("UpdateMCPServer: LLM reinit skipped", "err", err)
	}
	return nil
}

// DeleteMCPServer removes an MCP server configuration by ID and reloads tools.
func (a *App) DeleteMCPServer(id int64) error {
	if err := a.mcpStore.Delete(a.ctx, id); err != nil {
		return err
	}
	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("DeleteMCPServer: LLM reinit skipped", "err", err)
	}
	return nil
}

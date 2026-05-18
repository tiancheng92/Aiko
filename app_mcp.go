package main

import (
	"github.com/rs/zerolog/log"

	"aiko/internal/mcp"
)

// ListMCPServers returns all configured MCP server entries.
func (a *App) ListMCPServers() ([]mcp.ServerConfig, error) {
	return a.mcpStore.List(a.ctx)
}

// AddMCPServer adds a new MCP server configuration and reloads tools asynchronously.
func (a *App) AddMCPServer(cfg mcp.ServerConfig) (mcp.ServerConfig, error) {
	result, err := a.mcpStore.Add(a.ctx, cfg)
	if err != nil {
		return result, err
	}
	go func() {
		if err := a.initLLMComponents(a.ctx); err != nil {
			log.Warn().Err(err).Msg("AddMCPServer: LLM reinit skipped")
		}
	}()
	return result, nil
}

// UpdateMCPServer updates an existing MCP server configuration and reloads tools asynchronously.
func (a *App) UpdateMCPServer(cfg mcp.ServerConfig) error {
	if err := a.mcpStore.Update(a.ctx, cfg); err != nil {
		return err
	}
	go func() {
		if err := a.initLLMComponents(a.ctx); err != nil {
			log.Warn().Err(err).Msg("UpdateMCPServer: LLM reinit skipped")
		}
	}()
	return nil
}

// DeleteMCPServer removes an MCP server configuration by ID and reloads tools asynchronously.
func (a *App) DeleteMCPServer(id int64) error {
	if err := a.mcpStore.Delete(a.ctx, id); err != nil {
		return err
	}
	go func() {
		if err := a.initLLMComponents(a.ctx); err != nil {
			log.Warn().Err(err).Msg("DeleteMCPServer: LLM reinit skipped")
		}
	}()
	return nil
}

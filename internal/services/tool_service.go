//go:build darwin

package services

import (
	"log/slog"

	"aiko/internal/agent"
	internaltools "aiko/internal/tools"
)

// ToolService manages tool execution permissions and confirmation flow.
type ToolService struct{ s *sharedState }

// NewToolService creates a ToolService backed by the given shared state.
func NewToolService(s *sharedState) *ToolService { return &ToolService{s: s} }

// ConfirmToolExecution is called by the frontend when the user approves or rejects
// a pending tool execution request.
func (t *ToolService) ConfirmToolExecution(id string, approved bool, editedContent string) {
	v, ok := t.s.pendingConfirms.Load(id)
	if !ok {
		slog.Warn("ConfirmToolExecution: unknown id", "id", id)
		return
	}
	ch := v.(chan agent.ToolConfirmResponse)
	ch <- agent.ToolConfirmResponse{Approved: approved, EditedContent: editedContent}
}

// KillToolExecution forcibly terminates a running shell or code execution by its task UUID.
func (t *ToolService) KillToolExecution(id string) {
	v, ok := t.s.runningCmds.Load(id)
	if !ok {
		slog.Warn("KillToolExecution: unknown id", "id", id)
		return
	}
	cancel := v.(func())
	cancel()
}

// GetToolPermissions returns all tool permission rows for the settings UI.
func (t *ToolService) GetToolPermissions() ([]internaltools.PermissionRow, error) {
	return t.s.permStore.ListAll(t.s.ctx)
}

// SetToolPermission grants or revokes a tool permission.
func (t *ToolService) SetToolPermission(toolName string, granted bool) error {
	if granted {
		return t.s.permStore.Grant(t.s.ctx, toolName)
	}
	return t.s.permStore.Revoke(t.s.ctx, toolName)
}

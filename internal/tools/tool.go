// internal/tools/tool.go
package tools

import (
	"aiko/internal/tools/base"
	"aiko/internal/tools/location"
)

// Re-export base types so callers that import "aiko/internal/tools" keep working.

// PermissionLevel describes how much trust a tool requires.
type PermissionLevel = base.PermissionLevel

const (
	// PermPublic tools run without any user approval.
	PermPublic = base.PermPublic
	// PermProtected tools require one-time user approval stored in the DB.
	PermProtected = base.PermProtected
)

// Tool combines eino's InvokableTool with permission declaration.
type Tool = base.Tool

// EnhancedTool is a tool that may return multimodal (non-text) results.
type EnhancedTool = base.EnhancedTool

// ShellConfirmInfo, CodeConfirmInfo, UpdateConfirmInfo, ConfirmResult, PersistBeforeRestartKey
// are re-exported from base so agent code that imports "aiko/internal/tools" continues to work.
type ShellConfirmInfo = base.ShellConfirmInfo
type CodeConfirmInfo = base.CodeConfirmInfo
type UpdateConfirmInfo = base.UpdateConfirmInfo
type ConfirmResult = base.ConfirmResult
type PersistBeforeRestartKey = base.PersistBeforeRestartKey

// FetchLocation re-exports location.FetchLocation for callers that import internal/tools.
func FetchLocation() string {
	return location.FetchLocation()
}

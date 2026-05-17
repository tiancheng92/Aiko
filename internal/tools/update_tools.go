package tools

import (
	"context"
	"encoding/gob"
	"sync/atomic"

	"github.com/cloudwego/eino/schema"
)

func init() {
	gob.Register(UpdateConfirmInfo{})
}

// CheckAndUpdateTool checks for a newer release and installs it after user confirmation.
type CheckAndUpdateTool struct {
	// InstallFn is a.InstallUpdate from app.go — injected to avoid circular imports.
	InstallFn func(downloadURL string) error
	// EmitFn is wailsruntime.EventsEmit bound to the app context — used to signal
	// app:restarting to the frontend before the binary is replaced.
	EmitFn func(event string, data any)
	// CurrentVersion is the running binary's version string (e.g. "1.0.46").
	CurrentVersion string

	// pendingVersion / pendingURL persist interrupt state across the eino
	// interrupt → resume boundary. Only one update can be in-flight at a time.
	pendingVersion atomic.Value // string
	pendingURL     atomic.Value // string
}

// Name returns the tool's registered name.
func (t *CheckAndUpdateTool) Name() string { return "check_and_update" }

// Permission returns the tool's required permission level.
func (t *CheckAndUpdateTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino ToolInfo for this tool.
func (t *CheckAndUpdateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(
		t.Name(),
		"检查 Aiko 是否有新版本可用，若有则提示用户确认后自动下载安装并重启应用。安装过程不可逆，请在合适的时机调用。",
		nil,
	), nil
}

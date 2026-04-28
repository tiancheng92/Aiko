// internal/tools/registry.go
package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
	"aiko/internal/knowledge"
	"aiko/internal/memory"
	"aiko/internal/scheduler"
)

// permGate wraps a Tool with permission enforcement.
type permGate struct {
	inner Tool
	perm  *PermissionStore
}

// ToEino wraps a Tool with permission enforcement, returning an eino BaseTool.
func ToEino(t Tool, permStore *PermissionStore) tool.BaseTool {
	return &permGate{inner: t, perm: permStore}
}

// Info delegates to the inner tool.
func (g *permGate) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return g.inner.Info(ctx)
}

// InvokableRun checks permission before delegating to the inner tool.
func (g *permGate) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	ok, err := g.perm.IsGranted(ctx, g.inner)
	if err != nil {
		return "", fmt.Errorf("permission check failed: %w", err)
	}
	if !ok {
		return fmt.Sprintf("工具 %q 尚未授权，请在设置 → 工具权限 中开启后重试。", g.inner.Name()), nil
	}
	return g.inner.InvokableRun(ctx, input, opts...)
}

// EnhancedTool describes a tool that returns multimodal results via eino's
// EnhancedInvokableTool interface. It intentionally does NOT embed Tool
// (which carries the plain-string InvokableRun) to avoid a duplicate-method
// conflict; only the structured InvokableRun signature is exposed here.
type EnhancedTool interface {
	tool.BaseTool
	// Name returns the stable snake_case name used in permission storage.
	Name() string
	// Permission returns the required permission level.
	Permission() PermissionLevel
	InvokableRun(ctx context.Context, arg *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error)
}

// enhancedPermGate wraps an EnhancedTool with permission enforcement.
// It implements tool.EnhancedInvokableTool so eino ToolsNode uses the
// structured *schema.ToolResult return path.
type enhancedPermGate struct {
	inner EnhancedTool
	perm  *PermissionStore
}

// ToEinoEnhanced wraps an EnhancedTool with permission enforcement.
func ToEinoEnhanced(t EnhancedTool, permStore *PermissionStore) tool.BaseTool {
	return &enhancedPermGate{inner: t, perm: permStore}
}

// Info delegates to the inner tool.
func (g *enhancedPermGate) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return g.inner.Info(ctx)
}

// enhancedToolAdapter adapts an EnhancedTool to the Tool interface so that
// IsGranted (which only uses Name() and Permission()) can be reused without
// a duplicate-method conflict on InvokableRun.
type enhancedToolAdapter struct{ inner EnhancedTool }

func (a *enhancedToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return a.inner.Info(ctx)
}
func (a *enhancedToolAdapter) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	return "", nil // never called; adapter is used only for permission checks
}
func (a *enhancedToolAdapter) Name() string             { return a.inner.Name() }
func (a *enhancedToolAdapter) Permission() PermissionLevel { return a.inner.Permission() }

// InvokableRun implements tool.EnhancedInvokableTool with permission enforcement.
func (g *enhancedPermGate) InvokableRun(ctx context.Context, arg *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
	ok, err := g.perm.IsGranted(ctx, &enhancedToolAdapter{g.inner})
	if err != nil {
		return nil, fmt.Errorf("permission check failed: %w", err)
	}
	if !ok {
		return &schema.ToolResult{
			Parts: []schema.ToolOutputPart{
				{
					Type: schema.ToolPartTypeText,
					Text: fmt.Sprintf("工具 %q 尚未授权，请在设置 → 工具权限 中开启后重试。", g.inner.Name()),
				},
			},
		}, nil
	}
	return g.inner.InvokableRun(ctx, arg, opts...)
}

// All returns all stateless built-in Tool instances in registration order.
func All() []Tool {
	return []Tool{
		&GetCurrentTimeTool{},
		&GetTimezoneTool{},
		&FormatTimeTool{},
		&GetOSInfoTool{},
		&GetHardwareInfoTool{},
		&GetSystemStatsTool{},
		&GetNetworkStatusTool{},
		&GetLocationTool{},
		&GetWeatherTool{},
		&WebSearchTool{},
		&WebFetchTool{},
		&GetBrowserURLTool{},
		&GetRemindersTool{},
		&CompleteReminderTool{},
		&GetMailsTool{},
		&GetMailContentTool{},
		&ReadClipboardTool{},
		&WriteClipboardTool{},
		&ListRunningAppsTool{},
		&ControlAppTool{},
		&GetCalendarEventsTool{},
		&CreateCalendarEventTool{},
	}
}

// AllEino converts all stateless tools to eino BaseTool slice, including
// enhanced (multimodal) tools wrapped with the appropriate gate.
func AllEino(permStore *PermissionStore) []tool.BaseTool {
	all := All()
	result := make([]tool.BaseTool, 0, len(all)+1)
	for _, t := range all {
		result = append(result, ToEino(t, permStore))
	}
	// Enhanced multimodal tools (EnhancedInvokableTool interface).
	result = append(result, ToEinoEnhanced(&TakeScreenshotTool{}, permStore))
	return result
}

// AllPermissionDeclarations returns every built-in tool (stateless, contextual,
// enhanced) as a lightweight permission descriptor — the name and permission
// level only. app.startup iterates this list to populate tool_permissions rows,
// so adding a new tool requires only updating this function (plus the relevant
// constructor list), not a separate hardcoded block in app.go.
//
// Only returns tools defined in this package; callers are responsible for
// ensuring rows for tools defined elsewhere (e.g. proactive.ScheduleFollowupTool).
func AllPermissionDeclarations() []namedPermDecl {
	decls := make([]namedPermDecl, 0)
	for _, t := range All() {
		decls = append(decls, namedPermDecl{Name_: t.Name(), Perm_: t.Permission()})
	}
	// Contextual tools: we instantiate zero-value structs purely to read their
	// declared Name/Permission. Runtime dependencies are not required here.
	ctxPrototypes := []Tool{
		&SearchKnowledgeTool{},
		&CronTool{},
		&SaveMemoryTool{},
		&UpdateUserProfileTool{},
		&SaveSkillTool{},
		&ListDirectoryTool{},
		&ReadFileTool{},
		&WriteFileTool{},
		&DeleteFileTool{},
		&MakeDirectoryTool{},
		&MoveFileTool{},
		&ExecuteShellTool{},
		&ExecuteCodeTool{},
	}
	for _, t := range ctxPrototypes {
		decls = append(decls, namedPermDecl{Name_: t.Name(), Perm_: t.Permission()})
	}
	// Enhanced multimodal tools.
	decls = append(decls, namedPermDecl{
		Name_: (&TakeScreenshotTool{}).Name(),
		Perm_: (&TakeScreenshotTool{}).Permission(),
	})
	return decls
}

// namedPermDecl is a static tool-name + permission-level descriptor that
// satisfies the permission store's namedPerm interface without needing a full
// Tool implementation. Keeping it here avoids leaking tool types to callers.
type namedPermDecl struct {
	Name_ string
	Perm_ PermissionLevel
}

// Name returns the stable tool name.
func (d namedPermDecl) Name() string { return d.Name_ }

// Permission returns the required permission level.
func (d namedPermDecl) Permission() PermissionLevel { return d.Perm_ }

// AllContextual returns tools that require runtime dependencies injected at startup.
func AllContextual(
	permStore *PermissionStore,
	knowledgeSt *knowledge.Store,
	sched *scheduler.Scheduler,
	longMem *memory.LongStore,
	dataDir string,
	cfg *config.Config,
	registerCmd func(id string, cancel func()),
	unregisterCmd func(id string),
) []tool.BaseTool {
	contextTools := []Tool{
		&SearchKnowledgeTool{KnowledgeSt: knowledgeSt},
		&CronTool{Scheduler: sched},
		&SaveMemoryTool{LongMem: longMem},
		&UpdateUserProfileTool{DataDir: dataDir},
		&SaveSkillTool{DataDir: dataDir},
		// File system tools
		&ListDirectoryTool{Cfg: cfg},
		&ReadFileTool{Cfg: cfg},
		&WriteFileTool{Cfg: cfg},
		&DeleteFileTool{Cfg: cfg},
		&MakeDirectoryTool{Cfg: cfg},
		&MoveFileTool{Cfg: cfg},
		// Execution tools
		&ExecuteShellTool{Cfg: cfg, RegisterCmd: registerCmd, UnregisterCmd: unregisterCmd},
		&ExecuteCodeTool{Cfg: cfg, RegisterCmd: registerCmd, UnregisterCmd: unregisterCmd},
	}
	result := make([]tool.BaseTool, len(contextTools))
	for i, t := range contextTools {
		result[i] = ToEino(t, permStore)
	}
	return result
}

// internal/tools/registry.go
package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"desktop-pet/internal/knowledge"
	"desktop-pet/internal/scheduler"
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

// All returns all stateless built-in Tool instances in registration order.
func All() []Tool {
	return []Tool{
		&GetCurrentTimeTool{},
		&GetTimezoneTool{},
		&FormatTimeTool{},
		&GetOSInfoTool{},
		&GetHardwareInfoTool{},
		&GetNetworkStatusTool{},
		&GetLocationTool{},
		&WebSearchTool{},
		&WebFetchTool{},
	}
}

// AllEino converts all stateless tools to eino BaseTool slice.
func AllEino(permStore *PermissionStore) []tool.BaseTool {
	all := All()
	result := make([]tool.BaseTool, len(all))
	for i, t := range all {
		result[i] = ToEino(t, permStore)
	}
	return result
}

// AllContextual returns tools that require runtime dependencies injected at startup.
func AllContextual(
	permStore *PermissionStore,
	knowledgeSt *knowledge.Store,
	sched *scheduler.Scheduler,
) []tool.BaseTool {
	contextTools := []Tool{
		&SearchKnowledgeTool{KnowledgeSt: knowledgeSt},
		&CronTool{Scheduler: sched},
	}
	result := make([]tool.BaseTool, len(contextTools))
	for i, t := range contextTools {
		result[i] = ToEino(t, permStore)
	}
	return result
}

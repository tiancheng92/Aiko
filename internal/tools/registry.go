// internal/tools/registry.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// All returns all built-in Tool instances in registration order.
func All() []Tool {
	return []Tool{
		&GetCurrentTimeTool{},
		&GetTimezoneTool{},
		&GetOSInfoTool{},
		&GetHardwareInfoTool{},
	}
}

// einoTool wraps a Tool + PermissionStore as an eino InvokableTool.
type einoTool struct {
	inner Tool
	perm  *PermissionStore
}

// ToEino converts a Tool into an eino tool.BaseTool, gated by permStore.
func ToEino(t Tool, permStore *PermissionStore) tool.BaseTool {
	return &einoTool{inner: t, perm: permStore}
}

// AllEino converts all built-in tools to eino BaseTool slice.
func AllEino(permStore *PermissionStore) []tool.BaseTool {
	all := All()
	result := make([]tool.BaseTool, len(all))
	for i, t := range all {
		result[i] = ToEino(t, permStore)
	}
	return result
}

func (e *einoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: e.inner.Name(),
		Desc: e.inner.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"args": {
				Desc:     "JSON object with tool arguments (may be empty {})",
				Required: false,
				Type:     schema.String,
			},
		}),
	}, nil
}

func (e *einoTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	// Check permission.
	ok, err := e.perm.IsGranted(ctx, e.inner)
	if err != nil {
		return "", fmt.Errorf("permission check failed: %w", err)
	}
	if !ok {
		return fmt.Sprintf("工具 %q 尚未授权，请在设置 → 工具权限 中开启后重试。", e.inner.Name()), nil
	}

	// Parse args from JSON string (best-effort).
	var args map[string]any
	if input != "" && input != "{}" {
		_ = json.Unmarshal([]byte(input), &args)
	}
	if args == nil {
		args = map[string]any{}
	}

	result := e.inner.Execute(ctx, args)
	if result.Error != nil {
		return fmt.Sprintf("工具 %q 执行失败: %v", e.inner.Name(), result.Error), nil
	}
	return result.Content, nil
}

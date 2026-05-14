//go:build !darwin

package tools

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"
)

// InvokableRun returns an unsupported message on non-macOS platforms.
func (t *CheckAndUpdateTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
	return "check_and_update 仅支持 macOS", nil
}

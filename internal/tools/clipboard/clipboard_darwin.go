//go:build darwin

// internal/tools/clipboard/clipboard_darwin.go
package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"aiko/internal/tools/base"
)

// InvokableRun reads text from the macOS clipboard via pbpaste.
func (t *ReadClipboardTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	cmd := exec.Command("pbpaste")
	cmd.Env = append(os.Environ(), "LANG=en_US.UTF-8", "LC_CTYPE=en_US.UTF-8")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pbpaste: %w", err)
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return "剪贴板为空", nil
	}
	return text, nil
}

// InvokableRun writes text to the macOS clipboard via pbcopy.
func (t *WriteClipboardTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return "请提供 text 参数", nil
	}
	cmd := exec.Command("pbcopy")
	cmd.Stdin = bytes.NewBufferString(text)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pbcopy: %w", err)
	}
	return "已写入剪贴板", nil
}

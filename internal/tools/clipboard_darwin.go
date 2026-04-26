//go:build darwin

// internal/tools/clipboard_darwin.go
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// InvokableRun reads text from the macOS clipboard via pbpaste.
func (t *ReadClipboardTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return "", fmt.Errorf("pbpaste: %w", err)
	}
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return "剪贴板为空", nil
	}
	return text, nil
}

// InvokableRun writes text to the macOS clipboard via pbcopy.
func (t *WriteClipboardTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	text, _ := args["text"].(string)
	if text == "" {
		return "请提供 text 参数", nil
	}
	cmd := exec.Command("pbcopy")
	cmd.Stdin = bytes.NewBufferString(text)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pbcopy: %w", err)
	}
	return "已写入剪贴板", nil
}

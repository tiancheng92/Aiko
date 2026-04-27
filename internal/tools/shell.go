// internal/tools/shell.go
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
)

// InvokableRun implements the execute_shell tool.
// On first call it interrupts to request user confirmation.
// On resume it executes the (possibly edited) command.
func (t *ExecuteShellTool) InvokableRun(ctx context.Context, input string, opts ...einotool.Option) (string, error) {
	args := parseArgs(input)
	command, _ := args["command"].(string)
	workingDir, _ := args["working_dir"].(string)
	if command == "" {
		return "请提供 command 参数", nil
	}
	if workingDir == "" {
		home, _ := os.UserHomeDir()
		workingDir = home
	}

	// Bypass confirmation for trusted commands.
	if isTrustedCommand(command, t.Cfg.ShellTrustedCommands) {
		return runShellCommand(ctx, command, workingDir, t.Cfg.ShellTimeout, t.RegisterCmd, t.UnregisterCmd)
	}

	// Check if this is a resume (user has already confirmed).
	isTarget, hasData, confirmResult := einotool.GetResumeContext[ConfirmResult](ctx)
	if isTarget && hasData {
		if !confirmResult.Approved {
			return "用户已拒绝执行该命令", nil
		}
		// Use the (possibly edited) command from the confirmation modal.
		if confirmResult.EditedContent != "" {
			command = confirmResult.EditedContent
		}
		return runShellCommand(ctx, command, workingDir, t.Cfg.ShellTimeout, t.RegisterCmd, t.UnregisterCmd)
	}

	// First call — interrupt to ask for confirmation.
	id := fmt.Sprintf("shell-%d", time.Now().UnixNano())
	return "", einotool.Interrupt(ctx, ShellConfirmInfo{
		ID:         id,
		Command:    command,
		WorkingDir: workingDir,
	})
}

// isTrustedCommand reports whether command matches any trusted prefix.
// It checks exact equality or prefix + space to avoid "gitk" matching "git".
func isTrustedCommand(command string, trusted []string) bool {
	cmd := strings.TrimLeft(command, " \t")
	for _, entry := range trusted {
		e := strings.TrimSpace(entry)
		if e == "" {
			continue
		}
		if cmd == e || strings.HasPrefix(cmd, e+" ") {
			return true
		}
	}
	return false
}

// runShellCommand executes command in workingDir with the given timeout.
func runShellCommand(ctx context.Context, command, workingDir string, timeoutSecs int, register func(string, func()), unregister func(string)) (string, error) {
	id := fmt.Sprintf("shell-run-%d", time.Now().UnixNano())
	timeout := time.Duration(timeoutSecs) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Dir = workingDir

	if register != nil {
		register(id, cancel)
	}
	defer func() {
		if unregister != nil {
			unregister(id)
		}
	}()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := buf.String()
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("命令超时（%ds）\n%s", timeoutSecs, output), nil
		}
		return fmt.Sprintf("命令执行失败：%s\n%s", err.Error(), output), nil
	}
	if output == "" {
		return "命令执行成功（无输出）", nil
	}
	return output, nil
}

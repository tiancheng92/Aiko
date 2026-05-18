// internal/tools/exec/shell.go
package exec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	goexec "os/exec"
	"strings"
	"syscall"
	"time"

	"aiko/internal/execenv"
	"aiko/internal/tools/base"

	einotool "github.com/cloudwego/eino/components/tool"
)

// InvokableRun implements the execute_shell tool.
// On first call it interrupts to request user confirmation.
// On resume it executes the (possibly edited) command.
func (t *ExecuteShellTool) InvokableRun(ctx context.Context, input string, opts ...einotool.Option) (string, error) {
	if t.Cfg == nil {
		return "execute_shell 配置缺失，请在设置中完成初始化", nil
	}
	args := base.ParseArgs(input)
	command, _ := args["command"].(string)
	workingDir, _ := args["working_dir"].(string)
	if command == "" {
		return "请提供 command 参数", nil
	}
	if workingDir == "" {
		home, _ := os.UserHomeDir()
		workingDir = home
	}
	// Validate workingDir against the allowed-paths whitelist so the Agent
	// cannot escape sandbox boundaries via an unexpected cwd, even when the
	// user has marked a command as trusted.
	if len(t.Cfg.AllowedPaths) > 0 {
		if abs, err := base.CheckPath(workingDir, t.Cfg.AllowedPaths); err != nil {
			return err.Error(), nil
		} else {
			workingDir = abs
		}
	}

	// Bypass confirmation for trusted commands.
	if IsTrustedCommand(command, t.Cfg.ShellTrustedCommands) {
		return runShellCommand(ctx, command, workingDir, t.Cfg.ShellTimeout, t.RegisterCmd, t.UnregisterCmd)
	}

	// Check if this is a resume (user has already confirmed).
	isTarget, hasData, confirmResult := einotool.GetResumeContext[base.ConfirmResult](ctx)
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
	return "", einotool.Interrupt(ctx, base.ShellConfirmInfo{
		ID:         id,
		Command:    command,
		WorkingDir: workingDir,
	})
}

// IsTrustedCommand reports whether command matches any trusted prefix.
// It checks exact equality or prefix + space to avoid "gitk" matching "git".
func IsTrustedCommand(command string, trusted []string) bool {
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
// The bash process is placed in its own process group (Setpgid) so that
// cancel/timeout kills the entire group — including child processes that
// hold the stdout/stderr pipe open — ensuring cmd.Run() always returns
// promptly and the progress bar is dismissed.
func runShellCommand(ctx context.Context, command, workingDir string, timeoutSecs int, register func(string, func()), unregister func(string)) (string, error) {
	id := fmt.Sprintf("shell-run-%d", time.Now().UnixNano())
	timeout := time.Duration(timeoutSecs) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := goexec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Dir = workingDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Override CommandContext's default kill so the whole process group is
	// signalled, not just the bash parent.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

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
		if ctx.Err() != nil {
			return fmt.Sprintf("命令已终止\n%s", output), nil
		}
		return fmt.Sprintf("命令执行失败：%s\n%s", err.Error(), output), nil
	}
	if output == "" {
		return "命令执行成功（无输出）", nil
	}
	return output, nil
}

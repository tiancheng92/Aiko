// internal/tools/code.go
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
)

// interpreterFor maps a language name to the system interpreter binary and file extension.
func interpreterFor(lang string) (binary, ext string, ok bool) {
	switch lang {
	case "python":
		return "python3", ".py", true
	case "node":
		return "node", ".js", true
	case "ruby":
		return "ruby", ".rb", true
	case "bash":
		return "bash", ".sh", true
	default:
		return "", "", false
	}
}

// InvokableRun implements the execute_code tool.
// On first call it interrupts to request user confirmation.
// On resume it executes the (possibly edited) code.
func (t *ExecuteCodeTool) InvokableRun(ctx context.Context, input string, opts ...einotool.Option) (string, error) {
	if t.Cfg == nil {
		return "execute_code 配置缺失，请在设置中完成初始化", nil
	}
	args := parseArgs(input)
	language, _ := args["language"].(string)
	code, _ := args["code"].(string)
	workingDir, _ := args["working_dir"].(string)

	if language == "" || code == "" {
		return "请提供 language 和 code 参数", nil
	}
	if _, _, ok := interpreterFor(language); !ok {
		return fmt.Sprintf("不支持的语言 %q，支持：python、node、ruby、bash", language), nil
	}
	if workingDir == "" {
		home, _ := os.UserHomeDir()
		workingDir = home
	}
	// Validate workingDir against the allowed-paths whitelist. Code execution
	// is gated by user confirmation, but the Agent may still pick a workingDir
	// outside allowed paths; catching it early produces a clearer error.
	if len(t.Cfg.AllowedPaths) > 0 {
		if abs, err := checkPath(workingDir, t.Cfg.AllowedPaths); err != nil {
			return err.Error(), nil
		} else {
			workingDir = abs
		}
	}

	// Check if this is a resume.
	isTarget, hasData, confirmResult := einotool.GetResumeContext[ConfirmResult](ctx)
	if isTarget && hasData {
		if !confirmResult.Approved {
			return "用户已拒绝执行该代码", nil
		}
		if confirmResult.EditedContent != "" {
			code = confirmResult.EditedContent
		}
		return runCodeExecution(ctx, language, code, workingDir, t.Cfg.CodeTimeout, t.RegisterCmd, t.UnregisterCmd)
	}

	// First call — interrupt.
	id := fmt.Sprintf("code-%d", time.Now().UnixNano())
	return "", einotool.Interrupt(ctx, CodeConfirmInfo{
		ID:         id,
		Language:   language,
		Code:       code,
		WorkingDir: workingDir,
	})
}

// runCodeExecution writes code to a temp file and runs it.
func runCodeExecution(ctx context.Context, language, code, workingDir string, timeoutSecs int, register func(string, func()), unregister func(string)) (string, error) {
	binary, ext, _ := interpreterFor(language)

	tmp, err := os.CreateTemp("", "aiko-code-*"+ext)
	if err != nil {
		return fmt.Sprintf("创建临时文件失败：%s", err.Error()), nil
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(code); err != nil {
		tmp.Close()
		return fmt.Sprintf("写入代码失败：%s", err.Error()), nil
	}
	tmp.Close()

	id := fmt.Sprintf("code-run-%d", time.Now().UnixNano())
	timeout := time.Duration(timeoutSecs) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if language == "bash" {
		os.Chmod(tmpPath, 0o755)
	}

	cmd := exec.CommandContext(cmdCtx, binary, tmpPath)
	cmd.Dir = filepath.Clean(workingDir)

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

	err = cmd.Run()
	output := buf.String()
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("代码执行超时（%ds）\n%s", timeoutSecs, output), nil
		}
		return fmt.Sprintf("执行失败：%s\n%s", err.Error(), output), nil
	}
	if output == "" {
		return "执行成功（无输出）", nil
	}
	return output, nil
}

# Execution Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add file system tools (6), shell execution tool, and code execution tool to Aiko, with path-whitelist access control for files and per-call eino interrupt/resume confirmation for shell and code execution.

**Architecture:** File system tools are plain cross-platform Tool implementations that check `cfg.AllowedPaths` before every operation. Shell and code tools call `tool.Interrupt` to pause execution, surface a confirmation modal in the frontend via a Wails event, then resume via `runner.ResumeWithParams` after the user approves/rejects. A `sync.Map` on `App` bridges the async gap between the Wails binding call and the running iterator goroutine; a second `sync.Map` tracks live `exec.Cmd` handles for the kill endpoint.

**Tech Stack:** Go stdlib (`os`, `exec`, `filepath`), eino `tool.Interrupt` / `tool.GetResumeContext`, Wails v2 events + runtime bindings, Vue 3 `<script setup>`, existing `internal/config`, `internal/tools`, `internal/db` packages.

---

## File Structure

**New files:**
```
internal/tools/
  filesystem_tools.go    # Struct defs + Info() for 6 file system tools; no build tag
  filesystem.go          # InvokableRun for all 6 file system tools
  shell_tools.go         # ExecuteShellTool struct + Info()
  shell.go               # ExecuteShellTool.InvokableRun
  code_tools.go          # ExecuteCodeTool struct + Info()
  code.go                # ExecuteCodeTool.InvokableRun

frontend/src/components/
  ToolConfirmModal.vue   # Confirmation dialog triggered by tool:confirm event
  ExecutionProgress.vue  # In-chat progress indicator with kill button
```

**Modified files:**
```
internal/config/config.go        # Add AllowedPaths, ShellTimeout, CodeTimeout fields
internal/db/sqlite.go            # Add migration patches for 3 new settings keys
internal/tools/registry.go       # Register all 8 new tools
internal/agent/agent.go          # Handle interrupt events in drainRunner / drainRunnerMsg
app.go                           # Add pendingConfirms, runningCmds, ConfirmToolExecution, KillToolExecution
macos.go                         # Add .tool-confirm-modal, .execution-progress to hitTest selector
frontend/src/components/SettingsWindow.vue  # Add tool settings section
frontend/src/components/ChatPanel.vue       # Mount ExecutionProgress, listen to execution events
```

---

## Task 1: Config — add AllowedPaths, ShellTimeout, CodeTimeout

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: Add fields to Config struct**

In `internal/config/config.go`, add three fields after `NudgeInterval`:

```go
AllowedPaths  []string // file system path whitelist; empty = deny all
ShellTimeout  int      // execute_shell timeout in seconds; default 30
CodeTimeout   int      // execute_code timeout in seconds; default 60
```

- [ ] **Step 2: Load new fields in Load()**

In the `Load()` function, after `cfg.NudgeInterval = parseInt(...)`, add:

```go
cfg.AllowedPaths = splitLines(m["allowed_paths"])
cfg.ShellTimeout = parseInt(m["shell_timeout"], 30)
if cfg.ShellTimeout <= 0 {
    cfg.ShellTimeout = 30
}
cfg.CodeTimeout = parseInt(m["code_timeout"], 60)
if cfg.CodeTimeout <= 0 {
    cfg.CodeTimeout = 60
}
```

- [ ] **Step 3: Save new fields in Save()**

In the `pairs` map in `Save()`, add:

```go
"allowed_paths": joinLines(cfg.AllowedPaths),
"shell_timeout": strconv.Itoa(cfg.ShellTimeout),
"code_timeout":  strconv.Itoa(cfg.CodeTimeout),
```

- [ ] **Step 4: Add DB migration patches**

In `internal/db/sqlite.go`, in the `patches` slice, append three entries after the existing `images` patch:

```go
`ALTER TABLE settings ADD COLUMN allowed_paths TEXT NOT NULL DEFAULT ''`,
`ALTER TABLE settings ADD COLUMN shell_timeout INTEGER NOT NULL DEFAULT 30`,
`ALTER TABLE settings ADD COLUMN code_timeout INTEGER NOT NULL DEFAULT 60`,
```

Note: The settings table uses `key/value` rows, not columns — the `ALTER TABLE` patches above won't work. The settings table schema is key-value, so no migration is needed; `Load()` handles missing keys via `parseInt` defaults and `splitLines("")` returns nil. **Skip Step 4 — the key-value schema self-migrates.**

- [ ] **Step 5: Verify compilation**

```bash
go build ./internal/config/...
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/db/sqlite.go
git commit -m "feat(config): add AllowedPaths, ShellTimeout, CodeTimeout settings"
```

---

## Task 2: File system tool structs and Info()

**Files:**
- Create: `internal/tools/filesystem_tools.go`

- [ ] **Step 1: Create the file**

```go
// internal/tools/filesystem_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// ListDirectoryTool lists files and subdirectories at a given path.
type ListDirectoryTool struct{ Cfg interface{ GetAllowedPaths() []string } }

// Name returns the tool identifier.
func (t *ListDirectoryTool) Name() string { return "list_directory" }

// Permission declares this tool as protected.
func (t *ListDirectoryTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ListDirectoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "列出指定目录下的文件和子目录。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要列出的目录路径", Required: true},
		},
	), nil
}

// ReadFileTool reads the UTF-8 text content of a file.
type ReadFileTool struct{ Cfg interface{ GetAllowedPaths() []string } }

// Name returns the tool identifier.
func (t *ReadFileTool) Name() string { return "read_file" }

// Permission declares this tool as protected.
func (t *ReadFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ReadFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "读取文件的文本内容（UTF-8）。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "文件路径", Required: true},
		},
	), nil
}

// WriteFileTool writes or appends text content to a file.
type WriteFileTool struct{ Cfg interface{ GetAllowedPaths() []string } }

// Name returns the tool identifier.
func (t *WriteFileTool) Name() string { return "write_file" }

// Permission declares this tool as protected.
func (t *WriteFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *WriteFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "将文本内容写入（或追加到）文件。",
		map[string]*schema.ParameterInfo{
			"path":    {Type: schema.String, Desc: "文件路径", Required: true},
			"content": {Type: schema.String, Desc: "要写入的文本内容", Required: true},
			"append":  {Type: schema.Boolean, Desc: "true 表示追加，false 表示覆盖（默认 false）", Required: false},
		},
	), nil
}

// DeleteFileTool deletes a file at the given path.
type DeleteFileTool struct{ Cfg interface{ GetAllowedPaths() []string } }

// Name returns the tool identifier.
func (t *DeleteFileTool) Name() string { return "delete_file" }

// Permission declares this tool as protected.
func (t *DeleteFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *DeleteFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "删除指定路径的文件。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要删除的文件路径", Required: true},
		},
	), nil
}

// MakeDirectoryTool creates a directory and all necessary parents.
type MakeDirectoryTool struct{ Cfg interface{ GetAllowedPaths() []string } }

// Name returns the tool identifier.
func (t *MakeDirectoryTool) Name() string { return "make_directory" }

// Permission declares this tool as protected.
func (t *MakeDirectoryTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *MakeDirectoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "创建目录（包括所有必要的父目录）。",
		map[string]*schema.ParameterInfo{
			"path": {Type: schema.String, Desc: "要创建的目录路径", Required: true},
		},
	), nil
}

// MoveFileTool moves or renames a file or directory.
type MoveFileTool struct{ Cfg interface{ GetAllowedPaths() []string } }

// Name returns the tool identifier.
func (t *MoveFileTool) Name() string { return "move_file" }

// Permission declares this tool as protected.
func (t *MoveFileTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *MoveFileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "移动或重命名文件/目录。",
		map[string]*schema.ParameterInfo{
			"source":      {Type: schema.String, Desc: "源路径", Required: true},
			"destination": {Type: schema.String, Desc: "目标路径", Required: true},
		},
	), nil
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./internal/tools/...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/filesystem_tools.go
git commit -m "feat(tools): add file system tool structs and Info()"
```

---

## Task 3: File system tool implementations

**Files:**
- Create: `internal/tools/filesystem.go`

The `Cfg` field uses an interface to avoid import cycles. Since `AllowedPaths` is on `*config.Config`, we'll use a concrete `*config.Config` instead of the interface — simpler and consistent with `ExecuteShellTool`.

Revise: all 6 filesystem tool structs use `Cfg *config.Config` (not the interface). Update `filesystem_tools.go` accordingly and add `"aiko/internal/config"` import.

- [ ] **Step 1: Update filesystem_tools.go to use *config.Config**

Replace all 6 `Cfg interface{ GetAllowedPaths() []string }` fields with `Cfg *config.Config` and add the import:

```go
import (
    "context"

    "github.com/cloudwego/eino/schema"

    "aiko/internal/config"
)
```

Each struct becomes e.g.:
```go
type ListDirectoryTool struct{ Cfg *config.Config }
```

- [ ] **Step 2: Create filesystem.go**

```go
// internal/tools/filesystem.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// isPathAllowed reports whether absTarget is inside at least one of the allowed paths.
// Returns an error message suitable for returning directly to the agent if denied.
func isPathAllowed(absTarget string, allowedPaths []string) bool {
	for _, allowed := range allowedPaths {
		abs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absTarget, abs+string(filepath.Separator)) || absTarget == abs {
			return true
		}
	}
	return false
}

// checkPath resolves path to an absolute path and verifies it is within the whitelist.
// Returns the resolved absolute path and nil on success, or an empty string and a
// descriptive error on failure.
func checkPath(path string, allowedPaths []string) (string, error) {
	if len(allowedPaths) == 0 {
		return "", fmt.Errorf("文件系统访问已禁用，请在设置 → 工具设置中添加允许访问的路径")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("无效路径 %q: %w", path, err)
	}
	if !isPathAllowed(abs, allowedPaths) {
		return "", fmt.Errorf("路径 %q 不在允许列表中，请在设置 → 工具设置中添加该路径", abs)
	}
	return abs, nil
}

// InvokableRun lists files and subdirectories at the given path.
func (t *ListDirectoryTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Sprintf("读取目录失败：%s", err.Error()), nil
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size,omitempty"`
	}
	var result []entry
	for _, e := range entries {
		info, _ := e.Info()
		var size int64
		if info != nil && !e.IsDir() {
			size = info.Size()
		}
		result = append(result, entry{Name: e.Name(), IsDir: e.IsDir(), Size: size})
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

// InvokableRun reads the text content of a file.
func (t *ReadFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("读取文件失败：%s", err.Error()), nil
	}
	return string(data), nil
}

// InvokableRun writes or appends text to a file.
func (t *WriteFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	appendMode, _ := args["append"].(bool)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(abs, flag, 0o644)
	if err != nil {
		return fmt.Sprintf("打开文件失败：%s", err.Error()), nil
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return fmt.Sprintf("写入文件失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已写入 %d 字节到 %s", len(content), abs), nil
}

// InvokableRun deletes a file at the given path.
func (t *DeleteFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Sprintf("删除文件失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已删除 %s", abs), nil
}

// InvokableRun creates a directory and all necessary parents.
func (t *MakeDirectoryTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "请提供 path 参数", nil
	}
	abs, err := checkPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Sprintf("创建目录失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已创建目录 %s", abs), nil
}

// InvokableRun moves or renames a file or directory.
func (t *MoveFileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	src, _ := args["source"].(string)
	dst, _ := args["destination"].(string)
	if src == "" || dst == "" {
		return "请提供 source 和 destination 参数", nil
	}
	absSrc, err := checkPath(src, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	absDst, err := checkPath(dst, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.Rename(absSrc, absDst); err != nil {
		return fmt.Sprintf("移动失败：%s", err.Error()), nil
	}
	return fmt.Sprintf("已将 %s 移动到 %s", absSrc, absDst), nil
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./internal/tools/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/tools/filesystem_tools.go internal/tools/filesystem.go
git commit -m "feat(tools): implement file system tools with path whitelist"
```

---

## Task 4: Shell tool struct, Info(), and implementation

**Files:**
- Create: `internal/tools/shell_tools.go`
- Create: `internal/tools/shell.go`

- [ ] **Step 1: Create shell_tools.go**

```go
// internal/tools/shell_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

// ShellConfirmInfo is the interrupt payload sent to the frontend for user confirmation.
type ShellConfirmInfo struct {
	ID         string `json:"id"`
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
}

// ExecuteShellTool runs a shell command after user confirmation via eino interrupt.
type ExecuteShellTool struct {
	Cfg         *config.Config
	RegisterCmd func(id string, cancel func()) // called when cmd starts; injects into app.runningCmds
	UnregisterCmd func(id string)              // called on completion
}

// Name returns the tool identifier.
func (t *ExecuteShellTool) Name() string { return "execute_shell" }

// Permission declares this tool as protected.
func (t *ExecuteShellTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ExecuteShellTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "执行 Shell 命令（需要用户二次确认）。",
		map[string]*schema.ParameterInfo{
			"command":     {Type: schema.String, Desc: "要执行的 Shell 命令", Required: true},
			"working_dir": {Type: schema.String, Desc: "工作目录（可选，默认为用户主目录）", Required: false},
		},
	), nil
}
```

- [ ] **Step 2: Create shell.go**

```go
// internal/tools/shell.go
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/google/uuid"
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
	id := uuid.New().String()
	return "", einotool.Interrupt(ctx, ShellConfirmInfo{
		ID:         id,
		Command:    command,
		WorkingDir: workingDir,
	})
}

// runShellCommand executes command in workingDir with the given timeout.
func runShellCommand(ctx context.Context, command, workingDir string, timeoutSecs int, register func(string, func()), unregister func(string)) (string, error) {
	id := uuid.New().String()
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
```

- [ ] **Step 3: Add ConfirmResult type** (shared by shell and code tools)

In `shell_tools.go`, append:

```go
// ConfirmResult is passed as resume data from ConfirmToolExecution to the tool.
type ConfirmResult struct {
	Approved      bool   `json:"approved"`
	EditedContent string `json:"edited_content"` // user-edited command or code
}
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./internal/tools/...
```
Expected: no errors. If `github.com/google/uuid` is missing, run `go get github.com/google/uuid`.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/shell_tools.go internal/tools/shell.go
git commit -m "feat(tools): implement execute_shell with eino interrupt confirmation"
```

---

## Task 5: Code execution tool struct, Info(), and implementation

**Files:**
- Create: `internal/tools/code_tools.go`
- Create: `internal/tools/code.go`

- [ ] **Step 1: Create code_tools.go**

```go
// internal/tools/code_tools.go
package tools

import (
	"context"

	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

// CodeConfirmInfo is the interrupt payload sent to the frontend for user confirmation.
type CodeConfirmInfo struct {
	ID         string `json:"id"`
	Language   string `json:"language"`
	Code       string `json:"code"`
	WorkingDir string `json:"working_dir"`
}

// ExecuteCodeTool runs a code snippet using the system interpreter after user confirmation.
type ExecuteCodeTool struct {
	Cfg           *config.Config
	RegisterCmd   func(id string, cancel func())
	UnregisterCmd func(id string)
}

// Name returns the tool identifier.
func (t *ExecuteCodeTool) Name() string { return "execute_code" }

// Permission declares this tool as protected.
func (t *ExecuteCodeTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata.
func (t *ExecuteCodeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "执行代码片段（python/node/ruby/bash，需要用户二次确认）。",
		map[string]*schema.ParameterInfo{
			"language":    {Type: schema.String, Desc: "编程语言：python | node | ruby | bash", Required: true},
			"code":        {Type: schema.String, Desc: "要执行的代码内容", Required: true},
			"working_dir": {Type: schema.String, Desc: "工作目录（可选，默认为用户主目录）", Required: false},
		},
	), nil
}
```

- [ ] **Step 2: Create code.go**

```go
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
	"github.com/google/uuid"
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
	id := uuid.New().String()
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

	id := uuid.New().String()
	timeout := time.Duration(timeoutSecs) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// For bash scripts, make executable.
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
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./internal/tools/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/tools/code_tools.go internal/tools/code.go
git commit -m "feat(tools): implement execute_code with eino interrupt confirmation"
```

---

## Task 6: Register all 8 new tools in registry.go

**Files:**
- Modify: `internal/tools/registry.go`

- [ ] **Step 1: Read current registry.go**

Read `internal/tools/registry.go` lines 108–170 to understand `All()` and `AllContextual()`.

- [ ] **Step 2: Add file system tools to All()**

File system tools need `Cfg *config.Config`, so they can't go in `All()` (which is stateless). They belong in `AllContextual()` alongside `ExecuteShellTool` and `ExecuteCodeTool`.

Update `AllContextual` signature to accept `cfg *config.Config` and add all 8 tools:

```go
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
```

Add import for `"aiko/internal/config"` if not already present.

- [ ] **Step 3: Update the AllContextual call in app.go**

Search for `AllContextual(` in `app.go`. It currently passes 5 args. Update it to pass `cfg`, `registerCmd`, `unregisterCmd`:

```go
internaltools.AllContextual(
    a.permStore,
    a.knowledgeSt,
    a.scheduler,
    a.longMem,
    a.dataDir,
    cfg,                    // new
    func(id string, cancel func()) { a.runningCmds.Store(id, cancel) },  // new
    func(id string) { a.runningCmds.Delete(id) },                        // new
)
```

Also add `runningCmds sync.Map` field to the `App` struct in `app.go`:

```go
runningCmds sync.Map // map[string]func() — cancel funcs for running shell/code commands
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/registry.go app.go
git commit -m "feat(tools): register file system, shell, and code tools"
```

---

## Task 7: eino interrupt handling in agent.go

**Files:**
- Modify: `internal/agent/agent.go`

The key insight: when a tool calls `tool.Interrupt(ctx, info)`, the runner emits an `AgentEvent` with `event.Action.Interrupted != nil`. The `drainRunner` loop must detect this, extract the interrupt info, and block until the caller (App) signals a resume or cancel.

Because `drainRunner` runs inside a goroutine spawned by `Chat()`, we need a way to pass the `pendingConfirms` map into it. The cleanest approach: pass `pendingConfirms *sync.Map` as a parameter to `drainRunner` and `drainRunnerMsg`. When nil (ChatDirect), interrupts are auto-rejected.

- [ ] **Step 1: Add ToolConfirmRequest type to agent.go**

At the top of `agent.go`, add:

```go
// ToolConfirmRequest is emitted via Wails event when a tool requests user confirmation.
type ToolConfirmRequest struct {
	ID         string `json:"id"`
	ToolType   string `json:"tool_type"`   // "shell" or "code"
	Command    string `json:"command,omitempty"`
	Code       string `json:"code,omitempty"`
	Language   string `json:"language,omitempty"`
	WorkingDir string `json:"working_dir"`
}

// ToolConfirmResponse is sent back by the frontend via ConfirmToolExecution.
type ToolConfirmResponse struct {
	Approved      bool
	EditedContent string
}
```

- [ ] **Step 2: Add pendingConfirms and emitFn fields to Agent**

In the `Agent` struct, add:

```go
pendingConfirms *sync.Map // map[string]chan ToolConfirmResponse; bridged from App
emitEvent       func(event string, data ...interface{}) // Wails EventsEmit
```

In `New()`, accept these as parameters and assign them:

```go
func New(
    ctx context.Context,
    chatModel model.ToolCallingChatModel,
    shortMem *memory.ShortStore,
    longMem *memory.LongStore,
    tools []tool.BaseTool,
    cfg *config.Config,
    mw middleware.Middleware,
    skillMW adk.ChatModelAgentMiddleware,
    dataDir string,
    pendingConfirms *sync.Map,
    emitEvent func(event string, data ...interface{}),
) (*Agent, error) {
```

Assign in the return:
```go
return &Agent{
    ...
    pendingConfirms: pendingConfirms,
    emitEvent:       emitEvent,
}, nil
```

- [ ] **Step 3: Update drainRunner signature**

Change `drainRunner` to accept `pendingConfirms *sync.Map` and `emitEvent func(string, ...interface{})`:

```go
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
    pendingConfirms *sync.Map, emitEvent func(string, ...interface{})) (string, bool) {
```

Inside the event loop, after the existing `if event.Err != nil` check, add interrupt handling:

```go
if event.Action != nil && event.Action.Interrupted != nil {
    handled := handleInterrupt(ctx, runner, event, ch, pendingConfirms, emitEvent)
    if !handled {
        // Auto-reject when no pendingConfirms (e.g. ChatDirect)
        return "", false
    }
    // After resume the iterator continues — restart the loop iteration.
    continue
}
```

- [ ] **Step 4: Implement handleInterrupt**

Add this function to `agent.go`:

```go
// handleInterrupt processes an eino interrupt event by notifying the frontend and blocking
// until the user confirms or rejects. Returns false if the interrupt cannot be handled
// (no pendingConfirms map) and the caller should abort.
func handleInterrupt(
    ctx context.Context,
    runner *adk.Runner,
    event *adk.AgentEvent,
    ch chan<- StreamResult,
    pendingConfirms *sync.Map,
    emitEvent func(string, ...interface{}),
) bool {
    if pendingConfirms == nil || emitEvent == nil {
        return false
    }
    if event.Action.Interrupted == nil || len(event.Action.Interrupted.InterruptContexts) == 0 {
        return false
    }

    // Extract interrupt info from the first root-cause context.
    ictx := event.Action.Interrupted.InterruptContexts[0]
    interruptID := ictx.ID // used as ResumeParams.Targets key

    // Parse the tool-specific confirm info from ictx.Info.
    var req ToolConfirmRequest
    switch info := ictx.Info.(type) {
    case ShellConfirmInfo:
        req = ToolConfirmRequest{ID: info.ID, ToolType: "shell", Command: info.Command, WorkingDir: info.WorkingDir}
    case CodeConfirmInfo:
        req = ToolConfirmRequest{ID: info.ID, ToolType: "code", Language: info.Language, Code: info.Code, WorkingDir: info.WorkingDir}
    default:
        return false
    }

    // Register a response channel keyed by the tool-level ID (what the frontend knows).
    respCh := make(chan ToolConfirmResponse, 1)
    pendingConfirms.Store(req.ID, respCh)
    defer pendingConfirms.Delete(req.ID)

    // Notify frontend.
    emitEvent("tool:confirm", req)

    // Wait for user response or context cancellation.
    var resp ToolConfirmResponse
    select {
    case resp = <-respCh:
    case <-ctx.Done():
        return false
    }

    if !resp.Approved {
        // Resume with rejection data so the tool can return a graceful message.
        resumeData := map[string]any{
            interruptID: ConfirmResult{Approved: false},
        }
        iter, err := runner.ResumeWithParams(ctx, "aiko_checkpoint", &adk.ResumeParams{Targets: resumeData})
        if err != nil {
            ch <- StreamResult{Err: fmt.Errorf("resume after reject failed: %w", err)}
        }
        _ = iter
        return true
    }

    resumeData := map[string]any{
        interruptID: ConfirmResult{Approved: true, EditedContent: resp.EditedContent},
    }
    iter, err := runner.ResumeWithParams(ctx, "aiko_checkpoint", &adk.ResumeParams{Targets: resumeData})
    if err != nil {
        ch <- StreamResult{Err: fmt.Errorf("resume after confirm failed: %w", err)}
        return true
    }
    _ = iter
    return true
}
```

**Important note on checkpoint ID:** The runner auto-generates a checkpoint ID when `WithCheckPointID` is not specified — it uses `localbk` which is in-memory. We need to capture the checkpoint ID from `runner.Query`. Update the `Run`/`Query` calls to use `WithCheckPointID`:

In `Chat()` / `ChatWithMessage()` / `ChatDirect()`, pass a generated UUID checkpoint ID:

```go
checkpointID := uuid.New().String()
// pass to drainRunner
```

And update `drainRunner` to accept and pass `checkpointID string` as the `WithCheckPointID` option to `runner.Query`.

Actually, re-examining: `runner.Query` does NOT accept `WithCheckPointID` directly as a method param — it accepts `AgentRunOption`. Let's check the signature:

```go
func (r *Runner) Query(ctx context.Context, query string, opts ...AgentRunOption) *AsyncIterator[*AgentEvent]
```

So: `runner.Query(ctx, query, adk.WithCheckPointID(checkpointID))`.

The `drainRunner` function currently calls `runner.Query(ctx, query)`. Update it to:

```go
func drainRunner(ctx context.Context, runner *adk.Runner, query string, ch chan<- StreamResult,
    pendingConfirms *sync.Map, emitEvent func(string, ...interface{}), checkpointID string) (string, bool) {
    iter := runner.Query(ctx, query, adk.WithCheckPointID(checkpointID))
```

Similarly update `drainRunnerMsg`.

- [ ] **Step 5: Update all callers of drainRunner / drainRunnerMsg**

In `Chat()`:
```go
checkpointID := uuid.New().String()
fullResponse, ok := drainRunner(ctx, a.runner, query, ch, a.pendingConfirms, a.emitEvent, checkpointID)
```

In `ChatWithMessage()`:
```go
checkpointID := uuid.New().String()
fullResponse, ok := drainRunnerMsg(ctx, a.runner, &sendMsg, ch, a.pendingConfirms, a.emitEvent, checkpointID)
```

In `ChatDirect()`:
```go
checkpointID := uuid.New().String()
_, ok := drainRunner(ctx, a.runner, prompt, ch, nil, nil, checkpointID)
```

- [ ] **Step 6: Update New() call in app.go**

Find `agent.New(` in `app.go` (in `initLLMComponents`). Add `&a.pendingConfirms` and the emit function:

```go
a.petAgent, err = agent.New(
    ctx,
    chatModel,
    a.shortMem,
    a.longMem,
    tools,
    cfg,
    mw,
    skillMW,
    a.dataDir,
    &a.pendingConfirms,
    func(event string, data ...interface{}) {
        wailsruntime.EventsEmit(a.ctx, event, data...)
    },
)
```

Also add `pendingConfirms sync.Map` field to `App` struct.

- [ ] **Step 7: Verify compilation**

```bash
go build ./...
```
Expected: no errors. Fix any type mismatches.

- [ ] **Step 8: Commit**

```bash
git add internal/agent/agent.go app.go
git commit -m "feat(agent): handle eino tool interrupt/resume for confirmation flow"
```

---

## Task 8: App bindings — ConfirmToolExecution and KillToolExecution

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Add ConfirmToolExecution binding**

```go
// ConfirmToolExecution is called by the frontend when the user approves or rejects
// a pending tool execution request.
func (a *App) ConfirmToolExecution(id string, approved bool, editedContent string) {
	v, ok := a.pendingConfirms.Load(id)
	if !ok {
		slog.Warn("ConfirmToolExecution: unknown id", "id", id)
		return
	}
	ch := v.(chan agent.ToolConfirmResponse)
	ch <- agent.ToolConfirmResponse{Approved: approved, EditedContent: editedContent}
}
```

- [ ] **Step 2: Add KillToolExecution binding**

```go
// KillToolExecution forcibly terminates a running shell or code execution by its task UUID.
func (a *App) KillToolExecution(id string) {
	v, ok := a.runningCmds.Load(id)
	if !ok {
		slog.Warn("KillToolExecution: unknown id", "id", id)
		return
	}
	cancel := v.(func())
	cancel()
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add app.go
git commit -m "feat(app): add ConfirmToolExecution and KillToolExecution Wails bindings"
```

---

## Task 9: Frontend — ToolConfirmModal component

**Files:**
- Create: `frontend/src/components/ToolConfirmModal.vue`
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Create ToolConfirmModal.vue**

```vue
<!-- frontend/src/components/ToolConfirmModal.vue -->
<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { ConfirmToolExecution } from '../../wailsjs/go/main/App'

const visible = ref(false)
const request = ref(null) // ToolConfirmRequest
const editedContent = ref('')

const languageLabel = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell'
  const map = { python: 'Python', node: 'Node.js', ruby: 'Ruby', bash: 'Bash' }
  return map[request.value.language] || request.value.language
})

const riskText = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell 命令可修改系统文件、执行任意操作，请确认安全后再批准。'
  return `${languageLabel.value} 代码将使用系统解释器直接执行，请检查内容后再批准。`
})

function onConfirmEvent(req) {
  request.value = req
  editedContent.value = req.tool_type === 'shell' ? req.command : req.code
  visible.value = true
}

async function approve() {
  visible.value = false
  await ConfirmToolExecution(request.value.id, true, editedContent.value)
}

async function reject() {
  visible.value = false
  await ConfirmToolExecution(request.value.id, false, '')
}

onMounted(() => EventsOn('tool:confirm', onConfirmEvent))
onUnmounted(() => EventsOff('tool:confirm'))
</script>

<template>
  <Teleport to="body">
    <div v-if="visible" class="tool-confirm-modal">
      <div class="modal-backdrop" @click.self="reject" />
      <div class="modal-box">
        <div class="modal-header">
          <span class="badge">{{ languageLabel }}</span>
          <span class="title">⚠️ Agent 请求执行{{ request?.tool_type === 'shell' ? ' Shell 命令' : '代码' }}</span>
        </div>

        <div class="modal-field">
          <label>工作目录</label>
          <span class="dir-path">{{ request?.working_dir }}</span>
        </div>

        <div class="modal-field">
          <label>{{ request?.tool_type === 'shell' ? '命令' : '代码' }}（可编辑）</label>
          <textarea
            v-model="editedContent"
            class="content-editor"
            :rows="request?.tool_type === 'code' ? 8 : 3"
            spellcheck="false"
          />
        </div>

        <p class="risk-text">{{ riskText }}</p>

        <div class="modal-actions">
          <button class="btn-reject" @click="reject">拒绝</button>
          <button class="btn-approve" @click="approve">批准执行</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.tool-confirm-modal {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
}
.modal-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
}
.modal-box {
  position: relative;
  background: #1e1e2e;
  border: 1px solid rgba(255,255,255,0.12);
  border-radius: 12px;
  padding: 24px;
  width: 480px;
  max-width: 90vw;
  box-shadow: 0 20px 60px rgba(0,0,0,0.5);
}
.modal-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}
.badge {
  background: rgba(255,180,0,0.2);
  color: #ffb400;
  border: 1px solid rgba(255,180,0,0.3);
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 12px;
  font-weight: 600;
}
.title {
  font-size: 14px;
  font-weight: 600;
  color: #e0e0e0;
}
.modal-field {
  margin-bottom: 12px;
}
.modal-field label {
  display: block;
  font-size: 11px;
  color: #888;
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.dir-path {
  font-size: 12px;
  color: #aaa;
  font-family: monospace;
}
.content-editor {
  width: 100%;
  background: #12121e;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px;
  color: #e0e0e0;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 13px;
  padding: 10px 12px;
  resize: vertical;
  box-sizing: border-box;
  outline: none;
}
.content-editor:focus {
  border-color: rgba(120,160,255,0.4);
}
.risk-text {
  font-size: 12px;
  color: #f59e0b;
  margin: 12px 0 16px;
  line-height: 1.5;
}
.modal-actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}
.btn-reject {
  padding: 8px 20px;
  border-radius: 6px;
  border: 1px solid rgba(255,255,255,0.15);
  background: transparent;
  color: #ccc;
  cursor: pointer;
  font-size: 13px;
}
.btn-reject:hover { background: rgba(255,255,255,0.06); }
.btn-approve {
  padding: 8px 20px;
  border-radius: 6px;
  border: none;
  background: #3b6ff5;
  color: #fff;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
}
.btn-approve:hover { background: #4a7cf7; }
</style>
```

- [ ] **Step 2: Mount ToolConfirmModal in ChatPanel.vue**

In `ChatPanel.vue`, import and mount the modal:

```js
import ToolConfirmModal from './ToolConfirmModal.vue'
```

Add to template (inside the root element):
```html
<ToolConfirmModal />
```

- [ ] **Step 3: Verify frontend builds**

```bash
cd frontend && yarn build 2>&1 | tail -5
```
Expected: build succeeds, no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ToolConfirmModal.vue frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): add ToolConfirmModal for tool execution confirmation"
```

---

## Task 10: Frontend — ExecutionProgress component

**Files:**
- Create: `frontend/src/components/ExecutionProgress.vue`
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Create ExecutionProgress.vue**

This component appears as a bubble in the chat when a tool is running. The backend emits `tool:executing` on start and `tool:executed` on completion.

Add two new Wails events in `app.go`:
- `tool:executing` payload: `{id, tool_type, command_preview, timeout_secs}`
- `tool:executed` payload: `{id}` (signals removal)

Emit `tool:executing` in `runShellCommand` / `runCodeExecution` right after `register` is called — but these are in the tools package with no Wails access. The better approach: emit from the `registerCmd` callback in `app.go`.

Update the `registerCmd` lambda in Task 6 / app.go to also emit:

```go
func(id string, cancel func()) {
    a.runningCmds.Store(id, cancel)
    // Note: tool type and preview are not available here without extra params.
    // Emit from a wrapper in app.go instead.
    wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]interface{}{"id": id})
}
```

For simplicity, `ExecutionProgress` just shows a generic "正在执行工具..." message with a timer and kill button. The kill button calls `KillToolExecution(id)`.

```vue
<!-- frontend/src/components/ExecutionProgress.vue -->
<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { KillToolExecution } from '../../wailsjs/go/main/App'

const executions = ref([]) // [{id, elapsed, intervalId}]

function onExecuting({ id }) {
  const startTime = Date.now()
  const intervalId = setInterval(() => {
    const item = executions.value.find(e => e.id === id)
    if (item) item.elapsed = Math.floor((Date.now() - startTime) / 1000)
  }, 1000)
  executions.value.push({ id, elapsed: 0, intervalId })
}

function onExecuted({ id }) {
  const idx = executions.value.findIndex(e => e.id === id)
  if (idx !== -1) {
    clearInterval(executions.value[idx].intervalId)
    executions.value.splice(idx, 1)
  }
}

async function kill(id) {
  await KillToolExecution(id)
}

onMounted(() => {
  EventsOn('tool:executing', onExecuting)
  EventsOn('tool:executed', onExecuted)
})
onUnmounted(() => {
  EventsOff('tool:executing')
  EventsOff('tool:executed')
  executions.value.forEach(e => clearInterval(e.intervalId))
})
</script>

<template>
  <div v-for="exec in executions" :key="exec.id" class="execution-progress">
    <span class="exec-icon">⚙️</span>
    <span class="exec-label">正在执行工具…</span>
    <span class="exec-timer">{{ exec.elapsed }}s</span>
    <button class="exec-kill" @click="kill(exec.id)">终止</button>
  </div>
</template>

<style scoped>
.execution-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 8px;
  padding: 8px 14px;
  margin: 4px 0;
  font-size: 13px;
  color: #ccc;
}
.exec-icon { font-size: 14px; }
.exec-label { flex: 1; }
.exec-timer { color: #888; font-family: monospace; }
.exec-kill {
  padding: 3px 10px;
  border-radius: 4px;
  border: 1px solid rgba(255,80,80,0.4);
  background: rgba(255,80,80,0.1);
  color: #ff6b6b;
  cursor: pointer;
  font-size: 12px;
}
.exec-kill:hover { background: rgba(255,80,80,0.2); }
</style>
```

- [ ] **Step 2: Emit tool:executing / tool:executed from app.go**

Update `registerCmd` / `unregisterCmd` lambdas in the `AllContextual` call in `app.go`:

```go
func(id string, cancel func()) {
    a.runningCmds.Store(id, cancel)
    wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]interface{}{"id": id})
},
func(id string) {
    a.runningCmds.Delete(id)
    wailsruntime.EventsEmit(a.ctx, "tool:executed", map[string]interface{}{"id": id})
},
```

- [ ] **Step 3: Mount ExecutionProgress in ChatPanel.vue**

```js
import ExecutionProgress from './ExecutionProgress.vue'
```

Add to template in the message list area:
```html
<ExecutionProgress />
```

- [ ] **Step 4: Verify frontend builds**

```bash
cd frontend && yarn build 2>&1 | tail -5
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ExecutionProgress.vue frontend/src/components/ChatPanel.vue app.go
git commit -m "feat(frontend): add ExecutionProgress indicator with kill button"
```

---

## Task 11: Frontend — settings panel tool configuration

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

The settings window already has tabs. Add a "工具设置" section with:
1. File system path whitelist (add via directory picker, delete individual paths)
2. Shell timeout input
3. Code timeout input

- [ ] **Step 1: Read existing SettingsWindow.vue**

Read `frontend/src/components/SettingsWindow.vue` to understand the current tab structure and how config is loaded/saved.

- [ ] **Step 2: Add GetConfig / SaveConfig calls for new fields**

The existing settings window calls `GetConfig()` on load and `SaveConfig(cfg)` on save. The Go `Config` struct now includes `AllowedPaths`, `ShellTimeout`, `CodeTimeout` — these are already serialized by the existing Wails binding because the struct is JSON-marshalled.

Verify `GetConfig` returns these fields to the frontend (they should automatically appear since the Go struct now includes them).

- [ ] **Step 3: Add tool settings section to the template**

Find the appropriate tab or add a new "工具" tab. Add:

```html
<!-- Tool Settings Section -->
<div class="settings-section">
  <h3 class="section-title">文件系统访问白名单</h3>
  <p class="section-hint">留空则禁止所有文件操作</p>
  <div class="path-list">
    <div v-for="(p, i) in localCfg.AllowedPaths" :key="i" class="path-row">
      <span class="path-text">{{ p }}</span>
      <button class="btn-remove" @click="removePath(i)">删除</button>
    </div>
  </div>
  <button class="btn-add-path" @click="addPath">+ 添加路径</button>
</div>

<div class="settings-section">
  <h3 class="section-title">执行超时</h3>
  <div class="setting-row">
    <label>Shell 超时（秒）</label>
    <input type="number" v-model.number="localCfg.ShellTimeout" min="1" max="3600" class="num-input" />
  </div>
  <div class="setting-row">
    <label>代码执行超时（秒）</label>
    <input type="number" v-model.number="localCfg.CodeTimeout" min="1" max="3600" class="num-input" />
  </div>
</div>
```

- [ ] **Step 4: Add addPath / removePath methods**

```js
import { OpenDirectoryDialog } from '../../wailsjs/go/main/App'  // or use wailsruntime

async function addPath() {
  // Use Wails runtime dialog
  const { OpenDirectoryDialog } = await import('../../wailsjs/runtime/runtime')
  const selected = await OpenDirectoryDialog({})
  if (selected && !localCfg.value.AllowedPaths.includes(selected)) {
    localCfg.value.AllowedPaths.push(selected)
  }
}

function removePath(index) {
  localCfg.value.AllowedPaths.splice(index, 1)
}
```

Note: `OpenDirectoryDialog` is on `wailsruntime`, not a Go binding. Import from `../../wailsjs/runtime/runtime`.

- [ ] **Step 5: Verify frontend builds**

```bash
cd frontend && yarn build 2>&1 | tail -5
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(frontend): add tool settings section (path whitelist + timeouts)"
```

---

## Task 12: hitTest update — add modal and progress CSS classes

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: Find the hitTest selector string in macos.go**

Search for `.live2d-pet` in `macos.go` — this is the JS selector passed to the hitTest logic.

- [ ] **Step 2: Append new classes**

Find the line containing the selector string (it looks like `".live2d-pet,.chat-bubble,.settings-win,..."`). Append `.tool-confirm-modal,.execution-progress` to the end:

Before:
```
".live2d-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.lightbox"
```

After:
```
".live2d-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.lightbox,.tool-confirm-modal,.execution-progress"
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add macos.go
git commit -m "fix(macos): add tool-confirm-modal and execution-progress to hitTest selector"
```

---

## Task 13: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update current status section**

In the "当前状态" section of `CLAUDE.md`, add under the ✅ entries:

```
- ✅ 文件系统工具（`list_directory` / `read_file` / `write_file` / `delete_file` / `make_directory` / `move_file`，路径白名单访问控制）
- ✅ Shell 执行工具（`execute_shell`，eino interrupt 二次确认，用户可终止）
- ✅ 代码执行工具（`execute_code`，Python/Node/Ruby/Bash，eino interrupt 二次确认，用户可终止）
```

- [ ] **Step 2: Update 下阶段计划 section**

Remove the three tools from any pending items (they are now done).

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with execution tools status"
```

---

## Self-Review

**Spec coverage:**
- ✅ 6 file system tools with path whitelist — Tasks 2, 3
- ✅ Shell execution tool with per-call confirmation — Task 4
- ✅ Code execution tool with per-call confirmation — Task 5
- ✅ eino interrupt/resume integration — Tasks 4, 5, 7
- ✅ `AllowedPaths`, `ShellTimeout`, `CodeTimeout` in config — Task 1
- ✅ `ConfirmToolExecution` and `KillToolExecution` bindings — Task 8
- ✅ `ToolConfirmModal` frontend component — Task 9
- ✅ `ExecutionProgress` frontend component — Task 10
- ✅ Settings panel tool configuration — Task 11
- ✅ hitTest selector update — Task 12
- ✅ Registry registration — Task 6
- ✅ User can edit command/code before approving — Tasks 4, 5, 9
- ✅ User can kill running execution — Tasks 4, 5, 8, 10

**Checkpoint ID clarification:** The runner's `localbk` backend stores checkpoints in-memory keyed by the string we pass via `WithCheckPointID`. We use a per-chat UUID so each conversation turn gets its own checkpoint slot. When `Resume` is called, it loads from that same key. This works for the within-session flow.

**Type consistency check:**
- `ConfirmResult` defined in `shell_tools.go`, used in `shell.go`, `code.go`, `agent.go` — consistent
- `ShellConfirmInfo` / `CodeConfirmInfo` defined in `*_tools.go`, used in `*_.go` and `agent.go` — consistent
- `ToolConfirmResponse` defined in `agent.go`, used in `app.go` as `agent.ToolConfirmResponse` — consistent
- `pendingConfirms *sync.Map` on both `Agent` and `App` — different objects; `App.pendingConfirms` is the source of truth, passed by pointer to `Agent.New()` — consistent

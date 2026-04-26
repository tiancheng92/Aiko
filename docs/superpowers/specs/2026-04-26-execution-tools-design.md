# Execution Tools Design

## Overview

Add three categories of tools to Aiko:

1. **File system tools** — read/write/manage files within a user-defined path whitelist
2. **Shell execution tool** — run arbitrary shell commands with per-call user confirmation
3. **Code execution tool** — run Python/Node/Ruby/Bash scripts with per-call user confirmation

All execution tools use eino's built-in `tool.Interrupt` / `tool.GetResumeContext` mechanism for the confirmation flow.

---

## 1. File System Tools

### Tools (6)

| Tool | Parameters | Description |
|------|-----------|-------------|
| `list_directory` | `path` | List files and subdirectories at path |
| `read_file` | `path` | Read file contents as UTF-8 text |
| `write_file` | `path`, `content`, `append bool` | Write or append to file |
| `delete_file` | `path` | Delete a file |
| `make_directory` | `path` | Create directory (including parents) |
| `move_file` | `source`, `destination` | Move or rename a file/directory |

### Path Whitelist

- Stored as `AllowedPaths []string` in the `Settings` struct (config table)
- On each tool call, resolve target path(s) to absolute path via `filepath.Abs`, then verify with `strings.HasPrefix(absTarget, absAllowed+"/")` for each allowed path
- If `AllowedPaths` is empty, all file operations are rejected with a clear message
- `move_file` checks both `source` and `destination` against the whitelist
- No per-call confirmation dialog — whitelist is the sole access control mechanism

### File Structure

```
internal/tools/
  filesystem_tools.go      # Struct defs + Info() for all 6 tools; no build tag
  filesystem.go            # InvokableRun implementations (cross-platform)
```

---

## 2. Shell Execution Tool

### Tool (1)

| Tool | Parameters | Timeout | Confirmation |
|------|-----------|---------|-------------|
| `execute_shell` | `command string`, `working_dir string (optional)` | Configurable (default 30s) | ✅ per-call |

### Execution

- Uses `exec.CommandContext` with a timeout derived from `cfg.ShellTimeout`
- Captures combined stdout+stderr, returns as string
- On start, registers `cmd` in `app.go`'s `runningCmds sync.Map` (key = task UUID)
- On completion (success, error, or kill), removes entry from map
- `working_dir` defaults to `os.UserHomeDir()` if empty

### Confirmation Flow (eino interrupt)

1. Tool generates a UUID for this execution
2. Tool calls `tool.Interrupt(ctx, ShellConfirmInfo{ID: uuid, Command: command, WorkingDir: wd})`
3. `drainRunner` detects interrupt event, extracts `ShellConfirmInfo`, emits Wails event `tool:confirm`
4. Frontend shows `ToolConfirmModal`; user may edit the command before approving
5. User approves → frontend calls `ConfirmToolExecution(id, approved, editedCommand)`
6. Backend stores result in `pendingConfirms sync.Map`, calls `runner.Resume`
7. Tool re-enters via `tool.GetResumeContext`, reads `ConfirmResult{Approved, EditedCommand}`, proceeds or returns "用户已拒绝"

### File Structure

```
internal/tools/
  shell_tools.go           # Struct def + Info()
  shell.go                 # InvokableRun implementation
```

---

## 3. Code Execution Tool

### Tool (1)

| Tool | Parameters | Timeout | Confirmation |
|------|-----------|---------|-------------|
| `execute_code` | `language string`, `code string`, `working_dir string (optional)` | Configurable (default 60s) | ✅ per-call |

### Supported Languages

| `language` value | Interpreter |
|-----------------|------------|
| `python` | `python3` |
| `node` | `node` |
| `ruby` | `ruby` |
| `bash` | `bash` |

### Execution

- Code is written to a temp file (`os.CreateTemp`) with the appropriate extension
- Executed via `exec.CommandContext(ctx, interpreter, tempFile)`
- Temp file deleted after execution regardless of outcome
- Timeout derived from `cfg.CodeTimeout`
- Same `runningCmds` registration as shell tool (different UUID namespace)

### Confirmation Flow

Identical to shell tool. `CodeConfirmInfo` carries `{ID, Language, Code, WorkingDir}`. User may edit the code in the confirmation modal before approving.

### File Structure

```
internal/tools/
  code_tools.go            # Struct def + Info()
  code.go                  # InvokableRun implementation
```

---

## 4. eino Interrupt Integration

### drainRunner changes (`internal/agent/agent.go`)

```go
// Pseudo-code — drainRunner needs to handle interrupt events
if event.Type == adk.EventTypeInterrupt {
    info := event.InterruptInfo
    // emit to frontend
    runtime.EventsEmit(ctx, "tool:confirm", info)
    // block until ConfirmToolExecution is called
    result := <-pendingConfirms[info.ID]
    if result.Approved {
        runner.Resume(ctx, result.EditedContent)
    } else {
        runner.Cancel(ctx)
    }
}
```

`pendingConfirms` is a `sync.Map` on `App` struct, value type `chan ConfirmResult`.

### New App bindings (`app.go`)

```go
// ConfirmToolExecution is called by the frontend when the user approves or rejects a tool execution.
func (a *App) ConfirmToolExecution(id string, approved bool, editedContent string)

// KillToolExecution forcibly terminates a running shell or code execution by its task UUID.
func (a *App) KillToolExecution(id string)
```

---

## 5. Configuration Changes

### `Settings` struct additions (`internal/config/config.go`)

```go
AllowedPaths  []string `json:"allowed_paths"`   // file system whitelist
ShellTimeout  int      `json:"shell_timeout"`    // seconds, default 30
CodeTimeout   int      `json:"code_timeout"`     // seconds, default 60
```

### DB migration

```sql
ALTER TABLE settings ADD COLUMN allowed_paths TEXT NOT NULL DEFAULT '[]';
ALTER TABLE settings ADD COLUMN shell_timeout INTEGER NOT NULL DEFAULT 30;
ALTER TABLE settings ADD COLUMN code_timeout INTEGER NOT NULL DEFAULT 60;
```

---

## 6. Frontend UI

### Settings Panel — Tool Settings Tab

New section in the existing settings window:

- **File System Whitelist**: list of paths with add/delete controls; opens native directory picker via `OpenDirectoryDialog` Wails binding; empty list = all file ops rejected
- **Shell Timeout**: numeric input (seconds)
- **Code Timeout**: numeric input (seconds)

### ToolConfirmModal Component

Triggered by `tool:confirm` Wails event. Fields:

- Tool type badge (Shell / Python / Node / etc.)
- Working directory display
- Editable textarea for command or code (code version uses highlight.js for syntax highlighting)
- Risk warning text
- **拒绝** / **批准执行** buttons

On approve: calls `ConfirmToolExecution(id, true, editedContent)`
On reject: calls `ConfirmToolExecution(id, false, "")`

### Execution Progress Indicator

Rendered as a message bubble in `ChatPanel` while tool is running:

- Tool name + truncated command/code preview
- Timer counting up (updated every second via `setInterval`)
- Timeout bar (fills over configured timeout duration, turns red at limit)
- **终止** button → calls `KillToolExecution(id)`

---

## 7. Registry

### `internal/tools/registry.go`

File system tools added to `All()` (plain `Tool` interface, no confirmation needed):
```go
&ListDirectoryTool{},
&ReadFileTool{},
&WriteFileTool{},
&DeleteFileTool{},
&MakeDirectoryTool{},
&MoveFileTool{},
```

Shell and code tools require runtime config injection, added to `AllContextual()`:
```go
&ExecuteShellTool{Cfg: cfg},
&ExecuteCodeTool{Cfg: cfg},
```

`app.go` passes `cfg` to `AllContextual` (already passes other deps).

---

## 8. hitTest Update

`.tool-confirm-modal` and `.execution-progress` CSS classes must be added to the hitTest selector list in `macos.go` so mouse events are not passed through to the desktop while these overlays are visible.

---

## Non-Goals

- Docker/sandbox isolation (system interpreters only, as decided)
- Network sandboxing for code execution
- Windows/Linux support (can be added later without changing the interface)
- File search / grep tool (can be added as a separate task)

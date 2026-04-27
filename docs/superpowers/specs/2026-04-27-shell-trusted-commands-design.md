# Shell Trusted Commands Whitelist — Design Spec

**Date:** 2026-04-27  
**Status:** Approved

## Overview

Add a whitelist of trusted command prefixes to `ExecuteShellTool`. Commands whose first token matches a whitelist entry are executed immediately without the eino interrupt/confirm flow. The whitelist is persisted in SQLite via the existing `Config` mechanism and editable from the Settings UI.

## Config Layer

**New field** in `internal/config/config.go`:
```go
ShellTrustedCommands []string // command prefixes that bypass confirmation
```

**Settings key:** `shell_trusted_commands`  
**Serialization:** newline-separated string (same pattern as `AllowedPaths` and `SkillsDirs`)

Changes required in `config.go`:
- Add `ShellTrustedCommands []string` to the `Config` struct
- In `Load()`: `cfg.ShellTrustedCommands = splitLines(m["shell_trusted_commands"])`
- In `Save()`: `"shell_trusted_commands": joinLines(cfg.ShellTrustedCommands)`

## Logic Layer

**New helper** `isTrustedCommand(command string, trusted []string) bool` in `shell.go`:
- Trims leading whitespace from `command`
- For each entry in `trusted`, checks:
  - `command == entry` (exact match), OR
  - `strings.HasPrefix(command, entry+" ")` (prefix + space, prevents `gitk` matching `git`)
- Returns `true` if any entry matches

**Modified `InvokableRun`** in `shell.go`:
- After parsing `command` and `workingDir`, before the resume-check block, insert:
  ```go
  if isTrustedCommand(command, t.Cfg.ShellTrustedCommands) {
      return runShellCommand(ctx, command, workingDir, t.Cfg.ShellTimeout, t.RegisterCmd, t.UnregisterCmd)
  }
  ```
- Trusted commands bypass interrupt entirely — no confirmation modal, no resume cycle.

## Frontend Layer

**File:** `frontend/src/components/SettingsWindow.vue`

In the Shell settings section (near `ShellTimeout`), add a list editor for `ShellTrustedCommands`:
- Same UX pattern as the `AllowedPaths` list: text input + "Add" button, each entry shows a delete button
- Placeholder: `git`
- Label: "免确认命令白名单"
- Helper text: "以这些命令名开头的 Shell 命令将直接执行，无需二次确认"

Script additions (mirroring `addPath` / `removePath`):
```js
function addTrustedCommand() { /* trim + dedup + push to cfg.ShellTrustedCommands */ }
function removeTrustedCommand(index) { cfg.value.ShellTrustedCommands.splice(index, 1) }
```

## Data Flow

```
Settings UI → cfg.ShellTrustedCommands → SaveConfig() → SQLite
SQLite → Config.Load() → Config.ShellTrustedCommands → ExecuteShellTool.Cfg
Agent calls execute_shell → isTrustedCommand() → direct runShellCommand (trusted)
                                               → interrupt → user confirms (untrusted)
```

## Error Handling

- Empty/nil `ShellTrustedCommands` → no change in behavior (all commands require confirmation)
- Whitespace-only entries are ignored by `splitLines` (existing behavior)
- `isTrustedCommand` is case-sensitive (shell commands are case-sensitive on macOS/Linux)

## Testing

- `isTrustedCommand("git status", ["git"])` → true
- `isTrustedCommand("gitk", ["git"])` → false (no space boundary)
- `isTrustedCommand("git", ["git"])` → true (exact match)
- `isTrustedCommand("ls -la", ["ls", "cat"])` → true
- `isTrustedCommand("rm -rf /", [])` → false (empty whitelist)

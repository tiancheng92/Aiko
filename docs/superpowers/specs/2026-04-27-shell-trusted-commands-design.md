# Shell 免确认命令白名单 — 设计文档

**日期：** 2026-04-27  
**状态：** 已批准

## 概述

为 `ExecuteShellTool` 添加一个可信命令前缀白名单。命令的首个 token 若与白名单中的某个条目匹配，则直接执行，跳过 eino interrupt/confirm 流程。白名单通过现有 `Config` 机制持久化到 SQLite，并可在设置界面中编辑。

## Config 层

**新增字段**（`internal/config/config.go`）：
```go
ShellTrustedCommands []string // 免确认的命令前缀列表
```

**Settings key：** `shell_trusted_commands`  
**序列化方式：** 换行分隔字符串（与 `AllowedPaths`、`SkillsDirs` 保持一致）

`config.go` 改动：
- 在 `Config` 结构体中新增 `ShellTrustedCommands []string`
- `Load()` 中：`cfg.ShellTrustedCommands = splitLines(m["shell_trusted_commands"])`
- `Save()` 中：`"shell_trusted_commands": joinLines(cfg.ShellTrustedCommands)`

## 逻辑层

**新增辅助函数** `isTrustedCommand(command string, trusted []string) bool`（`shell.go`）：
- 去除 `command` 首部空白
- 遍历 `trusted` 中每个条目，判断：
  - `command == entry`（完全匹配），或
  - `strings.HasPrefix(command, entry+" ")`（前缀 + 空格，防止 `gitk` 误匹配 `git`）
- 任一条目匹配则返回 `true`

**修改 `InvokableRun`**（`shell.go`）：
- 解析 `command` 和 `workingDir` 之后、resume 检查之前，插入：
  ```go
  if isTrustedCommand(command, t.Cfg.ShellTrustedCommands) {
      return runShellCommand(ctx, command, workingDir, t.Cfg.ShellTimeout, t.RegisterCmd, t.UnregisterCmd)
  }
  ```
- 白名单命令完全跳过 interrupt，不弹确认框，也不进入 resume 流程。

## 前端层

**文件：** `frontend/src/components/SettingsWindow.vue`

在 Shell 设置区域（`ShellTimeout` 附近）新增 `ShellTrustedCommands` 列表编辑组件：
- 与 `AllowedPaths` 列表交互方式相同：文本输入框 + "添加" 按钮，每条记录显示删除按钮
- 输入框占位符：`git`
- 标签：免确认命令白名单
- 说明文字：以这些命令名开头的 Shell 命令将直接执行，无需二次确认

脚本新增（参考 `addPath` / `removePath`）：
```js
function addTrustedCommand() { /* trim + 去重 + push 到 cfg.ShellTrustedCommands */ }
function removeTrustedCommand(index) { cfg.value.ShellTrustedCommands.splice(index, 1) }
```

## 数据流

```
设置界面 → cfg.ShellTrustedCommands → SaveConfig() → SQLite
SQLite → Config.Load() → Config.ShellTrustedCommands → ExecuteShellTool.Cfg
Agent 调用 execute_shell → isTrustedCommand() → 直接执行（白名单命令）
                                               → interrupt → 用户确认（非白名单命令）
```

## 错误处理

- `ShellTrustedCommands` 为空/nil → 行为与现在完全一致（所有命令均需确认）
- 纯空白条目由 `splitLines` 忽略（现有行为）
- `isTrustedCommand` 大小写敏感（macOS/Linux shell 命令区分大小写）

## 测试用例

- `isTrustedCommand("git status", ["git"])` → true
- `isTrustedCommand("gitk", ["git"])` → false（无空格边界）
- `isTrustedCommand("git", ["git"])` → true（完全匹配）
- `isTrustedCommand("ls -la", ["ls", "cat"])` → true
- `isTrustedCommand("rm -rf /", [])` → false（空白名单）

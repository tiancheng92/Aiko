# execenv：统一子进程 PATH / 环境处理

**日期**：2026-05-06
**状态**：设计已批准，待实施
**涉及范围**：后端 Go，无 schema / 前端变更

## 背景

macOS 上，Aiko 以 `.app` 包被 Finder/Dock 启动时，launchd 给进程的 PATH 是 `/usr/bin:/bin:/usr/sbin:/sbin`，不包含 Homebrew、nvm、asdf、npm globals 等用户自装工具目录。

子进程需要用户工具时（例如 `npx` 的 shebang `#!/usr/bin/env node` 再找 `node`），这个最小 PATH 会导致进程立即退出，现象是 MCP stdio 报 `transport error: transport closed`、shell/code 工具报 `command not found`、lark-cli 找不到。

代码库里已有**两处独立实现**的 PATH 修补：
- `internal/lark/client.go` 有 `augmentedPATH()` + `candidateDirs`（已存在）。
- `internal/mcp/client.go` 有 `buildStdioEnv()`（本次迭代前刚加）。

还有**三处**没修：
- `internal/tools/code.go` 的代码执行工具（python / node / ruby PATH 查找）
- `internal/tools/shell.go` 的 shell 执行工具（bash 继承最小 PATH，用户命令找不到 `npx`/`gh` 等）
- `app.go` 的 Kokoro TTS 安装流程（`python3`、`pip` PATH 查找）

本次改动抽一个共享包 `internal/execenv`，消除重复，并**升级能力**：除了硬编码常见目录，还会尝试启动用户的登录 shell 读取其真实 env，覆盖 nvm、asdf、用户自定义 `.zshrc` 设置的场景。

## 非目标

- 不实现 Windows 的 PATH 修补（项目只在 macOS 运行）
- 不给 MCP 加 per-server env 配置（独立特性）
- 不改任何 osascript / codesign 等系统命令调用点（它们用的都是 `/usr/bin` 下的系统工具，launchd minimal PATH 就能找到）
- 不修 playwright 那个 npm-on-Node25 解析 bug（非本项目代码问题）

## 设计

### 包布局

```
internal/execenv/
├── env.go           # 公开 API + 纯函数逻辑
├── shell_darwin.go  # macOS: exec $SHELL -ilc env
├── shell_other.go   # 其他平台: stub
└── env_test.go      # 只测纯函数
```

### 公开 API

```go
// AugmentedPATH returns a PATH value suitable for subprocesses launched
// from a macOS .app bundle. Sources, in descending priority:
//   1. The user's login-shell PATH (cached; probed once on first call)
//   2. Hardcoded candidateDirs (Homebrew, npm globals, pipx, cargo, …)
//   3. The current process PATH
// Entries are deduplicated; first occurrence wins.
func AugmentedPATH() string

// AugmentedEnv returns an environment slice suitable for cmd.Env. It is
// os.Environ() merged with the login-shell environment (if probed), with
// PATH replaced by AugmentedPATH(). os.Environ() entries take precedence
// over shell-env entries — launchd-provided vars like HOME remain authoritative.
func AugmentedEnv() []string

// LookPath searches AugmentedPATH() for name and returns the absolute
// path if found and executable. Returns "" on miss. Does not mutate os.Environ.
func LookPath(name string) string
```

### 内部结构

```go
// candidateDirs are user-space bin directories macOS launchd omits.
var candidateDirs = []string{
    "/opt/homebrew/bin",
    "/opt/homebrew/sbin",
    "/usr/local/bin",
    "/usr/local/sbin",
}

// homeCandidateDirs returns $HOME-based dirs used by npm/yarn/pipx/cargo.
// Computed lazily so tests can control HOME via t.Setenv.
func homeCandidateDirs() []string

// loadShellEnv (darwin) runs $SHELL -ilc env with a 3s timeout and
// returns the parsed env. Returns nil on any failure (silent fallback).
// On non-darwin, returns nil.
func loadShellEnv() map[string]string

// parseEnvOutput parses the output of `env` into KEY=VALUE pairs.
// Lines without `=` are skipped. Multi-line values are not supported
// (env(1) doesn't emit them without -0).
func parseEnvOutput(b []byte) map[string]string

// mergePaths returns a `:`-joined PATH of the inputs concatenated in order,
// with empty entries and duplicates removed (first occurrence wins).
func mergePaths(sources ...string) string
```

### 缓存

```go
var (
    shellEnvOnce sync.Once
    shellEnv     map[string]string // guarded by Once; nil on any failure
)

// getShellEnv returns the cached login-shell env. The first call runs
// loadShellEnv once; all subsequent calls return the same result.
// Failure results (nil) are also cached — a broken .zshrc doesn't cause
// repeated 3-second probes.
func getShellEnv() map[string]string {
    shellEnvOnce.Do(func() { shellEnv = loadShellEnv() })
    return shellEnv
}
```

`shell_darwin.go` 的 `loadShellEnv`：

```go
func loadShellEnv() map[string]string {
    shell := os.Getenv("SHELL")
    if shell == "" {
        return nil
    }
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    // -l (login) 加载 .zprofile / .bash_profile
    // -i (interactive) 加载 .zshrc / .bashrc —— nvm/asdf 的 shim 常在这里
    out, err := exec.CommandContext(ctx, shell, "-ilc", "env").Output()
    if err != nil {
        return nil
    }
    return parseEnvOutput(out)
}
```

`shell_other.go`：

```go
//go:build !darwin

package execenv

func loadShellEnv() map[string]string { return nil }
```

### AugmentedPATH 实现骨架

```go
func AugmentedPATH() string {
    var shellPath string
    if env := getShellEnv(); env != nil {
        shellPath = env["PATH"]
    }
    home := homeCandidateDirs() // []string
    return mergePaths(
        shellPath,
        strings.Join(candidateDirs, ":"),
        strings.Join(home, ":"),
        os.Getenv("PATH"),
    )
}
```

### AugmentedEnv 实现骨架

```go
func AugmentedEnv() []string {
    base := map[string]string{}
    // 先铺 shell-env（低优先级）
    for k, v := range getShellEnv() {
        base[k] = v
    }
    // os.Environ() 覆盖（高优先级 —— launchd 传的变量权威）
    for _, kv := range os.Environ() {
        if i := strings.IndexByte(kv, '='); i > 0 {
            base[kv[:i]] = kv[i+1:]
        }
    }
    // PATH 强制用合并结果
    base["PATH"] = AugmentedPATH()

    out := make([]string, 0, len(base))
    for k, v := range base {
        out = append(out, k+"="+v)
    }
    return out
}
```

## 调用点替换

| 位置 | 现状 | 改为 |
|---|---|---|
| `internal/lark/client.go:36` | `"PATH="+augmentedPATH()` 拼接 | `cmd.Env = execenv.AugmentedEnv()` |
| `internal/lark/client.go:60-67` | 本地 `candidateDirs` | 删除 |
| `internal/lark/client.go:69-90` | 本地 `augmentedPATH()` | 删除 |
| `internal/lark/client.go:FindCLI` | 手搓目录遍历 | `execenv.LookPath("lark-cli")` |
| `internal/mcp/client.go:134` | `buildStdioEnv(cfg.Command, os.Environ())` | `execenv.AugmentedEnv()` |
| `internal/mcp/client.go` | `buildStdioEnv`、`commonBinDirs` | 删除 |
| `internal/mcp/client_test.go` | 测 `buildStdioEnv` 的 4 条 case | 删除（测试迁到 execenv 包） |
| `internal/tools/code.go:113` | 无 env 设置 | 新增 `cmd.Env = execenv.AugmentedEnv()` |
| `internal/tools/shell.go:94` | 无 env 设置 | 新增 `cmd.Env = execenv.AugmentedEnv()` |
| `app.go:2150` | `exec.Command("python3", ...)` | 改 `exec.CommandContext`，设 env |
| `app.go:2210-2218` `run()` helper | 无 env | 新增 `cmd.Env = execenv.AugmentedEnv()` |

## 测试

`internal/execenv/env_test.go`（纯函数，不起子进程）：

- `TestMergePathsOrder` — 多源拼接按顺序
- `TestMergePathsDeduplicates` — 重复目录只出现一次，保首次位置
- `TestMergePathsSkipsEmpty` — 空字符串 / 空 source 跳过
- `TestParseEnvOutputBasic` — `KEY=VALUE` 解析
- `TestParseEnvOutputSkipsInvalidLines` — 无 `=` 的行跳过
- `TestParseEnvOutputHandlesValuesWithEquals` — `FOO=a=b=c` 被解析为 `FOO` / `a=b=c`
- `TestHomeCandidateDirsRespectsHOME` — `t.Setenv("HOME", tmp)` 后返回的路径以 tmp 为前缀
- `TestHomeCandidateDirsEmptyHOME` — HOME 空时返回 nil

**不测**：`loadShellEnv` 的真实 exec（行为依赖开发机的 shell 配置，CI 不稳定）。

## 验证清单（实施阶段用）

- [ ] `go test ./internal/execenv/` 全绿
- [ ] `go test ./...` 保持全绿（无回归）
- [ ] `go build ./...` 通过
- [ ] E2E 手动复现：`PATH=/usr/bin:/bin:/usr/sbin:/sbin` 下运行一个小 Go 程序调 `execenv.AugmentedEnv()` 再启 `howtocook-mcp`，`initialize` 成功
- [ ] 用 `make run` 打包装到 `/Applications/Aiko.app`，从 Finder 启动后 howtocook MCP 不再报 `transport closed`

## 失败与回退

每一层都有静默回退：

| 失败 | 降级效果 |
|---|---|
| `$SHELL` 未设置 | 仅 candidateDirs + os.Environ 的 PATH |
| 登录 shell 超时 / 非零退出 / 输出异常 | 同上 |
| 非 darwin 平台 | `loadShellEnv` 直接返回 nil，同上 |
| candidateDirs 里目录不存在 | Go 的 exec.LookPath 会跳过，无副作用 |

## 风险与取舍

- **shell-env 一次性探测**：用户中途改 `.zshrc` 后需要重启 Aiko 才生效。合理 —— Electron 阵营的 `fix-path` 也是同样语义。
- **interactive shell (-i) 可能加载非预期逻辑**：例如 `.zshrc` 里的 `autoload -Uz compinit` 在某些配置下慢。3 秒超时是兜底。不想要 interactive 就手动 unset SHELL 或用 `bash -l` 作为 SHELL。
- **shell-env 里的变量覆盖 os.Environ**：**没有**覆盖 —— `AugmentedEnv` 设计里 os.Environ 最后写入 map，覆盖 shell-env。原因：`HOME`、`USER`、`TMPDIR` 这些由 launchd 或 Wails runtime 传的是权威值，用户 `.zshrc` 里要是 export 了错的会出问题。

# execenv: 统一子进程 PATH / 环境处理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 抽出共享 `internal/execenv` 包，统一项目中 5 处子进程 spawn 点的 PATH / env 处理；macOS 下额外探测用户登录 shell env，兼容 nvm/asdf/自定义 `.zshrc`。

**Architecture:** 新建 `internal/execenv` 包，对外暴露 `AugmentedPATH()` / `AugmentedEnv()` / `LookPath()`。内部用 `sync.Once` 缓存 `$SHELL -ilc env` 的探测结果（3s 超时，静默回落）。调用点（lark、mcp、tools/code、tools/shell、app.go kokoro 安装）改用该包；删除 `lark.augmentedPATH` 与 `mcp.buildStdioEnv` 两份重复实现。

**Tech Stack:** Go 1.21+，`os/exec`、`sync.Once`、`context.WithTimeout`；平台拆分用 `//go:build darwin` / `//go:build !darwin`。

**Spec:** `docs/superpowers/specs/2026-05-06-execenv-path-design.md`

---

## File Structure

### 新增

| 文件 | 职责 |
|---|---|
| `internal/execenv/env.go` | 公开 API（`AugmentedPATH`、`AugmentedEnv`、`LookPath`）+ 纯函数（`mergePaths`、`parseEnvOutput`、`homeCandidateDirs`）+ `sync.Once` 缓存 |
| `internal/execenv/shell_darwin.go` | macOS 专属：`loadShellEnv()` 跑 `$SHELL -ilc env` |
| `internal/execenv/shell_other.go` | 非 darwin stub：`loadShellEnv` 返回 nil |
| `internal/execenv/env_test.go` | 单元测纯函数，不起子进程 |

### 修改

| 文件 | 改动 |
|---|---|
| `internal/lark/client.go` | 删除 `candidateDirs`、`augmentedPATH`；`Run` 用 `execenv.AugmentedEnv()`；`FindCLI` 改用 `execenv.LookPath` |
| `internal/mcp/client.go` | 删除 `commonBinDirs`、`buildStdioEnv`；`connectAndDiscover` 改用 `execenv.AugmentedEnv()` |
| `internal/mcp/client_test.go` | 删除 `TestBuildStdioEnv*` 四条 case（逻辑已迁到 execenv 测试） |
| `internal/tools/shell.go` | 为 `runShellCommand` 内的 `exec.CommandContext` 设 `cmd.Env = execenv.AugmentedEnv()` |
| `internal/tools/code.go` | 为 `runCodeExecution` 内的 `exec.CommandContext` 设 `cmd.Env = execenv.AugmentedEnv()` |
| `app.go` | Kokoro 安装：`run()` helper 设 `cmd.Env = execenv.AugmentedEnv()`；预检测 `python3` 的 `exec.Command` 同步改造 |

---

## Task 1: 创建 execenv 包骨架与纯函数测试

**Files:**
- Create: `internal/execenv/env.go`
- Create: `internal/execenv/env_test.go`

- [ ] **Step 1.1: Write the failing test file**

Create `internal/execenv/env_test.go`:

```go
package execenv

import (
	"strings"
	"testing"
)

// TestMergePathsOrder ensures sources are concatenated in input order.
func TestMergePathsOrder(t *testing.T) {
	got := mergePaths("/a:/b", "/c:/d", "/e")
	want := "/a:/b:/c:/d:/e"
	if got != want {
		t.Errorf("mergePaths = %q, want %q", got, want)
	}
}

// TestMergePathsDeduplicates ensures duplicate dirs keep their first position.
func TestMergePathsDeduplicates(t *testing.T) {
	got := mergePaths("/a:/b:/c", "/b:/d", "/a:/e")
	want := "/a:/b:/c:/d:/e"
	if got != want {
		t.Errorf("mergePaths = %q, want %q", got, want)
	}
}

// TestMergePathsSkipsEmpty ensures empty sources and empty entries are dropped.
func TestMergePathsSkipsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		want    string
	}{
		{"empty source", []string{"", "/a:/b"}, "/a:/b"},
		{"all empty", []string{"", "", ""}, ""},
		{"empty entry in middle", []string{"/a::/b"}, "/a:/b"},
		{"trailing colon", []string{"/a:/b:"}, "/a:/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mergePaths(c.sources...); got != c.want {
				t.Errorf("mergePaths(%v) = %q, want %q", c.sources, got, c.want)
			}
		})
	}
}

// TestParseEnvOutputBasic parses plain KEY=VALUE lines.
func TestParseEnvOutputBasic(t *testing.T) {
	in := []byte("FOO=bar\nBAZ=qux\n")
	got := parseEnvOutput(in)
	if got["FOO"] != "bar" || got["BAZ"] != "qux" {
		t.Errorf("parseEnvOutput = %v", got)
	}
}

// TestParseEnvOutputSkipsInvalidLines drops lines without '='.
func TestParseEnvOutputSkipsInvalidLines(t *testing.T) {
	in := []byte("FOO=bar\ngarbage line\nBAZ=qux\n\n")
	got := parseEnvOutput(in)
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(got), got)
	}
}

// TestParseEnvOutputHandlesValuesWithEquals preserves '=' within values.
func TestParseEnvOutputHandlesValuesWithEquals(t *testing.T) {
	in := []byte("FOO=a=b=c\n")
	got := parseEnvOutput(in)
	if got["FOO"] != "a=b=c" {
		t.Errorf("parseEnvOutput[FOO] = %q, want %q", got["FOO"], "a=b=c")
	}
}

// TestHomeCandidateDirsRespectsHOME returns $HOME-based paths.
func TestHomeCandidateDirsRespectsHOME(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got := homeCandidateDirs()
	if len(got) == 0 {
		t.Fatalf("expected at least one dir, got none")
	}
	for _, dir := range got {
		if !strings.HasPrefix(dir, tmp) {
			t.Errorf("dir %q does not start with HOME %q", dir, tmp)
		}
	}
}

// TestHomeCandidateDirsEmptyHOME returns nil when HOME is unset.
func TestHomeCandidateDirsEmptyHOME(t *testing.T) {
	t.Setenv("HOME", "")
	got := homeCandidateDirs()
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
```

- [ ] **Step 1.2: Run tests to verify they fail (compile error — functions undefined)**

Run: `go test ./internal/execenv/ 2>&1 | head`
Expected: build failure listing `mergePaths`, `parseEnvOutput`, `homeCandidateDirs` as undefined.

- [ ] **Step 1.3: Implement `internal/execenv/env.go`**

Create `internal/execenv/env.go`:

```go
// Package execenv provides a consistent environment for subprocesses launched
// by Aiko, regardless of whether the parent was started from a terminal (full
// PATH) or from Finder/Dock via launchd (minimal PATH: /usr/bin:/bin:/usr/sbin:/sbin).
//
// macOS .app bundles receive a minimal PATH from launchd that excludes
// Homebrew, nvm, asdf, npm globals, etc. This causes subprocess shebangs
// like `#!/usr/bin/env node` to fail with "node: No such file or directory".
// This package rebuilds a usable PATH from three sources:
//
//   1. User's login-shell PATH (probed once via `$SHELL -ilc env`, cached).
//   2. Hardcoded candidateDirs (Homebrew, /usr/local, etc.).
//   3. The current process PATH.
//
// All failures (shell probe timeout, missing $SHELL, non-darwin platform)
// degrade silently to the remaining sources.
package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// candidateDirs are user-space bin directories macOS launchd omits.
var candidateDirs = []string{
	"/opt/homebrew/bin",
	"/opt/homebrew/sbin",
	"/usr/local/bin",
	"/usr/local/sbin",
}

// homeCandidateDirs returns $HOME-based dirs used by npm/yarn/pipx/cargo.
// Returns nil when $HOME is unset.
func homeCandidateDirs() []string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".local/bin"),
		filepath.Join(home, ".local/share/npm/bin"),
		filepath.Join(home, ".npm-global/bin"),
		filepath.Join(home, ".yarn/bin"),
		filepath.Join(home, ".cargo/bin"),
		filepath.Join(home, "node_modules/.bin"),
	}
}

// parseEnvOutput parses the output of env(1) into KEY=VALUE pairs.
// Lines without '=' are skipped. Values may contain further '='.
func parseEnvOutput(b []byte) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		out[line[:i]] = line[i+1:]
	}
	return out
}

// mergePaths joins sources with ':' and deduplicates entries, preserving
// first occurrence. Empty entries and empty sources are dropped.
func mergePaths(sources ...string) string {
	seen := make(map[string]struct{})
	var out []string
	for _, src := range sources {
		if src == "" {
			continue
		}
		for _, entry := range strings.Split(src, ":") {
			if entry == "" {
				continue
			}
			if _, ok := seen[entry]; ok {
				continue
			}
			seen[entry] = struct{}{}
			out = append(out, entry)
		}
	}
	return strings.Join(out, ":")
}

var (
	shellEnvOnce sync.Once
	shellEnv     map[string]string // guarded by Once; nil on any failure
)

// getShellEnv returns the cached login-shell env. First call runs
// loadShellEnv (platform-specific) once; subsequent calls return the same
// result. Failure is also cached — a broken .zshrc doesn't cause repeated
// 3-second probes.
func getShellEnv() map[string]string {
	shellEnvOnce.Do(func() { shellEnv = loadShellEnv() })
	return shellEnv
}

// AugmentedPATH returns a PATH value for subprocesses. Sources in priority:
//   1. Login-shell PATH (if probed successfully).
//   2. candidateDirs + homeCandidateDirs.
//   3. os.Getenv("PATH").
// Deduplicated, first occurrence wins.
func AugmentedPATH() string {
	var shellPath string
	if env := getShellEnv(); env != nil {
		shellPath = env["PATH"]
	}
	return mergePaths(
		shellPath,
		strings.Join(candidateDirs, ":"),
		strings.Join(homeCandidateDirs(), ":"),
		os.Getenv("PATH"),
	)
}

// AugmentedEnv returns an environment slice for cmd.Env. It merges
// os.Environ() over the login-shell env (os.Environ wins — HOME and TMPDIR
// from launchd/Wails are authoritative), then forces PATH = AugmentedPATH().
func AugmentedEnv() []string {
	base := make(map[string]string)
	for k, v := range getShellEnv() {
		base[k] = v
	}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			base[kv[:i]] = kv[i+1:]
		}
	}
	base["PATH"] = AugmentedPATH()

	out := make([]string, 0, len(base))
	for k, v := range base {
		out = append(out, k+"="+v)
	}
	return out
}

// LookPath searches AugmentedPATH() for name and returns the absolute path
// if found and executable. Returns "" on miss. Unlike exec.LookPath, it
// does not rely on the current process PATH.
func LookPath(name string) string {
	// Absolute/relative path — fall through to exec.LookPath behavior.
	if strings.ContainsRune(name, '/') {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return ""
	}
	for _, dir := range strings.Split(AugmentedPATH(), ":") {
		if dir == "" {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return full
		}
	}
	return ""
}

// Ensure exec package stays imported even if LookPath removes its last use
// in future refactors (keeps error-free compile if called sites change).
var _ = exec.LookPath
```

- [ ] **Step 1.4: Create platform stubs**

Create `internal/execenv/shell_other.go`:

```go
//go:build !darwin

package execenv

// loadShellEnv is a no-op on non-darwin platforms. Windows / Linux don't
// have the macOS launchd-minimal-PATH problem that motivates this package.
func loadShellEnv() map[string]string { return nil }
```

Create `internal/execenv/shell_darwin.go`:

```go
//go:build darwin

package execenv

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// loadShellEnv probes the user's login shell to capture their real environment
// (PATH, NVM_DIR, GOPATH, etc.) so subprocesses launched from an .app bundle
// inherit the same setup as a terminal session.
//
// Uses `$SHELL -ilc env` — both login (-l) and interactive (-i), because
// nvm/asdf shims are typically registered in .zshrc/.bashrc (interactive)
// rather than .zprofile (login).
//
// 3-second timeout: guards against a broken .zshrc that runs `read` or makes
// network calls. Returns nil on any failure — callers fall back to hardcoded
// candidateDirs.
func loadShellEnv() map[string]string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", "env").Output()
	if err != nil {
		return nil
	}
	return parseEnvOutput(out)
}
```

- [ ] **Step 1.5: Run tests to verify green**

Run: `go test ./internal/execenv/ -v 2>&1 | tail -20`
Expected: all tests PASS; count ≥ 7 passing cases.

- [ ] **Step 1.6: Run full build to ensure no regressions**

Run: `go build ./...`
Expected: exit 0, no output.

- [ ] **Step 1.7: Commit**

```bash
git add internal/execenv/
git commit -m "$(cat <<'EOF'
feat(execenv): add shared subprocess PATH/env package

Creates internal/execenv with AugmentedPATH, AugmentedEnv, and LookPath.
macOS builds probe the user's login shell ($SHELL -ilc env) once with a
3s timeout to capture nvm/asdf/custom .zshrc PATHs; other sources fall
back to hardcoded candidate dirs. All failures degrade silently.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: 迁移 lark 到 execenv

**Files:**
- Modify: `internal/lark/client.go`

- [ ] **Step 2.1: 读取当前文件定位要删的块**

Run: `sed -n '1,120p' internal/lark/client.go`
Expected: 输出包含 `candidateDirs`（行 60-67）、`augmentedPATH`（行 69-90）、`FindCLI`（行 97+）。

- [ ] **Step 2.2: 替换 client.go 的 import、Run、candidateDirs、augmentedPATH、FindCLI**

`internal/lark/client.go` 的关键改动：

1. imports 追加：`"aiko/internal/execenv"`
2. `Run()` 中 `cmd.Env = append(os.Environ(), "PATH="+augmentedPATH())` 改为 `cmd.Env = execenv.AugmentedEnv()`
3. 删除 `candidateDirs` 变量（行 60-67）
4. 删除 `augmentedPATH()` 函数（行 69-90）
5. `FindCLI()` 全函数改为：

```go
// FindCLI returns the absolute path of lark-cli, or an empty string if not
// found. It searches the augmented PATH (login shell + candidate dirs +
// current PATH) so .app bundles launched from Finder can still locate it.
func FindCLI() string {
	return execenv.LookPath("lark-cli")
}
```

使用 Edit：

Edit 1 — 改 Run 的 cmd.Env 设置（第 36 行附近）：

```
old_string: cmd.Env = append(os.Environ(), "PATH="+augmentedPATH())
new_string: cmd.Env = execenv.AugmentedEnv()
```

Edit 2 — 把 `candidateDirs` 和 `augmentedPATH` 一并删除，并把 `FindCLI` 简化。先 Read 文件当前状态，再用一个 Edit 把 58–120 行的整块替换为新的 `FindCLI`。

Edit 3 — import 调整：删除不再使用的 `"os"`（如果仅剩 os.Environ 的调用被移除）、`"path/filepath"`、`"strings"`（同上），加入 `"aiko/internal/execenv"`。

**注意：** 具体的 import 增删列表取决于该文件里其它函数是否还在用这些包。实施时先 Read 整个文件确认，再做最终 Edit。

- [ ] **Step 2.3: 验证编译 + lark 包测试**

Run: `go build ./internal/lark/... && go test ./internal/lark/... 2>&1 | tail`
Expected: build 成功；若 lark 包无测试，输出 `no test files`（通过）。

- [ ] **Step 2.4: 全项目 build 验证无连锁破坏**

Run: `go build ./...`
Expected: exit 0。

- [ ] **Step 2.5: Commit**

```bash
git add internal/lark/client.go
git commit -m "$(cat <<'EOF'
refactor(lark): use execenv for subprocess PATH

Drops local candidateDirs and augmentedPATH helpers; Run() now uses
execenv.AugmentedEnv(), FindCLI uses execenv.LookPath. Behavior is
strictly broader (adds login-shell PATH source) while preserving
existing candidate dirs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: 迁移 mcp 到 execenv

**Files:**
- Modify: `internal/mcp/client.go`
- Modify: `internal/mcp/client_test.go`

- [ ] **Step 3.1: 删除 mcp/client.go 里的 buildStdioEnv 与 commonBinDirs**

Edit 1 — 删除 `commonBinDirs` 变量与 `buildStdioEnv` 整个函数（上一轮添加的约 65 行代码块）。

Edit 2 — `import` 段里删除 `"os"`（如无其他使用点）、`"path/filepath"`、加上 `"aiko/internal/execenv"`。

Edit 3 — 替换 `connectAndDiscover` 内 stdio 分支：

```
old_string:
		args := append([]string{cfg.Command}, cfg.Args...)
		env := buildStdioEnv(cfg.Command, os.Environ())
		client, err = mcpgo.NewStdioMCPClient(args[0], env, args[1:]...)

new_string:
		args := append([]string{cfg.Command}, cfg.Args...)
		client, err = mcpgo.NewStdioMCPClient(args[0], execenv.AugmentedEnv(), args[1:]...)
```

- [ ] **Step 3.2: 删除已失效的 mcp 测试**

完整删除 `internal/mcp/client_test.go`：

Run: `rm internal/mcp/client_test.go`
Expected: 文件被删除。

理由：它的四条 case（`TestBuildStdioEnvPrependsCommandDir`、`TestBuildStdioEnvAddsCommonBinDirs`、`TestBuildStdioEnvNoDuplicates`、`TestBuildStdioEnvEmptyCommand`）测试的函数已删除；等价逻辑已在 `internal/execenv/env_test.go` 的 `TestMergePaths*` 覆盖。

- [ ] **Step 3.3: 编译 + mcp 包测试**

Run: `go build ./internal/mcp/... && go test ./internal/mcp/... 2>&1 | tail`
Expected: build 成功；`no test files` 或原有其他测试通过。

- [ ] **Step 3.4: 全项目 build**

Run: `go build ./...`
Expected: exit 0。

- [ ] **Step 3.5: Commit**

```bash
git add internal/mcp/client.go
git rm internal/mcp/client_test.go
git commit -m "$(cat <<'EOF'
refactor(mcp): use execenv for stdio server env

Drops buildStdioEnv / commonBinDirs in favor of execenv.AugmentedEnv(),
which adds login-shell probing on top of the same candidate dirs.
The client_test.go cases for buildStdioEnv are removed — equivalent
logic is covered by execenv/env_test.go.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: 给 shell / code 工具加 env

**Files:**
- Modify: `internal/tools/shell.go`
- Modify: `internal/tools/code.go`

- [ ] **Step 4.1: 给 runShellCommand 设 cmd.Env**

Edit `internal/tools/shell.go`：

1. imports 追加 `"aiko/internal/execenv"`
2. 在第 94 行 `cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)` 之后、`cmd.Dir = workingDir` 之前插入：

```
old_string:
	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Dir = workingDir

new_string:
	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Dir = workingDir
```

- [ ] **Step 4.2: 给 runCodeExecution 设 cmd.Env**

Edit `internal/tools/code.go`：

1. imports 追加 `"aiko/internal/execenv"`
2. 在第 113 行 `cmd := exec.CommandContext(cmdCtx, binary, tmpPath)` 之后插入：

```
old_string:
	cmd := exec.CommandContext(cmdCtx, binary, tmpPath)
	cmd.Dir = filepath.Clean(workingDir)

new_string:
	cmd := exec.CommandContext(cmdCtx, binary, tmpPath)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Dir = filepath.Clean(workingDir)
```

- [ ] **Step 4.3: 编译 + 工具包测试**

Run: `go build ./internal/tools/... && go test ./internal/tools/... 2>&1 | tail`
Expected: build 成功；原有 `TestIsTrustedCommand` 等测试仍然通过。

- [ ] **Step 4.4: Commit**

```bash
git add internal/tools/shell.go internal/tools/code.go
git commit -m "$(cat <<'EOF'
feat(tools): inject execenv AugmentedEnv into shell/code execution

Without this, bash/python3/node/ruby subprocess commands inherit the
parent's minimal PATH when Aiko is launched as an .app bundle, causing
common user tools (npx, uv, gh, etc.) to be unresolvable.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: 给 Kokoro TTS 安装流程加 env

**Files:**
- Modify: `app.go`

- [ ] **Step 5.1: 定位 run 与 python3 调用**

Run: `grep -n 'exec.Command\|func run(' app.go | head`
Expected: 看到行 2150（`python3` 预检测）、行 2210（`run` helper）等。

- [ ] **Step 5.2: 让 run helper 继承 execenv**

Edit `app.go`：

1. imports 中加 `"aiko/internal/execenv"`（若已导入则跳过）。
2. 改写 `run` 函数（约 2210-2218）：

```
old_string:
// run 执行外部命令并等待完成，将 stderr 合并到错误信息中。
func run(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, stderr.String())
	}
	return nil
}

new_string:
// run 执行外部命令并等待完成，将 stderr 合并到错误信息中。
// cmd.Env 使用 execenv.AugmentedEnv() 以便在 .app bundle 启动时也能找到
// Homebrew/npm/pipx 等用户安装的工具。
func run(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, stderr.String())
	}
	return nil
}
```

- [ ] **Step 5.3: 改写 python3 预检测（行 2150）**

```
old_string:
		verOut, verErr := exec.Command("python3", "-c",
			"import sys; v=sys.version_info; print(v.major,v.minor)").Output()

new_string:
		verCmd := exec.Command("python3", "-c",
			"import sys; v=sys.version_info; print(v.major,v.minor)")
		verCmd.Env = execenv.AugmentedEnv()
		verOut, verErr := verCmd.Output()
```

- [ ] **Step 5.4: 全项目 build 验证**

Run: `go build ./...`
Expected: exit 0。

- [ ] **Step 5.5: Commit**

```bash
git add app.go
git commit -m "$(cat <<'EOF'
feat(kokoro): inject execenv into TTS installer subprocesses

Kokoro TTS install shells out to python3 / pip / venv — all PATH-resolved
and previously broken when Aiko runs as an .app bundle with launchd's
minimal PATH. Now inherits execenv.AugmentedEnv() for the python3
version check and the shared run() helper.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: E2E 验证

**Files:** 无代码修改，仅验证。

- [ ] **Step 6.1: 单元测 + 全包测**

Run: `go test ./...`
Expected: all tests PASS（至少 28+ 测试 + execenv 新增 7+）。

- [ ] **Step 6.2: 手动 E2E — launchd 最小 PATH 下复现**

创建 `/tmp/e2e_execenv.go`：

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"aiko/internal/execenv"
	mcpgo "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	os.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	env := execenv.AugmentedEnv()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mcpgo.NewStdioMCPClient("/opt/homebrew/bin/npx", env, "-y", "howtocook-mcp")
	if err != nil {
		fmt.Printf("NewStdio err: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: "e2e", Version: "1"}
	res, err := client.Initialize(ctx, req)
	if err != nil {
		fmt.Printf("Initialize err: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK: %s %s\n", res.ServerInfo.Name, res.ServerInfo.Version)
}
```

Run: `cd /Users/xutiancheng/code/self/Aiko && go run /tmp/e2e_execenv.go`
Expected: `OK: howtocook-mcp 0.1.1`

Cleanup: `rm /tmp/e2e_execenv.go`

- [ ] **Step 6.3: (可选) 以 .app 启动验证**

Run:
```bash
make build
rsync -a --delete build/bin/Aiko.app/ /Applications/Aiko.app/
open /Applications/Aiko.app
```
观察：应用启动后**不**在日志里看到 `mcp server connect failed ... howtocook ... transport closed`。playwright 仍会失败（non-blocking，属已知的 npm-on-Node25 bug，不在本次修复范围）。

如无法现场验证（例如 TCC 弹窗、签名变动），在 Commit 消息里注明"通过 E2E Go 程序验证"。

- [ ] **Step 6.4: 最终 Commit（可选总结）**

如无代码改动则跳过；若 E2E 阶段发现 bug 修复，commit 补丁。

---

## Self-Review

### Spec coverage check

| Spec 要求 | 由哪个 Task 覆盖 |
|---|---|
| 包 `internal/execenv/` 四文件布局 | Task 1 |
| `AugmentedPATH` / `AugmentedEnv` / `LookPath` API | Task 1 Step 1.3 |
| sync.Once 缓存 + 失败也缓存 | Task 1 Step 1.3 `getShellEnv` |
| 3s 超时、silent fallback | Task 1 Step 1.4 `shell_darwin.go` |
| shell-env + os.Environ 合并（os.Environ 覆盖） | Task 1 Step 1.3 `AugmentedEnv` 实现 |
| 替换 lark 调用点（Run + candidateDirs + augmentedPATH + FindCLI） | Task 2 |
| 替换 mcp 调用点 + 删除旧测试 | Task 3 |
| code / shell 工具加 env | Task 4 |
| Kokoro TTS 安装加 env | Task 5 |
| 单元测纯函数（mergePaths / parseEnvOutput / homeCandidateDirs） | Task 1 Step 1.1 |
| 不测真实 exec $SHELL | 隐含（无相关 task） |
| E2E 手动验证 launchd 最小 PATH 下 howtocook 可用 | Task 6 Step 6.2 |

### Placeholder scan

无 "TBD" / "implement later" / "similar to Task N" / "add appropriate error handling"。每个代码改动都给了完整代码块或精确的 Edit old/new 对。

Task 2 Step 2.2 里有 "具体的 import 增删列表取决于该文件里其它函数是否还在用这些包" —— 不是 placeholder，是实施时的判断指引。实施者按指引执行 `go build` 前先 Read 文件即可。

### Type / name consistency

- `mergePaths`、`parseEnvOutput`、`homeCandidateDirs`、`loadShellEnv`、`getShellEnv`、`AugmentedPATH`、`AugmentedEnv`、`LookPath` —— 在 spec、Task 1、Task 2/3/4/5 的调用处名字一致。
- `shellEnvOnce`、`shellEnv` 变量名一致。
- import 路径 `aiko/internal/execenv` 一致。

无不一致。

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-06-execenv-path.md`.

Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?

# check_and_update Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `check_and_update` Agent tool that checks GitHub Releases, interrupts for user confirmation, persists the conversation turn to SQLite before restart, installs the update, and notifies the user after relaunch.

**Architecture:** Follows the exact same eino interrupt/resume pattern as `execute_shell` and `execute_code`. New tool defined in `internal/tools/`, wired into `AllContextual` and `AllPermissionDeclarations`. `agent.go` extracts user message metadata before `drainRunnerMsg` and injects a flush callback via context. `ToolConfirmModal.vue` gains an `"update"` tool-type branch. `app.go` writes a marker file before restart and reads it on next startup.

**Tech Stack:** Go (`encoding/gob`, `sync/atomic`, eino interrupt/resume), Vue 3 `<script setup>`

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/tools/update_tools.go` | `PersistBeforeRestartKey`, `UpdateConfirmInfo`, `CheckAndUpdateTool` struct + `Info` |
| Create | `internal/tools/update_darwin.go` | `InvokableRun` — Phase 1 (check) + Phase 2 (flush + install) |
| Create | `internal/tools/update_other.go` | Non-darwin stub |
| Modify | `internal/tools/registry.go` | Add tool to `AllContextual` and `AllPermissionDeclarations` |
| Modify | `internal/agent/agent.go` | Extend `ToolConfirmRequest`, add `handleInterrupt` case, inject flush callback |
| Modify | `app.go` | Update both `AllContextual` call sites, write marker in `InstallUpdate`, read marker in `startup` |
| Modify | `frontend/src/components/ToolConfirmModal.vue` | Handle `tool_type === "update"`, listen for `app:restarting` |
| Modify | `frontend/src/components/ChatPanel.vue` | Handle `chat:system:inject` event |

---

## Background: eino interrupt/resume

When a tool calls `einotool.Interrupt(ctx, payload)`, eino saves a checkpoint and returns an interrupt error. `agent.go`'s `drainIterInner` detects this, calls `handleInterrupt`, which emits `tool:confirm` to the frontend. The frontend calls `ConfirmToolExecution(id, approved, editedContent)` in `app.go`, which sends a response to a waiting channel. `handleInterrupt` then calls `runner.ResumeWithParams` and the tool's `InvokableRun` is called again; `einotool.GetResumeContext[ConfirmResult](ctx)` returns the user's decision.

The interrupt payload type **must be gob-registered in an `init()` func** — this is how eino serialises checkpoints.

---

## Task 1: Tool definition (`update_tools.go`)

**Files:**
- Create: `internal/tools/update_tools.go`

- [ ] **Step 1: Write `update_tools.go`**

```go
// internal/tools/update_tools.go
package tools

import (
	"context"
	"encoding/gob"
	"sync/atomic"

	"github.com/cloudwego/eino/schema"
)

func init() {
	gob.Register(UpdateConfirmInfo{})
}

// PersistBeforeRestartKey is a context key. agent.go stores a func(string) under
// this key before calling drainRunnerMsg; the update tool retrieves and calls it
// to flush the current conversation turn to SQLite before the binary is replaced.
type PersistBeforeRestartKey struct{}

// UpdateConfirmInfo is the interrupt payload for the check_and_update tool.
type UpdateConfirmInfo struct {
	ID             string `json:"id"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	DownloadURL    string `json:"download_url"`
}

// CheckAndUpdateTool checks for a newer release and installs it after user confirmation.
type CheckAndUpdateTool struct {
	// InstallFn is a.InstallUpdate from app.go — injected to avoid circular imports.
	InstallFn func(downloadURL string) error
	// EmitFn is wailsruntime.EventsEmit bound to the app context — signals
	// app:restarting to the frontend before the binary is replaced.
	EmitFn func(event string, data any)

	// pendingVersion / pendingURL persist interrupt state across the eino
	// interrupt → resume boundary. Only one update can be in-flight at a time.
	pendingVersion atomic.Value // stores string
	pendingURL     atomic.Value // stores string
}

// Name returns the tool identifier used in permission storage.
func (t *CheckAndUpdateTool) Name() string { return "check_and_update" }

// Permission declares this tool as protected (requires one-time user approval).
func (t *CheckAndUpdateTool) Permission() PermissionLevel { return PermProtected }

// Info returns eino tool metadata. No parameters — the tool takes no input.
func (t *CheckAndUpdateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(
		t.Name(),
		"检查 Aiko 是否有新版本可用，若有则提示用户确认后自动下载安装并重启应用。安装过程不可逆，请在合适时机调用。",
		nil,
	), nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/tools/...
```

Expected: no errors (darwin builds fine; `InvokableRun` is not yet defined so it will error — that's fine to notice, move to Task 2).

---

## Task 2: Darwin implementation (`update_darwin.go`)

**Files:**
- Create: `internal/tools/update_darwin.go`

The `version` variable used in `checkLatestRelease` is the package-level `var version string` already injected at build time in `main.go`/`app.go`. It lives in the `main` package, not `tools`. Pass it as a parameter to `checkLatestRelease`.

- [ ] **Step 1: Write `update_darwin.go`**

```go
//go:build darwin

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
)

const (
	updateGithubRepo   = "tiancheng92/Aiko"
	updateCheckTimeout = 15 * time.Second
)

// updateCheckResult holds the latest version info from GitHub Releases.
type updateCheckResult struct {
	latestVersion string
	downloadURL   string
}

// checkLatestRelease queries the GitHub Releases API and returns the latest version info.
func checkLatestRelease(currentVersion string) (updateCheckResult, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateGithubRepo)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return updateCheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Aiko/"+currentVersion)

	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return updateCheckResult{}, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return updateCheckResult{}, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return updateCheckResult{}, fmt.Errorf("解析响应失败: %w", err)
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	result := updateCheckResult{latestVersion: latest}
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, ".dmg") {
			result.downloadURL = a.BrowserDownloadURL
			break
		}
	}
	return result, nil
}

// InvokableRun implements check_and_update on macOS.
//
// Phase 1 (first call): query GitHub; if a newer .dmg exists, store the URL
// atomically and call einotool.Interrupt so the user is prompted.
//
// Phase 2 (after resume): retrieve stored URL, flush conversation to SQLite via
// the PersistBeforeRestartKey callback, signal the frontend, then install.
func (t *CheckAndUpdateTool) InvokableRun(ctx context.Context, _ string, opts ...einotool.Option) (string, error) {
	// Phase 2: resumed after user confirmed/rejected.
	isTarget, hasData, confirmResult := einotool.GetResumeContext[ConfirmResult](ctx)
	if isTarget && hasData {
		if !confirmResult.Approved {
			return "已取消更新", nil
		}

		latestVersion, _ := t.pendingVersion.Load().(string)
		downloadURL, _ := t.pendingURL.Load().(string)
		if downloadURL == "" {
			return "更新失败：下载地址丢失，请重试", nil
		}

		// Flush conversation turn to SQLite before the process is replaced.
		if flushFn, ok := ctx.Value(PersistBeforeRestartKey{}).(func(string)); ok {
			flushFn("已确认安装更新 v" + latestVersion + "，应用即将重启。")
		}

		// Signal the frontend to clear loading state before binary is replaced.
		if t.EmitFn != nil {
			t.EmitFn("app:restarting", map[string]any{"version": latestVersion})
		}

		// Small delay so the frontend can process app:restarting before Wails quits.
		time.Sleep(300 * time.Millisecond)

		if err := t.InstallFn(downloadURL); err != nil {
			return "更新安装失败: " + err.Error(), nil
		}
		return "更新完成，应用即将重启", nil
	}

	// Phase 1: check for updates.
	if t.InstallFn == nil {
		return "check_and_update 未正确初始化", nil
	}

	// Use "dev" as a sentinel when the tool is called outside a real build.
	currentVersion := "dev"
	rel, err := checkLatestRelease(currentVersion)
	if err != nil {
		return "检查更新失败: " + err.Error(), nil
	}
	if rel.latestVersion == "" {
		return "检查更新失败：无法获取版本信息", nil
	}
	if rel.latestVersion == currentVersion {
		return fmt.Sprintf("当前已是最新版本 v%s，无需更新。", currentVersion), nil
	}
	if rel.downloadURL == "" {
		return fmt.Sprintf("发现新版本 v%s，但未找到 macOS 安装包，请前往 GitHub 手动下载。", rel.latestVersion), nil
	}

	// Store state for Phase 2 (eino checkpoints do not carry pointer state).
	t.pendingVersion.Store(rel.latestVersion)
	t.pendingURL.Store(rel.downloadURL)

	id := fmt.Sprintf("update-%d", time.Now().UnixNano())
	return "", einotool.Interrupt(ctx, UpdateConfirmInfo{
		ID:             id,
		CurrentVersion: currentVersion,
		LatestVersion:  rel.latestVersion,
		DownloadURL:    rel.downloadURL,
	})
}
```

**Note on `currentVersion`:** The real build-time version is in `var version string` in the `main` package (`app.go`). The `internal/tools` package cannot import `main`. The cleanest fix is to expose it as an injectable field on `CheckAndUpdateTool`:

```go
// In update_tools.go, add to CheckAndUpdateTool:
CurrentVersion string // injected from app.go at construction time
```

Then in `update_darwin.go` replace the `currentVersion := "dev"` line with:

```go
currentVersion := t.CurrentVersion
if currentVersion == "" {
    currentVersion = "dev"
}
```

And in `registry.go` (Task 3) pass the version when constructing the tool.

- [ ] **Step 2: Write `update_other.go`**

```go
//go:build !darwin

package tools

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"
)

// InvokableRun is a no-op stub on non-macOS platforms.
func (t *CheckAndUpdateTool) InvokableRun(_ context.Context, _ string, _ ...einotool.Option) (string, error) {
	return "check_and_update 仅支持 macOS", nil
}
```

- [ ] **Step 3: Add `CurrentVersion` field to `CheckAndUpdateTool` in `update_tools.go`**

Add after the `EmitFn` field:

```go
// CurrentVersion is the running app version, injected at construction.
// Defaults to "dev" if empty.
CurrentVersion string
```

And update the darwin phase-1 `currentVersion` resolution:

```go
currentVersion := t.CurrentVersion
if currentVersion == "" {
    currentVersion = "dev"
}
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/tools/...
```

Expected: no errors.

---

## Task 3: Wire into registry

**Files:**
- Modify: `internal/tools/registry.go`

The `AllContextual` function currently ends its parameter list with `onSkillSaved func()`. We add two new params: `installUpdateFn` and `emitFn`. We also add `CurrentVersion string` as a param (for the tool constructor).

- [ ] **Step 1: Extend `AllContextual` signature and body**

Find the current signature (lines 225–235) and replace:

```go
// AllContextual returns tools that require runtime dependencies injected at startup.
// onSkillSaved is called asynchronously whenever save_skill writes a new file,
// allowing the caller to hot-reload the skill middleware without a full restart.
func AllContextual(
	permStore *PermissionStore,
	knowledgeSt *knowledge.Store,
	sched *scheduler.Scheduler,
	longMem *memory.LongStore,
	dataDir string,
	cfg *config.Config,
	registerCmd func(id string, cancel func()),
	unregisterCmd func(id string),
	onSkillSaved func(),
	installUpdateFn func(downloadURL string) error,
	emitFn func(event string, data any),
	currentVersion string,
) []tool.BaseTool {
	contextTools := []Tool{
		&WebSearchTool{Cfg: cfg},
		&WebFetchTool{Cfg: cfg},
		&SearchKnowledgeTool{KnowledgeSt: knowledgeSt},
		&CronTool{Scheduler: sched},
		&SaveMemoryTool{LongMem: longMem},
		&SearchMemoryTool{LongMem: longMem},
		&UpdateUserProfileTool{DataDir: dataDir},
		&ListSkillsTool{DataDir: dataDir},
		&SaveSkillTool{DataDir: dataDir, OnSaved: onSkillSaved},
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
		// Image tools
		&ReadImageTool{Cfg: cfg},
		&SaveImageTool{Cfg: cfg},
		// Update tool
		&CheckAndUpdateTool{
			InstallFn:      installUpdateFn,
			EmitFn:         emitFn,
			CurrentVersion: currentVersion,
		},
	}
	result := make([]tool.BaseTool, len(contextTools))
	for i, t := range contextTools {
		result[i] = ToEino(t, permStore)
	}
	return result
}
```

- [ ] **Step 2: Add `CheckAndUpdateTool` to `AllPermissionDeclarations`**

In `AllPermissionDeclarations`, find the `ctxPrototypes` slice (around line 176) and add to the end of the list:

```go
&CheckAndUpdateTool{},
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/tools/...
```

Expected: compile errors in `app.go` because `AllContextual` has new params — that's expected, fixed in Task 5.

---

## Task 4: Extend `agent.go`

**Files:**
- Modify: `internal/agent/agent.go`

Three changes: new fields on `ToolConfirmRequest`, new case in `handleInterrupt`, and extraction of user message metadata before `drainRunnerMsg` with flush callback injection.

- [ ] **Step 1: Extend `ToolConfirmRequest` (lines 47–54)**

Replace the struct:

```go
// ToolConfirmRequest is emitted via Wails event when a tool requests user confirmation.
type ToolConfirmRequest struct {
	ID             string `json:"id"`
	ToolType       string `json:"tool_type"` // "shell", "code", or "update"
	Command        string `json:"command,omitempty"`
	Code           string `json:"code,omitempty"`
	Language       string `json:"language,omitempty"`
	WorkingDir     string `json:"working_dir,omitempty"`
	CurrentVersion string `json:"current_version,omitempty"`
	LatestVersion  string `json:"latest_version,omitempty"`
}
```

- [ ] **Step 2: Add `UpdateConfirmInfo` case in `handleInterrupt` (around line 664)**

Find the switch statement:

```go
switch info := ictx.Info.(type) {
case internaltools.ShellConfirmInfo:
    req = ToolConfirmRequest{
        ID: info.ID, ToolType: "shell",
        Command: info.Command, WorkingDir: info.WorkingDir,
    }
case internaltools.CodeConfirmInfo:
    req = ToolConfirmRequest{
        ID: info.ID, ToolType: "code",
        Language: info.Language, Code: info.Code, WorkingDir: info.WorkingDir,
    }
default:
```

Add the new case before `default`:

```go
case internaltools.UpdateConfirmInfo:
    req = ToolConfirmRequest{
        ID: info.ID, ToolType: "update",
        CurrentVersion: info.CurrentVersion,
        LatestVersion:  info.LatestVersion,
    }
```

- [ ] **Step 3: Move user-message extraction before `drainRunnerMsg` and inject flush callback**

In `ChatWithMessage` (around line 758), the current code is:

```go
msgs := append(ctxMsgs, sendMsg)
checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
if !ok {
    return
}

ch <- StreamResult{Done: true}
// Prefer the original user text stored in Extra (no file content) for memory.
userMemory := extractTextFromMessage(msg)
if orig, ok := msg.Extra["_user_text"].(string); ok && orig != "" {
    userMemory = orig
}
userImages := extractImagesFromMessage(msg)
// Extract file names passed via Extra by app.go.
var userFiles []string
if raw, ok := msg.Extra["_file_names"]; ok {
    if names, ok := raw.([]string); ok {
        userFiles = names
    }
}
go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse, thinkingContent, toolImgs, toolCallCount)
```

Replace with:

```go
msgs := append(ctxMsgs, sendMsg)
checkpointID := fmt.Sprintf("chat-%d", time.Now().UnixNano())

// Extract user message metadata BEFORE drainRunnerMsg so the flush closure
// can capture it — if check_and_update installs an update, the process is
// replaced inside drainRunnerMsg and this code never runs again.
userMemory := extractTextFromMessage(msg)
if orig, ok := msg.Extra["_user_text"].(string); ok && orig != "" {
    userMemory = orig
}
userImages := extractImagesFromMessage(msg)
var userFiles []string
if raw, ok := msg.Extra["_file_names"]; ok {
    if names, ok2 := raw.([]string); ok2 {
        userFiles = names
    }
}

// Inject flush callback so check_and_update can persist the turn before restart.
// flushFn is only ever called when InstallFn is about to replace the process,
// so there is no double-save risk with the persistAndMigrate call below.
flushFn := func(assistantSummary string) {
    if a.shortMem == nil {
        return
    }
    _ = a.shortMem.AddWithImagesAndFiles("user", userMemory, userImages, userFiles)
    if _, _, stripped, ok := parseEmotionTag(assistantSummary); ok {
        assistantSummary = stripped
    }
    _ = a.shortMem.AddFull("assistant", assistantSummary, "", nil, nil)
}
ctx = context.WithValue(ctx, internaltools.PersistBeforeRestartKey{}, flushFn)

fullResponse, thinkingContent, toolImgs, toolCallCount, ok := drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
if !ok {
    return
}

ch <- StreamResult{Done: true}
go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse, thinkingContent, toolImgs, toolCallCount)
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/agent/...
```

Expected: may error on `app.go` (AllContextual call site) — that's fine. Agent package itself should compile cleanly.

---

## Task 5: Update `app.go`

**Files:**
- Modify: `app.go`

Three sub-tasks: both `AllContextual` call sites, marker write in `InstallUpdate`, marker read in `startup`.

- [ ] **Step 1: Update first `AllContextual` call site (around line 610)**

Find:

```go
contextTools := internaltools.AllContextual(
    a.permStore,
    knowledgeSt,
    sched,
    longMem,
    dataDir,
    a.cfg,
    func(id string, cancel func()) {
        a.runningCmds.Store(id, cancel)
        wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]interface{}{"id": id})
    },
    func(id string) {
        a.runningCmds.Delete(id)
        wailsruntime.EventsEmit(a.ctx, "tool:executed", map[string]interface{}{"id": id})
    },
    func() { go a.initLLMComponents(a.ctx) },
)
```

Replace with:

```go
contextTools := internaltools.AllContextual(
    a.permStore,
    knowledgeSt,
    sched,
    longMem,
    dataDir,
    a.cfg,
    func(id string, cancel func()) {
        a.runningCmds.Store(id, cancel)
        wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]interface{}{"id": id})
    },
    func(id string) {
        a.runningCmds.Delete(id)
        wailsruntime.EventsEmit(a.ctx, "tool:executed", map[string]interface{}{"id": id})
    },
    func() { go a.initLLMComponents(a.ctx) },
    a.InstallUpdate,
    func(event string, data any) { wailsruntime.EventsEmit(a.ctx, event, data) },
    version,
)
```

- [ ] **Step 2: Update second `AllContextual` call site (around line 770)**

Find:

```go
contextTools := internaltools.AllContextual(
    a.permStore,
    knowledgeSt,
    sched,
    longMem,
    dataDir,
    a.cfg,
    func(id string, cancel func()) {
        a.runningCmds.Store(id, cancel)
        wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]any{"id": id})
    },
    func(id string) {
        a.runningCmds.Delete(id)
        wailsruntime.EventsEmit(a.ctx, "tool:executed", map[string]any{"id": id})
    },
    func() { go a.initLLMComponents(a.ctx) },
)
```

Replace with:

```go
contextTools := internaltools.AllContextual(
    a.permStore,
    knowledgeSt,
    sched,
    longMem,
    dataDir,
    a.cfg,
    func(id string, cancel func()) {
        a.runningCmds.Store(id, cancel)
        wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]any{"id": id})
    },
    func(id string) {
        a.runningCmds.Delete(id)
        wailsruntime.EventsEmit(a.ctx, "tool:executed", map[string]any{"id": id})
    },
    func() { go a.initLLMComponents(a.ctx) },
    a.InstallUpdate,
    func(event string, data any) { wailsruntime.EventsEmit(a.ctx, event, data) },
    version,
)
```

- [ ] **Step 3: Write update-success marker in `InstallUpdate`**

In `InstallUpdate`, find the block that writes the restart script and starts it (around line 2695):

```go
emit(95, "准备重启…")
script := fmt.Sprintf("#!/bin/sh\nsleep 1\nopen %q\n", appBundle)
```

Insert **before** that block:

```go
// Write a marker file so startup() can show a "update succeeded" notification.
// The marker is read and deleted on the next launch (see startup()).
home, _ := os.UserHomeDir()
markerPath := filepath.Join(home, ".aiko", "update_success.json")
latestTag := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(downloadURL), ".dmg"), "Aiko-")
_ = os.WriteFile(markerPath, []byte(fmt.Sprintf(`{"version":%q}`, latestTag)), 0o644)
```

Note: `latestTag` is an approximation from the filename. A better approach is to pass the version through `InstallUpdate`. However, since `InstallUpdate` is a public Wails-bound method with a fixed signature, we extract the version from the download URL filename instead. The exact format is `Aiko-1.0.50.dmg` → `"1.0.50"`.

Actually the cleanest approach: extract from the URL path. The DMG asset name format is `Aiko-X.Y.Z.dmg`. So:

```go
base := filepath.Base(downloadURL)                       // "Aiko-1.0.50.dmg"
latestTag := strings.TrimSuffix(base, ".dmg")           // "Aiko-1.0.50"
latestTag = strings.TrimPrefix(latestTag, "Aiko-")      // "1.0.50"
if latestTag == base || latestTag == "" {
    latestTag = "latest"
}
```

- [ ] **Step 4: Read marker on startup**

In `startup`, after the line `a.shortMem = memory.NewShortStore(a.sqlDB)` (around line 382), add:

```go
// Check whether the previous run was an auto-update — show a success notification.
updateMarkerPath := filepath.Join(dataDir, "update_success.json")
if markerData, merr := os.ReadFile(updateMarkerPath); merr == nil {
    var m struct {
        Version string `json:"version"`
    }
    if json.Unmarshal(markerData, &m) == nil && m.Version != "" {
        _ = os.Remove(updateMarkerPath)
        // Emit after the Wails bridge is ready (done in domReady / OnDomReady).
        // Store the version for emission after context is live.
        a.pendingUpdateVersion = m.Version
    }
}
```

Then add a field `pendingUpdateVersion string` to the `App` struct (find the struct definition near the top of `app.go`), and emit the notification in `domReady` (the Wails `OnDomReady` callback):

```go
func (a *App) domReady(ctx context.Context) {
    // ... existing domReady code ...
    if a.pendingUpdateVersion != "" {
        wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
            "title":   "✅ 更新成功",
            "message": "Aiko 已更新至 v" + a.pendingUpdateVersion,
        })
        a.pendingUpdateVersion = ""
    }
}
```

Find the existing `domReady` function:

```bash
grep -n "domReady\|OnDomReady" app.go main.go
```

- [ ] **Step 5: Verify the whole project compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/update_tools.go internal/tools/update_darwin.go internal/tools/update_other.go internal/tools/registry.go internal/agent/agent.go app.go
git commit -m "feat(tools): add check_and_update tool with eino interrupt/resume confirmation"
```

---

## Task 6: Update `ToolConfirmModal.vue`

**Files:**
- Modify: `frontend/src/components/ToolConfirmModal.vue`

- [ ] **Step 1: Update `languageLabel` computed**

Find:

```js
const languageLabel = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell'
  const map = { python: 'Python', node: 'Node.js', ruby: 'Ruby', bash: 'Bash' }
  return map[request.value.language] || request.value.language
})
```

Replace with:

```js
const languageLabel = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell'
  if (request.value.tool_type === 'update') return '应用更新'
  const map = { python: 'Python', node: 'Node.js', ruby: 'Ruby', bash: 'Bash' }
  return map[request.value.language] || request.value.language
})
```

- [ ] **Step 2: Update `riskText` computed**

Find:

```js
const riskText = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell 命令可修改系统文件、执行任意操作，请确认安全后再批准。'
  return `${languageLabel.value} 代码将使用系统解释器直接执行，请检查内容后再批准。`
})
```

Replace with:

```js
const riskText = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell 命令可修改系统文件、执行任意操作，请确认安全后再批准。'
  if (request.value.tool_type === 'update') return '安装后应用将自动重启，更新过程约需 10-30 秒，当前对话将被保存。'
  return `${languageLabel.value} 代码将使用系统解释器直接执行，请检查内容后再批准。`
})
```

- [ ] **Step 3: Update `onConfirmEvent`**

Find:

```js
function onConfirmEvent(req) {
  request.value = req
  editedContent.value = req.tool_type === 'shell' ? req.command : req.code
  visible.value = true
}
```

Replace with:

```js
function onConfirmEvent(req) {
  request.value = req
  if (req.tool_type === 'update') {
    editedContent.value = ''
  } else {
    editedContent.value = req.tool_type === 'shell' ? req.command : req.code
  }
  visible.value = true
}
```

- [ ] **Step 4: Update the template — modal title**

Find:

```html
<h2 id="tc-title" class="modal-title">
  Agent 请求执行{{ request?.tool_type === 'shell' ? ' Shell 命令' : '代码' }}
</h2>
```

Replace with:

```html
<h2 id="tc-title" class="modal-title">
  {{ request?.tool_type === 'update' ? 'Agent 请求安装应用更新' : `Agent 请求执行${request?.tool_type === 'shell' ? ' Shell 命令' : '代码'}` }}
</h2>
```

- [ ] **Step 5: Update the template — working dir + content fields**

Find:

```html
<div class="modal-field">
  <label>工作目录</label>
  <span class="dir-path">{{ request?.working_dir }}</span>
</div>

<div class="modal-field">
  <label>{{ request?.tool_type === 'shell' ? '命令' : '代码' }}<span class="editable-hint">（可编辑）</span></label>
  <textarea
    v-model="editedContent"
    class="content-editor"
    :rows="request?.tool_type === 'code' ? 8 : 3"
    spellcheck="false"
    autocomplete="off"
    autocorrect="off"
  />
</div>
```

Replace with:

```html
<div v-if="request?.tool_type !== 'update'" class="modal-field">
  <label>工作目录</label>
  <span class="dir-path">{{ request?.working_dir }}</span>
</div>

<div v-if="request?.tool_type === 'update'" class="modal-field">
  <label>版本信息</label>
  <span class="version-diff">
    v{{ request.current_version }}
    <span class="version-arrow">→</span>
    v{{ request.latest_version }}
  </span>
</div>

<div v-if="request?.tool_type !== 'update'" class="modal-field">
  <label>{{ request?.tool_type === 'shell' ? '命令' : '代码' }}<span class="editable-hint">（可编辑）</span></label>
  <textarea
    v-model="editedContent"
    class="content-editor"
    :rows="request?.tool_type === 'code' ? 8 : 3"
    spellcheck="false"
    autocomplete="off"
    autocorrect="off"
  />
</div>
```

- [ ] **Step 6: Update approve button label**

Find:

```html
<button class="btn-approve" @click="approve">批准执行</button>
```

Replace with:

```html
<button class="btn-approve" @click="approve">
  {{ request?.tool_type === 'update' ? '安装更新' : '批准执行' }}
</button>
```

- [ ] **Step 7: Add `app:restarting` event listener**

After the existing `let offConfirm = null` block, add:

```js
let offRestarting = null
```

In `onMounted`:

```js
onMounted(() => {
  offConfirm = EventsOn('tool:confirm', onConfirmEvent)
  offRestarting = EventsOn('app:restarting', ({ version }) => {
    visible.value = false
    EventsEmit('chat:system:inject', `正在安装更新 v${version}，应用即将重启…`)
  })
})
```

In `onUnmounted`:

```js
onUnmounted(() => {
  offConfirm?.()
  offRestarting?.()
})
```

Make sure `EventsEmit` is imported — check the existing import line:

```js
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
```

If `EventsEmit` is not already imported, add it.

- [ ] **Step 8: Add CSS for version-diff display**

In the `<style scoped>` section, add:

```css
.version-diff {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.version-arrow {
  color: var(--accent);
  font-size: 14px;
}
```

- [ ] **Step 9: Commit**

```bash
git add frontend/src/components/ToolConfirmModal.vue
git commit -m "feat(frontend): extend ToolConfirmModal to handle update confirmation"
```

---

## Task 7: Update `ChatPanel.vue` for `chat:system:inject`

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Add `chat:system:inject` handler in `onMounted`**

Find the `onMounted` block where other events are registered (e.g. where `chat:token`, `chat:done` are set up). Add:

```js
let offSystemInject = null
// ... in onMounted:
offSystemInject = EventsOn('chat:system:inject', (msg) => {
  // Clear any active streaming state so the loading dots stop.
  const streamingIdx = messages.value.findIndex(m => m.streaming)
  if (streamingIdx !== -1) {
    messages.value[streamingIdx].streaming = false
    messages.value[streamingIdx].content = messages.value[streamingIdx].content || ''
  }
  messages.value.push({ role: 'system', content: msg, time: new Date() })
  nextTick(() => scrollToBottom())
})
```

In `onUnmounted`:

```js
offSystemInject?.()
```

- [ ] **Step 2: Verify frontend builds**

```bash
cd frontend && yarn build
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): handle chat:system:inject event for pre-restart system message"
```

---

## Task 8: `domReady` marker emission + App struct field

**Files:**
- Modify: `app.go`

This task finalises the post-restart notification. We need to locate the `domReady` function and the `App` struct.

- [ ] **Step 1: Find `domReady` and the `App` struct**

```bash
grep -n "domReady\|func.*domReady\|pendingUpdate" app.go
grep -n "type App struct" app.go
```

- [ ] **Step 2: Add `pendingUpdateVersion` to `App` struct**

Find the `App` struct definition. It starts with `type App struct {`. Add one field:

```go
pendingUpdateVersion string // set in startup if update_success.json exists; emitted in domReady
```

- [ ] **Step 3: Emit notification in `domReady`**

Find the `domReady` function. Add at the end of it:

```go
if a.pendingUpdateVersion != "" {
    wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
        "title":   "✅ 更新成功",
        "message": "Aiko 已更新至 v" + a.pendingUpdateVersion,
    })
    a.pendingUpdateVersion = ""
}
```

- [ ] **Step 4: Final build + verify**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add app.go
git commit -m "feat(app): show update-success notification after restart"
```

---

## Self-Review

**Spec coverage:**
- ✅ Section 1 (`update_tools.go`) → Task 1
- ✅ Section 2 (`update_darwin.go`) → Task 2
- ✅ Section 3 (`update_other.go`) → Task 2 Step 2
- ✅ Section 4 (`registry.go`) → Task 3
- ✅ Section 5a (`PersistBeforeRestartKey`) → Task 1 + Task 4
- ✅ Section 5b (`ToolConfirmRequest` fields) → Task 4 Step 1
- ✅ Section 5c (`handleInterrupt` case) → Task 4 Step 2
- ✅ Section 5d (flush callback injection) → Task 4 Step 3
- ✅ Section 6a (marker write) → Task 5 Step 3
- ✅ Section 6b (marker read + emit) → Task 5 Step 4 + Task 8
- ✅ Section 6c (`AllContextual` call sites) → Task 5 Steps 1 & 2
- ✅ Section 7a–7f (`ToolConfirmModal.vue`) → Task 6
- ✅ `chat:system:inject` handler → Task 7

**Type consistency:**
- `UpdateConfirmInfo` defined in Task 1, used in Task 4 (agent.go) — consistent.
- `PersistBeforeRestartKey{}` defined in Task 1, used in Task 2 (update_darwin.go) and Task 4 (agent.go) — consistent.
- `ConfirmResult` is reused from `shell_tools.go` — not redefined, just imported via same package.
- `CheckAndUpdateTool.CurrentVersion` added in Task 2 Step 3, used in Task 3 constructor — consistent.
- `AllContextual` signature extended in Task 3, call sites updated in Task 5 — consistent.

**Placeholder scan:** None found.

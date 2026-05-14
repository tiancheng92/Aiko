# check_and_update Tool Implementation Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `check_and_update` Agent tool that checks for a newer version on GitHub Releases, asks the user for confirmation via the existing interrupt/resume flow, saves the current conversation turn to SQLite before restarting, then installs the update and notifies the user on next launch.

**Architecture:** eino interrupt/resume (same pattern as `execute_shell`/`execute_code`). Three new Go files for the tool, small changes to `registry.go`, `agent.go`, `app.go`, and `ToolConfirmModal.vue`.

**Tech Stack:** Go (eino interrupt/resume, sync/atomic, encoding/gob), Vue 3 (ToolConfirmModal)

---

## File Map

| Action | Path |
|--------|------|
| Create | `internal/tools/update_tools.go` |
| Create | `internal/tools/update_darwin.go` |
| Create | `internal/tools/update_other.go` |
| Modify | `internal/tools/registry.go` |
| Modify | `internal/agent/agent.go` |
| Modify | `app.go` |
| Modify | `frontend/src/components/ToolConfirmModal.vue` |

---

## Section 1 — Tool definition (`update_tools.go`)

`UpdateConfirmInfo` is the interrupt payload; it carries everything `handleInterrupt` and the tool need across the interrupt/resume boundary.

```go
// internal/tools/update_tools.go
package tools

import (
    "context"
    "encoding/gob"
    "github.com/cloudwego/eino/schema"
)

func init() {
    gob.Register(UpdateConfirmInfo{})
}

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
    // EmitFn is wailsruntime.EventsEmit bound to the app context — used to signal
    // app:restarting to the frontend before the binary is replaced.
    EmitFn func(event string, data any)

    // pendingVersion / pendingURL persist interrupt state across the eino
    // interrupt → resume boundary. Only one update can be in-flight at a time.
    pendingVersion atomic.Value // string
    pendingURL     atomic.Value // string
}

func (t *CheckAndUpdateTool) Name() string            { return "check_and_update" }
func (t *CheckAndUpdateTool) Permission() PermissionLevel { return PermProtected }

func (t *CheckAndUpdateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
    return infoFromSchema(
        t.Name(),
        "检查 Aiko 是否有新版本可用，若有则提示用户确认后自动下载安装并重启应用。安装过程不可逆，请在合适的时机调用。",
        nil, // no parameters
    ), nil
}
```

---

## Section 2 — Darwin implementation (`update_darwin.go`)

Two phases separated by `GetResumeContext`:

**Phase 1 (first call):** Query GitHub Releases API → if no update return early → store `latestVersion`/`downloadURL` in `atomic.Value` → interrupt with `UpdateConfirmInfo`.

**Phase 2 (after resume):** Read stored `downloadURL`/`latestVersion` → if rejected return cancellation message → call `flushFn` from context to persist conversation → emit `app:restarting` → call `t.InstallFn`.

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
    updateGithubRepo    = "tiancheng92/Aiko"
    updateCheckTimeout  = 15 * time.Second
)

// updateCheckResult is a minimal GitHub Releases API response.
type updateCheckResult struct {
    latestVersion string
    downloadURL   string
}

// checkLatestRelease queries the GitHub Releases API and returns the latest version info.
func checkLatestRelease(currentVersion string) (updateCheckResult, error) {
    url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateGithubRepo)
    req, err := http.NewRequest(http.MethodGet, url, nil)
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

// InvokableRun implements the check_and_update tool.
// Phase 1: check GitHub, interrupt if update available.
// Phase 2 (after resume): flush conversation, emit restarting signal, install.
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

        // Persist the current conversation turn before the process is replaced.
        if flushFn, ok := ctx.Value(PersistBeforeRestartKey{}).(func(string)); ok {
            flushFn("已确认安装更新 " + latestVersion + "，应用即将重启。")
        }

        // Signal the frontend to clear loading state and show a farewell message.
        if t.EmitFn != nil {
            t.EmitFn("app:restarting", map[string]any{"version": latestVersion})
        }

        if err := t.InstallFn(downloadURL); err != nil {
            return "更新安装失败: " + err.Error(), nil
        }
        return "更新完成，应用即将重启", nil
    }

    // Phase 1: check for updates.
    if t.InstallFn == nil {
        return "check_and_update 未正确初始化", nil
    }

    rel, err := checkLatestRelease(version)
    if err != nil {
        return "检查更新失败: " + err.Error(), nil
    }
    if rel.latestVersion == "" || rel.latestVersion == version {
        return fmt.Sprintf("当前已是最新版本 v%s，无需更新。", version), nil
    }
    if rel.downloadURL == "" {
        return fmt.Sprintf("发现新版本 v%s，但未找到 macOS 安装包，请前往 GitHub 手动下载。", rel.latestVersion), nil
    }

    // Store state for Phase 2.
    t.pendingVersion.Store(rel.latestVersion)
    t.pendingURL.Store(rel.downloadURL)

    id := fmt.Sprintf("update-%d", time.Now().UnixNano())
    return "", einotool.Interrupt(ctx, UpdateConfirmInfo{
        ID:             id,
        CurrentVersion: version,
        LatestVersion:  rel.latestVersion,
        DownloadURL:    rel.downloadURL,
    })
}
```

---

## Section 3 — Non-darwin stub (`update_other.go`)

```go
//go:build !darwin

package tools

import (
    "context"
    einotool "github.com/cloudwego/eino/components/tool"
)

func (t *CheckAndUpdateTool) InvokableRun(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
    return "check_and_update 仅支持 macOS", nil
}
```

---

## Section 4 — `registry.go` changes

**`AllContextual` signature** gets one new parameter:

```go
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
    installUpdateFn func(downloadURL string) error,   // NEW
    emitFn func(event string, data any),              // NEW
) []tool.BaseTool {
```

Add to the `contextTools` slice:

```go
&CheckAndUpdateTool{InstallFn: installUpdateFn, EmitFn: emitFn},
```

**`AllPermissionDeclarations`** — add to `ctxPrototypes`:

```go
&CheckAndUpdateTool{},
```

---

## Section 5 — `agent.go` changes

### 5a — `persistBeforeRestartKey` context key

Defined in **`internal/tools/update_tools.go`** (not `agent.go`) to avoid a circular import: `agent` already imports `internal/tools`, so the key must live in `tools` and `agent.go` references it as `internaltools.PersistBeforeRestartKey{}`.

```go
// PersistBeforeRestartKey is a context key. agent.go stores a func(string) under
// this key before calling drainRunnerMsg; the update tool retrieves and calls it
// to flush the current conversation turn to SQLite before the binary is replaced.
type PersistBeforeRestartKey struct{}
```

### 5b — `ToolConfirmRequest` new fields

```go
type ToolConfirmRequest struct {
    ID             string `json:"id"`
    ToolType       string `json:"tool_type"` // "shell", "code", or "update"
    Command        string `json:"command,omitempty"`
    Code           string `json:"code,omitempty"`
    Language       string `json:"language,omitempty"`
    WorkingDir     string `json:"working_dir,omitempty"`
    CurrentVersion string `json:"current_version,omitempty"` // NEW
    LatestVersion  string `json:"latest_version,omitempty"`  // NEW
}
```

### 5c — `handleInterrupt` new case

```go
case internaltools.UpdateConfirmInfo:
    req = ToolConfirmRequest{
        ID: info.ID, ToolType: "update",
        CurrentVersion: info.CurrentVersion,
        LatestVersion:  info.LatestVersion,
    }
```

### 5d — `ChatWithMessage` — extract user message before `drainRunnerMsg`

Move the `userMemory`/`userImages`/`userFiles` extraction to **before** `drainRunnerMsg` (currently at lines 765–776, after `drainRunnerMsg` returns). Then inject the flush callback:

```go
// Extract BEFORE drainRunnerMsg so the flush closure can capture them.
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

// Inject flush callback so check_and_update can persist before restart.
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

fullResponse, thinkingContent, toolImgs, toolCallCount, ok :=
    drainRunnerMsg(ctx, a.runner, msgs, ch, a.pendingConfirms, a.emitEvent, checkpointID)
if !ok {
    return
}
ch <- StreamResult{Done: true}
go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles,
    fullResponse, thinkingContent, toolImgs, toolCallCount)
```

Note: there is no double-save risk. `flushFn` is only called when the user approves the update and `InstallFn` is about to replace the process. In that path `drainRunnerMsg` never returns and `persistAndMigrate` is never called.

---

## Section 6 — `app.go` changes

### 6a — Write update success marker before restart

Inside `InstallUpdate`, just before the `exec.Command(newExe, ...)` restart call, write a marker file:

```go
markerPath := filepath.Join(os.Getenv("HOME"), ".aiko", "update_success.json")
markerData := fmt.Sprintf(`{"version":%q}`, latestVersionTag)
_ = os.WriteFile(markerPath, []byte(markerData), 0o644)
```

### 6b — Read marker on startup

In `startup` (after Wails bridge is ready), check for and consume the marker:

```go
markerPath := filepath.Join(dataDir, "../update_success.json") // or absolute ~/.aiko/
if data, err := os.ReadFile(markerPath); err == nil {
    var m struct{ Version string `json:"version"` }
    if json.Unmarshal(data, &m) == nil && m.Version != "" {
        _ = os.Remove(markerPath)
        wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
            "title":   "✅ 更新成功",
            "message": "Aiko 已更新至 v" + m.Version,
        })
    }
}
```

### 6c — `AllContextual` call sites

Both call sites in `app.go` (startup and `initLLMComponents`) gain two new arguments:

```go
tools.AllContextual(
    ...,
    a.InstallUpdate,
    func(event string, data any) { wailsruntime.EventsEmit(a.ctx, event, data) },
)
```

---

## Section 7 — `ToolConfirmModal.vue` changes

### 7a — `languageLabel` computed

```js
if (request.value.tool_type === 'update') return '应用更新'
```

### 7b — `riskText` computed

```js
if (request.value.tool_type === 'update')
    return '安装后应用将自动重启，更新过程约需 10-30 秒，当前对话将被保存。'
```

### 7c — `onConfirmEvent`

```js
if (req.tool_type === 'update') {
    editedContent.value = ''   // no editable content for updates
} else {
    editedContent.value = req.tool_type === 'shell' ? req.command : req.code
}
```

### 7d — approve

```js
async function approve() {
    visible.value = false
    await ConfirmToolExecution(request.value.id, true, editedContent.value)
}
```

No change needed — `editedContent.value` will be `''` for update type, which is fine since `ConfirmResult.EditedContent` is ignored by the update tool.

### 7e — Template: conditionally show version info vs code editor

```html
<!-- version info block (update type) -->
<div v-if="request?.tool_type === 'update'" class="modal-field">
  <label>版本信息</label>
  <span class="version-diff">
    v{{ request.current_version }}
    <span class="version-arrow">→</span>
    v{{ request.latest_version }}
  </span>
</div>

<!-- editable command/code block (shell/code types) -->
<div v-else class="modal-field">
  <label>{{ request?.tool_type === 'shell' ? '命令' : '代码' }}<span class="editable-hint">（可编辑）</span></label>
  <textarea .../>
</div>
```

Also change the approve button label:

```html
<button class="btn-approve" @click="approve">
  {{ request?.tool_type === 'update' ? '安装更新' : '批准执行' }}
</button>
```

### 7f — `app:restarting` event handler

```js
let offRestarting = null
onMounted(() => {
    offRestarting = EventsOn('app:restarting', ({ version }) => {
        visible.value = false  // close any open modal
        // Inject a system message into the chat stream via a shared event
        EventsEmit('chat:system:inject', `正在安装更新 v${version}，应用即将重启…`)
    })
})
onUnmounted(() => offRestarting?.())
```

Note: `chat:system:inject` is a new frontend-only event that `ChatPanel.vue` listens to in order to push a system message into the message list and clear loading state. This requires a small addition to `ChatPanel.vue` as well (add listener in `onMounted`, push `{ role: 'system', content: msg }` and set `streaming = false`).

---

## Scope boundary

This design does NOT include:
- Background periodic update checks (would be a separate feature)
- Rollback or update history
- Changelog display in the confirmation modal
- Windows/Linux support

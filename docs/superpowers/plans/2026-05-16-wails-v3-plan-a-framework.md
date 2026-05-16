# Wails v3 迁移 Plan A：框架层替换

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Wails v2 依赖替换为 v3，重写 `main.go` 使应用能用 v3 框架启动，同时保留 `app.go` 单体结构（Service 拆分在 Plan B 完成）。

**Architecture:** 保留 `app.go` 的全部方法不变，只将其注册为单个 v3 Service；`main.go` 完整重写为 v3 风格；`macos.go` 和 `hotkey_darwin.go` 替换全局引用类型从 `context.Context` 改为 `*application.App`。

**Tech Stack:** `github.com/wailsapp/wails/v3 v3.0.0-alpha.92`，Go 1.25，Vue 3 + Vite

---

## 前置条件

- 当前分支 `main` 干净（无未提交更改）
- `go 1.25` 可用
- `wails3` CLI 已安装：`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.92`

---

## 文件变更总览

| 操作 | 文件 |
|------|------|
| 修改 | `go.mod` / `go.sum` |
| 完整重写 | `main.go` |
| 修改 | `hotkey_darwin.go` — 替换全局变量类型 |
| 修改 | `macos.go` — 替换所有 `wailsruntime.EventsEmit(globalAppCtx, ...)` 调用 |
| 修改 | `app.go` — 添加 `ServiceStartup` / `ServiceShutdown` 方法，移除 `startup` / `domReady` / `shutdown` v2 回调 |
| 删除 | `frontend/wailsjs/`（v2 生成的 bindings） |
| 生成 | `frontend/bindings/`（v3 `wails3 generate bindings` 生成） |
| 修改 | `wails.json` — 更新 schema |
| 修改 | `Makefile` — 更新 CLI 命令 |

---

## Task 1：替换 go.mod 依赖

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: 移除 v2，添加 v3**

```bash
go get github.com/wailsapp/wails/v3@v3.0.0-alpha.92
```

- [ ] **Step 2: 移除 v2 依赖**

```bash
go mod edit -droprequire github.com/wailsapp/wails/v2
go mod tidy
```

- [ ] **Step 3: 验证 go.mod 不含 v2**

```bash
grep "wailsapp/wails" go.mod
```

预期输出：只看到 `github.com/wailsapp/wails/v3`，无 `v2`。

- [ ] **Step 4: 确认项目目前编译失败（符合预期，因为 import path 还未更新）**

```bash
go build ./... 2>&1 | head -20
```

预期输出：大量 `cannot find package "github.com/wailsapp/wails/v2/..."` 错误。

---

## Task 2：重写 hotkey_darwin.go

**Files:**
- Modify: `hotkey_darwin.go`

`hotkey_darwin.go` 目前只声明 `globalAppCtx context.Context`，改为持有 v3 App 引用。

- [ ] **Step 1: 替换文件内容**

将 `hotkey_darwin.go` 改为：

```go
//go:build darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// globalApp holds the Wails v3 app instance after startup, used by registerGlobalHotkey.
var globalApp *application.App

// setGlobalApp stores the app reference for use by macos.go CGO callbacks.
func setGlobalApp(app *application.App) {
	globalApp = app
}
```

- [ ] **Step 2: 提交**

```bash
git add hotkey_darwin.go
git commit -m "refactor: replace globalAppCtx with v3 *application.App"
```

---

## Task 3：更新 macos.go 的 wails import 和 EventsEmit 调用

**Files:**
- Modify: `macos.go`

`macos.go` 顶部 import 了 `wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"`，并在 7 处调用 `wailsruntime.EventsEmit(globalAppCtx, ...)` 。

- [ ] **Step 1: 替换 import**

在 `macos.go` 中找到：

```go
wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
```

替换为：

```go
"github.com/wailsapp/wails/v3/pkg/application"
```

（`application` 包已隐式通过 `globalApp` 使用，无需 alias。）

- [ ] **Step 2: 替换所有 EventsEmit 调用**

在 `macos.go` 中批量替换（共 7 处，全在 Option 键回调中）：

| 旧代码 | 新代码 |
|--------|--------|
| `wailsruntime.EventsEmit(globalAppCtx, "bubble:toggle")` | `globalApp.Event.Emit("bubble:toggle")` |
| `wailsruntime.EventsEmit(globalAppCtx, "voice:start")` | `globalApp.Event.Emit("voice:start")` |
| `wailsruntime.EventsEmit(globalAppCtx, "voice:end")` | `globalApp.Event.Emit("voice:end")` |
| `wailsruntime.EventsEmit(globalAppCtx, "voice:final", text[6:])` | `globalApp.Event.Emit("voice:final", text[6:])` |
| `wailsruntime.EventsEmit(globalAppCtx, "voice:error", text[6:])` | `globalApp.Event.Emit("voice:error", text[6:])` |
| `wailsruntime.EventsEmit(globalAppCtx, "voice:transcript", text)` | `globalApp.Event.Emit("voice:transcript", text)` |

nil guard 由原来的 `if globalAppCtx == nil` 改为 `if globalApp == nil`。

- [ ] **Step 3: 移除已不使用的 `_ "github.com/wailsapp/wails/v2"` 等其他 v2 import（若有）**

```bash
grep "wailsapp/wails/v2" macos.go
```

预期：无输出（v2 import 已全部移除）。

- [ ] **Step 4: 提交**

```bash
git add macos.go
git commit -m "refactor: replace v2 EventsEmit with v3 app.Event.Emit in macos.go"
```

---

## Task 4：更新 app.go — v2 lifecycle → v3 ServiceStartup/ServiceShutdown

**Files:**
- Modify: `app.go`

`app.go` 目前有三个 v2 lifecycle 方法：`startup(ctx context.Context)`、`domReady(_ context.Context)`、`shutdown(_ context.Context)`。v3 用 `ServiceStartup` / `ServiceShutdown` 接口替代。同时所有 `wailsruntime.EventsEmit(a.ctx, ...)` 要改为 `globalApp.Event.Emit(...)`，`wailsruntime.ScreenGetAll` / `WindowSetSize` / `WindowSetPosition` 要改为 v3 API。

- [ ] **Step 1: 替换 app.go 顶部的 wails import**

找到：

```go
wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
```

替换为（v3 不需要 runtime import，所有 API 通过 `globalApp` 访问）：

```go
// wailsruntime 已移除；事件通过 globalApp.Event.Emit() 发出
```

即删除该 import 行。

- [ ] **Step 2: 替换 App 结构体中的 ctx 字段**

`App` 结构体中的 `ctx context.Context` 字段在 v3 中不再由 Wails 注入。将其保留为私有 context，在 `ServiceStartup` 中自行管理：

```go
type App struct {
    ctx    context.Context    // set in ServiceStartup
    cancel context.CancelFunc // set in ServiceStartup
    // ... 其余字段不变
}
```

- [ ] **Step 3: 将 startup 方法重命名为 ServiceStartup**

找到：

```go
func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
    // ...
}
```

替换函数签名为：

```go
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
    a.ctx, a.cancel = context.WithCancel(ctx)
    // 函数体不变，将最后的隐式 return 改为 return nil
    // ...
    return nil
}
```

注意：原 `startup` 中有 `panic(err)` 调用，全部改为 `return fmt.Errorf("startup: %w", err)`，让 Wails 正常报错而非 panic。

- [ ] **Step 4: 将 domReady 逻辑合并到 ServiceStartup 末尾**

原 `domReady` 方法：

```go
func (a *App) domReady(_ context.Context) {
    requestPermissionsEarly()
    hideNativeScrollbars()
    if a.pendingUpdateVersion != "" {
        wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{...})
        a.pendingUpdateVersion = ""
    }
}
```

v3 无 `OnDomReady` 回调。将这段逻辑移入 `ServiceStartup` 的末尾，但延迟到窗口加载完成后执行。在 `ServiceStartup` 末尾添加：

```go
// domReady 逻辑：等窗口加载完成后执行
app := application.Get()
app.Window.GetByName("main").OnWindowEvent(events.Common.WindowLoadComplete, func(_ *application.WindowEvent) {
    requestPermissionsEarly()
    hideNativeScrollbars()
    if a.pendingUpdateVersion != "" {
        globalApp.Event.Emit("notification:show", map[string]any{
            "title":   "✅ 更新成功",
            "message": "Aiko 已更新至 v" + a.pendingUpdateVersion,
        })
        a.pendingUpdateVersion = ""
    }
})
return nil
```

- [ ] **Step 5: 将 shutdown 方法重命名为 ServiceShutdown**

找到：

```go
func (a *App) shutdown(_ context.Context) {
```

替换为：

```go
func (a *App) ServiceShutdown() error {
    if a.cancel != nil {
        a.cancel()
    }
```

函数末尾添加 `return nil`，移除原有的 context 参数使用。

- [ ] **Step 6: 批量替换所有 wailsruntime.EventsEmit(a.ctx, ...) 调用**

`app.go` 中共有 ~35 处 `wailsruntime.EventsEmit(a.ctx, ...)`，全部替换为 `globalApp.Event.Emit(...)`：

```bash
sed -i '' 's/wailsruntime\.EventsEmit(a\.ctx,/globalApp.Event.Emit(/g' app.go
```

验证替换结果：

```bash
grep "wailsruntime" app.go
```

预期：无输出。

- [ ] **Step 7: 替换 ScreenGetAll / WindowSetSize / WindowSetPosition**

在 `startup`（现 `ServiceStartup`）中找到：

```go
screens, err := wailsruntime.ScreenGetAll(ctx)
if err == nil {
    for _, s := range screens {
        if s.IsPrimary {
            wailsruntime.WindowSetSize(ctx, s.Size.Width, s.Size.Height)
            wailsruntime.WindowSetPosition(ctx, 0, 0)
            break
        }
    }
}
```

替换为：

```go
screens := globalApp.Screen.GetAll()
for _, s := range screens {
    if s.IsPrimary {
        if win, ok := globalApp.Window.GetByName("main"); ok {
            win.SetSize(s.Size.Width, s.Size.Height)
            win.SetPosition(0, 0)
        }
        break
    }
}
```

- [ ] **Step 8: 替换 startScreenWatcher 中的 ScreenGetAll**

在 `startScreenWatcher` 方法中找到 `wailsruntime.ScreenGetAll(a.ctx)` 调用，替换为 `globalApp.Screen.GetAll()`。同时找到 `wailsruntime.EventsEmit(a.ctx, "screen:changed", current)` 已在 Step 6 全量替换，确认已处理。

- [ ] **Step 9: 添加 v3 import**

在 `app.go` 的 import 块中添加：

```go
"github.com/wailsapp/wails/v3/pkg/application"
"github.com/wailsapp/wails/v3/pkg/events"
```

- [ ] **Step 10: 提交**

```bash
git add app.go
git commit -m "refactor: migrate app.go lifecycle to v3 ServiceStartup/ServiceShutdown"
```

---

## Task 5：重写 main.go

**Files:**
- Modify: `main.go`（完整重写）

- [ ] **Step 1: 重写 main.go**

```go
package main

import (
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var version = "dev"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	var logOut io.Writer = os.Stderr
	if version != "dev" {
		if f, err := os.OpenFile("/tmp/aiko.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			logOut = f
		}
	}
	h := slog.NewTextHandler(logOut, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})
	slog.SetDefault(slog.New(h))

	if bundle, need := needsSigUpgrade(); need {
		if err := upgradeSignatureAndRelaunch(bundle); err != nil {
			slog.Error("signature upgrade failed; continuing with ad-hoc", "err", err)
		} else {
			os.Exit(0)
		}
	}

	appInstance := NewApp()

	app := application.New(application.Options{
		Name: "Aiko",
		Services: []application.Service{
			application.NewService(appInstance),
		},
		Assets: application.AssetOptions{
			Handler: newAssetHandler(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	setGlobalApp(app)

	mainWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Width:            1440,
		Height:           900,
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTransparent,
		},
	})
	_ = mainWin

	settingsWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "settings",
		Width:  900,
		Height: 680,
		Title:  "Aiko Settings",
		Hidden: true,
		URL:    "/?settings=1",
	})
	_ = settingsWin

	appMenu := app.NewMenu()
	appMenu.AddRole(application.AppMenu)
	appMenu.AddRole(application.EditMenu)

	viewMenu := appMenu.AddSubmenu("View")
	viewMenu.Add("Toggle Chat").
		SetAccelerator("CmdOrCtrl+Shift+P").
		OnClick(func(_ *application.Context) {
			app.Event.Emit("bubble:toggle")
		})

	settingsMenu := appMenu.AddSubmenu("Settings")
	settingsMenu.Add("Preferences...").
		SetAccelerator("CmdOrCtrl+,").
		OnClick(func(_ *application.Context) {
			if win, ok := app.Window.GetByName("settings"); ok {
				win.Show()
				win.Focus()
			}
		})

	app.Menu.Set(appMenu)

	if err := app.Run(); err != nil {
		panic(err)
	}
}

// newAssetHandler returns an http.Handler that serves the embedded frontend
// assets and also handles /user-vrm/ requests from the filesystem.
func newAssetHandler(assets embed.FS) http.Handler {
	bundled := application.BundledAssetFileServer(assets)
	vrm := userVRMHandler{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/user-vrm/") {
			vrm.ServeHTTP(w, r)
			return
		}
		bundled.ServeHTTP(w, r)
	})
}

// userVRMHandler serves .vrm files from ~/.aiko/vrm/ at /user-vrm/<name>.
type userVRMHandler struct{}

func (h userVRMHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/user-vrm/") {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(r.URL.Path)
	if !strings.HasSuffix(name, ".vrm") {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm", name)
	http.ServeFile(w, r, p)
}

func needsSigUpgrade() (string, bool) {
	if version == "dev" {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	exe, _ = filepath.EvalSymlinks(exe)
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(bundle, ".app") {
		return "", false
	}
	out, _ := exec.Command("codesign", "--display", "--verbose=2", bundle).CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Authority=") {
			return bundle, false
		}
	}
	return bundle, true
}

func upgradeSignatureAndRelaunch(bundle string) error {
	slog.Info("upgrading signature on first launch", "bundle", bundle)
	if err := ensureAikoCert(); err != nil {
		return fmt.Errorf("ensure cert: %w", err)
	}
	if err := run("codesign", "--force", "--sign", "Aiko",
		"--identifier", "com.xutiancheng.aiko",
		"--preserve-metadata=entitlements", bundle); err != nil {
		return fmt.Errorf("resign: %w", err)
	}
	script := fmt.Sprintf("#!/bin/sh\nsleep 1\nopen %q\n", bundle)
	scriptPath := filepath.Join(os.TempDir(), "aiko-resign-relaunch.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write relaunch script: %w", err)
	}
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start relaunch: %w", err)
	}
	return nil
}
```

注意：设置窗口使用 `URL: "/?settings=1"` 而不是独立路由，因为前端当前无 Vue Router，通过 query param 触发设置面板显示（前端改动在 Plan C 完成）。

- [ ] **Step 2: 提交**

```bash
git add main.go
git commit -m "feat: rewrite main.go for Wails v3"
```

---

## Task 6：更新 wails.json 和 Makefile

**Files:**
- Modify: `wails.json`
- Modify: `Makefile`

- [ ] **Step 1: 更新 wails.json**

将 `wails.json` 内容替换为：

```json
{
  "$schema": "https://wails.io/schemas/config.v3.json",
  "name": "Aiko",
  "outputfilename": "Aiko",
  "info": {
    "companyName": "xutiancheng",
    "productName": "Aiko",
    "productVersion": "1.0.52",
    "copyright": "Copyright © 2025 xutiancheng",
    "comments": "Aiko - AI Desktop Pet powered by Wails and eino"
  },
  "author": {
    "name": "xutiancheng",
    "email": "kiritoeva@icloud.com"
  },
  "frontend:dir": "frontend",
  "frontend:install": "yarn install",
  "frontend:build": "yarn build",
  "frontend:dev:watcher": "yarn dev",
  "frontend:dev:serverUrl": "auto",
  "wailsjsdir": "frontend/bindings",
  "mac": {
    "bundleIdentifier": "com.xutiancheng.aiko"
  }
}
```

- [ ] **Step 2: 更新 Makefile — 将 wails 命令替换为 wails3**

```bash
sed -i '' 's/\bwails build\b/wails3 build/g; s/\bwails dev\b/wails3 dev/g; s/wails generate module/wails3 generate bindings/g' Makefile
```

验证：

```bash
grep "wails" Makefile | grep -v "wails3\|wailsapp"
```

预期：无输出（旧 `wails` 命令已全部替换）。

- [ ] **Step 3: 提交**

```bash
git add wails.json Makefile
git commit -m "chore: update wails.json and Makefile for v3"
```

---

## Task 7：重新生成前端 bindings

**Files:**
- Delete: `frontend/wailsjs/`
- Create: `frontend/bindings/`（自动生成）

- [ ] **Step 1: 删除 v2 bindings**

```bash
rm -rf frontend/wailsjs
```

- [ ] **Step 2: 生成 v3 bindings**

```bash
wails3 generate bindings -f .
```

预期：在 `frontend/bindings/` 下生成 `App.js`、`App.d.ts` 和 `runtime/` 目录。

- [ ] **Step 3: 确认生成产物**

```bash
ls frontend/bindings/
```

预期输出类似：`github.com/  runtime/`（v3 按 Go module path 组织）。

- [ ] **Step 4: 提交**

```bash
git add frontend/bindings
git rm -r --cached frontend/wailsjs 2>/dev/null || true
git commit -m "chore: regenerate frontend bindings for Wails v3"
```

---

## Task 8：修复前端 import 路径

**Files:**
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/main.js`
- Modify: `frontend/src/components/*.vue`（所有 16 个含 wailsjs import 的文件）
- Modify: `frontend/src/composables/*.js`（含 wailsjs import 的文件）

v3 bindings 的 import 路径与 v2 不同。v2：
```js
import { Foo } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
```

v3（路径视 `wails3 generate bindings` 实际输出而定，通常为）：
```js
import { Foo } from '../../bindings/aiko/App'
import { EventsOn } from '@wailsio/runtime'
```

- [ ] **Step 1: 确认 v3 生成的 bindings 路径**

```bash
find frontend/bindings -name "*.js" | head -20
```

记录实际路径，后续步骤依此替换。

- [ ] **Step 2: 批量替换 go/main/App import 路径**

以实际路径为准，示例（假设路径为 `../../bindings/aiko/App`）：

```bash
find frontend/src -name "*.vue" -o -name "*.js" | xargs grep -l "wailsjs/go/main/App" | \
  xargs sed -i '' "s|../../wailsjs/go/main/App|../../bindings/aiko/App|g"
find frontend/src -name "*.vue" -o -name "*.js" | xargs grep -l "wailsjs/go/main/App" | \
  xargs sed -i '' "s|../wailsjs/go/main/App|../bindings/aiko/App|g"
```

- [ ] **Step 3: 替换 runtime import 路径**

```bash
find frontend/src -name "*.vue" -o -name "*.js" | xargs grep -l "wailsjs/runtime/runtime" | \
  xargs sed -i '' "s|../../wailsjs/runtime/runtime|@wailsio/runtime|g"
find frontend/src -name "*.vue" -o -name "*.js" | xargs grep -l "wailsjs/runtime/runtime" | \
  xargs sed -i '' "s|../wailsjs/runtime/runtime|@wailsio/runtime|g"
```

- [ ] **Step 4: 安装 @wailsio/runtime（若 v3 需要独立包）**

```bash
cd frontend && yarn add @wailsio/runtime
```

若提示找不到包，则 v3 runtime 已内嵌在 bindings，跳过此步。

- [ ] **Step 5: 验证无残留 wailsjs import**

```bash
grep -r "wailsjs" frontend/src/
```

预期：无输出。

- [ ] **Step 6: 提交**

```bash
git add frontend/src frontend/package.json frontend/yarn.lock 2>/dev/null
git commit -m "refactor: update frontend imports to Wails v3 bindings paths"
```

---

## Task 9：编译验证

- [ ] **Step 1: Go 编译检查**

```bash
go build ./...
```

预期：无错误输出。若有错误，根据错误信息修复残留的 v2 API 使用。

- [ ] **Step 2: 前端构建检查**

```bash
cd frontend && yarn build
```

预期：构建成功，无 import 错误。

- [ ] **Step 3: 开发模式启动验证**

```bash
wails3 dev
```

预期：应用启动，宠物窗口显示，聊天气泡可切换，无 panic。

- [ ] **Step 4: 提交（若 Step 1-3 有小修复）**

```bash
git add -A
git commit -m "fix: resolve remaining v2 API calls after v3 migration"
```

---

## 完成标准

- [ ] `go build ./...` 无错误
- [ ] `cd frontend && yarn build` 无错误
- [ ] `wails3 dev` 能启动，主窗口显示
- [ ] 菜单 Cmd+Shift+P 能切换聊天气泡
- [ ] 菜单 Cmd+, 能打开设置窗口
- [ ] 无 `wailsapp/wails/v2` 残留 import

---

## 注意事项

1. **v3 alpha API**：`v3.0.0-alpha.92` 是当前最新版。`application.MacBackdropTransparent` / `MacBackdropTranslucent` 等常量名以实际包内定义为准，编译报错时查看 `go doc github.com/wailsapp/wails/v3/pkg/application MacWindow`
2. **Screen API**：v3 的 `app.Screen.GetAll()` 返回 `[]application.Screen`，字段名可能与 v2 `Screen` 不同（v2 用 `.IsPrimary` / `.Size.Width`），实现时验证字段名
3. **设置窗口**：Plan A 使用 `URL: "/?settings=1"` 临时方案，Plan C 会改为独立路由
4. **前端 bindings 路径**：v3 `wails3 generate bindings` 的实际输出目录结构以运行结果为准，Task 8 的 sed 命令路径需按实际生成结果调整

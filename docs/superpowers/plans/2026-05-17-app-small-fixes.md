# app.go Small Safety Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three independent safety issues in `app.go`: unify HOME directory resolution via a cached `dataDir` field, make three config setter methods concurrency-safe, and replace `startup` panics with graceful degradation.

**Architecture:** All changes are confined to `app.go`. Two new fields are added to `App` (`dataDir string`, `startupErr string`). No public method signatures change. No tests are added — changes are equivalent refactors or defensive fixes verified by `-race` build and manual smoke test.

**Tech Stack:** Go, Wails v2 (`github.com/wailsapp/wails/v2`)

---

## File Map

| File | Changes |
|------|---------|
| `app.go` | Add `dataDir` + `startupErr` fields to `App`; update `startup`; update `domReady`; fix 3 setter methods; replace 4× `os.Getenv("HOME")` + 2× redundant `os.UserHomeDir()` calls |

---

### Task 1: Add `dataDir` and `startupErr` fields to `App`

**Files:**
- Modify: `app.go:56-92` (the `App` struct)

- [ ] **Step 1: Add the two new fields at the end of the struct**

  Open `app.go`. The `App` struct currently ends at line 92 with `pendingUpdateVersion string`. Add two fields immediately before the closing `}`:

  ```go
  	pendingUpdateVersion string             // non-empty when a successful update marker was found on startup
  	dataDir              string             // ~/.aiko; set once in startup, read-only thereafter
  	startupErr           string             // non-empty when startup failed; read in domReady to show notification
  }
  ```

  Neither field needs mutex protection: `dataDir` is written once before any goroutine starts, `startupErr` is written in `OnStartup` and read in `OnDomReady` — Wails guarantees these are called serially on the main thread.

- [ ] **Step 2: Verify the build compiles**

  ```bash
  go build ./...
  ```

  Expected: no errors.

- [ ] **Step 3: Commit**

  ```bash
  git add app.go
  git commit -m "refactor: add dataDir and startupErr fields to App struct"
  ```

---

### Task 2: Cache `dataDir` in `startup` and eliminate redundant `os.UserHomeDir()` calls

**Files:**
- Modify: `app.go` — `startup` (lines ~361–365), `initLLMComponents` (lines ~487–492), `rebuildAgentTools` (lines ~766–771)

- [ ] **Step 1: In `startup`, assign `a.dataDir` instead of a local variable**

  Find this block (around line 361):

  ```go
  	home, err := os.UserHomeDir()
  	if err != nil {
  		panic(fmt.Errorf("get home dir: %w", err))
  	}
  	dataDir := filepath.Join(home, ".aiko")
  ```

  The `panic` will be removed in Task 4. For now just add the assignment on the line after `dataDir :=`:

  ```go
  	home, err := os.UserHomeDir()
  	if err != nil {
  		panic(fmt.Errorf("get home dir: %w", err))
  	}
  	dataDir := filepath.Join(home, ".aiko")
  	a.dataDir = dataDir
  ```

- [ ] **Step 2: In `initLLMComponents`, replace the `os.UserHomeDir()` block with `a.dataDir`**

  Find this block (around line 487):

  ```go
  	home, err := os.UserHomeDir()
  	if err != nil {
  		return fmt.Errorf("get home dir: %w", err)
  	}
  	dataDir := filepath.Join(home, ".aiko")
  ```

  Replace it with:

  ```go
  	dataDir := a.dataDir
  ```

- [ ] **Step 3: In `rebuildAgentTools`, replace the `os.UserHomeDir()` block with `a.dataDir`**

  Find this block (around line 766):

  ```go
  	home, err := os.UserHomeDir()
  	if err != nil {
  		return fmt.Errorf("get home dir: %w", err)
  	}
  	dataDir := filepath.Join(home, ".aiko")
  ```

  Replace it with:

  ```go
  	dataDir := a.dataDir
  ```

- [ ] **Step 4: Verify build**

  ```bash
  go build ./...
  ```

  Expected: no errors.

- [ ] **Step 5: Commit**

  ```bash
  git add app.go
  git commit -m "refactor: cache dataDir in App struct, remove redundant UserHomeDir calls"
  ```

---

### Task 3: Replace `os.Getenv("HOME")` with `a.dataDir` in VRM methods

**Files:**
- Modify: `app.go` — `ListVRMModels` (~1738), `GetVRMPath` (~1765), `ImportVRMFile` (~1785), `DeleteVRMModel` (~1795)

- [ ] **Step 1: Fix `ListVRMModels`**

  Find (around line 1738):

  ```go
  	userDir := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm")
  ```

  Replace with:

  ```go
  	userDir := filepath.Join(a.dataDir, "vrm")
  ```

- [ ] **Step 2: Fix `GetVRMPath`**

  Find (around line 1765):

  ```go
  	userPath := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm", name)
  ```

  Replace with:

  ```go
  	userPath := filepath.Join(a.dataDir, "vrm", name)
  ```

- [ ] **Step 3: Fix `ImportVRMFile`**

  Find (around line 1785):

  ```go
  	userDir := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm")
  ```

  Replace with:

  ```go
  	userDir := filepath.Join(a.dataDir, "vrm")
  ```

- [ ] **Step 4: Fix `DeleteVRMModel`**

  Find (around line 1795):

  ```go
  	userPath := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm", filepath.Base(name))
  ```

  Replace with:

  ```go
  	userPath := filepath.Join(a.dataDir, "vrm", filepath.Base(name))
  ```

- [ ] **Step 5: Verify build**

  ```bash
  go build ./...
  ```

  Expected: no errors.

- [ ] **Step 6: Commit**

  ```bash
  git add app.go
  git commit -m "fix: replace os.Getenv(HOME) with a.dataDir in VRM methods"
  ```

---

### Task 4: Fix concurrency in `SetVoiceAutoSend`, `SetSoundsEnabled`, `SetTTSAutoPlay`

**Files:**
- Modify: `app.go` — `SetVoiceAutoSend` (~2132), `SetSoundsEnabled` (~2147), `SetTTSAutoPlay` (~2478)

The bug: all three methods write `a.cfg.xxx` under lock but call `configStore.Save(a.cfg)` outside the lock with the raw pointer. A concurrent `SaveConfig` call can mutate `*a.cfg` while `Save` is serialising it.

The fix: copy `*a.cfg` inside the lock, then save the copy outside.

- [ ] **Step 1: Fix `SetVoiceAutoSend`**

  Current code (lines 2132–2137):

  ```go
  func (a *App) SetVoiceAutoSend(enabled bool) error {
  	a.mu.Lock()
  	a.cfg.VoiceAutoSend = enabled
  	a.mu.Unlock()
  	return a.configStore.Save(a.cfg)
  }
  ```

  Replace with:

  ```go
  func (a *App) SetVoiceAutoSend(enabled bool) error {
  	a.mu.Lock()
  	a.cfg.VoiceAutoSend = enabled
  	cfgCopy := *a.cfg
  	a.mu.Unlock()
  	return a.configStore.Save(&cfgCopy)
  }
  ```

- [ ] **Step 2: Fix `SetSoundsEnabled`**

  Current code (lines 2147–2152):

  ```go
  func (a *App) SetSoundsEnabled(enabled bool) error {
  	a.mu.Lock()
  	a.cfg.SoundsEnabled = enabled
  	a.mu.Unlock()
  	return a.configStore.Save(a.cfg)
  }
  ```

  Replace with:

  ```go
  func (a *App) SetSoundsEnabled(enabled bool) error {
  	a.mu.Lock()
  	a.cfg.SoundsEnabled = enabled
  	cfgCopy := *a.cfg
  	a.mu.Unlock()
  	return a.configStore.Save(&cfgCopy)
  }
  ```

- [ ] **Step 3: Fix `SetTTSAutoPlay`**

  Current code (lines 2478–2481). Note: this method has **no lock at all** currently — it writes `a.cfg.TTSAutoPlay` without holding `mu`:

  ```go
  func (a *App) SetTTSAutoPlay(enabled bool) error {
  	a.cfg.TTSAutoPlay = enabled
  	return a.configStore.Save(a.cfg)
  }
  ```

  Replace with:

  ```go
  func (a *App) SetTTSAutoPlay(enabled bool) error {
  	a.mu.Lock()
  	a.cfg.TTSAutoPlay = enabled
  	cfgCopy := *a.cfg
  	a.mu.Unlock()
  	return a.configStore.Save(&cfgCopy)
  }
  ```

- [ ] **Step 4: Verify race-free build**

  ```bash
  go build -race ./...
  ```

  Expected: no errors, no data race warnings at build time.

- [ ] **Step 5: Commit**

  ```bash
  git add app.go
  git commit -m "fix: make SetVoiceAutoSend/SetSoundsEnabled/SetTTSAutoPlay concurrency-safe"
  ```

---

### Task 5: Replace `startup` panics with graceful degradation

**Files:**
- Modify: `app.go` — `startup` (~361–422), `domReady` (~1978–1992)

- [ ] **Step 1: Replace the `home` / `dataDir` panic in `startup`**

  Find (around line 361):

  ```go
  	home, err := os.UserHomeDir()
  	if err != nil {
  		panic(fmt.Errorf("get home dir: %w", err))
  	}
  	dataDir := filepath.Join(home, ".aiko")
  	a.dataDir = dataDir
  ```

  Replace with:

  ```go
  	home, err := os.UserHomeDir()
  	if err != nil {
  		a.startupErr = fmt.Sprintf("无法获取用户目录: %v", err)
  		slog.Error("startup: get home dir failed", "err", err)
  		return
  	}
  	dataDir := filepath.Join(home, ".aiko")
  	a.dataDir = dataDir
  ```

- [ ] **Step 2: Replace the `db.Open` panic**

  Find (around line 367):

  ```go
  	a.sqlDB, err = db.Open(dataDir)
  	if err != nil {
  		panic(err)
  	}
  ```

  Replace with:

  ```go
  	a.sqlDB, err = db.Open(dataDir)
  	if err != nil {
  		a.startupErr = fmt.Sprintf("数据库初始化失败: %v", err)
  		slog.Error("startup: db open failed", "err", err)
  		return
  	}
  ```

- [ ] **Step 3: Replace the `configStore.Load` panic**

  Find (around line 372):

  ```go
  	a.cfg, err = a.configStore.Load()
  	if err != nil {
  		panic(err)
  	}
  ```

  Replace with:

  ```go
  	a.cfg, err = a.configStore.Load()
  	if err != nil {
  		a.startupErr = fmt.Sprintf("配置加载失败: %v", err)
  		slog.Error("startup: config load failed", "err", err)
  		return
  	}
  ```

- [ ] **Step 4: Replace the `chromem.NewPersistentDB` panic**

  Find (around line 419):

  ```go
  	a.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
  	if err != nil {
  		panic(err)
  	}
  ```

  Replace with:

  ```go
  	a.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
  	if err != nil {
  		a.startupErr = fmt.Sprintf("向量库初始化失败: %v", err)
  		slog.Error("startup: vectordb open failed", "err", err)
  		return
  	}
  ```

- [ ] **Step 5: Emit the error notification in `domReady`**

  The current `domReady` ends at line 1992:

  ```go
  func (a *App) domReady(_ context.Context) {
  	requestPermissionsEarly()
  	hideNativeScrollbars()

  	// Emit update-success notification if a marker was found during startup.
  	if a.pendingUpdateVersion != "" {
  		wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
  			"title":   "✅ 更新成功",
  			"message": "Aiko 已更新至 v" + a.pendingUpdateVersion,
  		})
  		a.pendingUpdateVersion = ""
  	}
  }
  ```

  Add the startup-error check **after** the pendingUpdateVersion block:

  ```go
  func (a *App) domReady(_ context.Context) {
  	requestPermissionsEarly()
  	hideNativeScrollbars()

  	// Emit update-success notification if a marker was found during startup.
  	if a.pendingUpdateVersion != "" {
  		wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
  			"title":   "✅ 更新成功",
  			"message": "Aiko 已更新至 v" + a.pendingUpdateVersion,
  		})
  		a.pendingUpdateVersion = ""
  	}

  	// Show startup failure notification if startup() encountered a fatal error.
  	if a.startupErr != "" {
  		wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
  			"title":   "❌ 启动失败",
  			"message": a.startupErr,
  		})
  	}
  }
  ```

- [ ] **Step 6: Verify build**

  ```bash
  go build ./...
  ```

  Expected: no errors.

- [ ] **Step 7: Commit**

  ```bash
  git add app.go
  git commit -m "fix: replace startup panics with graceful degradation via domReady notification"
  ```

---

### Task 6: Final verification

**Files:** none modified

- [ ] **Step 1: Race-detector build**

  ```bash
  go build -race ./...
  ```

  Expected: exits 0 with no warnings.

- [ ] **Step 2: Smoke test**

  ```bash
  make run
  ```

  - App launches normally
  - VRM model list loads (Settings → Pet Model)
  - Toggle any setting that calls `SetSoundsEnabled` or `SetVoiceAutoSend` — no crash, value persists across restart
  - Confirm no regression in chat functionality

- [ ] **Step 3: Verify no remaining `os.Getenv("HOME")` in VRM paths**

  ```bash
  grep -n 'os\.Getenv("HOME")' app.go
  ```

  Expected output: only lines unrelated to VRM (e.g. the keychain path in the codesign helper — those are intentional and should be left alone).

- [ ] **Step 4: Verify no remaining bare `panic` in `startup`**

  ```bash
  grep -n 'panic(' app.go
  ```

  Expected: zero results inside the `startup` function body.

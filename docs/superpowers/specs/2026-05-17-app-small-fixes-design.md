# 子项目 1：app.go 小型安全修复

**日期：** 2026-05-17  
**范围：** `app.go`（少量涉及 `main.go`）  
**目标：** 零行为变更的等价重构或 bug 修复，不触碰任何业务逻辑。

---

## 背景

`app.go` 中存在三类低风险但真实存在的问题，需要在后续更大规模重构（SendMessage 提取、app.go 拆分）之前先修复，以降低后续改动的基准噪声。

---

## Fix 1：统一 HOME 获取 + 缓存 `dataDir`

### 问题

- `ListVRMModels`、`GetVRMPath`、`ImportVRMFile`、`DeleteVRMModel` 使用 `os.Getenv("HOME")` 获取家目录
- 其余地方（`startup`、`initLLMComponents`、`rebuildAgentTools`）使用 `os.UserHomeDir()`
- Wails 应用启动时 `HOME` 环境变量可能未被正确继承（尤其是通过 Finder/Dock 启动）
- `initLLMComponents` 和 `rebuildAgentTools` 中各自重新计算 `dataDir`，重复调用系统 API

### 方案

1. 在 `App` 结构体新增字段：
   ```go
   dataDir string // ~/.aiko，在 startup 中初始化
   ```

2. 在 `startup` 中赋值（替换已有的局部变量 `dataDir`）：
   ```go
   a.dataDir = filepath.Join(home, ".aiko")
   ```

3. 替换以下位置的 `os.Getenv("HOME")` 和重复的 `os.UserHomeDir()` 调用：
   - `ListVRMModels`（line ~1738）
   - `GetVRMPath`（line ~1765）
   - `ImportVRMFile`（line ~1785）
   - `DeleteVRMModel`（line ~1795）
   - `initLLMComponents` 中的 `home, err := os.UserHomeDir()` 块（line ~487）
   - `rebuildAgentTools` 中的 `home, err := os.UserHomeDir()` 块（line ~766）

### 约束

- `dataDir` 字段无需加锁：仅在 `startup` 初始化，之后只读
- `main.go` 中的 `upgradeSignatureAndRelaunch` 不涉及 `App` 结构体，保持不变

---

## Fix 2：`SetVoiceAutoSend` / `SetSoundsEnabled` / `SetTTSAutoPlay` 并发安全

### 问题

三个方法当前实现（以 `SetVoiceAutoSend` 为例）：

```go
func (a *App) SetVoiceAutoSend(enabled bool) error {
    a.mu.Lock()
    a.cfg.VoiceAutoSend = enabled
    a.mu.Unlock()
    return a.configStore.Save(a.cfg)  // 锁外读 a.cfg 指针，数据竞争
}
```

`configStore.Save(a.cfg)` 在锁外调用，而 `a.cfg` 是指针，`SaveConfig` 可能并发修改其内容，导致 Save 写入不一致的 config 快照。

### 方案

统一采用"锁内修改 + 拷贝，锁外 Save 拷贝"模式，与 `SaveConfig` 保持一致：

```go
func (a *App) SetVoiceAutoSend(enabled bool) error {
    a.mu.Lock()
    a.cfg.VoiceAutoSend = enabled
    cfgCopy := *a.cfg
    a.mu.Unlock()
    return a.configStore.Save(&cfgCopy)
}

func (a *App) SetSoundsEnabled(enabled bool) error {
    a.mu.Lock()
    a.cfg.SoundsEnabled = enabled
    cfgCopy := *a.cfg
    a.mu.Unlock()
    return a.configStore.Save(&cfgCopy)
}

func (a *App) SetTTSAutoPlay(enabled bool) error {
    a.mu.Lock()
    a.cfg.TTSAutoPlay = enabled
    cfgCopy := *a.cfg
    a.mu.Unlock()
    return a.configStore.Save(&cfgCopy)
}
```

### 不做的事

- 不在这三个方法中触发 `initLLMComponents`（当前行为：不触发；维持不变）
- 不合并这三个方法（保持 Wails 绑定接口不变）

---

## Fix 3：`startup` panic → 优雅降级

### 问题

`startup` 中三处 `panic`（数据库打开失败、配置加载失败、向量库打开失败），Wails 不捕获 `OnStartup` 中的 panic，应用直接崩溃且用户看不到任何提示。

### 方案

**新增字段：**
```go
startupErr string // 启动失败原因；只在 startup 写、domReady 读，无需加锁
```

**startup 中改 panic 为记录：**
```go
a.sqlDB, err = db.Open(a.dataDir)
if err != nil {
    a.startupErr = fmt.Sprintf("数据库初始化失败: %v", err)
    slog.Error("startup: db open failed", "err", err)
    return
}

a.cfg, err = a.configStore.Load()
if err != nil {
    a.startupErr = fmt.Sprintf("配置加载失败: %v", err)
    slog.Error("startup: config load failed", "err", err)
    return
}

a.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
if err != nil {
    a.startupErr = fmt.Sprintf("向量库初始化失败: %v", err)
    slog.Error("startup: vectordb open failed", "err", err)
    return
}
```

**domReady 中展示错误（追加到已有逻辑之后）：**
```go
if a.startupErr != "" {
    wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
        "title":   "❌ 启动失败",
        "message": a.startupErr,
    })
}
```

### 约束

- `startupErr` 不加锁：Wails 保证 `OnStartup` → `OnDomReady` 串行调用，写读不并发
- 不改 `main.go` 中 `panic(err)` 的处理（Wails `Run` 失败属于框架级错误，panic 合理）
- 启动失败后应用处于部分初始化状态，前端已有配置检测逻辑（`MissingRequiredConfig`）可兜底引导用户

---

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `app.go` | 修改 | 新增 `dataDir`、`startupErr` 字段；修改 startup/domReady；修改 3 个 setter；替换 HOME 获取 |

## 测试策略

- 无新增测试（改动全为等价重构或防御性修复）
- 人工验证：`make run` 启动应用，确认 VRM 列表正常加载，配置保存正常
- 并发验证：通过 `-race` flag 运行 `go build -race ./...` 确认无数据竞争报告

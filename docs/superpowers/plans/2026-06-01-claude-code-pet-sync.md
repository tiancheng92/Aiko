# Claude Code 宠物状态同步 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Aiko 内置 HTTP server 接收 Claude Code hook 事件，同步宠物 thinking/idle 状态并弹出通知气泡。

**Architecture:** 新增 `internal/claudecco/` 包实现轻量 HTTP server，监听 `127.0.0.1`；接收 `POST /event` 后通过现有 Wails 事件系统驱动宠物状态和通知。配置端新增 3 个字段（enabled/port/notificationSecs），SettingsWindow 新增一个设置标签页。

**Tech Stack:** Go `net/http`、Vue 3 Composition API、现有 Wails 事件系统

---

### Task 1: Config 结构体 + Store 新增字段

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Config 结构体新增三个字段**

在 `Config` 结构体末尾（`Language string` 之后）添加：

```go
// ClaudeCodeEnabled controls whether the built-in HTTP server listens for
// Claude Code hook events to sync pet state.
ClaudeCodeEnabled bool `json:"claude_code_enabled"`

// ClaudeCodePort is the TCP port for the Claude Code hook HTTP server.
// Default 9876. Only effective when ClaudeCodeEnabled is true.
ClaudeCodePort int `json:"claude_code_port"`

// ClaudeCodeNotificationSecs controls how long (in seconds) the
// Claude Code notification bubble stays visible. Default 15.
ClaudeCodeNotificationSecs int `json:"claude_code_notification_secs"`
```

- [ ] **Step 2: Load() 方法新增字段默认值**

在 `Load()` 的 `cfg := &Config{...}` 块末尾、`return cfg, nil` 之前添加：

```go
cfg.ClaudeCodeEnabled = m["claude_code_enabled"] == "true"
cfg.ClaudeCodePort = parseInt(m["claude_code_port"], 9876)
cfg.ClaudeCodeNotificationSecs = parseInt(m["claude_code_notification_secs"], 15)
```

- [ ] **Step 3: Save() 方法的 pairs map 新增字段**

在 `Save()` 的 `pairs := map[string]string{...}` 块末尾、`"language": ...` 之后添加：

```go
"claude_code_enabled":            strconv.FormatBool(cfg.ClaudeCodeEnabled),
"claude_code_port":               strconv.Itoa(cfg.ClaudeCodePort),
"claude_code_notification_secs":  strconv.Itoa(cfg.ClaudeCodeNotificationSecs),
```

- [ ] **Step 4: 编译验证**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add ClaudeCodeEnabled, ClaudeCodePort, ClaudeCodeNotificationSecs fields"
```

---

### Task 2: Claude Code HTTP Server

**Files:**
- Create: `internal/claudecco/server.go`

- [ ] **Step 1: 创建 server.go**

```go
// Package claudecco provides an HTTP server that receives Claude Code hook
// events and synchronizes pet state via Wails events.
package claudecco

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Event represents an incoming Claude Code hook event.
type Event struct {
	Event   string `json:"event"`   // "thinking" or "done"
	Summary string `json:"summary"` // only for "done"
}

// Emitter is the interface for emitting Wails events.
type Emitter func(event string, data any)

// Config provides the current Claude Code settings.
type Config struct {
	Port             int
	NotificationSecs int
}

// Server listens for Claude Code hook HTTP requests and emits pet state events.
type Server struct {
	cfg    Config
	emit   Emitter
	srv    *http.Server
	mu     sync.Mutex
	debounce *time.Timer
}

// New creates a new Server. The emitter is called to send Wails events.
func New(cfg Config, emit Emitter) *Server {
	return &Server{cfg: cfg, emit: emit}
}

// Start begins listening on 127.0.0.1:<port>. Returns an error if the port is
// already in use.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv != nil {
		s.srv.Close()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/event", s.handleEvent)

	s.srv = &http.Server{
		Addr:    net.JoinHostPort("127.0.0.1", itoa(s.cfg.Port)),
		Handler: mux,
	}

	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Warn().Err(err).Msg("claudecco: server error")
		}
	}()

	log.Info().Str("addr", s.srv.Addr).Msg("claudecco: server started")
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.debounce != nil {
		s.debounce.Stop()
		s.debounce = nil
	}
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
		s.srv = nil
		log.Info().Msg("claudecco: server stopped")
	}
}

// UpdateConfig applies new settings. If the port changed, the server is
// restarted.
func (s *Server) UpdateConfig(cfg Config) error {
	s.cfg = cfg
	if s.srv != nil {
		return s.Start() // restarts with new port
	}
	return nil
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var evt Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Debug().Str("event", evt.Event).Str("summary", evt.Summary).Msg("claudecco: received event")

	switch evt.Event {
	case "thinking":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
		}
		s.debounce = time.AfterFunc(3*time.Second, func() {
			s.emit("pet:state:change", "thinking")
		})
		s.mu.Unlock()
	case "done":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
			s.debounce = nil
		}
		s.mu.Unlock()
		s.emit("pet:state:change", "idle")
		summary := evt.Summary
		if summary == "" {
			summary = "Claude Code 已完成"
		}
		s.emit("notification:show", map[string]any{
			"title":        "Claude Code",
			"message":      summary,
			"durationSecs": s.cfg.NotificationSecs,
		})
	default:
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func itoa(i int) string {
	if i == 0 {
		return "9876"
	}
	return formatInt(i)
}

// formatInt is a minimal int→string helper that avoids importing strconv
// at the call site (keeps the file self-contained for clarity).
func formatInt(i int) string {
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}
```

Wait — `formatInt` 会造成 import cycle 错觉，Go 标准库的 `strconv` 没有这个问题。改用 `strconv.Itoa`。

修正版 server.go：

```go
package claudecco

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Event represents an incoming Claude Code hook event.
type Event struct {
	Event   string `json:"event"`
	Summary string `json:"summary,omitempty"`
}

// Emitter is the interface for emitting Wails events.
type Emitter func(event string, data any)

// Config provides the current Claude Code settings.
type Config struct {
	Port             int
	NotificationSecs int
}

// Server listens for Claude Code hook HTTP requests and emits pet state events.
type Server struct {
	cfg      Config
	emit     Emitter
	srv      *http.Server
	mu       sync.Mutex
	debounce *time.Timer
}

// New creates a new Server.
func New(cfg Config, emit Emitter) *Server {
	return &Server{cfg: cfg, emit: emit}
}

// Start begins listening on 127.0.0.1:<port>.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv != nil {
		s.srv.Close()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/event", s.handleEvent)

	s.srv = &http.Server{
		Addr:    net.JoinHostPort("127.0.0.1", strconv.Itoa(s.cfg.Port)),
		Handler: mux,
	}

	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Warn().Err(err).Msg("claudecco: server error")
		}
	}()

	log.Info().Str("addr", s.srv.Addr).Msg("claudecco: server started")
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.debounce != nil {
		s.debounce.Stop()
		s.debounce = nil
	}
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
		s.srv = nil
		log.Info().Msg("claudecco: server stopped")
	}
}

// UpdateConfig applies new settings. Restarts the server if the port changed.
func (s *Server) UpdateConfig(cfg Config) error {
	s.cfg = cfg
	if s.srv != nil {
		return s.Start()
	}
	return nil
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var evt Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Debug().Str("event", evt.Event).Str("summary", evt.Summary).Msg("claudecco: received event")

	switch evt.Event {
	case "thinking":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
		}
		s.debounce = time.AfterFunc(3*time.Second, func() {
			s.emit("pet:state:change", "thinking")
		})
		s.mu.Unlock()
	case "done":
		s.mu.Lock()
		if s.debounce != nil {
			s.debounce.Stop()
			s.debounce = nil
		}
		s.mu.Unlock()
		s.emit("pet:state:change", "idle")
		summary := evt.Summary
		if summary == "" {
			summary = "Claude Code 已完成"
		}
		s.emit("notification:show", map[string]any{
			"title":        "Claude Code",
			"message":      summary,
			"durationSecs": s.cfg.NotificationSecs,
		})
	default:
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/claudecco/server.go
git commit -m "feat(claudecco): add HTTP server for Claude Code hook events"
```

---

### Task 3: App 生命周期集成

**Files:**
- Modify: `app.go`

- [ ] **Step 1: 添加 import 和字段**

在 import 块中添加：
```go
"aiko/internal/claudecco"
```

在 `App` 结构体中添加字段（放在 `startupErr` 之后）：
```go
claudeccoServer *claudecco.Server // Claude Code hook HTTP server; guarded by mu
```

- [ ] **Step 2: startup() 中启动 server**

在 `startup()` 函数末尾、`domReady` 注册之前（`startSMSWatcher` 之后），添加：

```go
	// Start Claude Code hook HTTP server if enabled.
	if a.cfg.ClaudeCodeEnabled {
		ccCfg := claudecco.Config{
			Port:             a.cfg.ClaudeCodePort,
			NotificationSecs: a.cfg.ClaudeCodeNotificationSecs,
		}
		ccSrv := claudecco.New(ccCfg, func(event string, data any) {
			wailsruntime.EventsEmit(a.ctx, event, data)
		})
		if err := ccSrv.Start(); err != nil {
			log.Warn().Err(err).Msg("claudecco: server start failed")
		} else {
			a.claudeccoServer = ccSrv
		}
	}
```

- [ ] **Step 3: shutdown() 中停止 server**

在 `shutdown()` 中，`engine.Stop()` 之后、`mcpClosers` close 之前添加：

```go
	if a.claudeccoServer != nil {
		a.claudeccoServer.Stop()
	}
```

- [ ] **Step 4: 编译验证**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add app.go
git commit -m "feat(app): integrate Claude Code HTTP server lifecycle"
```

---

### Task 4: SaveConfig 中检测并重启 Server

**Files:**
- Modify: `app_config.go`

- [ ] **Step 1: 在 SaveConfig 中添加 server 重启逻辑**

在 `SaveConfig()` 中，pomodoro engine 更新和 `go func() { initLLMComponents...` 之间，添加：

```go
	// Restart Claude Code HTTP server if enabled/port changed.
	a.mu.Lock()
	ccSrv := a.claudeccoServer
	a.mu.Unlock()
	if cfg.ClaudeCodeEnabled {
		ccCfg := claudecco.Config{
			Port:             cfg.ClaudeCodePort,
			NotificationSecs: cfg.ClaudeCodeNotificationSecs,
		}
		if ccSrv == nil {
			ccSrv = claudecco.New(ccCfg, func(event string, data any) {
				wailsruntime.EventsEmit(a.ctx, event, data)
			})
			if err := ccSrv.Start(); err != nil {
				log.Warn().Err(err).Msg("claudecco: server start failed")
			} else {
				a.mu.Lock()
				a.claudeccoServer = ccSrv
				a.mu.Unlock()
			}
		} else {
			if err := ccSrv.UpdateConfig(ccCfg); err != nil {
				log.Warn().Err(err).Msg("claudecco: server restart failed")
			}
		}
	} else {
		if ccSrv != nil {
			ccSrv.Stop()
			a.mu.Lock()
			a.claudeccoServer = nil
			a.mu.Unlock()
		}
	}
```

需要添加 import：
```go
"aiko/internal/claudecco"
```

- [ ] **Step 2: 编译验证**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add app_config.go
git commit -m "feat(config): restart Claude Code server on SaveConfig"
```

---

### Task 5: NotificationBubble 支持动态时长 + 内容替换

**Files:**
- Modify: `frontend/src/components/NotificationBubble.vue`

- [ ] **Step 1: 修改 scheduleDismiss 支持自定义时长**

将第 22 行的常量改为可被覆盖：

```js
const DEFAULT_DISMISS_MS = 15000

/** scheduleDismiss arms (or re-arms) the auto-dismiss timer. */
function scheduleDismiss(durationMs) {
  if (hideTimer) clearTimeout(hideTimer)
  hideTimer = setTimeout(dismiss, durationMs || DEFAULT_DISMISS_MS)
}
```

- [ ] **Step 2: 修改 onMounted 监听逻辑**

将第 64-72 行的 `onMounted` 改为：

```js
onMounted(() => {
  offShow = EventsOn('notification:show', (data) => {
    // Replace current notification content and reset timer (no stacking).
    notification.value = {
      title: data.title || t('notification.fallbackTitle'),
      message: data.message,
      ts: new Date(),
    }
    nextTick(() => {
      if (bubbleEl.value) bubbleH.value = bubbleEl.value.offsetHeight
    })
    const durationMs = data.durationSecs ? data.durationSecs * 1000 : DEFAULT_DISMISS_MS
    scheduleDismiss(durationMs)
  })
})
```

- [ ] **Step 3: 编译验证**

```bash
cd frontend && yarn build
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/NotificationBubble.vue
git commit -m "feat(notification): support durationSecs override and content replacement"
```

---

### Task 6: SettingsWindow 新增 Claude Code 标签页

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`
- Modify: `frontend/src/utils/icons.js`
- Modify: `frontend/src/styles/tokens.css`

- [ ] **Step 1: 添加 tab 图标和颜色**

在 `icons.js` 末尾添加：
```js
export const ICON_TAB_CLAUDE_CODE = SVG('<path d="M12 2a4 4 0 0 1 4 4v1h2a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2h2V6a4 4 0 0 1 4-4zm0 2a2 2 0 0 0-2 2v1h4V6a2 2 0 0 0-2-2z"/><circle cx="12" cy="14" r="1.5" fill="currentColor"/><path d="M11 10h2v3h-2z"/><path d="M8 9h8" stroke="currentColor" stroke-width="1.2" fill="none"/>')
```

在 `tokens.css` 末尾添加：
```css
--cat-claude-code: #D97706;
```

- [ ] **Step 2: 在 tabMeta 中添加新标签**

在 `tabMeta` 数组中，`about` 之前添加：

```js
  { id: 'claudeCode', label: 'Claude Code', iconSvg: ICON_TAB_CLAUDE_CODE, iconBg: 'var(--cat-claude-code)',
    keywords: 'claude code hook sync pet 同步 状态 端口 气泡 通知' },
```

- [ ] **Step 3: 添加标签页内容 (template)**

在 `about` tab pane 之前添加。注意：`watch(cfg, debouncedSave, { deep: true })` (line 766) 会自动触发持久化，所以 numeric 字段直接用 `v-model.number` 即可，不用手动 `@change`。

```html
	        <!-- Claude Code -->
	        <div v-if="activeTab === 'claudeCode'" class="tab-pane">
	          <div class="group-label">{{ $t('claudeCode.title') }}</div>
	          <div class="settings-group">
	            <div class="settings-row">
	              <div class="row-body">
	                <div class="row-title">{{ $t('claudeCode.enabled') }}</div>
	                <div class="row-desc">{{ $t('claudeCode.enabledDesc') }}</div>
	              </div>
	              <label class="toggle">
	                <input type="checkbox" v-model="cfg.ClaudeCodeEnabled" @change="debouncedSaveFlush" />
	                <span class="toggle-track" />
	              </label>
	            </div>
	            <div class="settings-row">
	              <div class="row-body">
	                <div class="row-title">{{ $t('claudeCode.port') }}</div>
	                <div class="row-desc">{{ $t('claudeCode.portDesc') }}</div>
	              </div>
	              <div class="row-ctrl">
	                <input
	                  type="number"
	                  class="vrm-input"
	                  style="width:80px"
	                  v-model.number="cfg.ClaudeCodePort"
	                  :disabled="!cfg.ClaudeCodeEnabled"
	                  min="1024" max="65535"
	                />
	              </div>
	            </div>
	            <div class="settings-row">
	              <div class="row-body">
	                <div class="row-title">{{ $t('claudeCode.notificationSecs') }}</div>
	                <div class="row-desc">{{ $t('claudeCode.notificationSecsDesc') }}</div>
	              </div>
	              <div class="row-ctrl">
	                <input
	                  type="number"
	                  class="vrm-input"
	                  style="width:80px"
	                  v-model.number="cfg.ClaudeCodeNotificationSecs"
	                  :disabled="!cfg.ClaudeCodeEnabled"
	                  min="5" max="120"
	                />
	              </div>
	            </div>
	          </div>

	          <!-- Hook 配置展示区 -->
	          <div class="group-label">{{ $t('claudeCode.hookConfig') }}</div>
	          <div class="settings-group">
	            <div class="settings-row" style="flex-direction:column;align-items:stretch;gap:8px">
	              <div class="row-desc">{{ $t('claudeCode.hookConfigHint') }}</div>
	              <pre class="hook-config-snippet">{{ claudeCodeHookSnippet }}</pre>
	              <button class="btn-on-sm" @click="copyClaudeCodeHook" style="align-self:flex-end">
	                {{ claudeCodeCopyLabel }}
	              </button>
	            </div>
	          </div>
	        </div>
```

`debouncedSaveFlush` 是一个新增的函数，强制立即保存（用于 enabled 开关这类需要立即重启 server 的操作）：

```js
/** debouncedSaveFlush cancels any pending debounce and saves immediately. */
function debouncedSaveFlush() {
  clearTimeout(saveTimer)
  save()
}
```

- [ ] **Step 4: 添加 script 逻辑**

在 `<script setup>` 中添加 computed 和方法（在 `const activeTab` 附近）：

```js
const claudeCodeHookSnippet = computed(() => {
  const port = cfg.value.ClaudeCodePort || 9876
  return JSON.stringify({
    hooks: {
      PreToolUse: [{
        matcher: "",
        hooks: [{
          type: "http",
          url: `http://127.0.0.1:${port}/event`,
          method: "POST",
          body: "{\"event\":\"thinking\"}"
        }]
      }],
      Stop: [{
        matcher: "",
        hooks: [{
          type: "http",
          url: `http://127.0.0.1:${port}/event`,
          method: "POST",
          body: "{\"event\":\"done\",\"summary\":\"Claude Code 已完成\"}"
        }]
      }]
    }
  }, null, 2)
})

const claudeCodeCopyLabel = ref(t('claudeCode.copy'))

/** debouncedSaveFlush cancels pending debounce and saves immediately.
 *  Used for settings that need instant server restart (e.g. enabled toggle). */
function debouncedSaveFlush() {
  clearTimeout(saveTimer)
  save()
}

async function copyClaudeCodeHook() {
  try {
    await navigator.clipboard.writeText(claudeCodeHookSnippet.value)
    claudeCodeCopyLabel.value = t('claudeCode.copied')
    setTimeout(() => { claudeCodeCopyLabel.value = t('claudeCode.copy') }, 2000)
  } catch {
    // fallback — clipboard may not be available
  }
}
```

- [ ] **Step 5: 添加样式**

在 `<style scoped>` 中添加：

```css
.hook-config-snippet {
  background: var(--bg-tertiary);
  border: 1px solid var(--lg-border);
  border-radius: 8px;
  padding: 12px;
  font-size: 11px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
  color: var(--text-secondary);
}
```

- [ ] **Step 6: 编译验证**

```bash
cd frontend && yarn build
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue frontend/src/utils/icons.js frontend/src/styles/tokens.css
git commit -m "feat(settings): add Claude Code sync settings tab"
```

---

### Task 7: 国际化文案

**Files:**
- Modify: `frontend/src/locales/zh-CN.json`
- Modify: `frontend/src/locales/en.json`
- Modify: `frontend/src/locales/ja.json`
- Modify: `frontend/src/locales/ko.json`

- [ ] **Step 1: 添加中文文案**

在 `zh-CN.json` 中添加：

```json
  "claudeCode": {
    "title": "Claude Code 同步",
    "enabled": "启用状态同步",
    "enabledDesc": "Claude Code 工作时宠物自动切换思考和通知状态",
    "port": "监听端口",
    "portDesc": "HTTP 服务器监听端口，需与 Claude Code hook 配置一致",
    "notificationSecs": "气泡持续时间（秒）",
    "notificationSecsDesc": "Claude Code 完成时通知气泡的显示时长",
    "hookConfig": "Hook 配置",
    "hookConfigHint": "将以下配置合并到 ~/.claude/settings.json 的 \"hooks\" 字段中",
    "copy": "复制",
    "copied": "已复制到剪贴板"
  },
```

同时在 `settings.tabs` 中添加：
```json
    "claudeCode": "Claude Code"
```

- [ ] **Step 2: 添加英文文案**

在 `en.json` 中添加对应英文翻译。

- [ ] **Step 3: 添加日文/韩文文案**

在 `ja.json` 和 `ko.json` 中添加对应翻译（可使用英文作为 fallback，或添加对应译文）。

- [ ] **Step 4: 编译验证**

```bash
cd frontend && yarn build
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/locales/
git commit -m "feat(i18n): add Claude Code sync locale strings"
```

---

### Task 8: 端到端验证

- [ ] **Step 1: 构建应用**

```bash
make build
```

- [ ] **Step 2: 启动应用**

```bash
make run
```

- [ ] **Step 3: 测试 HTTP endpoint**

```bash
# 测试 thinking 事件
curl -X POST http://127.0.0.1:9876/event -H 'Content-Type: application/json' -d '{"event":"thinking"}'

# 等待 3 秒，观察宠物是否进入 thinking 状态

# 测试 done 事件
curl -X POST http://127.0.0.1:9876/event -H 'Content-Type: application/json' -d '{"event":"done","summary":"任务完成"}'

# 观察宠物是否恢复 idle + 弹出通知气泡
```

- [ ] **Step 4: 测试设置界面**

- 打开设置 → Claude Code 标签页
- 修改端口号 → 保存 → 用新端口 curl 测试
- 禁用开关 → 保存 → 用旧端口 curl 应连接被拒
- 点击"复制"按钮 → 粘贴验证 hook 配置 JSON

- [ ] **Step 5: Commit (如有修复)**

```bash
git add -A
git commit -m "chore: end-to-end fixes for Claude Code sync"
```

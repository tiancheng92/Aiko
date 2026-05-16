//go:build darwin

// Package services provides the split service types for the Aiko Wails v3 app.
// Each service holds a reference to the shared state that was previously contained
// in the monolithic App struct in app.go. This file defines sharedState — the
// single owner of all mutable application state — and the lifecycle methods
// startup/shutdown/initLLMComponents/rebuildAgentTools that were formerly on App.
package services

import (
	"bytes"
	"context"
	"database/sql"
	stdjson "encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/philippgille/chromem-go"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"aiko/internal/agent"
	"aiko/internal/agent/middleware"
	"aiko/internal/config"
	"aiko/internal/db"
	"aiko/internal/execenv"
	internaltools "aiko/internal/tools"
	"aiko/internal/knowledge"
	"aiko/internal/llm"
	"aiko/internal/mcp"
	"aiko/internal/memory"
	"aiko/internal/notify"
	"aiko/internal/proactive"
	"aiko/internal/scheduler"
	"aiko/internal/skill"
	"aiko/internal/sms"
	"aiko/internal/tts"
)

// ScreenInfo holds the logical resolution of a screen.
type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ScreenFrame holds the macOS frame of a single screen as returned by platform hooks.
type ScreenFrame struct {
	OriginX float64
	OriginY float64
	Width   float64
	Height  float64
	Valid   bool
}

// PlatformHooks holds callbacks for platform-specific (CGO/main-package) operations
// that cannot be called directly from an internal package.
type PlatformHooks struct {
	// Notification
	PostSystemNotification func(title, body string)
	RequestNotificationAuthorization func()
	// Window
	EnableClickThrough     func()
	RegisterGlobalHotkey   func()
	RequestPermissionsEarly func()
	HideNativeScrollbars   func()
	// Screen
	GetMouseX      func() float64
	GetMouseY      func() float64
	GetNumScreens  func() int
	GetScreenFrame func(n int) ScreenFrame
	MoveWindowToScreen func(n int)
	RegisterSystemWakeObserver func(onWake func())
	// AutoLaunch
	GetAutoLaunch func() bool
	SetAutoLaunch func(bool)
}

// sharedState holds all mutable application state that was previously on App.
// All 8 services embed a pointer to a single sharedState instance.
type sharedState struct {
	app      *application.App
	hooks    PlatformHooks
	assetsFS fs.FS // embedded frontend assets; set by main via SetAssetsFS before startup

	ctx          context.Context
	cancel       context.CancelFunc
	sqlDB        *sql.DB
	configStore  *config.Store
	profileStore *config.ProfileStore
	cfg          *config.Config
	vectorDB     *chromem.DB
	shortMem     *memory.ShortStore
	permStore    *internaltools.PermissionStore
	mcpStore     *mcp.ServerStore

	// mu guards fields that may be replaced on config save while agent goroutines run.
	mu           sync.RWMutex
	activeScreen ScreenInfo // current screen under the mouse cursor; guarded by mu
	scheduler    *scheduler.Scheduler
	longMem      *memory.LongStore
	knowledgeSt  *knowledge.Store
	petAgent     *agent.Agent
	smsWatcher       *sms.Watcher        // guarded by mu
	chatCancel       context.CancelFunc  // cancels the current in-flight SendMessage; guarded by mu
	chatGeneration   uint64              // incremented on each SendMessage; used to avoid stale cancel nils
	ttsSpeaker       tts.Speaker         // current TTS backend; replaced on profile switch
	ttsBackendKey    string              // backend key; guards against redundant reloads
	ttsCancel        context.CancelFunc  // cancels in-flight SpeakText; guarded by mu
	ttsGeneration    uint64              // incremented on each SpeakText call; used to avoid stale cancel nils
	isChatVisible    bool                // tracks whether the chat panel is open; guarded by mu
	proactiveEngine  *proactive.ProactiveEngine
	mcpClosers       []io.Closer         // guarded by mu; closed and rebuilt on initLLMComponents
	llmTransport     *llm.ErrorBodyTransport // captures raw error bodies from the active LLM HTTP client; guarded by mu
	chatModel        model.ToolCallingChatModel // current chat model; guarded by mu; reused by rebuildAgentTools
	rebuildGen       atomic.Int64        // incremented on each initLLMComponents; guards stale async MCP results
	runningCmds          sync.Map
	pendingConfirms      sync.Map
	watcherWG            sync.WaitGroup     // tracks background watchers started in startup
	cancelWatcher        context.CancelFunc // cancels the screen-watcher goroutine on shutdown
	pendingUpdateVersion string             // non-empty when a successful update marker was found on startup
}

// NewSharedState creates an uninitialised sharedState bound to the given Wails app.
// Call startup() from a ServiceStartup implementation to finish initialisation.
func NewSharedState(app *application.App, hooks PlatformHooks) *sharedState {
	return &sharedState{app: app, hooks: hooks}
}

// SetAssetsFS provides the embedded frontend FS (go:embed all:frontend/dist)
// so service methods that need to enumerate bundled assets can use it without
// importing from package main.  Must be called before startup().
func (s *sharedState) SetAssetsFS(f fs.FS) { s.assetsFS = f }

// IsChatVisible reports whether the chat bubble is currently open.
// Implements proactive.AppInterface.
func (s *sharedState) IsChatVisible() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isChatVisible
}

// EmitEvent emits a Wails runtime event with the given name and payload.
// Implements proactive.AppInterface.
func (s *sharedState) EmitEvent(name string, data any) {
	s.app.Event.Emit(name, data)
}

// startup initialises all application state. It mirrors ServiceStartup from app.go.
// Callers (individual service ServiceStartup implementations) must call this after
// the Wails context is available.
func (s *sharedState) startup(ctx context.Context, _ application.ServiceOptions) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Register the macOS UserNotifications sender and kick off the
	// authorization prompt *before* the scheduler/proactive engine start —
	// cron callbacks and proactive polls can fire immediately once Start() is
	// called, and the system silently drops notifications posted before the
	// user grants permission. Requesting here closes that race window.
	if s.hooks.PostSystemNotification != nil {
		notify.SetSender(s.hooks.PostSystemNotification)
	}
	if s.hooks.RequestNotificationAuthorization != nil {
		s.hooks.RequestNotificationAuthorization()
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("startup: get home dir: %w", err)
	}
	dataDir := filepath.Join(home, ".aiko")

	s.sqlDB, err = db.Open(dataDir)
	if err != nil {
		return fmt.Errorf("startup: open db: %w", err)
	}
	s.configStore = config.NewStore(s.sqlDB)
	s.cfg, err = s.configStore.Load()
	if err != nil {
		return fmt.Errorf("startup: load config: %w", err)
	}
	// Apply active model profile if set.
	profileStore := config.NewProfileStore(s.sqlDB)
	s.profileStore = profileStore
	if s.cfg.ActiveProfileID > 0 {
		if p, perr := profileStore.Get(s.cfg.ActiveProfileID); perr == nil {
			s.cfg.ApplyProfile(p)
			slog.Info("startup: applied profile", "provider", p.Provider, "base_url", s.cfg.LLMBaseURL)
			// Persist any defaults written back (e.g. OpenRouter base URL).
			if perr2 := profileStore.Save(p); perr2 != nil {
				slog.Warn("startup: save profile failed", "err", perr2)
			}
		}
	}

	s.shortMem = memory.NewShortStore(s.sqlDB)

	// Check for a successful-update marker written by InstallUpdate before restart.
	markerPath := filepath.Join(dataDir, "update_success.json")
	if markerData, merr := os.ReadFile(markerPath); merr == nil {
		var m struct {
			Version string `json:"version"`
		}
		if stdjson.Unmarshal(markerData, &m) == nil && m.Version != "" {
			_ = os.Remove(markerPath)
			s.pendingUpdateVersion = m.Version
		}
	}

	s.permStore = internaltools.NewPermissionStore(s.sqlDB)
	s.mcpStore = mcp.NewServerStore(s.sqlDB)
	// Remove stale tool rows that no longer exist.
	_, _ = s.sqlDB.Exec(`DELETE FROM tool_permissions WHERE tool_name = 'lark'`)
	// Ensure all built-in tools have rows in tool_permissions. The declarations
	// live next to the tool registry so adding a tool only touches one place.
	toolsCtx := context.Background()
	for _, d := range internaltools.AllPermissionDeclarations() {
		_ = s.permStore.EnsureRow(toolsCtx, d)
	}
	// Proactive tool is declared outside internal/tools to avoid import cycles,
	// so register its row here.
	_ = s.permStore.EnsureRow(toolsCtx, &proactive.ScheduleFollowupTool{})

	vectorPath := filepath.Join(dataDir, "vectors")
	s.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
	if err != nil {
		return fmt.Errorf("startup: open vector db: %w", err)
	}

	if len(s.cfg.MissingRequired()) == 0 {
		if err := s.initLLMComponents(s.ctx); err != nil {
			slog.Error("init llm components failed", "err", err)
		}
	}

	// Resize window to cover the full primary screen so position:fixed
	// coordinates in the WebView map to real screen coordinates.
	for _, sc := range s.app.Screen.GetAll() {
		if sc.IsPrimary {
			if win, ok := s.app.Window.GetByName("main"); ok {
				win.SetSize(sc.Size.Width, sc.Size.Height)
				win.SetPosition(0, 0)
			}
			break
		}
	}

	// Allow mouse events to pass through transparent window regions.
	if s.hooks.EnableClickThrough != nil {
		s.hooks.EnableClickThrough()
	}

	// Register global hotkey Shift+Cmd+P to toggle the chat bubble.
	if s.hooks.RegisterGlobalHotkey != nil {
		s.hooks.RegisterGlobalHotkey()
	}

	// Watch for mouse moving to a different screen and migrate the window.
	s.startScreenWatcher()

	// On system wake, trigger an immediate poll so jobs/reminders due during
	// sleep are delivered within seconds rather than waiting up to 1 minute.
	if s.hooks.RegisterSystemWakeObserver != nil {
		s.hooks.RegisterSystemWakeObserver(func() {
			slog.Info("system wake detected: triggering immediate poll")
			s.mu.RLock()
			sched := s.scheduler
			engine := s.proactiveEngine
			s.mu.RUnlock()
			if sched != nil {
				sched.TriggerPoll()
			}
			if engine != nil {
				engine.TriggerPoll()
			}
		})
	}

	// Start SMS watcher if enabled in config.
	if s.cfg.SMSWatcherEnabled {
		if err := s.startSMSWatcher(); err != nil {
			slog.Warn("SMS watcher start failed", "err", err)
		}
	}

	// domReady logic: runs after window has loaded
	if win, ok := s.app.Window.GetByName("main"); ok {
		win.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
			if s.hooks.RequestPermissionsEarly != nil {
				s.hooks.RequestPermissionsEarly()
			}
			if s.hooks.HideNativeScrollbars != nil {
				s.hooks.HideNativeScrollbars()
			}
			if s.pendingUpdateVersion != "" {
				s.app.Event.Emit("notification:show", map[string]any{
					"title":   "✅ 更新成功",
					"message": "Aiko 已更新至 v" + s.pendingUpdateVersion,
				})
				s.pendingUpdateVersion = ""
			}
		})
	}
	return nil
}

// shutdown tears down all application state. It mirrors ServiceShutdown from app.go.
func (s *sharedState) shutdown() error {
	if s.cancel != nil {
		s.cancel()
	}

	// Cancel the screen-watcher goroutine immediately so watcherWG.Wait() below
	// doesn't block — the goroutine uses its own derived context, not s.ctx.
	if s.cancelWatcher != nil {
		s.cancelWatcher()
	}

	s.mu.Lock()
	w := s.smsWatcher
	s.smsWatcher = nil
	pe := s.proactiveEngine
	sched := s.scheduler
	closers := s.mcpClosers
	s.mcpClosers = nil
	s.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	if pe != nil {
		pe.Stop()
	}
	if sched != nil {
		sched.Stop()
	}
	// Close MCP client connections accumulated across initLLMComponents calls.
	for _, c := range closers {
		if err := c.Close(); err != nil {
			slog.Warn("mcp client close failed", "err", err)
		}
	}
	// Wait for background watchers to exit (screen watcher exits promptly because
	// cancelWatcher was called above).
	s.watcherWG.Wait()
	// Close the SQLite connection pool so modernc.org/sqlite flushes any pending
	// writes and releases file handles before the process exits.
	if s.sqlDB != nil {
		if err := s.sqlDB.Close(); err != nil {
			slog.Warn("sqlite close failed", "err", err)
		}
	}
	return nil
}

// initLLMComponents initializes chat model, embedder, memory stores, skills, and agent.
// Callers must NOT hold mu when calling this function.
func (s *sharedState) initLLMComponents(ctx context.Context) error {
	s.mu.RLock()
	cfgSnapshot := *s.cfg
	s.mu.RUnlock()
	cfg := &cfgSnapshot

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	dataDir := filepath.Join(home, ".aiko")

	chatModel, transport, err := llm.NewChatModel(ctx, cfg)
	if err != nil {
		return fmt.Errorf("new chat model: %w", err)
	}

	// Create optional summarizer.
	summarizer, err := llm.NewSummarizer(ctx, cfg)
	if err != nil {
		// Non-fatal: proceed without summarization.
		slog.Warn("summarizer init failed, continuing without summarization", "err", err)
		summarizer = nil
	}

	embedder, err := llm.NewEmbedder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("new embedder: %w", err)
	}

	var longMem *memory.LongStore
	var knowledgeSt *knowledge.Store
	if embedder != nil {
		longMem, err = memory.NewLongStore(s.vectorDB, s.sqlDB, embedder, summarizer)
		if err != nil {
			return fmt.Errorf("new long store: %w", err)
		}
		knowledgeSt, err = knowledge.NewStore(s.vectorDB, s.sqlDB, embedder)
		if err != nil {
			return fmt.Errorf("new knowledge store: %w", err)
		}
	}

	// Built-in tools + context-aware tools (knowledge) + skill tools
	builtinTools := internaltools.AllEino(s.permStore)

	// Build a chat function for the scheduler.
	chatFn := func(ctx context.Context, job scheduler.Job) (string, error) {
		s.mu.RLock()
		ag := s.petAgent
		s.mu.RUnlock()
		if ag == nil {
			return "", fmt.Errorf("agent not ready")
		}
		var ch <-chan agent.StreamResult
		if job.SaveToMemory {
			s.app.Event.Emit("chat:cron:start", map[string]any{
				"name":   job.Name,
				"prompt": job.Prompt,
			})
			ch = ag.Chat(ctx, job.Prompt)
		} else {
			ch = ag.ChatDirect(ctx, job.Prompt)
		}
		ep := agent.NewEmotionParser()
		var sb strings.Builder
		for r := range ch {
			if r.Err != nil {
				if job.SaveToMemory {
					s.app.Event.Emit("chat:done", "")
				}
				return "", r.Err
			}
			if job.SaveToMemory && len(r.Images) > 0 {
				s.app.Event.Emit("chat:image", r.Images)
			}
			if r.Done {
				break
			}
			if r.ThinkingToken != "" {
				if job.SaveToMemory {
					s.app.Event.Emit("chat:thinking", r.ThinkingToken)
				}
				continue
			}
			text, _, _ := ep.Feed(r.Token)
			sb.WriteString(text)
			if job.SaveToMemory && text != "" {
				s.app.Event.Emit("chat:token", text)
			}
		}
		// Flush any buffered tail after the last token.
		if tail := ep.Flush(); tail != "" {
			sb.WriteString(tail)
			if job.SaveToMemory {
				s.app.Event.Emit("chat:token", tail)
			}
		}
		if job.SaveToMemory {
			s.app.Event.Emit("chat:done", "")
		}
		return sb.String(), nil
	}

	onResult := func(job scheduler.Job, result string, err error) {
		if err != nil {
			slog.Error("cron job failed", "job", job.Name, "err", err)
			failMsg := "任务执行失败: " + err.Error()
			s.app.Event.Emit("notification:show", map[string]any{
				"title":   job.Name,
				"message": failMsg,
			})
			go notify.System("⏰ "+job.Name, failMsg)
			s.app.Event.Emit("cron:job:done", job.ID)
			return
		}
		slog.Info("cron job completed", "job", job.Name, "result_len", len(result))
		bubbleMsg := result
		const bubbleMaxRunes = 200
		if runes := []rune(result); len(runes) > bubbleMaxRunes {
			bubbleMsg = string(runes[:bubbleMaxRunes]) + "…"
		}
		s.app.Event.Emit("notification:show", map[string]any{
			"title":   "⏰ " + job.Name,
			"message": bubbleMsg,
		})
		if job.Notify {
			go notify.System("⏰ "+job.Name, result)
		}
		s.app.Event.Emit("cron:job:done", job.ID)
	}

	// NOTE: scheduler.Start is deferred until after s.mu is released below —
	// cron callbacks emit Wails events which may reenter Go, and emitting while
	// holding s.mu can deadlock if the event handler calls a service method.
	sched := scheduler.New(s.sqlDB, chatFn, onResult)

	contextTools := internaltools.AllContextual(
		s.permStore,
		knowledgeSt,
		sched,
		longMem,
		dataDir,
		s.cfg,
		func(id string, cancel func()) {
			s.runningCmds.Store(id, cancel)
			s.app.Event.Emit("tool:executing", map[string]interface{}{"id": id})
		},
		func(id string) {
			s.runningCmds.Delete(id)
			s.app.Event.Emit("tool:executed", map[string]interface{}{"id": id})
		},
		func() { go s.initLLMComponents(s.ctx) },
		s.InstallUpdate,
		func(event string, data any) { s.app.Event.Emit(event, data) },
		version,
	)
	proactiveStore := proactive.NewStore(s.sqlDB)
	followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), s.permStore)
	allTools := append(builtinTools, contextTools...)
	allTools = append(allTools, followupTool)

	// Build skill middleware from configured directories.
	autoSkillsDir := filepath.Join(dataDir, "auto-skills")
	skillDirs := append(append([]string{}, cfg.SkillsDirs...), autoSkillsDir)
	skillMW, err := skill.NewMiddleware(ctx, skillDirs)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	// Middleware chain: logging -> retry -> error recovery (outermost first)
	mw := middleware.Chain(
		middleware.Logging(),
		middleware.Retry(3, 200*time.Millisecond),
		middleware.ErrorRecovery(),
	)

	newAgent, err := agent.New(ctx, chatModel, s.shortMem, longMem, allTools, s.cfg, mw, skillMW, dataDir,
		&s.pendingConfirms,
		func(event string, data ...any) {
			s.app.Event.Emit(event, data...)
		},
	)
	if err != nil {
		return fmt.Errorf("new agent: %w", err)
	}

	s.mu.Lock()
	if s.scheduler != nil {
		s.scheduler.Stop()
	}
	if s.proactiveEngine != nil {
		s.proactiveEngine.Stop()
	}
	// Swap MCP closers: close old connections, leave new list empty until async load finishes.
	oldClosers := s.mcpClosers
	s.mcpClosers = nil
	s.scheduler = sched
	s.longMem = longMem
	s.knowledgeSt = knowledgeSt
	s.petAgent = newAgent
	s.llmTransport = transport
	s.chatModel = chatModel
	// Only rebuild TTS when backend or model dir changes.
	newKey := s.cfg.TTSBackend + "|" + s.cfg.TTSModelDir
	if s.ttsSpeaker == nil || newKey != s.ttsBackendKey {
		s.ttsSpeaker = tts.New(s.cfg.TTSBackend, s.cfg.TTSModelDir)
		s.ttsBackendKey = newKey
	}
	engine := proactive.NewEngine(s, proactiveStore)
	s.proactiveEngine = engine
	// Capture the current generation so the async MCP callback can detect stale results.
	gen := s.rebuildGen.Add(1)
	s.mu.Unlock()

	for _, c := range oldClosers {
		if err := c.Close(); err != nil {
			slog.Warn("mcp client close failed on reload", "err", err)
		}
	}

	// Start cron engines outside the mu critical section — callbacks emit
	// Wails events and must not run with the mutex held.
	if err := sched.Start(s.ctx); err != nil {
		slog.Error("scheduler start failed", "err", err)
	}
	engine.Start(s.ctx)

	// Phase 2: connect MCP servers in the background and inject their tools into the agent.
	mcp.LoadToolsAsync(s.ctx, s.mcpStore, 30*time.Second, func(mcpTools []einotool.BaseTool, closers []io.Closer) {
		if s.rebuildGen.Load() != gen {
			for _, c := range closers {
				_ = c.Close()
			}
			slog.Info("mcp async load: discarding stale results (gen mismatch)")
			return
		}
		if len(mcpTools) == 0 {
			return
		}
		if err := s.rebuildAgentTools(s.ctx, mcpTools, closers); err != nil {
			slog.Error("mcp async load: rebuildAgentTools failed", "err", err)
			for _, c := range closers {
				_ = c.Close()
			}
			return
		}
		s.app.Event.Emit("mcp:ready", map[string]any{"count": len(mcpTools)})
		slog.Info("mcp async load: agent rebuilt with mcp tools", "count", len(mcpTools))
	})
	return nil
}

// rebuildAgentTools rebuilds the eino Agent using the current chatModel / longMem / scheduler
// plus the provided MCP tools, then atomically swaps s.petAgent and s.mcpClosers.
// It is called from the async MCP-load goroutine after all MCP servers have connected.
// Callers must NOT hold s.mu when calling this function.
func (s *sharedState) rebuildAgentTools(ctx context.Context, mcpTools []einotool.BaseTool, closers []io.Closer) error {
	// Snapshot the stable components under RLock.
	s.mu.RLock()
	chatModel := s.chatModel
	longMem := s.longMem
	knowledgeSt := s.knowledgeSt
	sched := s.scheduler
	s.mu.RUnlock()

	if chatModel == nil {
		return fmt.Errorf("rebuildAgentTools: chatModel not initialised")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	dataDir := filepath.Join(home, ".aiko")

	builtinTools := internaltools.AllEino(s.permStore)
	chatFn := func(ctx context.Context, prompt string) (string, error) {
		s.mu.RLock()
		ag := s.petAgent
		s.mu.RUnlock()
		if ag == nil {
			return "", fmt.Errorf("agent not ready")
		}
		ch := ag.ChatDirect(ctx, prompt)
		var sb strings.Builder
		for r := range ch {
			if r.Err != nil {
				return "", r.Err
			}
			if r.Done {
				break
			}
			sb.WriteString(r.Token)
		}
		return sb.String(), nil
	}
	contextTools := internaltools.AllContextual(
		s.permStore,
		knowledgeSt,
		sched,
		longMem,
		dataDir,
		s.cfg,
		func(id string, cancel func()) {
			s.runningCmds.Store(id, cancel)
			s.app.Event.Emit("tool:executing", map[string]any{"id": id})
		},
		func(id string) {
			s.runningCmds.Delete(id)
			s.app.Event.Emit("tool:executed", map[string]any{"id": id})
		},
		func() { go s.initLLMComponents(s.ctx) },
		s.InstallUpdate,
		func(event string, data any) { s.app.Event.Emit(event, data) },
		version,
	)
	proactiveStore := proactive.NewStore(s.sqlDB)
	followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), s.permStore)
	allTools := append(builtinTools, contextTools...)
	allTools = append(allTools, mcpTools...)
	allTools = append(allTools, followupTool)

	autoSkillsDir := filepath.Join(dataDir, "auto-skills")
	s.mu.RLock()
	cfgSnapshot := *s.cfg
	s.mu.RUnlock()
	skillDirs := append(append([]string{}, cfgSnapshot.SkillsDirs...), autoSkillsDir)
	skillMW, err := skill.NewMiddleware(ctx, skillDirs)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	mw := middleware.Chain(
		middleware.Logging(),
		middleware.Retry(3, 200*time.Millisecond),
		middleware.ErrorRecovery(),
	)

	newAgent, err := agent.New(ctx, chatModel, s.shortMem, longMem, allTools, s.cfg, mw, skillMW, dataDir,
		&s.pendingConfirms,
		func(event string, data ...any) {
			s.app.Event.Emit(event, data...)
		},
	)
	if err != nil {
		return fmt.Errorf("new agent: %w", err)
	}
	_ = chatFn // used above via contextTools closure

	s.mu.Lock()
	oldClosers := s.mcpClosers
	s.mcpClosers = closers
	s.petAgent = newAgent
	s.mu.Unlock()

	for _, c := range oldClosers {
		if err := c.Close(); err != nil {
			slog.Warn("mcp client close failed on mcp inject", "err", err)
		}
	}
	return nil
}

// startSMSWatcher creates and starts an SMS watcher, emitting verification code
// events to the frontend and copying the code to the clipboard.
// Caller must NOT hold s.mu.
func (s *sharedState) startSMSWatcher() error {
	w, err := sms.NewWatcher(func(evt sms.Event) {
		s.app.Clipboard.SetText(evt.Code)
		s.app.Event.Emit("sms:verification_code", map[string]any{
			"code":   evt.Code,
			"sender": evt.Sender,
			"text":   evt.Text,
		})
		s.app.Event.Emit("notification:show", map[string]any{
			"title":   "📱 验证码：" + evt.Code,
			"message": evt.Sender + "：" + evt.Text,
		})
	})
	if err != nil {
		return err
	}
	if err := w.Start(s.ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.smsWatcher = w
	s.mu.Unlock()
	return nil
}

// startScreenWatcher polls the mouse position every 500ms and migrates the Wails window
// to the screen containing the cursor. Emits "screen:changed" when the active screen changes.
func (s *sharedState) startScreenWatcher() {
	if s.hooks.GetNumScreens == nil || s.hooks.GetScreenFrame == nil {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancelWatcher = cancel
	s.watcherWG.Add(1)
	go func() {
		defer s.watcherWG.Done()
		defer cancel()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lastFoundIdx := -1
		lastNumScreens := -1
		var lastFrame ScreenFrame
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			var mx, my float64
			if s.hooks.GetMouseX != nil {
				mx = s.hooks.GetMouseX()
			}
			if s.hooks.GetMouseY != nil {
				my = s.hooks.GetMouseY()
			}
			n := s.hooks.GetNumScreens()

			foundIdx := -1
			for i := 0; i < n; i++ {
				frame := s.hooks.GetScreenFrame(i)
				if !frame.Valid {
					continue
				}
				if mx >= frame.OriginX && mx < frame.OriginX+frame.Width &&
					my >= frame.OriginY && my < frame.OriginY+frame.Height {
					foundIdx = i
					break
				}
			}
			if foundIdx < 0 {
				continue
			}

			frame := s.hooks.GetScreenFrame(foundIdx)
			displayChanged := n != lastNumScreens ||
				frame.Width != lastFrame.Width ||
				frame.Height != lastFrame.Height ||
				frame.OriginX != lastFrame.OriginX ||
				frame.OriginY != lastFrame.OriginY

			if foundIdx == lastFoundIdx && !displayChanged {
				continue
			}
			lastFoundIdx = foundIdx
			lastNumScreens = n
			lastFrame = frame

			current := ScreenInfo{Width: int(frame.Width), Height: int(frame.Height)}

			if s.hooks.MoveWindowToScreen != nil {
				s.hooks.MoveWindowToScreen(foundIdx)
			}

			s.mu.Lock()
			s.activeScreen = current
			s.mu.Unlock()

			s.app.Event.Emit("screen:changed", current)
			slog.Info("startScreenWatcher: screen changed", "width", current.Width, "height", current.Height, "numScreens", n)
		}
	}()
}

// InstallUpdate downloads the DMG at downloadURL, replaces only the main
// binary inside the running .app bundle, re-signs with the original signing
// identity (preserving TCC permission grants), then restarts the app.
// Progress is emitted as "update:progress" Wails events (0–100).
func (s *sharedState) InstallUpdate(downloadURL string) error {
	emit := func(pct int, msg string) {
		s.app.Event.Emit("update:progress", map[string]any{"pct": pct, "msg": msg})
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法获取当前路径: %w", err)
	}
	exe, _ = filepath.EvalSymlinks(exe)
	appBundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(appBundle, ".app") || version == "dev" {
		appBundle = "/Applications/Aiko.app"
	}

	sigOut, _ := exec.Command("codesign", "--display", "--verbose=2", appBundle).CombinedOutput()
	signID := "-"
	for _, line := range strings.Split(string(sigOut), "\n") {
		if after, ok := strings.CutPrefix(line, "Authority="); ok {
			signID = strings.TrimSpace(after)
			break
		}
	}
	if signID == "-" {
		if err := ensureAikoCert(); err == nil {
			signID = "Aiko"
		}
	}

	emit(5, "正在下载新版本…")
	tmpDMG := filepath.Join(os.TempDir(), "Aiko-update.dmg")
	if err := downloadFileWithProgress(s.ctx, tmpDMG, downloadURL, func(pct int) {
		emit(5+pct*55/100, fmt.Sprintf("下载中 %d%%…", pct))
	}); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}

	emit(62, "挂载 DMG…")
	mountPoint := filepath.Join(os.TempDir(), "AikoUpdateMount")
	_ = os.MkdirAll(mountPoint, 0o755)
	if err := runCmd("hdiutil", "attach", "-nobrowse", "-quiet", tmpDMG, "-mountpoint", mountPoint); err != nil {
		return fmt.Errorf("挂载失败: %w", err)
	}
	defer func() {
		_ = runCmd("hdiutil", "detach", "-quiet", mountPoint)
		_ = os.Remove(tmpDMG)
	}()

	emit(70, "解析安装包…")
	srcApp := ""
	entries, _ := os.ReadDir(mountPoint)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".app") {
			srcApp = filepath.Join(mountPoint, e.Name())
			break
		}
	}
	if srcApp == "" {
		return fmt.Errorf("DMG 中未找到 .app")
	}

	emit(75, "安装中…")
	exeName := filepath.Base(exe)
	if exeName == "" || exeName == "." {
		exeName = "Aiko"
	}
	srcBin := filepath.Join(srcApp, "Contents", "MacOS", exeName)
	dstBin := filepath.Join(appBundle, "Contents", "MacOS", exeName)
	if err := runCmd("cp", srcBin, dstBin); err != nil {
		return fmt.Errorf("复制二进制失败: %w", err)
	}

	emit(88, "重新签名…")
	if err := runCmd("codesign", "--force", "--sign", signID,
		"--identifier", "com.xutiancheng.aiko",
		"--preserve-metadata=entitlements", appBundle); err != nil && signID != "-" {
		_ = runCmd("codesign", "--force", "--sign", "-",
			"--identifier", "com.xutiancheng.aiko",
			"--preserve-metadata=entitlements", appBundle)
	}

	if home, err := os.UserHomeDir(); err == nil {
		base := filepath.Base(downloadURL)
		latestTag := strings.TrimSuffix(strings.TrimPrefix(base, "Aiko-"), ".dmg")
		if latestTag == base || latestTag == "" {
			latestTag = "latest"
		}
		markerPath := filepath.Join(home, ".aiko", "update_success.json")
		_ = os.WriteFile(markerPath, []byte(fmt.Sprintf(`{"version":%q}`, latestTag)), 0o644)
	}

	emit(95, "准备重启…")
	script := fmt.Sprintf("#!/bin/sh\nsleep 1\nopen %q\n", appBundle)
	scriptPath := filepath.Join(os.TempDir(), "aiko-restart.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("写入重启脚本失败: %w", err)
	}
	restartCmd := exec.Command("/bin/sh", scriptPath)
	restartCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := restartCmd.Start(); err != nil {
		return fmt.Errorf("启动重启脚本失败: %w", err)
	}

	emit(100, "正在重启…")
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.app.Quit()
	}()
	return nil
}

// runCmd executes an external command and waits for it to finish,
// merging stderr into the returned error message.
func runCmd(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, stderr.String())
	}
	return nil
}

// ensureAikoCert makes sure a self-signed code-signing certificate named
// "Aiko" exists in the user's login keychain and is trusted for codesign.
func ensureAikoCert() error {
	out, _ := exec.Command("security", "find-identity", "-p", "codesigning").CombinedOutput()
	if strings.Contains(string(out), `"Aiko"`) {
		return nil
	}

	tmp, err := os.MkdirTemp("", "aiko-cert-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmp)

	keyPath := filepath.Join(tmp, "key.pem")
	cfgPath := filepath.Join(tmp, "openssl.cnf")
	crtPath := filepath.Join(tmp, "cert.pem")
	p12Path := filepath.Join(tmp, "cert.p12")

	cfg := `[req]
distinguished_name = dn
prompt = no
x509_extensions = v3

[dn]
CN = Aiko

[v3]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("写入 openssl 配置失败: %w", err)
	}

	if err := runCmd("openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048",
		"-keyout", keyPath, "-out", crtPath, "-days", "3650",
		"-config", cfgPath); err != nil {
		return fmt.Errorf("生成证书失败: %w", err)
	}

	const p12Pass = "aiko"
	if err := runCmd("openssl", "pkcs12", "-export",
		"-inkey", keyPath, "-in", crtPath,
		"-name", "Aiko", "-out", p12Path,
		"-passout", "pass:"+p12Pass,
		"-macalg", "SHA1",
		"-keypbe", "PBE-SHA1-3DES",
		"-certpbe", "PBE-SHA1-3DES"); err != nil {
		return fmt.Errorf("打包 PKCS#12 失败: %w", err)
	}

	loginKC := filepath.Join(os.Getenv("HOME"), "Library", "Keychains", "login.keychain-db")
	if _, err := os.Stat(loginKC); err != nil {
		loginKC = filepath.Join(os.Getenv("HOME"), "Library", "Keychains", "login.keychain")
	}

	if err := runCmd("security", "import", p12Path, "-k", loginKC,
		"-P", p12Pass, "-T", "/usr/bin/codesign", "-A"); err != nil {
		return fmt.Errorf("导入证书失败: %w", err)
	}

	out2, _ := exec.Command("security", "find-identity", "-p", "codesigning").CombinedOutput()
	if !strings.Contains(string(out2), `"Aiko"`) {
		return fmt.Errorf("证书导入后未被 codesign 识别")
	}
	return nil
}

// downloadFileWithProgress downloads url to dst and calls progress(0–100) as data arrives.
func downloadFileWithProgress(_ context.Context, dst, rawURL string, progress func(int)) error {
	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			written += int64(n)
			if total > 0 {
				progress(int(written * 100 / total))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// version holds the build-time version string. It is set from main.version via
// the services package init path, or falls back to "dev".
var version = "dev"

// SetVersion sets the application version string used in tool context.
// Should be called once from main before any service starts.
func SetVersion(v string) { version = v }

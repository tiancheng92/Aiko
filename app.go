package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "github.com/bytedance/sonic"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	chromem "github.com/philippgille/chromem-go"
	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/agent"
	"aiko/internal/claudecco"
	"aiko/internal/agent/middleware"
	"aiko/internal/config"
	"aiko/internal/db"
	"aiko/internal/knowledge"
	"aiko/internal/llm"
	"aiko/internal/mcp"
	"aiko/internal/memory"
	"aiko/internal/notify"
	"aiko/internal/pomodoro"
	"aiko/internal/proactive"
	"aiko/internal/scheduler"
	"aiko/internal/skill"
	"aiko/internal/sms"
	internaltools "aiko/internal/tools"
	toolsystem "aiko/internal/tools/system"
	"aiko/internal/tts"
)

// App is the main application struct. All exported methods are Wails bindings.
type App struct {
	ctx          context.Context
	cancelCtx    context.CancelFunc // cancels a.ctx on shutdown to unblock in-flight LLM streams
	sqlDB        *sql.DB
	configStore  *config.Store
	profileStore *config.ProfileStore
	cfg          *config.Config
	vectorDB     *chromem.DB
	shortMem     *memory.ShortStore
	permStore    *internaltools.PermissionStore
	mcpStore     *mcp.ServerStore

	// mu guards fields that may be replaced on config save while agent goroutines run.
	mu                   sync.RWMutex
	activeScreen         ScreenInfo // current screen under the mouse cursor, guarded by mu
	scheduler            *scheduler.Scheduler
	longMem              *memory.LongStore
	knowledgeSt          *knowledge.Store
	petAgent             *agent.Agent
	smsWatcher           *sms.Watcher       // guarded by mu
	chatCancel           context.CancelFunc // cancels the current in-flight SendMessage; guarded by mu
	chatGeneration       uint64             // incremented on each SendMessage; used to avoid stale cancel nils
	ttsSpeaker           tts.Speaker        // current TTS backend; replaced on profile switch
	ttsBackendKey        string             // backend key; guards against redundant reloads
	ttsCancel            context.CancelFunc // cancels in-flight SpeakText; guarded by mu
	ttsGeneration        uint64             // incremented on each SpeakText call; used to avoid stale cancel nils
	isChatVisible        bool               // tracks whether the chat panel is open; guarded by mu
	proactiveEngine      *proactive.ProactiveEngine
	pomodoroEngine       *pomodoro.Engine
	shuttingDown         atomic.Bool                // set true at shutdown start; guards pomodoro callbacks
	mcpClosers           []io.Closer                // guarded by mu; closed and rebuilt on initLLMComponents
	llmTransport         *llm.ErrorBodyTransport    // captures raw error bodies from the active LLM HTTP client; guarded by mu
	chatModel            model.ToolCallingChatModel // current chat model; guarded by mu; reused by rebuildAgentTools
	rebuildGen           atomic.Int64               // incremented on each initLLMComponents; guards stale async MCP results
	runningCmds          sync.Map
	pendingConfirms      sync.Map
	watcherWG            sync.WaitGroup     // tracks background watchers started in startup
	cancelWatcher        context.CancelFunc // cancels the screen-watcher goroutine on shutdown
	cancelStats          context.CancelFunc // cancels the stats polling goroutine on shutdown
	statsWG              sync.WaitGroup              // tracks the stats polling goroutine
	lastNetwork          toolsystem.NetworkStats      // last computed network rate, updated by ticker
	pendingUpdateVersion string             // non-empty when a successful update marker was found on startup
	dataDir              string             // ~/.aiko; set once in startup, read-only thereafter
	startupErr           string             // non-empty when startup failed; read in domReady to show notification
	claudeccoServer      *claudecco.Server  // Claude Code hook HTTP server; guarded by mu
}

// NewApp creates a new App instance.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx, a.cancelCtx = context.WithCancel(ctx)

	// Register the macOS UserNotifications sender and kick off the
	// authorization prompt *before* the scheduler/proactive engine start —
	// cron callbacks and proactive polls can fire immediately once Start() is
	// called, and the system silently drops notifications posted before the
	// user grants permission. Requesting here closes that race window.
	notify.SetSender(postSystemNotification)
	requestNotificationAuthorization()

	home, err := os.UserHomeDir()
	if err != nil {
		a.startupErr = fmt.Sprintf("无法获取用户目录: %v", err)
		log.Error().Err(err).Msg("startup: get home dir failed")
		return
	}
	dataDir := filepath.Join(home, ".aiko")
	a.dataDir = dataDir

	a.sqlDB, err = db.Open(dataDir)
	if err != nil {
		a.startupErr = fmt.Sprintf("数据库初始化失败: %v", err)
		log.Error().Err(err).Msg("startup: db open failed")
		return
	}
	a.configStore = config.NewStore(a.sqlDB)
	a.cfg, err = a.configStore.Load()
	if err != nil {
		a.startupErr = fmt.Sprintf("配置加载失败: %v", err)
		log.Error().Err(err).Msg("startup: config load failed")
		return
	}
	// Apply active model profile if set.
	profileStore := config.NewProfileStore(a.sqlDB)
	a.profileStore = profileStore
	if a.cfg.ActiveProfileID > 0 {
		if p, perr := profileStore.Get(a.cfg.ActiveProfileID); perr == nil {
			a.cfg.ApplyProfile(p)
			log.Info().Str("provider", string(p.Provider)).Str("base_url", a.cfg.LLMBaseURL).Msg("startup: applied profile")
			// Persist any defaults written back (e.g. OpenRouter base URL).
			if perr2 := profileStore.Save(p); perr2 != nil {
				log.Warn().Err(perr2).Msg("startup: save profile failed")
			}
		}
	}

	a.shortMem = memory.NewShortStore(a.sqlDB)

	// Check for a successful-update marker written by InstallUpdate before restart.
	markerPath := filepath.Join(dataDir, "update_success.json")
	if markerData, merr := os.ReadFile(markerPath); merr == nil {
		var m struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(markerData, &m) == nil && m.Version != "" {
			_ = os.Remove(markerPath) // best-effort: marker has already been read; removal failure is harmless
			a.pendingUpdateVersion = m.Version
		}
	}

	a.permStore = internaltools.NewPermissionStore(a.sqlDB)
	a.mcpStore = mcp.NewServerStore(a.sqlDB)
	// Remove stale tool rows that no longer exist.
	if _, err := a.sqlDB.Exec(`DELETE FROM tool_permissions WHERE tool_name = 'lark'`); err != nil {
		log.Warn().Err(err).Msg("startup: remove stale lark permission row")
	}
	// Ensure all built-in tools have rows in tool_permissions. The declarations
	// live next to the tool registry so adding a tool only touches one place.
	toolsCtx := context.Background()
	for _, d := range internaltools.AllPermissionDeclarations() {
		if err := a.permStore.EnsureRow(toolsCtx, d); err != nil {
			log.Warn().Str("tool", fmt.Sprintf("%T", d)).Err(err).Msg("startup: ensure tool permission row")
		}
	}
	// Proactive tool is declared outside internal/tools to avoid import cycles,
	// so register its row here.
	if err := a.permStore.EnsureRow(toolsCtx, &proactive.ScheduleFollowupTool{}); err != nil {
		log.Warn().Err(err).Msg("startup: ensure proactive tool permission row")
	}

	vectorPath := filepath.Join(dataDir, "vectors")
	a.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
	if err != nil {
		a.startupErr = fmt.Sprintf("向量库初始化失败: %v", err)
		log.Error().Err(err).Msg("startup: vectordb open failed")
		return
	}

	if len(a.cfg.MissingRequired()) == 0 {
		if err := a.initLLMComponents(ctx); err != nil {
			log.Error().Err(err).Msg("init llm components failed")
		}
	}

	// Initialize pomodoro engine (independent of LLM).
	pomoCfg := pomodoro.Config{
		FocusDuration:         a.cfg.PomodoroFocusDuration,
		ShortBreakDuration:    a.cfg.PomodoroShortBreakDuration,
		LongBreakDuration:     a.cfg.PomodoroLongBreakDuration,
		RoundsBeforeLongBreak: a.cfg.PomodoroRoundsBeforeLongBreak,
	}
	a.mu.Lock()
	a.pomodoroEngine = pomodoro.New(pomoCfg)
	engine := a.pomodoroEngine
	a.mu.Unlock()

	engine.OnTick = func(p pomodoro.TickPayload) {
		if a.shuttingDown.Load() {
			return
		}
		a.EmitEvent("pomodoro:tick", p)
	}
	engine.OnPhaseChange = func(p pomodoro.PhasePayload) {
		if a.shuttingDown.Load() {
			return
		}
		a.EmitEvent("pomodoro:phase:changed", p)
		a.EmitEvent("notification:show", map[string]any{
			"title":   "番茄钟",
			"message": p.Message,
		})
	}
	engine.OnStateChange = func(p pomodoro.StatePayload) {
		if a.shuttingDown.Load() {
			return
		}
		a.EmitEvent("pomodoro:state:changed", p)
		switch {
		case p.State == "running" && engine.Status().Phase == "focus":
			a.EmitEvent("pet:state:change", "focusing")
		case p.State == "running":
			a.EmitEvent("pet:state:change", "resting")
		case p.State == "idle" || p.State == "paused":
			a.EmitEvent("pet:state:change", "idle")
		}
	}

	// Resize window to cover the full primary screen so position:fixed
	// coordinates in the WebView map to real screen coordinates.
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

	// Allow mouse events to pass through transparent window regions.
	enableClickThrough()

	// Register global hotkey Shift+Cmd+P to toggle the chat bubble.
	globalAppCtx = ctx
	registerGlobalHotkey()

	// Watch for mouse moving to a different screen and migrate the window.
	a.startScreenWatcher()

	// Start system stats polling ticker.
	a.startStatsTicker()

	// On system wake, trigger an immediate poll so jobs/reminders due during
	// sleep are delivered within seconds rather than waiting up to 1 minute.
	// The poll-based schedulers already use wall-clock timestamps in the DB, so
	// no restart is needed — a single extra tick is sufficient.
	registerSystemWakeObserver(func() {
		log.Info().Msg("system wake detected: triggering immediate poll")
		a.mu.RLock()
		sched := a.scheduler
		engine := a.proactiveEngine
		a.mu.RUnlock()
		if sched != nil {
			sched.TriggerPoll()
		}
		if engine != nil {
			engine.TriggerPoll()
		}
	})

	// Start SMS watcher if enabled in config.
	if a.cfg.SMSWatcherEnabled {
		if err := a.startSMSWatcher(); err != nil {
			log.Warn().Err(err).Msg("SMS watcher start failed")
		}
	}

	// Start Claude Code hook HTTP server if enabled.
	if a.cfg.ClaudeCodeEnabled {
		ccCfg := claudecco.Config{
			Port: a.cfg.ClaudeCodePort,
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
}

// buildAgent assembles the full tool list and constructs a new eino Agent from
// already-resolved components. extraTools is nil for the initial build and
// contains MCP tools for subsequent rebuilds. proactiveStore is shared with the
// proactive engine so the followup tool and engine operate on the same instance.
func (a *App) buildAgent(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	longMem *memory.LongStore,
	knowledgeSt *knowledge.Store,
	sched *scheduler.Scheduler,
	proactiveStore proactive.Store,
	extraTools []tool.BaseTool,
	cfgSkillsDirs []string,
) (*agent.Agent, error) {
	dataDir := a.dataDir
	builtinTools := internaltools.AllEino(a.permStore)
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
	followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), a.permStore)
	allTools := append(builtinTools, contextTools...)
	allTools = append(allTools, extraTools...)
	allTools = append(allTools, followupTool)

	autoSkillsDir := filepath.Join(dataDir, "auto-skills")
	skillDirs := append(append([]string{}, cfgSkillsDirs...), autoSkillsDir)
	agentHub := skill.NewAgentHub(chatModel, a.cfg.SystemPrompt)
	skillMW, err := skill.NewMiddleware(ctx, skillDirs, agentHub, nil)
	if err != nil {
		return nil, fmt.Errorf("load skills: %w", err)
	}

	mw := middleware.Chain(
		middleware.Logging(),
		middleware.Retry(3, 200*time.Millisecond),
		middleware.ErrorRecovery(),
	)

	summaryStore := memory.NewSummaryStore(a.sqlDB)

	return agent.New(ctx, chatModel, a.shortMem, longMem, summaryStore, knowledgeSt, allTools, a.cfg, mw, skillMW, dataDir,
		&a.pendingConfirms,
		func(event string, data ...any) {
			wailsruntime.EventsEmit(a.ctx, event, data...)
		},
	)
}

// initLLMComponents initializes chat model, embedder, memory stores, skills, and agent.
// Callers must NOT hold mu when calling this function.
func (a *App) initLLMComponents(ctx context.Context) error {
	a.mu.RLock()
	cfgSnapshot := *a.cfg
	a.mu.RUnlock()
	cfg := &cfgSnapshot

	chatModel, transport, err := llm.NewChatModel(ctx, cfg)
	if err != nil {
		return fmt.Errorf("new chat model: %w", err)
	}

	embedder, err := llm.NewEmbedder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("new embedder: %w", err)
	}

	var longMem *memory.LongStore
	var knowledgeSt *knowledge.Store
	if embedder != nil {
		longMem, err = memory.NewLongStore(a.vectorDB, embedder)
		if err != nil {
			return fmt.Errorf("new long store: %w", err)
		}
		knowledgeSt, err = knowledge.NewStore(a.vectorDB, a.sqlDB, embedder)
		if err != nil {
			return fmt.Errorf("new knowledge store: %w", err)
		}
	}

	// Build a chat function for the scheduler.
	// When SaveToMemory is true the job uses ag.Chat() so the exchange goes
	// through persistAndMigrate (identical to typing in the chat panel) AND
	// streams tokens to the chat panel via chat:cron:start / chat:token / chat:done.
	// Otherwise ChatDirect is used to avoid polluting short/long-term memory.
	chatFn := func(ctx context.Context, job scheduler.Job) (string, error) {
		a.mu.RLock()
		ag := a.petAgent
		a.mu.RUnlock()
		if ag == nil {
			return "", fmt.Errorf("agent not ready")
		}
		var ch <-chan agent.StreamResult
		if job.SaveToMemory {
			// Signal the chat panel to open a streaming assistant bubble and
			// show the cron job prompt as the user-side trigger message.
			wailsruntime.EventsEmit(a.ctx, "chat:cron:start", map[string]any{
				"name":   job.Name,
				"prompt": job.Prompt,
			})
			// Prepend the job name so the stored user message matches the
			// formatted label shown by the frontend's chat:cron:start handler.
			ch = ag.ChatDirectSave(ctx, fmt.Sprintf("⏰ **%s**\n%s", job.Name, job.Prompt))
		} else {
			ch = ag.ChatDirect(ctx, job.Prompt)
		}
		bp := agent.NewBehaviorParser()
		var sb strings.Builder
		for r := range ch {
			if r.Err != nil {
				if job.SaveToMemory {
					wailsruntime.EventsEmit(a.ctx, "chat:done", "")
				}
				return "", r.Err
			}
			if job.SaveToMemory && len(r.Images) > 0 {
				wailsruntime.EventsEmit(a.ctx, "chat:image", r.Images)
			}
			if r.Done {
				break
			}
			if r.ThinkingToken != "" {
				if job.SaveToMemory {
					wailsruntime.EventsEmit(a.ctx, "chat:thinking", r.ThinkingToken)
				}
				continue
			}
			text, _, _ := bp.Feed(r.Token)
			sb.WriteString(text)
			if job.SaveToMemory && text != "" {
				wailsruntime.EventsEmit(a.ctx, "chat:token", text)
			}
		}
		// Flush any buffered tail after the last token.
		if tail := bp.Flush(); tail != "" {
			sb.WriteString(tail)
			if job.SaveToMemory {
				wailsruntime.EventsEmit(a.ctx, "chat:token", tail)
			}
		}
		if job.SaveToMemory {
			wailsruntime.EventsEmit(a.ctx, "chat:done", "")
		}
		return sb.String(), nil
	}

	onResult := func(job scheduler.Job, result string, err error) {
		if err != nil {
			log.Error().Str("job", job.Name).Err(err).Msg("cron job failed")
			failMsg := "任务执行失败: " + err.Error()
			// Always show in-app bubble for failures.
			wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
				"title":   job.Name,
				"message": failMsg,
			})
			go notify.System("⏰ "+job.Name, failMsg)
			wailsruntime.EventsEmit(a.ctx, "cron:job:done", job.ID)
			return
		}
		log.Info().Str("job", job.Name).Int("result_len", len(result)).Msg("cron job completed")
		// Show in-app bubble only when the chat panel is closed; if it's open
		// the streamed tokens are already visible in the chat history.
		if !a.IsChatVisible() {
			bubbleMsg := result
			const bubbleMaxRunes = 200
			if runes := []rune(result); len(runes) > bubbleMaxRunes {
				bubbleMsg = string(runes[:bubbleMaxRunes]) + "…"
			}
			wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
				"title":   "⏰ " + job.Name,
				"message": bubbleMsg,
			})
		}
		// Extra macOS system notification when Notify is enabled.
		if job.Notify {
			go notify.System("⏰ "+job.Name, result)
		}
		// When SaveToMemory is on, chatFn already used ag.Chat() (persistAndMigrate)
		// and streamed tokens to the chat panel — nothing more to do here.
		// Notify the settings window to refresh the job list so LastRun updates.
		wailsruntime.EventsEmit(a.ctx, "cron:job:done", job.ID)
	}

	// NOTE: scheduler.Start is deferred until after a.mu is released below —
	// cron callbacks emit Wails events which may reenter Go, and emitting while
	// holding a.mu can deadlock if the event handler calls an App method.
	sched := scheduler.New(a.sqlDB, chatFn, onResult)

	proactiveStore := proactive.NewStore(a.sqlDB)
	// MCP tools are loaded asynchronously after the agent is online (see below).
	newAgent, err := a.buildAgent(ctx, chatModel, longMem, knowledgeSt, sched, proactiveStore, nil, cfg.SkillsDirs)
	if err != nil {
		return fmt.Errorf("build agent: %w", err)
	}

	a.mu.Lock()
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	if a.proactiveEngine != nil {
		a.proactiveEngine.Stop()
	}
	// Swap MCP closers: close old connections, leave new list empty until async load finishes.
	oldClosers := a.mcpClosers
	a.mcpClosers = nil
	a.scheduler = sched
	a.longMem = longMem
	a.knowledgeSt = knowledgeSt
	a.petAgent = newAgent
	a.llmTransport = transport
	a.chatModel = chatModel
	// 只在 backend 或模型目录变化时重建 TTS 实例。
	newKey := a.cfg.TTSBackend + "|" + a.cfg.TTSModelDir
	if a.ttsSpeaker == nil || newKey != a.ttsBackendKey {
		a.ttsSpeaker = tts.New(a.cfg.TTSBackend, a.cfg.TTSModelDir)
		a.ttsBackendKey = newKey
	}
	engine := proactive.NewEngine(a, proactiveStore)
	a.proactiveEngine = engine
	// Capture the current generation so the async MCP callback can detect stale results.
	gen := a.rebuildGen.Add(1)
	a.mu.Unlock()

	for _, c := range oldClosers {
		if err := c.Close(); err != nil {
			log.Warn().Err(err).Msg("mcp client close failed on reload")
		}
	}

	// Start cron engines outside the mu critical section — callbacks emit
	// Wails events and must not run with the mutex held.
	if err := sched.Start(a.ctx); err != nil {
		log.Error().Err(err).Msg("scheduler start failed")
	}
	engine.Start(a.ctx)

	// Phase 2: connect MCP servers in the background and inject their tools into the agent.
	// Each server gets 30 s; the done callback is always called exactly once.
	mcp.LoadToolsAsync(a.ctx, a.mcpStore, 30*time.Second, func(mcpTools []tool.BaseTool, closers []io.Closer) {
		// If initLLMComponents was called again while we were connecting, discard stale results.
		if a.rebuildGen.Load() != gen {
			for _, c := range closers {
				if err := c.Close(); err != nil {
					log.Warn().Err(err).Msg("mcp: close stale closer")
				}
			}
			log.Info().Msg("mcp async load: discarding stale results (gen mismatch)")
			return
		}
		if len(mcpTools) == 0 {
			return
		}
		if err := a.rebuildAgentTools(a.ctx, mcpTools, closers); err != nil {
			log.Error().Err(err).Msg("mcp async load: rebuildAgentTools failed")
			for _, c := range closers {
				if err := c.Close(); err != nil {
					log.Warn().Err(err).Msg("mcp: close closer after rebuild error")
				}
			}
			return
		}
		wailsruntime.EventsEmit(a.ctx, "mcp:ready", map[string]any{"count": len(mcpTools)})
		log.Info().Int("count", len(mcpTools)).Msg("mcp async load: agent rebuilt with mcp tools")
	})
	return nil
}

// rebuildAgentTools rebuilds the eino Agent using the current chatModel / longMem / scheduler
// plus the provided MCP tools, then atomically swaps a.petAgent and a.mcpClosers.
// It is called from the async MCP-load goroutine after all MCP servers have connected.
// Callers must NOT hold a.mu when calling this function.
func (a *App) rebuildAgentTools(ctx context.Context, mcpTools []tool.BaseTool, closers []io.Closer) error {
	// Snapshot the stable components under RLock.
	a.mu.RLock()
	chatModel := a.chatModel
	longMem := a.longMem
	knowledgeSt := a.knowledgeSt
	sched := a.scheduler
	a.mu.RUnlock()

	if chatModel == nil {
		return fmt.Errorf("rebuildAgentTools: chatModel not initialised")
	}

	a.mu.RLock()
	cfgSnapshot := *a.cfg
	a.mu.RUnlock()

	proactiveStore := proactive.NewStore(a.sqlDB)
	newAgent, err := a.buildAgent(ctx, chatModel, longMem, knowledgeSt, sched, proactiveStore, mcpTools, cfgSnapshot.SkillsDirs)
	if err != nil {
		return fmt.Errorf("build agent: %w", err)
	}

	a.mu.Lock()
	oldClosers := a.mcpClosers
	a.mcpClosers = closers
	a.petAgent = newAgent
	a.mu.Unlock()

	for _, c := range oldClosers {
		if err := c.Close(); err != nil {
			log.Warn().Err(err).Msg("mcp client close failed on mcp inject")
		}
	}
	return nil
}

// domReady is called by Wails after the frontend DOM is fully loaded.
// At this point the window is visible and the app is in the foreground,
// so macOS will show proper permission dialogs instead of silent banners.
func (a *App) domReady(_ context.Context) {
	requestPermissionsEarly()
	// Re-apply scrollbar suppression after DOM is ready — WKWebView's internal
	// scroll view may not exist yet during startup(), so we call it again here.
	hideNativeScrollbars()

	// Emit update-success notification if a marker was found during startup.
	if a.pendingUpdateVersion != "" {
		wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
			"title":   "✅ 更新成功",
			"message": "Aiko 已更新至 " + a.pendingUpdateVersion,
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

func (a *App) shutdown(_ context.Context) {
	a.shuttingDown.Store(true)

	// Cancel a.ctx first — this unblocks any goroutine reading from an LLM/TTS
	// HTTP stream (chatCancel and ttsCancel are derived from a.ctx so they are
	// implicitly cancelled too, but calling them explicitly clears the fields).
	if a.cancelCtx != nil {
		a.cancelCtx()
	}
	a.mu.Lock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
	if a.ttsCancel != nil {
		a.ttsCancel()
		a.ttsCancel = nil
	}
	a.mu.Unlock()

	// Cancel the screen-watcher goroutine immediately so watcherWG.Wait() below
	// doesn't block — the goroutine uses its own derived context, not a.ctx.
	if a.cancelWatcher != nil {
		a.cancelWatcher()
	}

	// Stop system stats polling.
	a.stopStatsTicker()

	a.mu.Lock()
	w := a.smsWatcher
	a.smsWatcher = nil
	pe := a.proactiveEngine
	sched := a.scheduler
	closers := a.mcpClosers
	a.mcpClosers = nil
	a.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	if pe != nil {
		pe.Stop()
	}
	if sched != nil {
		sched.Stop()
	}
	a.mu.RLock()
	engine := a.pomodoroEngine
	a.mu.RUnlock()
	if engine != nil {
		engine.Stop()
	}
	if a.claudeccoServer != nil {
		a.claudeccoServer.Stop()
	}
	// Close MCP client connections accumulated across initLLMComponents calls.
	for _, c := range closers {
		if err := c.Close(); err != nil {
			log.Warn().Err(err).Msg("mcp client close failed")
		}
	}
	// Wait for background watchers to exit (screen watcher exits promptly because
	// cancelWatcher was called above).
	a.watcherWG.Wait()
	// Close the SQLite connection pool so modernc.org/sqlite flushes any pending
	// writes and releases file handles before the process exits.
	if a.sqlDB != nil {
		if err := a.sqlDB.Close(); err != nil {
			log.Warn().Err(err).Msg("sqlite close failed")
		}
	}
}

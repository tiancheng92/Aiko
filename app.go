package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	json "github.com/bytedance/sonic"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	chromem "github.com/philippgille/chromem-go"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/agent"
	"aiko/internal/agent/middleware"
	"aiko/internal/config"
	"aiko/internal/db"
	"aiko/internal/knowledge"
	"aiko/internal/llm"
	"aiko/internal/lark"
	"aiko/internal/mcp"
	"aiko/internal/memory"
	"aiko/internal/proactive"
	"aiko/internal/scheduler"
	"aiko/internal/skill"
	"aiko/internal/sms"
	"aiko/internal/tts"
	internaltools "aiko/internal/tools"
)

// App is the main application struct. All exported methods are Wails bindings.
type App struct {
	ctx          context.Context
	sqlDB        *sql.DB
	configStore  *config.Store
	profileStore *config.ProfileStore
	cfg         *config.Config
	vectorDB    *chromem.DB
	shortMem    *memory.ShortStore
	permStore   *internaltools.PermissionStore
	mcpStore    *mcp.ServerStore

	// mu guards fields that may be replaced on config save while agent goroutines run.
	mu           sync.RWMutex
	activeScreen ScreenInfo // current screen under the mouse cursor, guarded by mu
	scheduler    *scheduler.Scheduler
	longMem     *memory.LongStore
	knowledgeSt *knowledge.Store
	petAgent    *agent.Agent
	smsWatcher      *sms.Watcher // guarded by mu
	chatCancel      context.CancelFunc // cancels the current in-flight SendMessage; guarded by mu
	chatGeneration  uint64             // incremented on each SendMessage; used to avoid stale cancel nils
	ttsSpeaker      tts.Speaker        // current TTS backend; replaced on profile switch
	ttsBackendKey   string             // backend key; guards against redundant reloads
	ttsCancel       context.CancelFunc // cancels in-flight SpeakText; guarded by mu
	ttsGeneration   uint64             // incremented on each SpeakText call; used to avoid stale cancel nils
	isChatVisible   bool               // tracks whether the chat panel is open; guarded by mu
	proactiveEngine *proactive.ProactiveEngine
}

// NewApp creates a new App instance.
func NewApp() *App { return &App{} }

// IsChatVisible returns whether the chat panel is currently visible.
func (a *App) IsChatVisible() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isChatVisible
}

// ListProactiveItems returns all pending proactive reminders ordered by trigger time.
func (a *App) ListProactiveItems() ([]proactive.Item, error) {
	a.mu.RLock()
	pe := a.proactiveEngine
	a.mu.RUnlock()
	if pe == nil {
		return nil, nil
	}
	return pe.Store().List(context.Background())
}

// DeleteProactiveItem cancels a pending proactive reminder by ID.
func (a *App) DeleteProactiveItem(id int64) error {
	a.mu.RLock()
	pe := a.proactiveEngine
	a.mu.RUnlock()
	if pe == nil {
		return nil
	}
	return pe.Store().Delete(context.Background(), id)
}

// SetChatVisible updates the tracked chat-panel visibility state.
// Called by the frontend when the chat bubble is opened or closed.
func (a *App) SetChatVisible(visible bool) {
	a.mu.Lock()
	a.isChatVisible = visible
	a.mu.Unlock()
}

// EmitEvent emits a Wails runtime event with the given name and payload.
func (a *App) EmitEvent(name string, data any) {
	wailsruntime.EventsEmit(a.ctx, name, data)
}

// ChatDirect streams a proactive AI response for the given prompt without saving to memory.
func (a *App) ChatDirect(ctx context.Context, prompt string) error {
	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()
	if ag == nil {
		return fmt.Errorf("agent not initialized")
	}
	ch := ag.ChatDirect(ctx, prompt)
	for r := range ch {
		if r.Err != nil {
			wailsruntime.EventsEmit(a.ctx, "chat:error", r.Err.Error())
			wailsruntime.EventsEmit(a.ctx, "chat:done", nil)
			return r.Err
		}
		if r.Done {
			break
		}
		wailsruntime.EventsEmit(a.ctx, "chat:token", r.Token)
	}
	wailsruntime.EventsEmit(a.ctx, "chat:done", nil)
	return nil
}

// ChatDirectCollect runs a proactive AI generation and collects the full response text.
func (a *App) ChatDirectCollect(ctx context.Context, prompt string) (string, error) {
	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()
	if ag == nil {
		return "", fmt.Errorf("agent not initialized")
	}
	return ag.ChatDirectCollect(ctx, prompt)
}

// ScreenInfo holds the logical resolution of a screen.
type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Errorf("get home dir: %w", err))
	}
	dataDir := filepath.Join(home, ".aiko")

	a.sqlDB, err = db.Open(dataDir)
	if err != nil {
		panic(err)
	}
	a.configStore = config.NewStore(a.sqlDB)
	a.cfg, err = a.configStore.Load()
	if err != nil {
		panic(err)
	}
	// Apply active model profile if set.
	profileStore := config.NewProfileStore(a.sqlDB)
	a.profileStore = profileStore
	if a.cfg.ActiveProfileID > 0 {
		if p, perr := profileStore.Get(a.cfg.ActiveProfileID); perr == nil {
			a.cfg.ApplyProfile(p)
			slog.Info("startup: applied profile", "provider", p.Provider, "base_url", a.cfg.LLMBaseURL)
			// Persist any defaults written back (e.g. OpenRouter base URL).
			if perr2 := profileStore.Save(p); perr2 != nil {
				slog.Warn("startup: save profile failed", "err", perr2)
			}
		}
	}

	a.shortMem = memory.NewShortStore(a.sqlDB)

	a.permStore = internaltools.NewPermissionStore(a.sqlDB)
	a.mcpStore = mcp.NewServerStore(a.sqlDB)
	// Remove stale tool rows that no longer exist.
	_, _ = a.sqlDB.Exec(`DELETE FROM tool_permissions WHERE tool_name = 'lark'`)
	// Ensure all built-in tools have rows in tool_permissions.
	toolsCtx := context.Background()
	for _, t := range internaltools.All() {
		_ = a.permStore.EnsureRow(toolsCtx, t)
	}
	// Ensure contextual tool permission rows (store not needed for row creation).
	for _, t := range []internaltools.Tool{
		&internaltools.SearchKnowledgeTool{},
		&internaltools.CronTool{},
		&internaltools.SaveMemoryTool{},
		&internaltools.UpdateUserProfileTool{},
		&internaltools.SaveSkillTool{},
		&proactive.ScheduleFollowupTool{},
	} {
		_ = a.permStore.EnsureRow(toolsCtx, t)
	}

	vectorPath := filepath.Join(dataDir, "vectors")
	a.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
	if err != nil {
		panic(err)
	}

	if len(a.cfg.MissingRequired()) == 0 {
		if err := a.initLLMComponents(ctx); err != nil {
			slog.Error("init llm components failed", "err", err)
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

	// Start SMS watcher if enabled in config.
	if a.cfg.SMSWatcherEnabled {
		if err := a.startSMSWatcher(); err != nil {
			slog.Warn("SMS watcher start failed", "err", err)
		}
	}
}

// initLLMComponents initializes chat model, embedder, memory stores, skills, and agent.
// Callers must NOT hold mu when calling this function.
func (a *App) initLLMComponents(ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	dataDir := filepath.Join(home, ".aiko")

	chatModel, err := llm.NewChatModel(ctx, a.cfg)
	if err != nil {
		return fmt.Errorf("new chat model: %w", err)
	}

	// Create optional summarizer.
	summarizer, err := llm.NewSummarizer(ctx, a.cfg)
	if err != nil {
		// Non-fatal: proceed without summarization.
		slog.Warn("summarizer init failed, continuing without summarization", "err", err)
		summarizer = nil
	}

	embedder, err := llm.NewEmbedder(ctx, a.cfg)
	if err != nil {
		return fmt.Errorf("new embedder: %w", err)
	}

	var longMem *memory.LongStore
	var knowledgeSt *knowledge.Store
	if embedder != nil {
		longMem, err = memory.NewLongStore(a.vectorDB, a.sqlDB, embedder, summarizer)
		if err != nil {
			return fmt.Errorf("new long store: %w", err)
		}
		knowledgeSt, err = knowledge.NewStore(a.vectorDB, a.sqlDB, embedder)
		if err != nil {
			return fmt.Errorf("new knowledge store: %w", err)
		}
	}

	// Built-in tools + context-aware tools (knowledge) + skill tools
	builtinTools := internaltools.AllEino(a.permStore)

	// Build a chat function for the scheduler.
	// IMPORTANT: Scheduler jobs use a direct LLM call that bypasses persistAndMigrate,
	// so job prompts and results are NOT written to short/long-term memory.
	chatFn := func(ctx context.Context, prompt string) (string, error) {
		a.mu.RLock()
		ag := a.petAgent
		a.mu.RUnlock()
		if ag == nil {
			return "", fmt.Errorf("agent not ready")
		}
		ch := ag.ChatDirect(ctx, prompt) // ChatDirect skips memory persistence
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

	onResult := func(job scheduler.Job, result string, err error) {
		if err != nil {
			slog.Error("cron job failed", "job", job.Name, "err", err)
			return
		}
		// Emit to the unified notification channel consumed by NotificationBubble.vue.
		wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
			"title":   job.Name,
			"message": result,
		})
	}

	sched := scheduler.New(a.sqlDB, chatFn, onResult)
	if err := sched.Start(a.ctx); err != nil {
		slog.Error("scheduler start failed", "err", err)
	}

	contextTools := internaltools.AllContextual(a.permStore, knowledgeSt, sched, longMem, dataDir)
	mcpTools := mcp.LoadTools(ctx, a.mcpStore)
	proactiveStore := proactive.NewStore(a.sqlDB)
	followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), a.permStore)
	allTools := append(builtinTools, contextTools...)
	allTools = append(allTools, mcpTools...)
	allTools = append(allTools, followupTool)

	// Build skill middleware from configured directories.
	autoSkillsDir := filepath.Join(dataDir, "auto-skills")
	skillDirs := append(append([]string{}, a.cfg.SkillsDirs...), autoSkillsDir)
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

	newAgent, err := agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW, dataDir)
	if err != nil {
		return fmt.Errorf("new agent: %w", err)
	}

	a.mu.Lock()
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	if a.proactiveEngine != nil {
		a.proactiveEngine.Stop()
	}
	a.scheduler = sched
	a.longMem = longMem
	a.knowledgeSt = knowledgeSt
	a.petAgent = newAgent
	// 只在 backend 或模型目录变化时重建 TTS 实例。
	newKey := a.cfg.TTSBackend + "|" + a.cfg.TTSModelDir
	if a.ttsSpeaker == nil || newKey != a.ttsBackendKey {
		a.ttsSpeaker = tts.New(a.cfg.TTSBackend, a.cfg.TTSModelDir)
		a.ttsBackendKey = newKey
	}
	engine := proactive.NewEngine(a, proactiveStore)
	a.proactiveEngine = engine
	a.mu.Unlock()

	engine.Start(a.ctx)
	return nil
}

// GetConfig returns the current config to the frontend.
func (a *App) GetConfig() *config.Config { return a.cfg }

// SaveConfig persists updated config and reinitializes LLM components.
// LLM init errors are non-fatal (user may save non-LLM settings before configuring the model).
func (a *App) SaveConfig(cfg *config.Config) error {
	// Preserve fields that are managed independently (not via the settings form).
	cfg.SMSWatcherEnabled = a.cfg.SMSWatcherEnabled
	cfg.VoiceAutoSend = a.cfg.VoiceAutoSend
	cfg.SoundsEnabled = a.cfg.SoundsEnabled
	cfg.TTSAutoPlay = a.cfg.TTSAutoPlay
	// Preserve profile-derived fields so SaveConfig never clobbers them.
	// These fields live in model_profiles, not settings, and are applied via ApplyProfile.
	cfg.TTSVoice = a.cfg.TTSVoice
	cfg.TTSSpeed = a.cfg.TTSSpeed
	cfg.TTSBackend = a.cfg.TTSBackend
	cfg.TTSModelDir = a.cfg.TTSModelDir
	if err := a.configStore.Save(cfg); err != nil {
		return err
	}
	a.cfg = cfg
	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("SaveConfig: LLM reinit skipped", "err", err)
	}
	return nil
}

// ListModelProfiles returns all saved model profiles.
func (a *App) ListModelProfiles() ([]config.ModelProfile, error) {
	return a.profileStore.List()
}

// SaveModelProfile creates or updates a model profile.
// If the saved profile is the currently active one, cfg is updated in-place so
// TTS voice/speed/backend changes take effect immediately without a full reinit.
func (a *App) SaveModelProfile(p config.ModelProfile) (config.ModelProfile, error) {
	slog.Info("SaveModelProfile", "id", p.ID, "backend", p.TTSBackend, "voice", p.TTSVoice, "speed", p.TTSSpeed, "activeID", a.cfg.ActiveProfileID)
	if err := a.profileStore.Save(&p); err != nil {
		return p, err
	}
	// Sync cfg when saving the active profile, so voice/speed/backend changes apply immediately.
	if p.ID == a.cfg.ActiveProfileID {
		a.cfg.ApplyProfile(&p)
		slog.Info("SaveModelProfile: applied to cfg", "voice", a.cfg.TTSVoice)
	}
	return p, nil
}

// DeleteModelProfile removes a model profile by id.
func (a *App) DeleteModelProfile(id int64) error {
	return a.profileStore.Delete(id)
}

// ActivateModelProfile switches to the given profile and reinitializes LLM components.
func (a *App) ActivateModelProfile(id int64) error {
	p, err := a.profileStore.Get(id)
	if err != nil {
		return err
	}
	a.cfg.ApplyProfile(p)
	// Persist any defaults written back to the profile (e.g. OpenRouter base URL).
	if err := a.profileStore.Save(p); err != nil {
		slog.Warn("ActivateModelProfile: save profile failed", "err", err)
	}
	if err := a.configStore.Save(a.cfg); err != nil {
		return err
	}
	return a.initLLMComponents(a.ctx)
}

// GetBallPosition returns the saved ball [x, y] for the given screen resolution,
// or [-1, -1] if no position has been saved for that resolution yet.
func (a *App) GetBallPosition(screenW, screenH int) []int {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return []int{-1, -1}
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return []int{-1, -1}
	}
	x, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return []int{-1, -1}
	}
	return []int{x, y}
}

// SaveBallPosition persists the ball position for the given screen resolution.
func (a *App) SaveBallPosition(x, y, screenW, screenH int) error {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	val := fmt.Sprintf("%d,%d", x, y)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}

// GetScreenList returns all connected screens as ScreenInfo values.
func (a *App) GetScreenList() []ScreenInfo {
	screens, err := wailsruntime.ScreenGetAll(a.ctx)
	if err != nil {
		slog.Warn("GetScreenList: ScreenGetAll failed", "err", err)
		return nil
	}
	result := make([]ScreenInfo, 0, len(screens))
	for _, s := range screens {
		result = append(result, ScreenInfo{Width: s.Size.Width, Height: s.Size.Height})
	}
	return result
}

// startScreenWatcher polls the mouse position every 500ms and migrates the Wails window
// to the screen containing the cursor. Emits "screen:changed" when the active screen changes.
func (a *App) startScreenWatcher() {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lastFoundIdx := -1
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
			}

			// Use CGO-only calls to locate the cursor's screen — no Wails IPC needed.
			mx := getMouseX()
			my := getMouseY()
			n := getNumScreens()

			foundIdx := -1
			for i := 0; i < n; i++ {
				frame := getScreenFrame(i)
				if !frame.Valid {
					continue
				}
				if mx >= frame.OriginX && mx < frame.OriginX+frame.Width &&
					my >= frame.OriginY && my < frame.OriginY+frame.Height {
					foundIdx = i
					break
				}
			}
			if foundIdx < 0 || foundIdx == lastFoundIdx {
				continue
			}
			lastFoundIdx = foundIdx

			// Screen changed — only now pay the cost of a Wails IPC call.
			screens, err := wailsruntime.ScreenGetAll(a.ctx)
			if err != nil {
				slog.Warn("startScreenWatcher: ScreenGetAll failed", "err", err)
				continue
			}
			if foundIdx >= len(screens) {
				continue
			}
			found := &screens[foundIdx]
			current := ScreenInfo{Width: found.Size.Width, Height: found.Size.Height}

			// Move the window directly via CGO to bypass Wails' WindowSetPosition,
			// which is relative to the current screen and cannot reliably migrate
			// the window to a different screen.
			moveWindowToScreen(foundIdx)

			a.mu.Lock()
			a.activeScreen = current
			a.mu.Unlock()

			wailsruntime.EventsEmit(a.ctx, "screen:changed", current)
			slog.Info("startScreenWatcher: screen changed", "width", current.Width, "height", current.Height)
		}
	}()
}

// GetPetSize returns the saved pet height for the given screen resolution, or 0 if not set or on error.
func (a *App) GetPetSize(screenW, screenH int) int {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

// SavePetSize persists the pet height for the given screen resolution.
func (a *App) SavePetSize(size, screenW, screenH int) error {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, strconv.Itoa(size))
	return err
}

// GetChatSize returns the saved chat bubble [width, height] for the given screen resolution.
// Returns [0, 0] if no size has been saved for that resolution yet.
func (a *App) GetChatSize(screenW, screenH int) []int {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return []int{0, 0}
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return []int{0, 0}
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return []int{0, 0}
	}
	return []int{w, h}
}

// SaveChatSize persists the chat bubble dimensions for the given screen resolution.
func (a *App) SaveChatSize(width, height, screenW, screenH int) error {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	val := fmt.Sprintf("%d,%d", width, height)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}

// MousePosition holds the CSS coordinates of the mouse cursor.
type MousePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// GetMousePosition returns the current mouse cursor position in CSS coordinates.
// This works even when the app is not focused, enabling eye tracking while unfocused.
func (a *App) GetMousePosition() MousePosition {
	x, y := GetMousePosition()
	return MousePosition{X: x, Y: y}
}

// MissingRequiredConfig returns names of empty required config fields.
func (a *App) MissingRequiredConfig() []string {
	return a.cfg.MissingRequired()
}

// SendMessage sends a user message and streams response tokens as Wails events.
// Events emitted: "chat:token" (string), "chat:done" (""), "chat:error" (string).
// Any in-flight request is cancelled before starting the new one.
func (a *App) SendMessage(userInput string) error {
	// Cancel any previous in-flight request.
	a.mu.Lock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
	chatCtx, cancel := context.WithCancel(a.ctx)
	a.chatCancel = cancel
	a.chatGeneration++
	myGen := a.chatGeneration
	a.mu.Unlock()

	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()

	if ag == nil {
		a.mu.Lock()
		a.chatCancel = nil
		a.mu.Unlock()
		cancel()
		slog.Error("SendMessage: petAgent is nil", "input", userInput)
		return fmt.Errorf("agent not initialized: complete settings first")
	}
	go func() {
		defer cancel() // ensure context is always released
		defer func() {
			a.mu.Lock()
			if a.chatGeneration == myGen {
				a.chatCancel = nil
			}
			a.mu.Unlock()
		}()
		ch := ag.Chat(chatCtx, userInput)
		for result := range ch {
			if result.Err != nil {
				// Ignore context cancellation — user triggered StopGeneration; frontend handles UI.
				if errors.Is(result.Err, context.Canceled) {
					return
				}
				wailsruntime.EventsEmit(a.ctx, "chat:error", result.Err.Error())
				return
			}
			if result.Done {
				wailsruntime.EventsEmit(a.ctx, "chat:done", "")
				return
			}
			wailsruntime.EventsEmit(a.ctx, "chat:token", result.Token)
		}
		// Fallback: ensure frontend unblocks if channel closes without a terminal result.
		wailsruntime.EventsEmit(a.ctx, "chat:done", "")
	}()
	return nil
}

// GetMessages returns recent chat history (up to limit messages).
func (a *App) GetMessages(limit int) ([]memory.Message, error) {
	return a.shortMem.Recent(limit)
}

// ImportKnowledge imports a file into the knowledge base.
// Emits "knowledge:progress" events during import.
func (a *App) ImportKnowledge(filePath string) error {
	a.mu.RLock()
	ks := a.knowledgeSt
	a.mu.RUnlock()

	if ks == nil {
		return fmt.Errorf("knowledge store not initialized: configure embedding model first")
	}
	return knowledge.Import(a.ctx, ks, filePath, func(p knowledge.ImportProgress) {
		wailsruntime.EventsEmit(a.ctx, "knowledge:progress", p)
	})
}

// ListKnowledgeSources returns distinct source filenames in the knowledge base.
func (a *App) ListKnowledgeSources() ([]string, error) {
	a.mu.RLock()
	ks := a.knowledgeSt
	a.mu.RUnlock()

	if ks == nil {
		return nil, nil
	}
	return ks.ListSources(a.ctx)
}

// DeleteKnowledgeSource removes all chunks for a given source file.
func (a *App) DeleteKnowledgeSource(source string) error {
	a.mu.RLock()
	ks := a.knowledgeSt
	a.mu.RUnlock()

	if ks == nil {
		return fmt.Errorf("knowledge store not initialized")
	}
	return ks.DeleteBySource(a.ctx, source)
}

// OpenFileDialog opens a native file picker and returns the selected path.
func (a *App) OpenFileDialog(title string, filters []wailsruntime.FileFilter) (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:   title,
		Filters: filters,
	})
}

// GetScreenSize returns the primary screen's [width, height] in pixels.
func (a *App) GetScreenSize() []int {
	screens, err := wailsruntime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return []int{1440, 900}
	}
	for _, s := range screens {
		if s.IsPrimary {
			return []int{s.Size.Width, s.Size.Height}
		}
	}
	return []int{screens[0].Size.Width, screens[0].Size.Height}
}

// GetToolPermissions returns all tool permission rows for the settings UI.
func (a *App) GetToolPermissions() ([]internaltools.PermissionRow, error) {
	return a.permStore.ListAll(a.ctx)
}

// SetToolPermission grants or revokes a tool permission.
func (a *App) SetToolPermission(toolName string, granted bool) error {
	if granted {
		return a.permStore.Grant(a.ctx, toolName)
	}
	return a.permStore.Revoke(a.ctx, toolName)
}

// ClearChatHistory deletes all short-term messages from SQLite and all
// long-term memory vectors from the chromem collection.
func (a *App) ClearChatHistory() error {
	if err := a.shortMem.DeleteAll(); err != nil {
		return fmt.Errorf("clear short-term memory: %w", err)
	}
	a.mu.RLock()
	longMem := a.longMem
	a.mu.RUnlock()
	if longMem != nil {
		embedder, err := llm.NewEmbedder(a.ctx, a.cfg)
		if err != nil {
			slog.Warn("ClearChatHistory: embedder init failed, skipping long-term memory clear", "err", err)
		} else if err := longMem.DeleteAll(a.vectorDB, embedder); err != nil {
			return fmt.Errorf("clear long-term memory: %w", err)
		}
	}
	slog.Info("ClearChatHistory: done")
	return nil
}

// GetAvailableModels returns a list of available Live2D model names by
// scanning subdirectories of the bundled live2d assets directory.
// The special "core" directory is excluded.
func (a *App) GetAvailableModels() []string {
	entries, err := assets.ReadDir("frontend/dist/live2d")
	if err != nil {
		return []string{"hiyori"}
	}
	var models []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "core" {
			models = append(models, e.Name())
		}
	}
	if len(models) == 0 {
		return []string{"hiyori"}
	}
	return models
}

// ExportChatHistory opens a native save dialog and writes the recent 1000
// messages as plain text to the user-chosen file. Returns nil if the user
// cancels without choosing a file.
func (a *App) ExportChatHistory() error {
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出聊天记录",
		DefaultFilename: fmt.Sprintf("chat-export-%s.txt", time.Now().Format("20060102-150405")),
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "文本文件", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return nil // user cancelled
	}

	msgs, err := a.shortMem.Recent(1000)
	if err != nil {
		return fmt.Errorf("load messages: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "聊天记录导出 — %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	for _, m := range msgs {
		label := m.Role
		switch m.Role {
		case "user":
			label = "用户"
		case "assistant":
			label = "宠物"
		}
		fmt.Fprintf(&sb, "[%s] %s\n%s\n\n", m.CreatedAt, label, m.Content)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// IsFirstLaunch reports whether the welcome message has never been shown.
func (a *App) IsFirstLaunch() bool {
	var val string
	err := a.sqlDB.QueryRowContext(a.ctx,
		`SELECT value FROM settings WHERE key = 'welcome_shown'`).Scan(&val)
	return errors.Is(err, sql.ErrNoRows)
}

// MarkWelcomeShown records that the welcome message has been displayed.
func (a *App) MarkWelcomeShown() error {
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key, value) VALUES('welcome_shown','1')
		 ON CONFLICT(key) DO UPDATE SET value='1'`)
	if err != nil {
		return fmt.Errorf("mark welcome shown: %w", err)
	}
	return nil
}

// ListLLMModels queries the OpenAI-compatible /v1/models endpoint using the
// provided baseURL and apiKey (taken directly from the settings form, not the
// saved config), and returns a sorted list of model IDs.
func (a *App) ListLLMModels(baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("LLM Base URL is not configured")
	}
	url := baseURL + "/models"

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ListOpenRouterModels fetches available models from OpenRouter's /api/v1/models/user endpoint.
// baseURL defaults to "https://openrouter.ai/api/v1" when empty.
func (a *App) ListOpenRouterModels(baseURL, apiKey string) ([]string, error) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	modelsURL := base + "/models/user"

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ListMCPServers returns all configured MCP server entries.
func (a *App) ListMCPServers() ([]mcp.ServerConfig, error) {
	return a.mcpStore.List(a.ctx)
}

// shutdown is called by Wails when the application is closing.
func (a *App) shutdown(_ context.Context) {
	a.mu.Lock()
	w := a.smsWatcher
	a.smsWatcher = nil
	pe := a.proactiveEngine
	a.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	if pe != nil {
		pe.Stop()
	}
}

// startSMSWatcher creates and starts an SMS watcher, emitting verification code
// events to the frontend and copying the code to the clipboard.
// Caller must NOT hold a.mu.
func (a *App) startSMSWatcher() error {
	w, err := sms.NewWatcher(func(evt sms.Event) {
		wailsruntime.ClipboardSetText(a.ctx, evt.Code)
		wailsruntime.EventsEmit(a.ctx, "sms:verification_code", map[string]any{
			"code":   evt.Code,
			"sender": evt.Sender,
			"text":   evt.Text,
		})
		wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
			"title":   "📱 验证码：" + evt.Code,
			"message": evt.Sender + "：" + evt.Text,
		})
	})
	if err != nil {
		return err
	}
	if err := w.Start(a.ctx); err != nil {
		return err
	}
	a.mu.Lock()
	a.smsWatcher = w
	a.mu.Unlock()
	return nil
}

// StartSMSWatcher enables SMS monitoring, persists the setting, and starts the watcher.
func (a *App) StartSMSWatcher() error {
	a.mu.RLock()
	running := a.smsWatcher != nil
	a.mu.RUnlock()
	if running {
		return nil // already running
	}
	a.cfg.SMSWatcherEnabled = true
	if err := a.configStore.Save(a.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return a.startSMSWatcher()
}

// StopSMSWatcher disables SMS monitoring, persists the setting, and stops the watcher.
func (a *App) StopSMSWatcher() error {
	a.cfg.SMSWatcherEnabled = false
	if err := a.configStore.Save(a.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	a.mu.Lock()
	w := a.smsWatcher
	a.smsWatcher = nil
	a.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	return nil
}

// IsSMSWatcherRunning reports whether the SMS watcher is currently active.
func (a *App) IsSMSWatcherRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.smsWatcher != nil
}

// StopGeneration cancels the current in-flight chat stream.
// The frontend is responsible for marking the interrupted messages as ghost bubbles.
func (a *App) StopGeneration() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
}

// GetVoiceAutoSend returns whether voice messages are sent automatically
// after the final STT result arrives.
func (a *App) GetVoiceAutoSend() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.VoiceAutoSend
}

// SetVoiceAutoSend sets the voice auto-send flag and persists it.
func (a *App) SetVoiceAutoSend(enabled bool) error {
	a.mu.Lock()
	a.cfg.VoiceAutoSend = enabled
	a.mu.Unlock()
	return a.configStore.Save(a.cfg)
}

// GetSoundsEnabled returns whether chat sound effects are enabled.
func (a *App) GetSoundsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.SoundsEnabled
}

// SetSoundsEnabled sets the sounds enabled flag and persists it.
func (a *App) SetSoundsEnabled(enabled bool) error {
	a.mu.Lock()
	a.cfg.SoundsEnabled = enabled
	a.mu.Unlock()
	return a.configStore.Save(a.cfg)
}

// AddMCPServer adds a new MCP server configuration and reloads tools.
func (a *App) AddMCPServer(cfg mcp.ServerConfig) (mcp.ServerConfig, error) {
	result, err := a.mcpStore.Add(a.ctx, cfg)
	if err != nil {
		return result, err
	}
	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("AddMCPServer: LLM reinit skipped", "err", err)
	}
	return result, nil
}

// UpdateMCPServer updates an existing MCP server configuration and reloads tools.
func (a *App) UpdateMCPServer(cfg mcp.ServerConfig) error {
	if err := a.mcpStore.Update(a.ctx, cfg); err != nil {
		return err
	}
	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("UpdateMCPServer: LLM reinit skipped", "err", err)
	}
	return nil
}

// DeleteMCPServer removes an MCP server configuration by ID and reloads tools.
func (a *App) DeleteMCPServer(id int64) error {
	if err := a.mcpStore.Delete(a.ctx, id); err != nil {
		return err
	}
	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("DeleteMCPServer: LLM reinit skipped", "err", err)
	}
	return nil
}

// ListCronJobs returns all scheduled jobs.
func (a *App) ListCronJobs() ([]scheduler.Job, error) {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return []scheduler.Job{}, nil
	}
	return sched.ListJobs(a.ctx)
}

// CreateCronJob creates a new scheduled job.
func (a *App) CreateCronJob(name, description, schedule, prompt string) (scheduler.Job, error) {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return scheduler.Job{}, fmt.Errorf("scheduler not ready")
	}
	return sched.CreateJob(a.ctx, name, description, schedule, prompt)
}

// UpdateCronJob updates an existing scheduled job.
func (a *App) UpdateCronJob(id int64, name, description, schedule, prompt string) (scheduler.Job, error) {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return scheduler.Job{}, fmt.Errorf("scheduler not ready")
	}
	return sched.UpdateJob(a.ctx, id, name, description, schedule, prompt)
}

// DeleteCronJob removes a scheduled job by ID.
func (a *App) DeleteCronJob(id int64) error {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.DeleteJob(a.ctx, id)
}

// SetCronJobEnabled enables or disables a scheduled job.
func (a *App) SetCronJobEnabled(id int64, enabled bool) error {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.SetJobEnabled(a.ctx, id, enabled)
}

// RunCronJobNow fires a scheduled job immediately regardless of its schedule.
func (a *App) RunCronJobNow(id int64) error {
	a.mu.RLock()
	sched := a.scheduler
	a.mu.RUnlock()
	if sched == nil {
		return fmt.Errorf("scheduler not ready")
	}
	return sched.RunJobNow(id)
}

// LarkStatus returns the output of `lark-cli auth status`.
func (a *App) LarkStatus() (string, error) {
	cliPath := lark.FindCLI()
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装，请运行：npm install -g @larksuite/cli")
	}
	return lark.NewClient(cliPath).Status(a.ctx)
}

// LarkRunCommand executes an arbitrary lark-cli command string and returns stdout.
func (a *App) LarkRunCommand(args string) (string, error) {
	cliPath := lark.FindCLI()
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装")
	}
	return lark.NewClient(cliPath).Run(a.ctx, strings.Fields(args)...)
}

// stripNonSpeech removes emoji and kaomoji from text before TTS synthesis.
// Emoji are identified by Unicode ranges (Emoji/Symbol/Misc blocks).
// Kaomoji are matched by common bracket patterns like (=^･ω･^=) and (╥_╥).
func stripNonSpeech(s string) string {
	// Remove kaomoji: sequences starting with ( or ╥ etc. containing non-ASCII
	// Use a simple rune scan: strip parenthesized runs that contain non-letter non-digit runes.
	var buf strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		// Detect emoji / symbols / misc unicode blocks
		if isEmojiRune(r) {
			i++
			// Skip variation selectors and zero-width joiners that follow
			for i < len(runes) && (runes[i] == 0xFE0F || runes[i] == 0x200D || (runes[i] >= 0x1F3FB && runes[i] <= 0x1F3FF)) {
				i++
			}
			continue
		}
		// Detect kaomoji: opening paren followed by run with non-letter/digit content, closing paren
		if (r == '(' || r == '（') && i+1 < len(runes) {
			end := -1
			hasKaomoji := false
			for j := i + 1; j < len(runes) && j < i+20; j++ {
				if runes[j] == ')' || runes[j] == '）' {
					end = j
					break
				}
				if !unicode.IsLetter(runes[j]) && !unicode.IsDigit(runes[j]) && !unicode.IsSpace(runes[j]) {
					hasKaomoji = true
				}
			}
			if end > 0 && hasKaomoji {
				i = end + 1
				continue
			}
		}
		buf.WriteRune(r)
		i++
	}
	// Collapse multiple spaces left behind
	return strings.Join(strings.Fields(buf.String()), " ")
}

// isEmojiRune reports whether r is in an emoji/symbol Unicode range.
func isEmojiRune(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FFFF) || // Mahjong, dominoes, misc symbols & pictographs, emoticons, etc.
		(r >= 0x2600 && r <= 0x27BF) || // Misc symbols, dingbats
		(r >= 0x2300 && r <= 0x23FF) || // Misc technical
		(r >= 0xFE00 && r <= 0xFE0F) || // Variation selectors
		r == 0x200D || // Zero-width joiner
		(r >= 0x1F900 && r <= 0x1F9FF) || // Supplemental symbols
		(r >= 0x1FA00 && r <= 0x1FAFF) // Chess, symbols extended
}

// SpeakText synthesizes text to speech using the current TTS backend.
// If text exceeds TTSSummarizeThreshold runes, it is first summarized by the LLM.
// Audio bytes are emitted as tts:audio (base64 WAV); system speaker plays directly without tts:audio.
// Events: tts:start, tts:audio (optional), tts:done, tts:error.
func (a *App) SpeakText(text string) error {
	a.mu.Lock()
	if a.ttsCancel != nil {
		a.ttsCancel()
	}
	a.ttsGeneration++
	myGen := a.ttsGeneration
	ctx, cancel := context.WithCancel(a.ctx)
	a.ttsCancel = cancel
	speaker := a.ttsSpeaker
	cfg := a.cfg
	a.mu.Unlock()

	slog.Info("tts: SpeakText called", "backend", cfg.TTSBackend, "len", len([]rune(text)), "speaker", fmt.Sprintf("%T", speaker), "voice", cfg.TTSVoice)

	if speaker == nil {
		speaker = &tts.SystemSpeaker{}
	}

	wailsruntime.EventsEmit(a.ctx, "tts:start", nil)

	go func() {
		// Only nil out ttsCancel if this goroutine's generation is still current,
		// to avoid wiping a newer call's cancel when overlapping SpeakText calls race.
		defer func() {
			a.mu.Lock()
			if a.ttsGeneration == myGen {
				a.ttsCancel = nil
			}
			a.mu.Unlock()
		}()

		finalText := text
		threshold := cfg.TTSSummarizeThreshold
		if threshold > 0 && len([]rune(text)) > threshold {
			summary, err := a.ChatDirectCollect(ctx, "请用简洁的中文口语总结以下内容，控制在100字以内，适合朗读：\n"+text)
			if err == nil && strings.TrimSpace(summary) != "" {
				finalText = strings.TrimSpace(summary)
			}
		}

		speakText := stripNonSpeech(finalText)
		slog.Info("tts: calling Speak", "text_len", len([]rune(speakText)), "voice", cfg.TTSVoice, "speed", cfg.TTSSpeed)
		audioBytes, err := speaker.Speak(ctx, speakText, cfg.TTSVoice, cfg.TTSSpeed)
		if err != nil {
			slog.Warn("tts: Speak error", "err", err)
			if ctx.Err() != nil {
				wailsruntime.EventsEmit(a.ctx, "tts:done", nil)
				return
			}
			wailsruntime.EventsEmit(a.ctx, "tts:error", err.Error())
			return
		}

		slog.Info("tts: Speak done", "audio_bytes", len(audioBytes))
		if len(audioBytes) > 0 {
			encoded := base64.StdEncoding.EncodeToString(audioBytes)
			wailsruntime.EventsEmit(a.ctx, "tts:audio", map[string]string{
				"data":   encoded,
				"format": "wav",
			})
		}
		wailsruntime.EventsEmit(a.ctx, "tts:done", nil)
	}()

	return nil
}

// StopTTS cancels any in-flight TTS synthesis or playback.
func (a *App) StopTTS() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ttsCancel != nil {
		a.ttsCancel()
		a.ttsCancel = nil
	}
}

// GetKokoroTTSVoices returns the static list of Kokoro Chinese voices.
func (a *App) GetKokoroTTSVoices() ([]string, error) {
	speaker := tts.New("kokoro", "")
	return speaker.Voices(a.ctx)
}

// GetTTSAutoPlay returns whether TTS auto-play is enabled.
func (a *App) GetTTSAutoPlay() bool {
	return a.cfg.TTSAutoPlay
}

// SetTTSAutoPlay sets TTS auto-play and persists it.
func (a *App) SetTTSAutoPlay(enabled bool) error {
	a.cfg.TTSAutoPlay = enabled
	return a.configStore.Save(a.cfg)
}

// SetupKokoroTTS 在后台异步安装 Kokoro TTS 环境：
// 1. 创建 Python venv (~/.aiko/tts-venv)
// 2. 升级 pip
// 3. 安装 kokoro-onnx、misaki[zh]、soundfile
// 4. 下载模型文件 kokoro-v1.0.onnx 和 voices-v1.0.bin
// 进度通过 notification:show 事件汇报。方法立即返回 nil，安装在 goroutine 中运行。
func (a *App) SetupKokoroTTS() error {
	go func() {
		notify := func(title, msg string) {
			wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
				"title": title, "message": msg,
			})
		}
		home, _ := os.UserHomeDir()
		venvDir := filepath.Join(home, ".aiko", "tts-venv")
		modelsDir := filepath.Join(venvDir, "models")
		pip := filepath.Join(venvDir, "bin", "pip")

		// Step 0: check Python version (kokoro-onnx requires >= 3.10)
		verOut, verErr := exec.Command("python3", "-c",
			"import sys; v=sys.version_info; print(v.major,v.minor)").Output()
		if verErr != nil {
			notify("❌ TTS 安装失败", "未找到 python3，请先安装 Python 3.10+")
			return
		}
		var major, minor int
		fmt.Sscanf(strings.TrimSpace(string(verOut)), "%d %d", &major, &minor)
		if major < 3 || (major == 3 && minor < 10) {
			notify("❌ Python 版本过低",
				fmt.Sprintf("当前 Python %d.%d，kokoro-onnx 需要 3.10+，请先升级 Python", major, minor))
			return
		}

		// cleanup 在安装失败时删除不完整的 venv 目录，避免残留干扰下次安装。
		cleanup := func(msg string) {
			_ = os.RemoveAll(venvDir)
			notify("❌ TTS 安装失败", msg)
		}

		// Step 1: venv
		notify("🐍 Kokoro TTS", "创建 Python 虚拟环境…")
		if err := run("python3", "-m", "venv", venvDir); err != nil {
			cleanup(err.Error())
			return
		}

		// Step 2: pip upgrade (best-effort)
		_ = run(pip, "install", "--upgrade", "pip", "-q")

		// Step 3: pip install
		notify("📦 Kokoro TTS", "安装依赖包（约 1-2 分钟）…")
		if err := run(pip, "install", "-q", "kokoro-onnx", "misaki[zh]", "soundfile"); err != nil {
			cleanup(err.Error())
			return
		}

		// Step 4: download models
		if err := os.MkdirAll(modelsDir, 0755); err != nil {
			cleanup(err.Error())
			return
		}
		base := "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/"
		for _, f := range []string{"kokoro-v1.0.onnx", "voices-v1.0.bin"} {
			dst := filepath.Join(modelsDir, f)
			if _, err := os.Stat(dst); err == nil {
				continue // already exists, skip
			}
			notify("⬇️ Kokoro TTS", fmt.Sprintf("下载 %s…", f))
			if err := downloadFile(dst, base+f); err != nil {
				cleanup(err.Error())
				return
			}
		}
		notify("✅ Kokoro TTS", "环境安装完成！请保存配置即可使用。")
	}()
	return nil
}

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

// downloadFile 通过 HTTP GET 将远程文件流式写入本地路径。
func downloadFile(dst, url string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	chromem "github.com/philippgille/chromem-go"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"desktop-pet/internal/agent"
	"desktop-pet/internal/agent/middleware"
	"desktop-pet/internal/config"
	"desktop-pet/internal/db"
	"desktop-pet/internal/knowledge"
	"desktop-pet/internal/llm"
	"desktop-pet/internal/memory"
	"desktop-pet/internal/skill"
	internaltools "desktop-pet/internal/tools"
)

// App is the main application struct. All exported methods are Wails bindings.
type App struct {
	ctx         context.Context
	sqlDB       *sql.DB
	configStore *config.Store
	cfg         *config.Config
	vectorDB    *chromem.DB
	shortMem    *memory.ShortStore
	permStore   *internaltools.PermissionStore

	// mu guards fields that may be replaced on config save while agent goroutines run.
	mu          sync.RWMutex
	longMem     *memory.LongStore
	knowledgeSt *knowledge.Store
	petAgent    *agent.Agent
}

// NewApp creates a new App instance.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Errorf("get home dir: %w", err))
	}
	dataDir := filepath.Join(home, ".desktop-pet")

	a.sqlDB, err = db.Open(dataDir)
	if err != nil {
		panic(err)
	}
	a.configStore = config.NewStore(a.sqlDB)
	a.cfg, err = a.configStore.Load()
	if err != nil {
		panic(err)
	}

	a.shortMem = memory.NewShortStore(a.sqlDB)

	a.permStore = internaltools.NewPermissionStore(a.sqlDB)
	// Ensure all built-in tools have rows in tool_permissions.
	toolsCtx := context.Background()
	for _, t := range internaltools.All() {
		_ = a.permStore.EnsureRow(toolsCtx, t)
	}

	vectorPath := filepath.Join(dataDir, "vectors")
	a.vectorDB, err = chromem.NewPersistentDB(vectorPath, false)
	if err != nil {
		panic(err)
	}

	if len(a.cfg.MissingRequired()) == 0 {
		if err := a.initLLMComponents(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "init llm components: %v\n", err)
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
}

// initLLMComponents initializes chat model, embedder, memory stores, skills, and agent.
// Callers must NOT hold mu when calling this function.
func (a *App) initLLMComponents(ctx context.Context) error {
	chatModel, err := llm.NewChatModel(ctx, a.cfg)
	if err != nil {
		return fmt.Errorf("new chat model: %w", err)
	}

	embedder, err := llm.NewEmbedder(ctx, a.cfg)
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

	// Built-in tools + skill tools
	builtinTools := internaltools.AllEino(a.permStore)
	skillTools, err := skill.LoadAll(a.cfg.SkillsDir)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	allTools := append(builtinTools, skillTools...)

	// Middleware chain: logging -> retry -> error recovery (outermost first)
	mw := middleware.Chain(
		middleware.Logging(),
		middleware.Retry(3, 200*time.Millisecond),
		middleware.ErrorRecovery(),
	)

	newAgent, err := agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw)
	if err != nil {
		return fmt.Errorf("new agent: %w", err)
	}

	a.mu.Lock()
	a.longMem = longMem
	a.knowledgeSt = knowledgeSt
	a.petAgent = newAgent
	a.mu.Unlock()
	return nil
}

// GetConfig returns the current config to the frontend.
func (a *App) GetConfig() *config.Config { return a.cfg }

// SaveConfig persists updated config and reinitializes LLM components.
func (a *App) SaveConfig(cfg *config.Config) error {
	if err := a.configStore.Save(cfg); err != nil {
		return err
	}
	a.cfg = cfg
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

// MissingRequiredConfig returns names of empty required config fields.
func (a *App) MissingRequiredConfig() []string {
	return a.cfg.MissingRequired()
}

// SendMessage sends a user message and streams response tokens as Wails events.
// Events emitted: "chat:token" (string), "chat:done" (""), "chat:error" (string).
func (a *App) SendMessage(userInput string) error {
	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()

	if ag == nil {
		return fmt.Errorf("agent not initialized: complete settings first")
	}
	go func() {
		ch := ag.Chat(a.ctx, userInput)
		for result := range ch {
			if result.Err != nil {
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

// ToggleBubble emits the bubble:toggle event to open/close the chat bubble.
func (a *App) ToggleBubble() {
	wailsruntime.EventsEmit(a.ctx, "bubble:toggle")
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

// EmitPetState broadcasts a pet state change event to the frontend.
// Valid states: "idle", "thinking", "speaking", "listening", "error".
func (a *App) EmitPetState(state string) {
	wailsruntime.EventsEmit(a.ctx, "pet:state:change", state)
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
			return fmt.Errorf("rebuild embedder for clear: %w", err)
		}
		if err := longMem.DeleteAll(a.vectorDB, embedder); err != nil {
			return fmt.Errorf("clear long-term memory: %w", err)
		}
	}
	return nil
}

// GetAvailableModels returns a list of available Live2D model names by
// scanning subdirectories of the bundled live2d assets directory.
// The special "core" directory is excluded.
func (a *App) GetAvailableModels() []string {
	// Wails serves static files from frontend/public; at runtime the
	// WebView root maps to that directory, but the Go process needs the
	// filesystem path. We locate it relative to the executable's working dir.
	base := "frontend/public/live2d"
	entries, err := os.ReadDir(base)
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

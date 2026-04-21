package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	chromem "github.com/philippgille/chromem-go"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"desktop-pet/internal/agent"
	"desktop-pet/internal/config"
	"desktop-pet/internal/db"
	"desktop-pet/internal/knowledge"
	"desktop-pet/internal/llm"
	"desktop-pet/internal/memory"
	"desktop-pet/internal/skill"
)

// App is the main application struct. All exported methods are Wails bindings.
type App struct {
	ctx         context.Context
	sqlDB       *sql.DB
	configStore *config.Store
	cfg         *config.Config
	vectorDB    *chromem.DB
	shortMem    *memory.ShortStore
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
}

// initLLMComponents initializes chat model, embedder, memory stores, skills, and agent.
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
		knowledgeSt, err = knowledge.NewStore(a.vectorDB, embedder)
		if err != nil {
			return fmt.Errorf("new knowledge store: %w", err)
		}
	}
	a.longMem = longMem
	a.knowledgeSt = knowledgeSt

	skills, err := skill.LoadAll(a.cfg.SkillsDir)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	a.petAgent, err = agent.New(ctx, chatModel, a.shortMem, longMem, skills, a.cfg)
	if err != nil {
		return fmt.Errorf("new agent: %w", err)
	}
	return nil
}

// GetConfig returns the current config to the frontend.
func (a *App) GetConfig() *config.Config { return a.cfg }

// SaveConfig persists updated config and reinitializes LLM components.
func (a *App) SaveConfig(cfg *config.Config) error {
	a.cfg = cfg
	if err := a.configStore.Save(cfg); err != nil {
		return err
	}
	return a.initLLMComponents(a.ctx)
}

// MissingRequiredConfig returns names of empty required config fields.
func (a *App) MissingRequiredConfig() []string {
	return a.cfg.MissingRequired()
}

// SendMessage sends a user message and streams response tokens as Wails events.
// Events emitted: "chat:token" (string), "chat:done" (""), "chat:error" (string).
func (a *App) SendMessage(userInput string) error {
	if a.petAgent == nil {
		return fmt.Errorf("agent not initialized: complete settings first")
	}
	go func() {
		ch := a.petAgent.Chat(a.ctx, userInput)
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
	if a.knowledgeSt == nil {
		return fmt.Errorf("knowledge store not initialized: configure embedding model first")
	}
	return knowledge.Import(a.ctx, a.knowledgeSt, filePath, func(p knowledge.ImportProgress) {
		wailsruntime.EventsEmit(a.ctx, "knowledge:progress", p)
	})
}

// ListKnowledgeSources returns distinct source filenames in the knowledge base.
func (a *App) ListKnowledgeSources() ([]string, error) {
	if a.knowledgeSt == nil {
		return nil, nil
	}
	return a.knowledgeSt.ListSources(a.ctx)
}

// DeleteKnowledgeSource removes all chunks for a given source file.
func (a *App) DeleteKnowledgeSource(source string) error {
	if a.knowledgeSt == nil {
		return fmt.Errorf("knowledge store not initialized")
	}
	return a.knowledgeSt.DeleteBySource(a.ctx, source)
}

// GetScreenSize returns the primary screen's width and height in pixels.
func (a *App) GetScreenSize() (int, int) {
	screens, err := wailsruntime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return 1440, 900
	}
	return screens[0].Width, screens[0].Height
}

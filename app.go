package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"desktop-pet/internal/config"
	"desktop-pet/internal/db"
)

// App is the main application struct. Methods on this struct are exposed to the frontend via Wails bindings.
type App struct {
	ctx         context.Context
	sqlDB       *sql.DB
	configStore *config.Store
	cfg         *config.Config
}

// NewApp creates a new App instance.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dataDir := filepath.Join(os.Getenv("HOME"), ".desktop-pet")
	var err error
	a.sqlDB, err = db.Open(dataDir)
	if err != nil {
		panic(err)
	}
	a.configStore = config.NewStore(a.sqlDB)
	a.cfg, err = a.configStore.Load()
	if err != nil {
		panic(err)
	}
}

// GetConfig returns the current config to the frontend.
func (a *App) GetConfig() *config.Config { return a.cfg }

// SaveConfig saves updated config from the frontend.
func (a *App) SaveConfig(cfg *config.Config) error {
	a.cfg = cfg
	return a.configStore.Save(cfg)
}

// MissingRequiredConfig returns field names that are required but empty.
func (a *App) MissingRequiredConfig() []string {
	return a.cfg.MissingRequired()
}

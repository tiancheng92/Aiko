package main

import (
	"fmt"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/knowledge"
)

// ImportKnowledge imports a file into the knowledge base asynchronously.
// Emits "knowledge:progress" during import, "knowledge:done" on success,
// and "knowledge:error" on failure.
func (a *App) ImportKnowledge(filePath string) error {
	a.mu.RLock()
	ks := a.knowledgeSt
	a.mu.RUnlock()

	if ks == nil {
		return fmt.Errorf("knowledge store not initialized: configure embedding model first")
	}
	go func() {
		err := knowledge.Import(a.ctx, ks, filePath, func(p knowledge.ImportProgress) {
			wailsruntime.EventsEmit(a.ctx, "knowledge:progress", p)
		})
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "knowledge:error", err.Error())
			return
		}
		wailsruntime.EventsEmit(a.ctx, "knowledge:done", nil)
	}()
	return nil
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

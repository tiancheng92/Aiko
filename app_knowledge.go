package main

import (
	"fmt"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/knowledge"
)

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

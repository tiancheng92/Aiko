//go:build darwin

package services

import (
	"fmt"

	"aiko/internal/knowledge"
)

// KnowledgeService manages the RAG knowledge base.
type KnowledgeService struct{ s *sharedState }

// NewKnowledgeService creates a KnowledgeService backed by the given shared state.
func NewKnowledgeService(s *sharedState) *KnowledgeService { return &KnowledgeService{s: s} }

// ImportKnowledge imports a file into the knowledge base.
// Emits "knowledge:progress" events during import.
func (k *KnowledgeService) ImportKnowledge(filePath string) error {
	k.s.mu.RLock()
	ks := k.s.knowledgeSt
	k.s.mu.RUnlock()

	if ks == nil {
		return fmt.Errorf("knowledge store not initialized: configure embedding model first")
	}
	return knowledge.Import(k.s.ctx, ks, filePath, func(p knowledge.ImportProgress) {
		k.s.app.Event.Emit("knowledge:progress", p)
	})
}

// ListKnowledgeSources returns distinct source filenames in the knowledge base.
func (k *KnowledgeService) ListKnowledgeSources() ([]string, error) {
	k.s.mu.RLock()
	ks := k.s.knowledgeSt
	k.s.mu.RUnlock()

	if ks == nil {
		return nil, nil
	}
	return ks.ListSources(k.s.ctx)
}

// DeleteKnowledgeSource removes all chunks for a given source file.
func (k *KnowledgeService) DeleteKnowledgeSource(source string) error {
	k.s.mu.RLock()
	ks := k.s.knowledgeSt
	k.s.mu.RUnlock()

	if ks == nil {
		return fmt.Errorf("knowledge store not initialized")
	}
	return ks.DeleteBySource(k.s.ctx, source)
}

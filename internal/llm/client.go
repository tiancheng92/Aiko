package llm

import (
	"context"
	"fmt"

	embeddopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"

	"desktop-pet/internal/config"
)

// NewChatModel creates an eino ToolCallingChatModel from config.
// The openai implementation satisfies both model.ChatModel and model.ToolCallingChatModel.
func NewChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	if cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
		return nil, fmt.Errorf("llm_base_url and llm_model are required")
	}
	return einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		BaseURL: cfg.LLMBaseURL,
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
	})
}

// NewEmbedder creates an eino Embedder from config. Returns nil, nil if embedding not configured.
func NewEmbedder(ctx context.Context, cfg *config.Config) (embedding.Embedder, error) {
	if !cfg.VectorEnabled() {
		return nil, nil
	}
	return embeddopenai.NewEmbedder(ctx, &embeddopenai.EmbeddingConfig{
		BaseURL: cfg.LLMBaseURL,
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.EmbeddingModel,
	})
}

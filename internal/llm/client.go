package llm

import (
	"context"
	"fmt"
	"strings"

	embeddopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	einoopenrouter "github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"desktop-pet/internal/config"
)

// NewChatModel creates an eino ToolCallingChatModel from config.
// Selects the backend based on cfg.LLMProvider.
func NewChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	if cfg.LLMBaseURL == "" && cfg.LLMProvider != string(config.ProviderOpenRouter) {
		return nil, fmt.Errorf("llm_base_url is required")
	}
	if cfg.LLMModel == "" {
		return nil, fmt.Errorf("llm_model is required")
	}
	switch config.Provider(cfg.LLMProvider) {
	case config.ProviderOpenRouter:
		return einoopenrouter.NewChatModel(ctx, &einoopenrouter.Config{
			APIKey:  cfg.LLMAPIKey,
			BaseURL: cfg.LLMBaseURL, // empty = default openrouter endpoint
			Model:   cfg.LLMModel,
		})
	default: // openai-compatible
		return einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
			BaseURL: cfg.LLMBaseURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
		})
	}
}

// NewEmbedder creates an eino Embedder from config. Returns nil, nil if embedding not configured.
// Always uses the OpenAI-compatible embedder (OpenRouter does not expose an embeddings endpoint).
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

// Summarizer generates a one-sentence summary of a text block.
type Summarizer interface {
	Summarize(ctx context.Context, text string) (string, error)
}

// llmSummarizer calls the chat model with a fixed summarization prompt.
type llmSummarizer struct {
	model model.ToolCallingChatModel
}

// NewSummarizer creates a Summarizer backed by the chat model.
// Returns nil if cfg has no LLM configured (so caller can skip summarization).
func NewSummarizer(ctx context.Context, cfg *config.Config) (Summarizer, error) {
	if cfg.LLMModel == "" {
		return nil, nil
	}
	m, err := NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new summarizer model: %w", err)
	}
	return &llmSummarizer{model: m}, nil
}

// Summarize generates a one-sentence summary of text using the chat model.
func (s *llmSummarizer) Summarize(ctx context.Context, text string) (string, error) {
	prompt := "请用一句话总结以下对话内容的核心主题，不超过30个字：\n\n" + text
	msgs := []*schema.Message{
		{Role: schema.User, Content: prompt},
	}
	resp, err := s.model.Generate(ctx, msgs)
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	if resp == nil || resp.Content == "" {
		return "", nil
	}
	return strings.TrimSpace(resp.Content), nil
}

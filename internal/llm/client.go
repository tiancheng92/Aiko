package llm

import (
	"context"
	"fmt"
	"strings"

	embeddopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"desktop-pet/internal/config"
)

// NewChatModel creates an eino ToolCallingChatModel from config.
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
	if cfg.LLMBaseURL == "" || cfg.LLMModel == "" {
		return nil, nil
	}
	m, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		BaseURL: cfg.LLMBaseURL,
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
	})
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

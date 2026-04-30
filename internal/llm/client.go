package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	embeddopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	einoopenrouter "github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

// ErrorBodyTransport wraps http.DefaultTransport and stores the raw response
// body of the most recent non-2xx response. This lets callers retrieve the
// original provider error JSON that go-openai's APIError may not fully expose
// (e.g. OpenRouter's error.metadata.raw field).
type ErrorBodyTransport struct {
	mu   sync.Mutex
	body []byte
	base http.RoundTripper
}

// RoundTrip executes the request. For non-2xx responses it buffers the body so
// it can be read both by the underlying go-openai client and by LastErrorBody.
func (t *ErrorBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil {
			t.mu.Lock()
			t.body = raw
			t.mu.Unlock()
			// Restore body so go-openai can still parse the error response.
			resp.Body = io.NopCloser(bytes.NewReader(raw))
		}
	}
	return resp, nil
}

// LastErrorBody returns the raw body from the most recent non-2xx response.
func (t *ErrorBodyTransport) LastErrorBody() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.body) == 0 {
		return nil
	}
	cp := make([]byte, len(t.body))
	copy(cp, t.body)
	return cp
}

// NewChatModel creates an eino ToolCallingChatModel from config.
// Selects the backend based on cfg.LLMProvider.
// Returns the model and an ErrorBodyTransport that captures raw error responses.
func NewChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, *ErrorBodyTransport, error) {
	if cfg.LLMBaseURL == "" && cfg.LLMProvider != string(config.ProviderOpenRouter) {
		return nil, nil, fmt.Errorf("llm_base_url is required")
	}
	if cfg.LLMModel == "" {
		return nil, nil, fmt.Errorf("llm_model is required")
	}
	transport := &ErrorBodyTransport{}
	httpClient := &http.Client{Transport: transport}
	switch config.Provider(cfg.LLMProvider) {
	case config.ProviderOpenRouter:
		m, err := einoopenrouter.NewChatModel(ctx, &einoopenrouter.Config{
			APIKey:     cfg.LLMAPIKey,
			BaseURL:    cfg.LLMBaseURL,
			Model:      cfg.LLMModel,
			HTTPClient: httpClient,
		})
		return m, transport, err
	default: // openai-compatible
		m, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
			BaseURL:    cfg.LLMBaseURL,
			APIKey:     cfg.LLMAPIKey,
			Model:      cfg.LLMModel,
			HTTPClient: httpClient,
		})
		return m, transport, err
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
	m, _, err := NewChatModel(ctx, cfg)
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

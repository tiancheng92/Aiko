package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	embeddopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	einoopenrouter "github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/config"
)

// sharedTransport is an HTTP transport tuned for LLM API calls: higher
// per-host connection pool and shorter TLS handshake timeout than
// http.DefaultTransport (which only keeps 2 idle conns per host).
var sharedTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 20,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

// ErrorBodyTransport wraps sharedTransport and stores the raw response
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
		base = sharedTransport
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
			// Enable prompt caching at the request level. Static prefix
			// (system prompt + USER.md + summary) will be cached for 1 hour,
			// saving ~50-90% token cost on repeat sends.
			CacheControl: &einoopenrouter.CacheControl{TTL: einoopenrouter.CacheControlTTL1Hour},
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
// Uses cfg.EmbeddingBaseURL and cfg.EmbeddingAPIKey (resolved by ApplyProfile from the active
// ModelProfile — either inherited from LLM config or independently set).
func NewEmbedder(ctx context.Context, cfg *config.Config) (embedding.Embedder, error) {
	if !cfg.VectorEnabled() {
		return nil, nil
	}
	return embeddopenai.NewEmbedder(ctx, &embeddopenai.EmbeddingConfig{
		BaseURL:    cfg.EmbeddingBaseURL,
		APIKey:     cfg.EmbeddingAPIKey,
		Model:      cfg.EmbeddingModel,
		HTTPClient: &http.Client{Transport: sharedTransport},
	})
}

// EnablePromptCaching marks a message for prompt caching by attaching an
// OpenRouter cache_control extra field. For non-OpenRouter providers the
// extra field is silently ignored by the serialiser, so it is always safe.
// Messages up to and including the marked message are eligible for caching;
// mark the last message in the static prefix (e.g. USER.md + summary) so
// the cache stays valid across turns.
func EnablePromptCaching(msg *schema.Message) {
	einoopenrouter.EnableMessageContentCacheControl(msg)
}


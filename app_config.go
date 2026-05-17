package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/config"
	"aiko/internal/tts"
)

// GetConfig returns a snapshot of the current config to the frontend so that
// concurrent writes via SaveConfig / ActivateModelProfile cannot tear reads.
func (a *App) GetConfig() *config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.cfg == nil {
		return &config.Config{}
	}
	copy := *a.cfg
	return &copy
}

// SaveConfig persists updated config and reinitializes LLM components.
// LLM init errors are non-fatal (user may save non-LLM settings before configuring the model).
func (a *App) SaveConfig(cfg *config.Config) error {
	// Preserve fields that are managed independently (not via the settings form).
	// Snapshot the preserved values under the read lock, then hand off the
	// merged struct to configStore.Save outside the lock to avoid holding mu
	// across a potentially slow DB transaction.
	a.mu.RLock()
	cfg.SMSWatcherEnabled = a.cfg.SMSWatcherEnabled
	cfg.VoiceAutoSend = a.cfg.VoiceAutoSend
	cfg.SoundsEnabled = a.cfg.SoundsEnabled
	cfg.TTSAutoPlay = a.cfg.TTSAutoPlay
	// Preserve ALL profile-derived fields so SaveConfig never clobbers them.
	// These fields live in model_profiles and are set exclusively via
	// SaveModelProfile / ActivateModelProfile. Any stale frontend value
	// (e.g. from a cfg.value that was loaded before a profile switch) must
	// not overwrite the profile that is currently active in a.cfg.
	cfg.LLMBaseURL = a.cfg.LLMBaseURL
	cfg.LLMAPIKey = a.cfg.LLMAPIKey
	cfg.LLMModel = a.cfg.LLMModel
	cfg.LLMProvider = a.cfg.LLMProvider
	cfg.EmbeddingModel = a.cfg.EmbeddingModel
	cfg.EmbeddingDim = a.cfg.EmbeddingDim
	cfg.TTSVoice = a.cfg.TTSVoice
	cfg.TTSSpeed = a.cfg.TTSSpeed
	cfg.TTSBackend = a.cfg.TTSBackend
	cfg.TTSModelDir = a.cfg.TTSModelDir
	a.mu.RUnlock()

	if err := a.configStore.Save(cfg); err != nil {
		return err
	}

	a.mu.Lock()
	*a.cfg = *cfg // update in-place so existing tool pointers see the new values
	a.mu.Unlock()

	if err := a.initLLMComponents(a.ctx); err != nil {
		slog.Warn("SaveConfig: LLM reinit skipped", "err", err)
	} else {
		wailsruntime.EventsEmit(a.ctx, "config:model:changed", nil)
	}
	return nil
}

// SetAvatar stores a custom avatar data URL for the given role ("ai" or "user")
// and emits config:avatar:changed so the frontend can update immediately.
func (a *App) SetAvatar(role string, dataURL string) error {
	a.mu.Lock()
	switch role {
	case "ai":
		a.cfg.AIAvatar = dataURL
	case "user":
		a.cfg.UserAvatar = dataURL
	default:
		a.mu.Unlock()
		return fmt.Errorf("unknown avatar role: %s", role)
	}
	cfgCopy := *a.cfg
	a.mu.Unlock()

	if err := a.configStore.Save(&cfgCopy); err != nil {
		return err
	}
	wailsruntime.EventsEmit(a.ctx, "config:avatar:changed", map[string]string{"role": role, "dataURL": dataURL})
	return nil
}

// ResetAvatar removes the custom avatar for the given role ("ai" or "user"),
// reverting to the built-in default, and emits config:avatar:changed.
func (a *App) ResetAvatar(role string) error {
	return a.SetAvatar(role, "")
}

// ListModelProfiles returns all saved model profiles.
func (a *App) ListModelProfiles() ([]config.ModelProfile, error) {
	return a.profileStore.List()
}

// SaveModelProfile creates or updates a model profile.
// If the saved profile is the currently active one, cfg is updated in-place so
// TTS voice/speed/backend changes take effect immediately without a full reinit.
func (a *App) SaveModelProfile(p config.ModelProfile) (config.ModelProfile, error) {
	a.mu.RLock()
	activeID := a.cfg.ActiveProfileID
	a.mu.RUnlock()
	slog.Info("SaveModelProfile", "id", p.ID, "backend", p.TTSBackend, "voice", p.TTSVoice, "speed", p.TTSSpeed, "activeID", activeID)
	if err := a.profileStore.Save(&p); err != nil {
		return p, err
	}
	// Sync cfg when saving the active profile, so changes apply immediately.
	if p.ID == activeID {
		a.mu.RLock()
		oldBaseURL := a.cfg.LLMBaseURL
		oldAPIKey := a.cfg.LLMAPIKey
		oldModel := a.cfg.LLMModel
		oldProvider := string(a.cfg.LLMProvider)
		a.mu.RUnlock()

		llmChanged := p.BaseURL != oldBaseURL || p.APIKey != oldAPIKey || p.Model != oldModel || string(p.Provider) != oldProvider

		a.mu.Lock()
		a.cfg.ApplyProfile(&p)
		slog.Info("SaveModelProfile: applied to cfg", "voice", a.cfg.TTSVoice)
		// 重建 TTS 实例，使 backend/voice/speed 变更立即生效。
		newKey := a.cfg.TTSBackend + "|" + a.cfg.TTSModelDir
		slog.Info("SaveModelProfile: tts rebuild check", "newKey", newKey, "oldKey", a.ttsBackendKey, "speakerNil", a.ttsSpeaker == nil)
		if a.ttsSpeaker == nil || newKey != a.ttsBackendKey {
			a.ttsSpeaker = tts.New(a.cfg.TTSBackend, a.cfg.TTSModelDir)
			a.ttsBackendKey = newKey
			slog.Info("SaveModelProfile: tts rebuilt", "type", fmt.Sprintf("%T", a.ttsSpeaker))
		}
		a.mu.Unlock()

		if llmChanged {
			if err := a.initLLMComponents(a.ctx); err != nil {
				slog.Warn("SaveModelProfile: LLM reinit skipped", "err", err)
			} else {
				wailsruntime.EventsEmit(a.ctx, "config:model:changed", nil)
			}
		}
	}
	return p, nil
}

// DeleteModelProfile removes a model profile by id.
func (a *App) DeleteModelProfile(id int64) error {
	return a.profileStore.Delete(id)
}

// ActivateModelProfile switches to the given profile and reinitializes LLM components.
func (a *App) ActivateModelProfile(id int64) error {
	p, err := a.profileStore.Get(id)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg.ApplyProfile(p)
	cfgCopy := *a.cfg
	a.mu.Unlock()
	// Persist any defaults written back to the profile (e.g. OpenRouter base URL).
	if err := a.profileStore.Save(p); err != nil {
		slog.Warn("ActivateModelProfile: save profile failed", "err", err)
	}
	if err := a.configStore.Save(&cfgCopy); err != nil {
		return err
	}
	if err := a.initLLMComponents(a.ctx); err != nil {
		return err
	}
	wailsruntime.EventsEmit(a.ctx, "config:model:changed", nil)
	return nil
}

// MissingRequiredConfig returns names of empty required config fields.
func (a *App) MissingRequiredConfig() []string {
	return a.cfg.MissingRequired()
}

// GetAvailableModels returns a list of available Live2D model names by
// scanning subdirectories of the bundled live2d assets directory.
// The special "core" directory is excluded.
func (a *App) GetAvailableModels() []string {
	entries, err := assets.ReadDir("frontend/dist/live2d")
	if err != nil {
		return []string{"hiyori"}
	}
	var models []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "core" {
			models = append(models, e.Name())
		}
	}
	if len(models) == 0 {
		return []string{"hiyori"}
	}
	return models
}

// ListLLMModels queries the OpenAI-compatible /v1/models endpoint using the
// provided baseURL and apiKey (taken directly from the settings form, not the
// saved config), and returns a sorted list of model IDs.
func (a *App) ListLLMModels(baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("LLM Base URL is not configured")
	}
	url := baseURL + "/models"

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ListOpenRouterModels fetches available models from OpenRouter's /api/v1/models/user endpoint.
// baseURL defaults to "https://openrouter.ai/api/v1" when empty.
func (a *App) ListOpenRouterModels(baseURL, apiKey string) ([]string, error) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	modelsURL := base + "/models/user"

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

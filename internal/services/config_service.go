//go:build darwin

package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	json "github.com/bytedance/sonic"

	"aiko/internal/config"
	"aiko/internal/execenv"
	"aiko/internal/tts"
)

// ConfigService handles app configuration, model profiles, and layout persistence.
type ConfigService struct{ s *sharedState }

// NewConfigService creates a ConfigService backed by the given shared state.
func NewConfigService(s *sharedState) *ConfigService { return &ConfigService{s: s} }

// GetConfig returns a snapshot of the current config to the frontend so that
// concurrent writes via SaveConfig / ActivateModelProfile cannot tear reads.
func (c *ConfigService) GetConfig() *config.Config {
	c.s.mu.RLock()
	defer c.s.mu.RUnlock()
	if c.s.cfg == nil {
		return &config.Config{}
	}
	copy := *c.s.cfg
	return &copy
}

// SaveConfig persists updated config and reinitializes LLM components.
// LLM init errors are non-fatal (user may save non-LLM settings before configuring the model).
func (c *ConfigService) SaveConfig(cfg *config.Config) error {
	// Preserve fields that are managed independently (not via the settings form).
	c.s.mu.RLock()
	cfg.SMSWatcherEnabled = c.s.cfg.SMSWatcherEnabled
	cfg.VoiceAutoSend = c.s.cfg.VoiceAutoSend
	cfg.SoundsEnabled = c.s.cfg.SoundsEnabled
	cfg.TTSAutoPlay = c.s.cfg.TTSAutoPlay
	// Preserve ALL profile-derived fields so SaveConfig never clobbers them.
	cfg.LLMBaseURL = c.s.cfg.LLMBaseURL
	cfg.LLMAPIKey = c.s.cfg.LLMAPIKey
	cfg.LLMModel = c.s.cfg.LLMModel
	cfg.LLMProvider = c.s.cfg.LLMProvider
	cfg.EmbeddingModel = c.s.cfg.EmbeddingModel
	cfg.EmbeddingDim = c.s.cfg.EmbeddingDim
	cfg.TTSVoice = c.s.cfg.TTSVoice
	cfg.TTSSpeed = c.s.cfg.TTSSpeed
	cfg.TTSBackend = c.s.cfg.TTSBackend
	cfg.TTSModelDir = c.s.cfg.TTSModelDir
	c.s.mu.RUnlock()

	if err := c.s.configStore.Save(cfg); err != nil {
		return err
	}

	c.s.mu.Lock()
	*c.s.cfg = *cfg
	c.s.mu.Unlock()

	if err := c.s.initLLMComponents(c.s.ctx); err != nil {
		slog.Warn("SaveConfig: LLM reinit skipped", "err", err)
	} else {
		c.s.app.Event.Emit("config:model:changed", nil)
	}
	return nil
}

// SetAvatar stores a custom avatar data URL for the given role ("ai" or "user")
// and emits config:avatar:changed so the frontend can update immediately.
func (c *ConfigService) SetAvatar(role string, dataURL string) error {
	c.s.mu.Lock()
	switch role {
	case "ai":
		c.s.cfg.AIAvatar = dataURL
	case "user":
		c.s.cfg.UserAvatar = dataURL
	default:
		c.s.mu.Unlock()
		return fmt.Errorf("unknown avatar role: %s", role)
	}
	cfgCopy := *c.s.cfg
	c.s.mu.Unlock()

	if err := c.s.configStore.Save(&cfgCopy); err != nil {
		return err
	}
	c.s.app.Event.Emit("config:avatar:changed", map[string]string{"role": role, "dataURL": dataURL})
	return nil
}

// ResetAvatar removes the custom avatar for the given role ("ai" or "user"),
// reverting to the built-in default, and emits config:avatar:changed.
func (c *ConfigService) ResetAvatar(role string) error {
	return c.SetAvatar(role, "")
}

// ListModelProfiles returns all saved model profiles.
func (c *ConfigService) ListModelProfiles() ([]config.ModelProfile, error) {
	return c.s.profileStore.List()
}

// SaveModelProfile creates or updates a model profile.
// If the saved profile is the currently active one, cfg is updated in-place so
// TTS voice/speed/backend changes take effect immediately without a full reinit.
func (c *ConfigService) SaveModelProfile(p config.ModelProfile) (config.ModelProfile, error) {
	c.s.mu.RLock()
	activeID := c.s.cfg.ActiveProfileID
	c.s.mu.RUnlock()
	slog.Info("SaveModelProfile", "id", p.ID, "backend", p.TTSBackend, "voice", p.TTSVoice, "speed", p.TTSSpeed, "activeID", activeID)
	if err := c.s.profileStore.Save(&p); err != nil {
		return p, err
	}
	// Sync cfg when saving the active profile, so changes apply immediately.
	if p.ID == activeID {
		c.s.mu.RLock()
		oldBaseURL := c.s.cfg.LLMBaseURL
		oldAPIKey := c.s.cfg.LLMAPIKey
		oldModel := c.s.cfg.LLMModel
		oldProvider := string(c.s.cfg.LLMProvider)
		c.s.mu.RUnlock()

		llmChanged := p.BaseURL != oldBaseURL || p.APIKey != oldAPIKey || p.Model != oldModel || string(p.Provider) != oldProvider

		c.s.mu.Lock()
		c.s.cfg.ApplyProfile(&p)
		slog.Info("SaveModelProfile: applied to cfg", "voice", c.s.cfg.TTSVoice)
		newKey := c.s.cfg.TTSBackend + "|" + c.s.cfg.TTSModelDir
		slog.Info("SaveModelProfile: tts rebuild check", "newKey", newKey, "oldKey", c.s.ttsBackendKey, "speakerNil", c.s.ttsSpeaker == nil)
		if c.s.ttsSpeaker == nil || newKey != c.s.ttsBackendKey {
			c.s.ttsSpeaker = tts.New(c.s.cfg.TTSBackend, c.s.cfg.TTSModelDir)
			c.s.ttsBackendKey = newKey
			slog.Info("SaveModelProfile: tts rebuilt", "type", fmt.Sprintf("%T", c.s.ttsSpeaker))
		}
		c.s.mu.Unlock()

		if llmChanged {
			if err := c.s.initLLMComponents(c.s.ctx); err != nil {
				slog.Warn("SaveModelProfile: LLM reinit skipped", "err", err)
			} else {
				c.s.app.Event.Emit("config:model:changed", nil)
			}
		}
	}
	return p, nil
}

// DeleteModelProfile removes a model profile by id.
func (c *ConfigService) DeleteModelProfile(id int64) error {
	return c.s.profileStore.Delete(id)
}

// ActivateModelProfile switches to the given profile and reinitializes LLM components.
func (c *ConfigService) ActivateModelProfile(id int64) error {
	p, err := c.s.profileStore.Get(id)
	if err != nil {
		return err
	}
	c.s.mu.Lock()
	c.s.cfg.ApplyProfile(p)
	cfgCopy := *c.s.cfg
	c.s.mu.Unlock()
	if err := c.s.profileStore.Save(p); err != nil {
		slog.Warn("ActivateModelProfile: save profile failed", "err", err)
	}
	if err := c.s.configStore.Save(&cfgCopy); err != nil {
		return err
	}
	if err := c.s.initLLMComponents(c.s.ctx); err != nil {
		return err
	}
	c.s.app.Event.Emit("config:model:changed", nil)
	return nil
}

// GetBallPosition returns the saved ball [x, y] for the given screen resolution,
// or [-1, -1] if no position has been saved for that resolution yet.
func (c *ConfigService) GetBallPosition(screenW, screenH int) []int {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	var val string
	if err := c.s.sqlDB.QueryRowContext(c.s.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return []int{-1, -1}
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return []int{-1, -1}
	}
	x, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return []int{-1, -1}
	}
	return []int{x, y}
}

// SaveBallPosition persists the ball position for the given screen resolution.
func (c *ConfigService) SaveBallPosition(x, y, screenW, screenH int) error {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	val := fmt.Sprintf("%d,%d", x, y)
	_, err := c.s.sqlDB.ExecContext(c.s.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}

// ResetBallPosition deletes the saved ball position for the given screen resolution,
// causing the pet to return to its default position on next render.
func (c *ConfigService) ResetBallPosition(screenW, screenH int) error {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	_, err := c.s.sqlDB.ExecContext(c.s.ctx, `DELETE FROM settings WHERE key=?`, key)
	return err
}

// GetPetSize returns the saved pet height for the given screen resolution, or 0 if not set or on error.
func (c *ConfigService) GetPetSize(screenW, screenH int) int {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	var val string
	if err := c.s.sqlDB.QueryRowContext(c.s.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

// SavePetSize persists the pet height for the given screen resolution.
func (c *ConfigService) SavePetSize(size, screenW, screenH int) error {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	_, err := c.s.sqlDB.ExecContext(c.s.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, strconv.Itoa(size))
	return err
}

// GetChatSize returns the saved chat bubble [width, height] for the given screen resolution.
// Returns [0, 0] if no size has been saved for that resolution yet.
func (c *ConfigService) GetChatSize(screenW, screenH int) []int {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	var val string
	if err := c.s.sqlDB.QueryRowContext(c.s.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return []int{0, 0}
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return []int{0, 0}
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return []int{0, 0}
	}
	return []int{w, h}
}

// SaveChatSize persists the chat bubble dimensions for the given screen resolution.
func (c *ConfigService) SaveChatSize(width, height, screenW, screenH int) error {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	val := fmt.Sprintf("%d,%d", width, height)
	_, err := c.s.sqlDB.ExecContext(c.s.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}

// MissingRequiredConfig returns names of empty required config fields.
func (c *ConfigService) MissingRequiredConfig() []string {
	return c.s.cfg.MissingRequired()
}

// PingLLM measures the round-trip latency to the active model provider's
// Base URL by issuing an HTTP HEAD request with a 4-second timeout.
// Returns elapsed milliseconds, or -1 on any error (empty URL, timeout, etc.).
func (c *ConfigService) PingLLM() int64 {
	c.s.mu.RLock()
	baseURL := c.s.cfg.LLMBaseURL
	c.s.mu.RUnlock()

	if baseURL == "" {
		return -1
	}

	client := &http.Client{Timeout: 4 * time.Second}
	start := time.Now()
	resp, err := client.Head(baseURL)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return -1
	}
	resp.Body.Close()
	return elapsed
}

// ListLLMModels queries the OpenAI-compatible /v1/models endpoint using the
// provided baseURL and apiKey (taken directly from the settings form, not the
// saved config), and returns a sorted list of model IDs.
func (c *ConfigService) ListLLMModels(baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("LLM Base URL is not configured")
	}
	url := baseURL + "/models"

	ctx, cancel := context.WithTimeout(c.s.ctx, 10*time.Second)
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

// GetAvailableModels returns a list of available Live2D model names by
// scanning subdirectories of the bundled live2d assets directory.
// The special "core" directory is excluded.
func (c *ConfigService) GetAvailableModels() []string {
	if c.s.assetsFS == nil {
		return []string{"hiyori"}
	}
	entries, err := fs.ReadDir(c.s.assetsFS, "frontend/dist/live2d")
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

// ListOpenRouterModels fetches available models from OpenRouter's /api/v1/models/user endpoint.
// baseURL defaults to "https://openrouter.ai/api/v1" when empty.
func (c *ConfigService) ListOpenRouterModels(baseURL, apiKey string) ([]string, error) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	modelsURL := base + "/models/user"

	ctx, cancel := context.WithTimeout(c.s.ctx, 10*time.Second)
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

// GetAutoLaunch reports whether Aiko is configured to launch at login.
func (c *ConfigService) GetAutoLaunch() bool {
	if c.s.hooks.GetAutoLaunch != nil {
		return c.s.hooks.GetAutoLaunch()
	}
	return false
}

// SetAutoLaunch enables or disables launch-at-login for Aiko.
func (c *ConfigService) SetAutoLaunch(enabled bool) {
	if c.s.hooks.SetAutoLaunch != nil {
		c.s.hooks.SetAutoLaunch(enabled)
	}
}

// GetSoundsEnabled returns whether chat sound effects are enabled.
func (c *ConfigService) GetSoundsEnabled() bool {
	c.s.mu.RLock()
	defer c.s.mu.RUnlock()
	return c.s.cfg.SoundsEnabled
}

// SetSoundsEnabled sets the sounds enabled flag and persists it.
func (c *ConfigService) SetSoundsEnabled(enabled bool) error {
	c.s.mu.Lock()
	c.s.cfg.SoundsEnabled = enabled
	c.s.mu.Unlock()
	return c.s.configStore.Save(c.s.cfg)
}

// GetTTSAutoPlay returns whether TTS auto-play is enabled.
func (c *ConfigService) GetTTSAutoPlay() bool {
	return c.s.cfg.TTSAutoPlay
}

// SetTTSAutoPlay sets TTS auto-play and persists it.
func (c *ConfigService) SetTTSAutoPlay(enabled bool) error {
	c.s.cfg.TTSAutoPlay = enabled
	return c.s.configStore.Save(c.s.cfg)
}

// GetVoiceAutoSend returns whether voice messages are sent automatically
// after the final STT result arrives.
func (c *ConfigService) GetVoiceAutoSend() bool {
	c.s.mu.RLock()
	defer c.s.mu.RUnlock()
	return c.s.cfg.VoiceAutoSend
}

// SetVoiceAutoSend sets the voice auto-send flag and persists it.
func (c *ConfigService) SetVoiceAutoSend(enabled bool) error {
	c.s.mu.Lock()
	c.s.cfg.VoiceAutoSend = enabled
	c.s.mu.Unlock()
	return c.s.configStore.Save(c.s.cfg)
}

// GetKokoroTTSVoices returns the static list of Kokoro Chinese voices.
func (c *ConfigService) GetKokoroTTSVoices() ([]string, error) {
	speaker := tts.New("kokoro", "")
	return speaker.Voices(c.s.ctx)
}

// SetupKokoroTTS installs the Kokoro TTS Python environment asynchronously.
// Progress is reported via notification:show events.
func (c *ConfigService) SetupKokoroTTS() error {
	go func() {
		notifyFn := func(title, msg string) {
			c.s.app.Event.Emit("notification:show", map[string]any{
				"title": title, "message": msg,
			})
		}
		home, _ := os.UserHomeDir()
		venvDir := filepath.Join(home, ".aiko", "tts-venv")
		modelsDir := filepath.Join(venvDir, "models")
		pip := filepath.Join(venvDir, "bin", "pip")

		// Step 0: check Python version (kokoro-onnx requires >= 3.10)
		verCmd := exec.Command("python3", "-c",
			"import sys; v=sys.version_info; print(v.major,v.minor)")
		verCmd.Env = execenv.AugmentedEnv()
		verOut, verErr := verCmd.Output()
		if verErr != nil {
			notifyFn("❌ TTS 安装失败", "未找到 python3，请先安装 Python 3.10+")
			return
		}
		var major, minor int
		fmt.Sscanf(strings.TrimSpace(string(verOut)), "%d %d", &major, &minor)
		if major < 3 || (major == 3 && minor < 10) {
			notifyFn("❌ Python 版本过低",
				fmt.Sprintf("当前 Python %d.%d，kokoro-onnx 需要 3.10+，请先升级 Python", major, minor))
			return
		}

		cleanup := func(msg string) {
			_ = os.RemoveAll(venvDir)
			notifyFn("❌ TTS 安装失败", msg)
		}

		// Step 1: venv
		notifyFn("🐍 Kokoro TTS", "创建 Python 虚拟环境…")
		if err := configRun("python3", "-m", "venv", venvDir); err != nil {
			cleanup(err.Error())
			return
		}

		// Step 2: pip upgrade (best-effort)
		_ = configRun(pip, "install", "--upgrade", "pip", "-q")

		// Step 3: pip install
		notifyFn("📦 Kokoro TTS", "安装依赖包（约 1-2 分钟）…")
		if err := configRun(pip, "install", "-q", "kokoro-onnx", "misaki[zh]", "soundfile"); err != nil {
			cleanup(err.Error())
			return
		}

		// Step 4: download models
		if err := os.MkdirAll(modelsDir, 0755); err != nil {
			cleanup(err.Error())
			return
		}
		base := "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/"
		for _, f := range []string{"kokoro-v1.0.onnx", "voices-v1.0.bin"} {
			dst := filepath.Join(modelsDir, f)
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			notifyFn("⬇️ Kokoro TTS", fmt.Sprintf("下载 %s…", f))
			if err := configDownloadFile(dst, base+f); err != nil {
				cleanup(err.Error())
				return
			}
		}
		notifyFn("✅ Kokoro TTS", "环境安装完成！请保存配置即可使用。")
	}()
	return nil
}

// SpeakText synthesizes text to speech using the current TTS backend.
// If text exceeds TTSSummarizeThreshold runes, it is first summarized by the LLM.
// Audio bytes are emitted as tts:audio (base64 WAV); system speaker plays directly without tts:audio.
// Events: tts:start, tts:audio (optional), tts:done, tts:error.
func (c *ConfigService) SpeakText(text string) error {
	c.s.mu.Lock()
	if c.s.ttsCancel != nil {
		c.s.ttsCancel()
	}
	c.s.ttsGeneration++
	myGen := c.s.ttsGeneration
	ctx, cancel := context.WithCancel(c.s.ctx)
	c.s.ttsCancel = cancel
	speaker := c.s.ttsSpeaker
	cfg := c.s.cfg
	c.s.mu.Unlock()

	slog.Info("tts: SpeakText called", "backend", cfg.TTSBackend, "len", len([]rune(text)), "speaker", fmt.Sprintf("%T", speaker), "voice", cfg.TTSVoice)

	if speaker == nil {
		speaker = &tts.SystemSpeaker{}
	}

	c.s.app.Event.Emit("tts:start", nil)

	go func() {
		defer func() {
			c.s.mu.Lock()
			if c.s.ttsGeneration == myGen {
				c.s.ttsCancel = nil
			}
			c.s.mu.Unlock()
		}()

		finalText := text
		threshold := cfg.TTSSummarizeThreshold
		if threshold > 0 && len([]rune(text)) > threshold {
			c.s.mu.RLock()
			ag := c.s.petAgent
			c.s.mu.RUnlock()
			if ag != nil {
				summary, err := ag.ChatDirectCollect(ctx, "请用简洁的中文口语总结以下内容，控制在100字以内，适合朗读：\n"+text)
				if err == nil && strings.TrimSpace(summary) != "" {
					finalText = strings.TrimSpace(summary)
				}
			}
		}

		speakText := configStripNonSpeech(finalText)
		slog.Info("tts: calling Speak", "text_len", len([]rune(speakText)), "voice", cfg.TTSVoice, "speed", cfg.TTSSpeed)
		audioBytes, err := speaker.Speak(ctx, speakText, cfg.TTSVoice, cfg.TTSSpeed)
		if err != nil {
			slog.Warn("tts: Speak error", "err", err)
			if ctx.Err() != nil {
				c.s.app.Event.Emit("tts:done", nil)
				return
			}
			c.s.app.Event.Emit("tts:error", err.Error())
			return
		}

		slog.Info("tts: Speak done", "audio_bytes", len(audioBytes))
		if len(audioBytes) > 0 {
			encoded := base64.StdEncoding.EncodeToString(audioBytes)
			c.s.app.Event.Emit("tts:audio", map[string]string{
				"data":   encoded,
				"format": "wav",
			})
		}
		c.s.app.Event.Emit("tts:done", nil)
	}()

	return nil
}

// StopTTS cancels any in-flight TTS synthesis or playback.
func (c *ConfigService) StopTTS() {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	if c.s.ttsCancel != nil {
		c.s.ttsCancel()
		c.s.ttsCancel = nil
	}
}

// configRun executes an external command and waits for it to finish,
// merging stderr into the returned error message.
func configRun(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, stderr.String())
	}
	return nil
}

// configDownloadFile downloads a remote file via HTTP GET to the given local path.
func configDownloadFile(dst, rawURL string) error {
	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// configStripNonSpeech removes emoji and kaomoji from text before TTS synthesis.
func configStripNonSpeech(s string) string {
	var buf strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if configIsEmojiRune(r) {
			i++
			for i < len(runes) && (runes[i] == 0xFE0F || runes[i] == 0x200D || (runes[i] >= 0x1F3FB && runes[i] <= 0x1F3FF)) {
				i++
			}
			continue
		}
		if (r == '(' || r == '（') && i+1 < len(runes) {
			end := -1
			hasKaomoji := false
			for j := i + 1; j < len(runes) && j < i+20; j++ {
				if runes[j] == ')' || runes[j] == '）' {
					end = j
					break
				}
				if !unicode.IsLetter(runes[j]) && !unicode.IsDigit(runes[j]) && !unicode.IsSpace(runes[j]) {
					hasKaomoji = true
				}
			}
			if end > 0 && hasKaomoji {
				i = end + 1
				continue
			}
		}
		buf.WriteRune(r)
		i++
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// configIsEmojiRune reports whether r is in an emoji/symbol Unicode range.
func configIsEmojiRune(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FFFF) ||
		(r >= 0x2600 && r <= 0x27BF) ||
		(r >= 0x2300 && r <= 0x23FF) ||
		(r >= 0xFE00 && r <= 0xFE0F) ||
		r == 0x200D ||
		(r >= 0x1F900 && r <= 0x1F9FF) ||
		(r >= 0x1FA00 && r <= 0x1FAFF)
}

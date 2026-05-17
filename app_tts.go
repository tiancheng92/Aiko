package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/execenv"
	"aiko/internal/tts"
)

// stripNonSpeech removes emoji and kaomoji from text before TTS synthesis.
// Emoji are identified by Unicode ranges (Emoji/Symbol/Misc blocks).
// Kaomoji are matched by common bracket patterns like (=^･ω･^=) and (╥_╥).
func stripNonSpeech(s string) string {
	// Remove kaomoji: sequences starting with ( or ╥ etc. containing non-ASCII
	// Use a simple rune scan: strip parenthesized runs that contain non-letter non-digit runes.
	var buf strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		// Detect emoji / symbols / misc unicode blocks
		if isEmojiRune(r) {
			i++
			// Skip variation selectors and zero-width joiners that follow
			for i < len(runes) && (runes[i] == 0xFE0F || runes[i] == 0x200D || (runes[i] >= 0x1F3FB && runes[i] <= 0x1F3FF)) {
				i++
			}
			continue
		}
		// Detect kaomoji: opening paren followed by run with non-letter/digit content, closing paren
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
	// Collapse multiple spaces left behind
	return strings.Join(strings.Fields(buf.String()), " ")
}

// isEmojiRune reports whether r is in an emoji/symbol Unicode range.
func isEmojiRune(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FFFF) || // Mahjong, dominoes, misc symbols & pictographs, emoticons, etc.
		(r >= 0x2600 && r <= 0x27BF) || // Misc symbols, dingbats
		(r >= 0x2300 && r <= 0x23FF) || // Misc technical
		(r >= 0xFE00 && r <= 0xFE0F) || // Variation selectors
		r == 0x200D || // Zero-width joiner
		(r >= 0x1F900 && r <= 0x1F9FF) || // Supplemental symbols
		(r >= 0x1FA00 && r <= 0x1FAFF) // Chess, symbols extended
}

// SpeakText synthesizes text to speech using the current TTS backend.
// If text exceeds TTSSummarizeThreshold runes, it is first summarized by the LLM.
// Audio bytes are emitted as tts:audio (base64 WAV); system speaker plays directly without tts:audio.
// Events: tts:start, tts:audio (optional), tts:done, tts:error.
func (a *App) SpeakText(text string) error {
	a.mu.Lock()
	if a.ttsCancel != nil {
		a.ttsCancel()
	}
	a.ttsGeneration++
	myGen := a.ttsGeneration
	ctx, cancel := context.WithCancel(a.ctx)
	a.ttsCancel = cancel
	speaker := a.ttsSpeaker
	cfg := a.cfg
	a.mu.Unlock()

	log.Info().Str("backend", cfg.TTSBackend).Int("len", utf8.RuneCountInString(text)).Str("speaker", fmt.Sprintf("%T", speaker)).Str("voice", cfg.TTSVoice).Msg("tts: SpeakText called")

	if speaker == nil {
		speaker = &tts.SystemSpeaker{}
	}

	wailsruntime.EventsEmit(a.ctx, "tts:start", nil)

	go func() {
		// Only nil out ttsCancel if this goroutine's generation is still current,
		// to avoid wiping a newer call's cancel when overlapping SpeakText calls race.
		defer func() {
			a.mu.Lock()
			if a.ttsGeneration == myGen {
				a.ttsCancel = nil
			}
			a.mu.Unlock()
		}()

		finalText := text
		threshold := cfg.TTSSummarizeThreshold
		if threshold > 0 && utf8.RuneCountInString(text) > threshold {
			summary, err := a.ChatDirectCollect(ctx, "请用简洁的中文口语总结以下内容，控制在100字以内，适合朗读：\n"+text)
			if err == nil && strings.TrimSpace(summary) != "" {
				finalText = strings.TrimSpace(summary)
			}
		}

		speakText := stripNonSpeech(finalText)
		log.Info().Int("text_len", utf8.RuneCountInString(speakText)).Str("voice", cfg.TTSVoice).Float64("speed", cfg.TTSSpeed).Msg("tts: calling Speak")
		audioBytes, err := speaker.Speak(ctx, speakText, cfg.TTSVoice, cfg.TTSSpeed)
		if err != nil {
			log.Warn().Err(err).Msg("tts: Speak error")
			if ctx.Err() != nil {
				wailsruntime.EventsEmit(a.ctx, "tts:done", nil)
				return
			}
			wailsruntime.EventsEmit(a.ctx, "tts:error", err.Error())
			return
		}

		log.Info().Int("audio_bytes", len(audioBytes)).Msg("tts: Speak done")
		if len(audioBytes) > 0 {
			encoded := base64.StdEncoding.EncodeToString(audioBytes)
			wailsruntime.EventsEmit(a.ctx, "tts:audio", map[string]string{
				"data":   encoded,
				"format": "wav",
			})
		}
		wailsruntime.EventsEmit(a.ctx, "tts:done", nil)
	}()

	return nil
}

// StopTTS cancels any in-flight TTS synthesis or playback.
func (a *App) StopTTS() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ttsCancel != nil {
		a.ttsCancel()
		a.ttsCancel = nil
	}
}

// GetKokoroTTSVoices returns the static list of Kokoro Chinese voices.
func (a *App) GetKokoroTTSVoices() ([]string, error) {
	speaker := tts.New("kokoro", "")
	return speaker.Voices(a.ctx)
}

// GetTTSAutoPlay returns whether TTS auto-play is enabled.
func (a *App) GetTTSAutoPlay() bool {
	return a.cfg.TTSAutoPlay
}

// SetTTSAutoPlay sets TTS auto-play and persists it.
func (a *App) SetTTSAutoPlay(enabled bool) error {
	a.mu.Lock()
	a.cfg.TTSAutoPlay = enabled
	cfgCopy := *a.cfg
	a.mu.Unlock()
	return a.configStore.Save(&cfgCopy)
}

// SetupKokoroTTS 在后台异步安装 Kokoro TTS 环境：
// 1. 创建 Python venv (~/.aiko/tts-venv)
// 2. 升级 pip
// 3. 安装 kokoro-onnx、misaki[zh]、soundfile
// 4. 下载模型文件 kokoro-v1.0.onnx 和 voices-v1.0.bin
// 进度通过 notification:show 事件汇报。方法立即返回 nil，安装在 goroutine 中运行。
func (a *App) SetupKokoroTTS() error {
	go func() {
		notify := func(title, msg string) {
			wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
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
			notify("❌ TTS 安装失败", "未找到 python3，请先安装 Python 3.10+")
			return
		}
		var major, minor int
		fmt.Sscanf(strings.TrimSpace(string(verOut)), "%d %d", &major, &minor)
		if major < 3 || (major == 3 && minor < 10) {
			notify("❌ Python 版本过低",
				fmt.Sprintf("当前 Python %d.%d，kokoro-onnx 需要 3.10+，请先升级 Python", major, minor))
			return
		}

		// cleanup 在安装失败时删除不完整的 venv 目录，避免残留干扰下次安装。
		cleanup := func(msg string) {
			_ = os.RemoveAll(venvDir)
			notify("❌ TTS 安装失败", msg)
		}

		// Step 1: venv
		notify("🐍 Kokoro TTS", "创建 Python 虚拟环境…")
		if err := run("python3", "-m", "venv", venvDir); err != nil {
			cleanup(err.Error())
			return
		}

		// Step 2: pip upgrade (best-effort)
		_ = run(pip, "install", "--upgrade", "pip", "-q")

		// Step 3: pip install
		notify("📦 Kokoro TTS", "安装依赖包（约 1-2 分钟）…")
		if err := run(pip, "install", "-q", "kokoro-onnx", "misaki[zh]", "soundfile"); err != nil {
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
				continue // already exists, skip
			}
			notify("⬇️ Kokoro TTS", fmt.Sprintf("下载 %s…", f))
			if err := downloadFile(dst, base+f); err != nil {
				cleanup(err.Error())
				return
			}
		}
		notify("✅ Kokoro TTS", "环境安装完成！请保存配置即可使用。")
	}()
	return nil
}

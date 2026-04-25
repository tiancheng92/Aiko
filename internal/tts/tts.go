package tts

import (
	"context"
	"log/slog"
	"strings"
)

// Speaker 是统一的 TTS 抽象接口。
type Speaker interface {
	// Speak 将文本转换为 WAV 音频字节。SystemSpeaker 直接在本机播放，返回 nil。
	Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error)
	// Voices 返回当前后端的可用声线名称列表。
	Voices(ctx context.Context) ([]string, error)
}

// New 根据 backend 返回对应的 Speaker 实现。
//   - backend=="sherpa"：使用 modelDir 初始化 SherpaSpeaker；失败时降级为 SystemSpeaker。
//   - backend=="openai" 或（baseURL 非空且 model 非空）：返回 OpenAISpeaker。
//   - 其他情况：返回 SystemSpeaker（macOS say）。
//
// modelDir 仅在 backend=="sherpa" 时使用。
func New(backend, baseURL, apiKey, model, modelDir string) Speaker {
	switch backend {
	case "sherpa":
		s, err := NewSherpaSpeaker(modelDir)
		if err != nil {
			slog.Warn("tts: sherpa 初始化失败，降级为系统 say", "err", err)
			return &SystemSpeaker{}
		}
		return s
	default:
		if baseURL != "" && model != "" {
			base := strings.TrimRight(baseURL, "/")
			base = strings.TrimSuffix(base, "/v1")
			return &OpenAISpeaker{baseURL: base, apiKey: apiKey, model: model}
		}
		return &SystemSpeaker{}
	}
}

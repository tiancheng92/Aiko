package tts

import (
	"context"
	"log/slog"
)

// Speaker 是统一的 TTS 抽象接口。
type Speaker interface {
	// Speak 将文本转换为 WAV 音频字节。SystemSpeaker 直接在本机播放，返回 nil。
	Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error)
	// Voices 返回当前后端的可用声线名称列表。
	Voices(ctx context.Context) ([]string, error)
}

// New 根据 backend 返回对应的 Speaker 实现。
//   - backend=="kokoro"：使用 kokoro_tts.py 子进程合成；modelDir 为模型目录（空串使用默认路径）。
//   - 其他情况：返回 SystemSpeaker（macOS say）。
func New(backend, modelDir string) Speaker {
	switch backend {
	case "kokoro":
		s, err := newKokoroSpeaker(modelDir)
		if err != nil {
			slog.Warn("tts: kokoro 初始化失败，降级为系统 say", "err", err)
			return &SystemSpeaker{}
		}
		return s
	default:
		return &SystemSpeaker{}
	}
}

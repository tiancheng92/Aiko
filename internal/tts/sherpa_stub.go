//go:build !darwin

package tts

import (
	"context"
	"fmt"
)

// SherpaSpeaker 在非 darwin 平台为桩实现。
type SherpaSpeaker struct{}

// NewSherpaSpeaker 在非 darwin 平台始终返回错误。
func NewSherpaSpeaker(_ string) (*SherpaSpeaker, error) {
	return nil, fmt.Errorf("sherpa TTS 仅支持 macOS")
}

// Speak 在非 darwin 平台始终返回错误。
func (s *SherpaSpeaker) Speak(_ context.Context, _, _ string, _ float64) ([]byte, error) {
	return nil, fmt.Errorf("sherpa TTS 仅支持 macOS")
}

// Voices 在非 darwin 平台始终返回错误。
func (s *SherpaSpeaker) Voices(_ context.Context) ([]string, error) {
	return nil, fmt.Errorf("sherpa TTS 仅支持 macOS")
}

//go:build !darwin

package tts

import "fmt"

// newKokoroSpeaker 在非 darwin 平台始终返回错误。
func newKokoroSpeaker(_ string) (Speaker, error) {
	return nil, fmt.Errorf("kokoro TTS 仅支持 macOS")
}

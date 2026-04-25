package tts

import (
	"context"
	"strings"
)

// Speaker is the unified TTS abstraction.
type Speaker interface {
	// Speak converts text to audio bytes (WAV). Returns nil bytes for system speaker
	// which plays directly on the backend.
	Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error)
	// Voices returns available voice names for this backend.
	Voices(ctx context.Context) ([]string, error)
}

// New returns an OpenAISpeaker when baseURL and model are non-empty,
// otherwise returns a SystemSpeaker (macOS say fallback).
// baseURL may include a /v1 suffix (common in OpenAI-compatible configs);
// it is stripped so OpenAISpeaker can append /v1/audio/speech correctly.
func New(baseURL, apiKey, model string) Speaker {
	if baseURL != "" && model != "" {
		base := strings.TrimRight(baseURL, "/")
		// Strip trailing /v1 so paths like http://host/v1/v1/audio/speech never occur.
		base = strings.TrimSuffix(base, "/v1")
		return &OpenAISpeaker{baseURL: base, apiKey: apiKey, model: model}
	}
	return &SystemSpeaker{}
}

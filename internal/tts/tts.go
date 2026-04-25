package tts

import "context"

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
func New(baseURL, apiKey, model string) Speaker {
	if baseURL != "" && model != "" {
		return &OpenAISpeaker{baseURL: baseURL, apiKey: apiKey, model: model}
	}
	return &SystemSpeaker{}
}

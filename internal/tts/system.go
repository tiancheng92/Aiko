package tts

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SystemSpeaker uses macOS `say` command as TTS fallback.
// Speak() plays audio directly on the backend; no bytes are returned.
type SystemSpeaker struct{}

// isKokoroVoice returns true if voice is a Kokoro-specific voice name (zf_/zm_ prefix),
// which are not valid macOS say voices and must be ignored.
func isKokoroVoice(voice string) bool {
	return strings.HasPrefix(voice, "zf_") || strings.HasPrefix(voice, "zm_")
}

// Speak plays text using macOS say command. Returns nil bytes (plays directly).
func (s *SystemSpeaker) Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	// Escape double quotes and backslashes in text to avoid injection via osascript
	safe := strings.ReplaceAll(text, `\`, `\\`)
	safe = strings.ReplaceAll(safe, `"`, `\"`)

	// Ignore Kokoro-specific voice names (zf_*/zm_*) — they are invalid for macOS say.
	if isKokoroVoice(voice) {
		voice = ""
	}

	var script string
	if voice != "" {
		script = fmt.Sprintf(`say "%s" using "%s"`, safe, voice)
	} else {
		script = fmt.Sprintf(`say "%s"`, safe)
	}

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err() // cancelled by StopTTS
		}
		return nil, fmt.Errorf("say command failed: %w", err)
	}
	return nil, nil // system speaker plays directly, no bytes to return
}

// Voices returns voices from `say -v ?` filtered to Chinese and English entries.
func (s *SystemSpeaker) Voices(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "say", "-v", "?").Output()
	if err != nil {
		return nil, fmt.Errorf("say -v ?: %w", err)
	}
	var voices []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Each line: "VoiceName    lang    # description"
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		// Keep zh-CN, zh-TW, and en-US voices
		if len(fields) >= 2 {
			lang := fields[1]
			if strings.HasPrefix(lang, "zh") || strings.HasPrefix(lang, "en") {
				voices = append(voices, name)
			}
		}
	}
	return voices, nil
}

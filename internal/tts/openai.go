package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAISpeaker calls an OpenAI-compatible /v1/audio/speech endpoint.
type OpenAISpeaker struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func (s *OpenAISpeaker) httpClient() *http.Client {
	if s.client == nil {
		s.client = &http.Client{Timeout: 60 * time.Second}
	}
	return s.client
}

// Speak calls POST {baseURL}/v1/audio/speech and returns WAV bytes.
func (s *OpenAISpeaker) Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	if speed <= 0 {
		speed = 1.0
	}
	body, err := json.Marshal(map[string]any{
		"model":           s.model,
		"input":           text,
		"voice":           voice,
		"speed":           speed,
		"response_format": "wav",
	})
	if err != nil {
		return nil, fmt.Errorf("tts marshal: %w", err)
	}

	url := strings.TrimRight(s.baseURL, "/") + "/v1/audio/speech"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tts server error %d: %s", resp.StatusCode, string(b))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tts read body: %w", err)
	}
	return data, nil
}

// Voices calls GET {baseURL}/v1/audio/voices. Returns empty list if endpoint absent.
func (s *OpenAISpeaker) Voices(ctx context.Context) ([]string, error) {
	url := strings.TrimRight(s.baseURL, "/") + "/v1/audio/voices"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient().Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, nil // endpoint not available, caller falls back to manual input
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil
	}

	// Try {"voices": [...]} first, then {"data": [...]} (OpenAI-compatible variants).
	var r1 struct {
		Voices []string `json:"voices"`
	}
	if json.Unmarshal(raw, &r1) == nil && len(r1.Voices) > 0 {
		return r1.Voices, nil
	}
	var r2 struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &r2) == nil && len(r2.Data) > 0 {
		voices := make([]string, len(r2.Data))
		for i, d := range r2.Data {
			voices[i] = d.ID
		}
		return voices, nil
	}
	// Last resort: try a plain string array
	var r3 []string
	if json.Unmarshal(raw, &r3) == nil && len(r3) > 0 {
		return r3, nil
	}
	return nil, nil
}

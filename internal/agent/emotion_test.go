package agent

import (
	"testing"
)

func TestParseEmotionTag(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		emotion   string
		intensity float64
		rest      string
		ok        bool
	}{
		{"normal", "[情绪:joy/0.8]\n你好", "joy", 0.8, "你好", true},
		{"no newline", "[情绪:sad/1.0]文字", "sad", 1.0, "文字", true},
		{"neutral zero", "[情绪:neutral/0.0]\n", "neutral", 0.0, "", true},
		{"no tag", "普通回复", "", 0, "普通回复", false},
		{"partial tag", "[情绪:joy", "", 0, "[情绪:joy", false},
		{"missing slash", "[情绪:joy]\n", "", 0, "[情绪:joy]\n", false},
		{"bad intensity", "[情绪:joy/abc]\n", "", 0, "[情绪:joy/abc]\n", false},
		{"empty", "", "", 0, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emotion, intensity, rest, ok := parseEmotionTag(tc.input)
			if ok != tc.ok {
				t.Errorf("ok: got %v want %v", ok, tc.ok)
			}
			if ok {
				if emotion != tc.emotion {
					t.Errorf("emotion: got %q want %q", emotion, tc.emotion)
				}
				if intensity != tc.intensity {
					t.Errorf("intensity: got %v want %v", intensity, tc.intensity)
				}
				if rest != tc.rest {
					t.Errorf("rest: got %q want %q", rest, tc.rest)
				}
			} else {
				if rest != tc.input {
					t.Errorf("on fail, rest should equal input: got %q want %q", rest, tc.input)
				}
			}
		})
	}
}

func TestEmotionParserStreaming(t *testing.T) {
	p := NewEmotionParser()
	tokens := []string{"[情绪:", "joy/", "0.7]\n", "你好世界"}
	var emittedEmotion string
	var emittedIntensity float64
	var emittedTokens []string

	for _, tok := range tokens {
		text, emotion, intensity := p.Feed(tok)
		if text != "" {
			emittedTokens = append(emittedTokens, text)
		}
		if emotion != "" {
			emittedEmotion = emotion
			emittedIntensity = intensity
		}
	}
	if emittedEmotion != "joy" {
		t.Errorf("emotion: got %q want joy", emittedEmotion)
	}
	if emittedIntensity != 0.7 {
		t.Errorf("intensity: got %v want 0.7", emittedIntensity)
	}
	if len(emittedTokens) != 1 || emittedTokens[0] != "你好世界" {
		t.Errorf("tokens: got %v want [你好世界]", emittedTokens)
	}
}

func TestEmotionParserFallback(t *testing.T) {
	p := NewEmotionParser()
	long := "[这是一段超过三十个字节的普通文字不是情绪标签"
	text, emotion, _ := p.Feed(long)
	if emotion != "" {
		t.Errorf("should not emit emotion for long non-tag: got %q", emotion)
	}
	if text != long {
		t.Errorf("should flush buffer as text: got %q want %q", text, long)
	}
}

func TestEmotionParserFlush(t *testing.T) {
	// Partial tag at stream end — Flush must return buffered content.
	p := NewEmotionParser()
	text, _, _ := p.Feed("[情绪:joy")
	if text != "" {
		t.Errorf("should buffer partial tag, got %q", text)
	}
	tail := p.Flush()
	if tail != "[情绪:joy" {
		t.Errorf("Flush: got %q want %q", tail, "[情绪:joy")
	}
}

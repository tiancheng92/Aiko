package agent

import (
	"testing"
)

func TestParseBehaviorTag(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		emotion string
		action  string
		rest    string
		ok      bool
	}{
		{"emotion only", "[表现:joy]\n你好", "joy", "", "你好", true},
		{"emotion with action", "[表现:joy,动作:wave]\n你好", "joy", "wave", "你好", true},
		{"neutral", "[表现:neutral]\n", "neutral", "", "", true},
		{"sad with action", "[表现:sad,动作:nod]\n文字", "sad", "nod", "文字", true},
		{"no tag", "普通回复", "", "", "普通回复", false},
		{"old format", "[情绪:joy/0.8]\n你好", "", "", "[情绪:joy/0.8]\n你好", false},
		{"partial tag", "[表现:joy", "", "", "[表现:joy", false},
		{"missing bracket", "表现:joy]\n", "", "", "表现:joy]\n", false},
		{"empty", "", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emotion, action, rest, ok := parseBehaviorTag(tc.input)
			if ok != tc.ok {
				t.Errorf("ok: got %v want %v", ok, tc.ok)
			}
			if ok {
				if emotion != tc.emotion {
					t.Errorf("emotion: got %q want %q", emotion, tc.emotion)
				}
				if action != tc.action {
					t.Errorf("action: got %q want %q", action, tc.action)
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

func TestBehaviorParserStreaming(t *testing.T) {
	p := NewBehaviorParser()
	tokens := []string{"[表现:", "joy,动作:", "wave]\n", "你好世界"}
	var emittedEmotion string
	var emittedAction string
	var emittedTokens []string

	for _, tok := range tokens {
		text, emotion, action := p.Feed(tok)
		if text != "" {
			emittedTokens = append(emittedTokens, text)
		}
		if emotion != "" {
			emittedEmotion = emotion
			emittedAction = action
		}
	}
	if emittedEmotion != "joy" {
		t.Errorf("emotion: got %q want joy", emittedEmotion)
	}
	if emittedAction != "wave" {
		t.Errorf("action: got %q want wave", emittedAction)
	}
	if len(emittedTokens) != 1 || emittedTokens[0] != "你好世界" {
		t.Errorf("tokens: got %v want [你好世界]", emittedTokens)
	}
}

func TestBehaviorParserStreamingNoAction(t *testing.T) {
	p := NewBehaviorParser()
	tokens := []string{"[表现:", "sad]\n", "抱歉"}
	var emittedEmotion string
	var emittedAction string
	var emittedTokens []string

	for _, tok := range tokens {
		text, emotion, action := p.Feed(tok)
		if text != "" {
			emittedTokens = append(emittedTokens, text)
		}
		if emotion != "" {
			emittedEmotion = emotion
			emittedAction = action
		}
	}
	if emittedEmotion != "sad" {
		t.Errorf("emotion: got %q want sad", emittedEmotion)
	}
	if emittedAction != "" {
		t.Errorf("action: got %q want empty", emittedAction)
	}
	if len(emittedTokens) != 1 || emittedTokens[0] != "抱歉" {
		t.Errorf("tokens: got %v want [抱歉]", emittedTokens)
	}
}

func TestBehaviorParserFallback(t *testing.T) {
	p := NewBehaviorParser()
	long := "[这是一段超过六十个字节的普通文字不是行为标签，纯粹是正文内容"
	text, emotion, _ := p.Feed(long)
	if emotion != "" {
		t.Errorf("should not emit emotion for long non-tag: got %q", emotion)
	}
	if text != long {
		t.Errorf("should flush buffer as text: got %q want %q", text, long)
	}
}

func TestBehaviorParserFlush(t *testing.T) {
	p := NewBehaviorParser()
	text, _, _ := p.Feed("[表现:joy")
	if text != "" {
		t.Errorf("should buffer partial tag, got %q", text)
	}
	tail := p.Flush()
	if tail != "[表现:joy" {
		t.Errorf("Flush: got %q want %q", tail, "[表现:joy")
	}
}

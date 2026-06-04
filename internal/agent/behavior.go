package agent

import (
	"regexp"
	"strings"
)

var behaviorTagRe = regexp.MustCompile(`^\[表现:(\w+)(?:,动作:(\w+))?\]\n?`)

// behaviorTagGlobalRe matches a behavior tag anywhere in a string (no ^ anchor).
// Used to strip tags from persisted responses when the tag is not at position 0
// (e.g. when tool-call indicators precede the tag in the full response).
var behaviorTagGlobalRe = regexp.MustCompile(`\[表现:\w+(?:,动作:\w+)?\]\n?`)

// stripBehaviorTag removes all behavior tags from s regardless of position.
func stripBehaviorTag(s string) string {
	return behaviorTagGlobalRe.ReplaceAllString(s, "")
}

// parseBehaviorTag extracts a behavior tag from the start of s.
// On success returns (emotion, action, textAfterTag, true).
// action is empty string when no action is specified.
// On failure returns ("", "", s, false) — original string preserved.
func parseBehaviorTag(s string) (string, string, string, bool) {
	m := behaviorTagRe.FindStringSubmatchIndex(s)
	if m == nil {
		return "", "", s, false
	}
	emotion := s[m[2]:m[3]]
	// action is optional capture group (index 4-5), may be -1 if absent.
	var action string
	if m[4] >= 0 {
		action = s[m[4]:m[5]]
	}
	return emotion, action, s[m[1]:], true
}

// BehaviorParser is a per-response streaming state machine that extracts the
// optional behavior prefix tag from the token stream.
// It is not safe for concurrent use.
type BehaviorParser struct {
	parsing bool
	buf     strings.Builder
}

// NewBehaviorParser returns a fresh parser ready for a new assistant response.
func NewBehaviorParser() *BehaviorParser {
	return &BehaviorParser{parsing: true}
}

// Feed processes one incoming token.
// Returns (textToEmit, emotion, action).
//   - textToEmit is non-empty when text should be forwarded to the display.
//   - emotion is non-empty when a behavior tag was detected.
//   - action may be empty even when emotion is set (no action specified).
func (p *BehaviorParser) Feed(tok string) (text string, emotion string, action string) {
	if !p.parsing {
		return tok, "", ""
	}
	p.buf.WriteString(tok)
	s := p.buf.String()

	// Try to parse a complete tag when we see the closing bracket.
	if strings.Contains(s, "]") {
		em, act, rest, ok := parseBehaviorTag(s)
		if ok {
			p.parsing = false
			p.buf.Reset()
			return rest, em, act
		}
		// Has ] but doesn't match — give up, flush.
		p.parsing = false
		p.buf.Reset()
		return s, "", ""
	}

	// Buffer exceeds 60 bytes without seeing ] — give up.
	if p.buf.Len() > 60 {
		p.parsing = false
		p.buf.Reset()
		return s, "", ""
	}

	// Still accumulating — withhold from display.
	return "", "", ""
}

// Flush returns any remaining buffered text (call on stream end).
func (p *BehaviorParser) Flush() string {
	if !p.parsing || p.buf.Len() == 0 {
		return ""
	}
	s := p.buf.String()
	p.buf.Reset()
	p.parsing = false
	return s
}

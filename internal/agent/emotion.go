package agent

import (
	"regexp"
	"strconv"
	"strings"
)

var emotionTagRe = regexp.MustCompile(`^\[情绪:(\w+)/([\d.]+)\]\n?`)

// parseEmotionTag extracts an emotion tag from the start of s.
// On success returns (emotion, intensity, textAfterTag, true).
// On failure returns ("", 0, s, false) — original string preserved.
func parseEmotionTag(s string) (string, float64, string, bool) {
	m := emotionTagRe.FindStringSubmatchIndex(s)
	if m == nil {
		return "", 0, s, false
	}
	emotion := s[m[2]:m[3]]
	intensity, err := strconv.ParseFloat(s[m[4]:m[5]], 64)
	if err != nil {
		return "", 0, s, false
	}
	return emotion, intensity, s[m[1]:], true
}

// EmotionParser is a per-response streaming state machine that strips the
// optional emotion prefix tag from the token stream.
// It is not safe for concurrent use.
type EmotionParser struct {
	parsing bool
	buf     strings.Builder
}

// NewEmotionParser returns a fresh parser ready for a new assistant response.
func NewEmotionParser() *EmotionParser {
	return &EmotionParser{parsing: true}
}

// Feed processes one incoming token.
// Returns (textToEmit, emotion, intensity).
//   - textToEmit is non-empty when text should be forwarded to the frontend.
//   - emotion is non-empty (with intensity) when an emotion tag was detected.
//
// Once parsing is complete (tag found or given up), all subsequent tokens are
// returned verbatim as textToEmit.
func (p *EmotionParser) Feed(tok string) (text string, emotion string, intensity float64) {
	if !p.parsing {
		return tok, "", 0
	}
	p.buf.WriteString(tok)
	s := p.buf.String()

	// Try to parse a complete tag.
	if strings.Contains(s, "]") {
		em, intens, rest, ok := parseEmotionTag(s)
		if ok {
			p.parsing = false
			p.buf.Reset()
			return rest, em, intens
		}
		// Has ] but doesn't match — give up, flush.
		p.parsing = false
		p.buf.Reset()
		return s, "", 0
	}

	// Buffer exceeds 30 bytes without seeing ] — give up.
	if p.buf.Len() > 30 {
		p.parsing = false
		p.buf.Reset()
		return s, "", 0
	}

	// Still accumulating — withhold from frontend.
	return "", "", 0
}

// Flush returns any remaining buffered text (call on stream end).
func (p *EmotionParser) Flush() string {
	if !p.parsing || p.buf.Len() == 0 {
		return ""
	}
	s := p.buf.String()
	p.buf.Reset()
	p.parsing = false
	return s
}

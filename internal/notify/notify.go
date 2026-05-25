// Package notify pushes system-level notifications to the user's OS.
//
// The actual delivery mechanism is injected via SetSender so this package
// stays free of CGO / platform-specific imports and can be tested in pure
// Go. The main binary registers a sender backed by UNUserNotificationCenter
// (see macos.go) so notifications are posted under the Aiko app identity
// rather than Script Editor's.
//
// When no sender is registered (tests, non-darwin builds), System is a no-op.
package notify

import (
	"regexp"
	"sync/atomic"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

// Patterns stripped from notification bodies before delivery.
var (
	reToolCall  = regexp.MustCompile(`<tool-call[^>]*></tool-call>`)
	reSkillCall = regexp.MustCompile(`<skill-call[^>]*></skill-call>`)
	reEmotion   = regexp.MustCompile(`\[情绪:\w+/[\d.]+\]\n?`)
)

// maxBodyRunes caps the notification body length. macOS truncates very long
// bodies at ~250 chars anyway, and a shorter cap keeps notifications readable
// at a glance.
const maxBodyRunes = 200

// Sender delivers a notification with the given title and body. Called in a
// dedicated goroutine and must not block — the app-identity implementation on
// macOS dispatches asynchronously to the main queue.
type Sender func(title, body string)

// senderWrapper is stored inside the atomic.Value so we can represent
// "no sender" as a nil *senderWrapper rather than a nil interface, which
// would cause atomic.Value to panic on type inconsistency.
type senderWrapper struct{ fn Sender }

// sender stores the active *senderWrapper. Always stores a non-nil pointer;
// a nil fn inside means no-op.
var sender atomic.Value

// SetSender registers the concrete notification backend. Passing nil reverts
// to a no-op. Typically called once at startup from main.
func SetSender(s Sender) {
	sender.Store(&senderWrapper{fn: s})
}

// System sends a non-blocking desktop notification. If no sender has been
// registered (e.g. on non-macOS or during tests), the call is a no-op.
func System(title, body string) {
	v, _ := sender.Load().(*senderWrapper)
	if v == nil || v.fn == nil {
		log.Debug().Str("title", title).Msg("notify: no sender registered, dropping")
		return
	}
	v.fn(title, truncate(sanitize(body), maxBodyRunes))
}

// sanitize strips tool-call XML tags, skill-call XML tags, and emotion tags
// from notification bodies so users see clean readable text.
func sanitize(s string) string {
	s = reToolCall.ReplaceAllString(s, "")
	s = reSkillCall.ReplaceAllString(s, "")
	s = reEmotion.ReplaceAllString(s, "")
	return s
}

// truncate returns the first n runes of s, appending "…" when truncated.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "…"
}

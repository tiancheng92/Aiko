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
	"log/slog"
	"sync/atomic"
	"unicode/utf8"
)

// maxBodyRunes caps the notification body length. macOS truncates very long
// bodies at ~250 chars anyway, and a shorter cap keeps notifications readable
// at a glance.
const maxBodyRunes = 200

// Sender delivers a notification with the given title and body. Called in a
// dedicated goroutine and must not block — the app-identity implementation on
// macOS dispatches asynchronously to the main queue.
type Sender func(title, body string)

// sender stores the active Sender. atomic.Value so SetSender and System can
// race safely without a mutex.
var sender atomic.Value

// SetSender registers the concrete notification backend. Passing nil reverts
// to a no-op. Typically called once at startup from main.
func SetSender(s Sender) {
	if s == nil {
		sender.Store(Sender(nil))
		return
	}
	sender.Store(s)
}

// System sends a non-blocking desktop notification. If no sender has been
// registered (e.g. on non-macOS or during tests), the call is a no-op.
func System(title, body string) {
	v, _ := sender.Load().(Sender)
	if v == nil {
		slog.Debug("notify: no sender registered, dropping", "title", title)
		return
	}
	v(title, truncate(body, maxBodyRunes))
}

// truncate returns the first n runes of s, appending "…" when truncated.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "…"
}

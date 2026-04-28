package proactive

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
)

const (
	// notifMaxRunes is the max rune length for notification messages.
	notifMaxRunes = 80
	// fireDeadline is how long after trigger_at an item is still fired; beyond this it is silently dropped.
	fireDeadline = 5 * time.Minute
)

// AppInterface is the subset of *app.App that ProactiveEngine needs.
// Defined here to break the import cycle (proactive → app would be circular).
type AppInterface interface {
	// IsChatVisible reports whether the chat bubble is currently open.
	IsChatVisible() bool
	// EmitEvent emits a Wails event to the frontend.
	EmitEvent(name string, data any)
}

// ProactiveEngine drives scheduled and follow-up proactive messages.
type ProactiveEngine struct {
	app   AppInterface
	store Store
	cron  *cron.Cron
}

// NewEngine creates a ProactiveEngine. store may be nil (engine skips poll jobs).
func NewEngine(app AppInterface, store Store) *ProactiveEngine {
	return &ProactiveEngine{
		app:   app,
		store: store,
		cron:  cron.New(),
	}
}

// Store returns the underlying Store. Used by app.go to expose List/Delete to the frontend.
func (e *ProactiveEngine) Store() Store {
	return e.store
}

// Start registers cron jobs and begins the scheduler.
// ctx is used as a lifecycle signal — once it is cancelled, subsequent poll
// ticks become no-ops. A fresh 30s context is created per poll so cancellation
// of the outer ctx does not propagate to an in-flight DB read.
func (e *ProactiveEngine) Start(ctx context.Context) {
	if e.store != nil {
		_, _ = e.cron.AddFunc("* * * * *", func() {
			if ctx.Err() != nil {
				return
			}
			pollCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			e.Poll(pollCtx)
		})
	}
	e.cron.Start()
}

// Stop stops the cron scheduler.
func (e *ProactiveEngine) Stop() {
	e.cron.Stop()
}

// Fire delivers a proactive message directly to the user without LLM processing.
// If chat is open, it pushes the message to the chat panel.
// If chat is closed, it shows a notification bubble (truncated to notifMaxRunes).
func (e *ProactiveEngine) Fire(_ context.Context, prompt string) error {
	if e.app.IsChatVisible() {
		e.app.EmitEvent("chat:proactive:message", prompt)
		return nil
	}
	text := prompt
	if utf8.RuneCountInString(text) > notifMaxRunes {
		runes := []rune(text)
		text = string(runes[:notifMaxRunes]) + "…"
	}
	e.app.EmitEvent("notification:show", map[string]any{
		"title":   "✨ (=^･ω･^=)",
		"message": text,
	})
	return nil
}

// Poll queries the store for due items and fires each one.
// The row is deleted before Fire is called to avoid double-firing.
// If Fire fails, a failure notification is emitted.
// Exported for testing.
func (e *ProactiveEngine) Poll(ctx context.Context) {
	if e.store == nil {
		return
	}
	items, err := e.store.DueItems(ctx, time.Now().UTC())
	if err != nil {
		slog.Warn("proactive poll: query due items", "err", err)
		return
	}
	for _, item := range items {
		// Delete before Fire to prevent double-firing if Fire is slow.
		if err := e.store.Delete(ctx, item.ID); err != nil {
			slog.Warn("proactive poll: delete item", "id", item.ID, "err", err)
			continue
		}
		// Drop items that are more than fireDeadline past their trigger time.
		// On parse failure we log and still fire — better to deliver a slightly
		// late reminder than to silently drop a user-facing item because the DB
		// row has an unexpected timestamp format.
		triggerAt, parseErr := time.Parse(time.RFC3339, item.TriggerAt)
		if parseErr != nil {
			slog.Warn("proactive poll: invalid trigger_at, firing anyway",
				"id", item.ID, "trigger_at", item.TriggerAt, "err", parseErr)
		} else if time.Now().UTC().Sub(triggerAt) > fireDeadline {
			slog.Info("proactive poll: item expired, dropped", "id", item.ID, "trigger_at", item.TriggerAt)
			continue
		}
		if err := e.Fire(ctx, item.Prompt); err != nil {
			slog.Warn("proactive poll: fire failed", "id", item.ID, "err", err)
		}
	}
}

// truncate returns the first n runes of s. If s is longer, it appends "…".
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

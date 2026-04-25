package proactive

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
)

const (
	// notifMaxRunes is the max rune length for notification messages.
	notifMaxRunes = 80
)

// AppInterface is the subset of *app.App that ProactiveEngine needs.
// Defined here to break the import cycle (proactive → app would be circular).
type AppInterface interface {
	// ChatDirect streams tokens to the frontend via chat:token / chat:done events.
	ChatDirect(ctx context.Context, prompt string) error
	// ChatDirectCollect runs the agent and returns the full response text with no events emitted.
	ChatDirectCollect(ctx context.Context, prompt string) (string, error)
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

// Start registers cron jobs and begins the scheduler.
// ctx is used as a base context for all fired messages.
func (e *ProactiveEngine) Start(ctx context.Context) {
	// Poll for due follow-up items every minute.
	if e.store != nil {
		_, _ = e.cron.AddFunc("* * * * *", func() {
			e.Poll(ctx)
		})
	}
	e.cron.Start()
}

// Stop stops the cron scheduler.
func (e *ProactiveEngine) Stop() {
	e.cron.Stop()
}

// Fire delivers a proactive message using the given prompt.
// If chat is open, it streams tokens to the frontend.
// If chat is closed, it collects the response and shows a notification.
// Returns an error if the underlying chat call fails.
func (e *ProactiveEngine) Fire(ctx context.Context, prompt string) error {
	if e.app.IsChatVisible() {
		// Emit sentinel so frontend can style the bubble.
		e.app.EmitEvent("chat:proactive:start", nil)
		if err := e.app.ChatDirect(ctx, prompt); err != nil {
			return fmt.Errorf("ChatDirect: %w", err)
		}
		return nil
	}
	// Chat is closed: collect and deliver via notification bubble.
	text, err := e.app.ChatDirectCollect(ctx, prompt)
	if err != nil {
		return fmt.Errorf("ChatDirectCollect: %w", err)
	}
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
		if err := e.Fire(ctx, item.Prompt); err != nil {
			slog.Warn("proactive poll: fire failed", "id", item.ID, "err", err)
			e.app.EmitEvent("notification:show", map[string]any{
				"title":   "提醒触发失败",
				"message": truncate(item.Prompt, 30),
			})
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

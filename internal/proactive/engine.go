package proactive

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
)

const (
	greetingMorningPrompt = `你是桌面宠物 Aiko，现在是早上，主动向用户发一句温暖简短的早安问候（1-2句话，自然随意，不要过于正式）。不要提及你是AI。`
	greetingEveningPrompt = `你是桌面宠物 Aiko，现在是晚上，主动向用户发一句轻松的晚间问候（1-2句话）。可以关心今天过得怎样。不要提及你是AI。`

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
	// Morning greeting at 09:00 local time.
	_, _ = e.cron.AddFunc("0 9 * * *", func() {
		e.Fire(ctx, greetingMorningPrompt)
	})
	// Evening check-in at 21:00 local time.
	_, _ = e.cron.AddFunc("0 21 * * *", func() {
		e.Fire(ctx, greetingEveningPrompt)
	})
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
func (e *ProactiveEngine) Fire(ctx context.Context, prompt string) {
	if e.app.IsChatVisible() {
		// Emit sentinel so frontend can style the bubble.
		e.app.EmitEvent("chat:proactive:start", nil)
		if err := e.app.ChatDirect(ctx, prompt); err != nil {
			slog.Warn("proactive fire: ChatDirect error", "err", err)
		}
		return
	}
	// Chat is closed: collect and deliver via notification bubble.
	text, err := e.app.ChatDirectCollect(ctx, prompt)
	if err != nil {
		slog.Warn("proactive fire: ChatDirectCollect error", "err", err)
		return
	}
	if utf8.RuneCountInString(text) > notifMaxRunes {
		runes := []rune(text)
		text = string(runes[:notifMaxRunes]) + "…"
	}
	e.app.EmitEvent("notification:show", map[string]any{
		"title":   "✨ (=^･ω･^=)",
		"message": text,
	})
}

// Poll queries the store for due items and fires each one.
// Exported for testing.
func (e *ProactiveEngine) Poll(ctx context.Context) {
	if e.store == nil {
		return
	}
	items, err := e.store.DueItems(ctx, time.Now())
	if err != nil {
		slog.Warn("proactive poll: query due items", "err", err)
		return
	}
	for _, item := range items {
		// Delete before calling Fire to avoid double-firing if Fire is slow.
		if err := e.store.Delete(ctx, item.ID); err != nil {
			slog.Warn("proactive poll: delete item", "id", item.ID, "err", err)
			continue
		}
		e.Fire(ctx, item.Prompt)
	}
}

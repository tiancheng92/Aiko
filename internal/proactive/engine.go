package proactive

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
	"time"
	"unicode/utf8"

	"aiko/internal/notify"
)

const (
	// notifMaxRunes is the max rune length for notification messages.
	notifMaxRunes = 80
	// fireDeadline is how long after trigger_at an item is still fired; beyond this it is silently dropped.
	fireDeadline = 5 * time.Minute
	// pollInterval is how often the engine checks for due proactive items.
	pollInterval = time.Minute
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
// It uses a simple ticker loop that compares wall-clock time against
// trigger_at stored in SQLite, so it is immune to system sleep/wake drift —
// the same approach used by Hermes Agent's cron/scheduler.py.
type ProactiveEngine struct {
	app     AppInterface
	store   Store
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
	pollNow chan struct{}
}

// NewEngine creates a ProactiveEngine. store may be nil (engine skips poll jobs).
func NewEngine(app AppInterface, store Store) *ProactiveEngine {
	return &ProactiveEngine{
		app:     app,
		store:   store,
		pollNow: make(chan struct{}, 1),
	}
}

// Store returns the underlying Store. Used by app.go to expose List/Delete to the frontend.
func (e *ProactiveEngine) Store() Store {
	return e.store
}

// Start launches the background poll loop.
// ctx is used as a lifecycle signal — once cancelled the loop exits.
func (e *ProactiveEngine) Start(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done != nil {
		return // already running
	}
	e.done = make(chan struct{})
	if e.store != nil {
		e.wg.Go(func() {
			e.loop(ctx)
		})
	}
}

// Stop stops the poll loop and waits for it to exit before returning.
// Safe to call even if Start was never called.
func (e *ProactiveEngine) Stop() {
	e.mu.Lock()
	if e.done == nil {
		e.mu.Unlock()
		return
	}
	close(e.done)
	e.done = nil
	e.mu.Unlock()
	e.wg.Wait()
}

// TriggerPoll requests an immediate poll without waiting for the next tick.
// Non-blocking: if a poll is already queued the call is a no-op.
func (e *ProactiveEngine) TriggerPoll() {
	select {
	case e.pollNow <- struct{}{}:
	default:
	}
}

// loop is the background goroutine that drives periodic polling.
func (e *ProactiveEngine) loop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.pollOnce(ctx)
		case <-e.pollNow:
			e.pollOnce(ctx)
		case <-e.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// pollOnce wraps Poll with a fresh timeout context.
func (e *ProactiveEngine) pollOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	pollCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e.Poll(pollCtx)
}

// Fire delivers a proactive message directly to the user without LLM processing.
// If chat is open, it pushes the message to the chat panel.
// If chat is closed, it shows an in-app notification bubble and also pushes
// a macOS system notification so the user sees the reminder even when the
// pet window is not focused.
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
	const title = "✨ (=^･ω･^=)"
	e.app.EmitEvent("notification:show", map[string]any{
		"title":   title,
		"message": text,
	})
	go notify.System(title, text)
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
		log.Warn().Err(err).Msg("proactive poll: query due items")
		return
	}
	for i := range items {
		// Delete before Fire to prevent double-firing if Fire is slow.
		if err := e.store.Delete(ctx, items[i].ID); err != nil {
			log.Warn().Int64("id", items[i].ID).Err(err).Msg("proactive poll: delete item")
			continue
		}
		// Drop items that are more than fireDeadline past their trigger time.
		// On parse failure we log and still fire — better to deliver a slightly
		// late reminder than to silently drop a user-facing item because the DB
		// row has an unexpected timestamp format.
		triggerAt, parseErr := time.Parse(time.RFC3339, items[i].TriggerAt)
		if parseErr != nil {
			log.Warn().Int64("id", items[i].ID).Str("trigger_at", items[i].TriggerAt).Err(parseErr).Msg("proactive poll: invalid trigger_at, firing anyway")
		} else if time.Now().UTC().Sub(triggerAt) > fireDeadline {
			log.Info().Int64("id", items[i].ID).Str("trigger_at", items[i].TriggerAt).Msg("proactive poll: item expired, dropped")
			continue
		}
		if err := e.Fire(ctx, items[i].Prompt); err != nil {
			log.Warn().Int64("id", items[i].ID).Err(err).Msg("proactive poll: fire failed")
		}
	}
}


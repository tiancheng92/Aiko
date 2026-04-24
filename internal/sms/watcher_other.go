//go:build !darwin

// Package sms is a no-op stub on non-macOS platforms.
package sms

import "context"

// Event carries a detected verification code and its context.
type Event struct {
	Code   string `json:"code"`
	Sender string `json:"sender"`
	Text   string `json:"text"`
}

// Handler is called on each detected verification code event.
type Handler func(Event)

// Watcher is a no-op on non-macOS platforms.
type Watcher struct{}

// NewWatcher returns a stub watcher that does nothing.
func NewWatcher(_ Handler) (*Watcher, error) { return &Watcher{}, nil }

// Start is a no-op.
func (w *Watcher) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (w *Watcher) Stop() {}

package main

import (
	"database/sql"
	"errors"
	"fmt"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/sms"
)

// GetVoiceAutoSend returns whether voice messages are sent automatically
// after the final STT result arrives.
func (a *App) GetVoiceAutoSend() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.VoiceAutoSend
}

// SetVoiceAutoSend sets the voice auto-send flag and persists it.
func (a *App) SetVoiceAutoSend(enabled bool) error {
	a.mu.Lock()
	a.cfg.VoiceAutoSend = enabled
	cfgCopy := *a.cfg
	a.mu.Unlock()
	return a.configStore.Save(&cfgCopy)
}

// GetSoundsEnabled returns whether chat sound effects are enabled.
func (a *App) GetSoundsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.SoundsEnabled
}

// SetSoundsEnabled sets the sounds enabled flag and persists it.
func (a *App) SetSoundsEnabled(enabled bool) error {
	a.mu.Lock()
	a.cfg.SoundsEnabled = enabled
	cfgCopy := *a.cfg
	a.mu.Unlock()
	return a.configStore.Save(&cfgCopy)
}

// startSMSWatcher creates and starts an SMS watcher, emitting verification code
// events to the frontend and copying the code to the clipboard.
// Caller must NOT hold a.mu.
func (a *App) startSMSWatcher() error {
	allMessages := a.cfg.SMSAllMessagesEnabled
	w, err := sms.NewWatcherWithOptions(func(evt sms.Event) {
		switch evt.Kind {
		case "code":
			wailsruntime.ClipboardSetText(a.ctx, evt.Code)
			wailsruntime.EventsEmit(a.ctx, "sms:verification_code", map[string]any{
				"code":   evt.Code,
				"sender": evt.Sender,
				"text":   evt.Text,
			})
			wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
				"title":   "📱 验证码：" + evt.Code,
				"message": evt.Sender + "：" + evt.Text,
			})
		case "message":
			wailsruntime.EventsEmit(a.ctx, "sms:message", map[string]any{
				"sender": evt.Sender,
				"text":   evt.Text,
			})
			wailsruntime.EventsEmit(a.ctx, "notification:show", map[string]any{
				"title":   "📱 " + evt.Sender,
				"message": evt.Text,
			})
		}
	}, allMessages)
	if err != nil {
		return err
	}
	if err := w.Start(a.ctx); err != nil {
		return err
	}
	a.mu.Lock()
	a.smsWatcher = w
	a.mu.Unlock()
	return nil
}

// StartSMSWatcher enables SMS monitoring, persists the setting, and starts the watcher.
func (a *App) StartSMSWatcher() error {
	a.mu.RLock()
	running := a.smsWatcher != nil
	a.mu.RUnlock()
	if running {
		return nil // already running
	}
	a.cfg.SMSWatcherEnabled = true
	if err := a.configStore.Save(a.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return a.startSMSWatcher()
}

// StopSMSWatcher disables SMS monitoring, persists the setting, and stops the watcher.
func (a *App) StopSMSWatcher() error {
	a.cfg.SMSWatcherEnabled = false
	if err := a.configStore.Save(a.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	a.mu.Lock()
	w := a.smsWatcher
	a.smsWatcher = nil
	a.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	return nil
}

// IsSMSWatcherRunning reports whether the SMS watcher is currently active.
func (a *App) IsSMSWatcherRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.smsWatcher != nil
}

// GetSMSAllMessagesEnabled returns whether all SMS messages (not just
// verification codes) are forwarded to the frontend.
func (a *App) GetSMSAllMessagesEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.SMSAllMessagesEnabled
}

// SetSMSAllMessagesEnabled persists the setting and restarts the watcher if
// it is currently running so the change takes effect immediately.
func (a *App) SetSMSAllMessagesEnabled(enabled bool) error {
	a.mu.Lock()
	a.cfg.SMSAllMessagesEnabled = enabled
	wasRunning := a.smsWatcher != nil
	oldWatcher := a.smsWatcher
	a.smsWatcher = nil
	cfgCopy := *a.cfg
	a.mu.Unlock()

	if oldWatcher != nil {
		oldWatcher.Stop()
	}

	if err := a.configStore.Save(&cfgCopy); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	if wasRunning {
		if err := a.startSMSWatcher(); err != nil {
			return fmt.Errorf("restart sms watcher: %w", err)
		}
	}
	return nil
}

// IsFirstLaunch reports whether the welcome message has never been shown.
func (a *App) IsFirstLaunch() bool {
	var val string
	err := a.sqlDB.QueryRowContext(a.ctx,
		`SELECT value FROM settings WHERE key = 'welcome_shown'`).Scan(&val)
	return errors.Is(err, sql.ErrNoRows)
}

// MarkWelcomeShown records that the welcome message has been displayed.
func (a *App) MarkWelcomeShown() error {
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key, value) VALUES('welcome_shown','1')
		 ON CONFLICT(key) DO UPDATE SET value='1'`)
	if err != nil {
		return fmt.Errorf("mark welcome shown: %w", err)
	}
	return nil
}

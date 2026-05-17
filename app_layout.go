package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ScreenInfo holds the logical resolution of a screen.
type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// MousePosition holds the CSS coordinates of the mouse cursor.
type MousePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// GetBallPosition returns the saved ball [x, y] for the given screen resolution,
// or [-1, -1] if no position has been saved for that resolution yet.
func (a *App) GetBallPosition(screenW, screenH int) []int {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return []int{-1, -1}
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return []int{-1, -1}
	}
	x, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return []int{-1, -1}
	}
	return []int{x, y}
}

// SaveBallPosition persists the ball position for the given screen resolution.
func (a *App) SaveBallPosition(x, y, screenW, screenH int) error {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	val := fmt.Sprintf("%d,%d", x, y)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}

// ResetBallPosition deletes the saved ball position for the given screen resolution,
// causing the pet to return to its default position on next render.
func (a *App) ResetBallPosition(screenW, screenH int) error {
	key := fmt.Sprintf("ball_pos_%dx%d", screenW, screenH)
	_, err := a.sqlDB.ExecContext(a.ctx, `DELETE FROM settings WHERE key=?`, key)
	return err
}

// GetScreenList returns all connected screens as ScreenInfo values.
func (a *App) GetScreenList() []ScreenInfo {
	screens, err := wailsruntime.ScreenGetAll(a.ctx)
	if err != nil {
		slog.Warn("GetScreenList: ScreenGetAll failed", "err", err)
		return nil
	}
	result := make([]ScreenInfo, 0, len(screens))
	for _, s := range screens {
		result = append(result, ScreenInfo{Width: s.Size.Width, Height: s.Size.Height})
	}
	return result
}

// startScreenWatcher polls the mouse position every 500ms and migrates the Wails window
// to the screen containing the cursor. Emits "screen:changed" when the active screen changes.
// It also detects display reconfiguration (e.g. after wake from sleep) by tracking the
// screen count and the current screen's geometry — re-emitting if either changes.
func (a *App) startScreenWatcher() {
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancelWatcher = cancel
	a.watcherWG.Add(1)
	go func() {
		defer a.watcherWG.Done()
		defer cancel()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lastFoundIdx := -1
		lastNumScreens := -1
		var lastFrame ScreenFrame
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			// Use CGO-only calls to locate the cursor's screen — no Wails IPC needed.
			mx := getMouseX()
			my := getMouseY()
			n := getNumScreens()

			foundIdx := -1
			for i := 0; i < n; i++ {
				frame := getScreenFrame(i)
				if !frame.Valid {
					continue
				}
				if mx >= frame.OriginX && mx < frame.OriginX+frame.Width &&
					my >= frame.OriginY && my < frame.OriginY+frame.Height {
					foundIdx = i
					break
				}
			}
			if foundIdx < 0 {
				continue
			}

			frame := getScreenFrame(foundIdx)
			displayChanged := n != lastNumScreens ||
				frame.Width != lastFrame.Width ||
				frame.Height != lastFrame.Height ||
				frame.OriginX != lastFrame.OriginX ||
				frame.OriginY != lastFrame.OriginY

			if foundIdx == lastFoundIdx && !displayChanged {
				continue
			}
			lastFoundIdx = foundIdx
			lastNumScreens = n
			lastFrame = frame

			current := ScreenInfo{Width: int(frame.Width), Height: int(frame.Height)}

			// Move the window directly via CGO to bypass Wails' WindowSetPosition,
			// which is relative to the current screen and cannot reliably migrate
			// the window to a different screen.
			moveWindowToScreen(foundIdx)

			a.mu.Lock()
			a.activeScreen = current
			a.mu.Unlock()

			wailsruntime.EventsEmit(a.ctx, "screen:changed", current)
			slog.Info("startScreenWatcher: screen changed", "width", current.Width, "height", current.Height, "numScreens", n)
		}
	}()
}

// GetPetSize returns the saved pet height for the given screen resolution, or 0 if not set or on error.
func (a *App) GetPetSize(screenW, screenH int) int {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

// SavePetSize persists the pet height for the given screen resolution.
func (a *App) SavePetSize(size, screenW, screenH int) error {
	key := fmt.Sprintf("pet_size_%dx%d", screenW, screenH)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, strconv.Itoa(size))
	return err
}

// GetChatSize returns the saved chat bubble [width, height] for the given screen resolution.
// Returns [0, 0] if no size has been saved for that resolution yet.
func (a *App) GetChatSize(screenW, screenH int) []int {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	var val string
	if err := a.sqlDB.QueryRowContext(a.ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&val); err != nil {
		return []int{0, 0}
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) != 2 {
		return []int{0, 0}
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return []int{0, 0}
	}
	return []int{w, h}
}

// SaveChatSize persists the chat bubble dimensions for the given screen resolution.
func (a *App) SaveChatSize(width, height, screenW, screenH int) error {
	key := fmt.Sprintf("chat_size_%dx%d", screenW, screenH)
	val := fmt.Sprintf("%d,%d", width, height)
	_, err := a.sqlDB.ExecContext(a.ctx,
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, val)
	return err
}

// GetMousePosition returns the current mouse cursor position in CSS coordinates.
// This works even when the app is not focused, enabling eye tracking while unfocused.
func (a *App) GetMousePosition() MousePosition {
	x, y := GetMousePosition()
	return MousePosition{X: x, Y: y}
}

// GetScreenSize returns the primary screen's [width, height] in pixels.
func (a *App) GetScreenSize() []int {
	screens, err := wailsruntime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return []int{1440, 900}
	}
	for _, s := range screens {
		if s.IsPrimary {
			return []int{s.Size.Width, s.Size.Height}
		}
	}
	return []int{screens[0].Size.Width, screens[0].Size.Height}
}

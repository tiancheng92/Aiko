//go:build darwin

package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// MousePosition holds the CSS coordinates of the mouse cursor.
type MousePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// WindowService manages window state, screen info, and dialog helpers.
type WindowService struct{ s *sharedState }

// NewWindowService creates a WindowService backed by the given shared state.
func NewWindowService(s *sharedState) *WindowService { return &WindowService{s: s} }

// OpenSettings shows the settings window and temporarily lowers the main
// window out of always-on-top so the settings panel is not occluded.
// Always-on-top is restored the first time the settings window loses focus.
func (w *WindowService) OpenSettings() {
	settingsWin, ok := w.s.app.Window.GetByName("settings")
	if !ok {
		return
	}
	mainWin, hasMain := w.s.app.Window.GetByName("main")
	if hasMain {
		mainWin.SetAlwaysOnTop(false)
		// Restore always-on-top once — unregister immediately so repeated
		// OpenSettings calls don't accumulate handlers.
		var off func()
		off = settingsWin.OnWindowEvent(events.Common.WindowLostFocus, func(_ *application.WindowEvent) {
			mainWin.SetAlwaysOnTop(true)
			if off != nil {
				off()
				off = nil
			}
		})
	}
	settingsWin.Show()
	settingsWin.Focus()
}

// CloseSettings hides the settings window and restores main always-on-top.
func (w *WindowService) CloseSettings() {
	if win, ok := w.s.app.Window.GetByName("settings"); ok {
		win.Hide()
	}
	if mainWin, ok := w.s.app.Window.GetByName("main"); ok {
		mainWin.SetAlwaysOnTop(true)
	}
}

// IsChatVisible reports whether the chat panel is currently open.
func (w *WindowService) IsChatVisible() bool {
	w.s.mu.RLock()
	defer w.s.mu.RUnlock()
	return w.s.isChatVisible
}

// SetChatVisible updates the tracked chat-panel visibility state.
// Called by the frontend when the chat bubble is opened or closed.
func (w *WindowService) SetChatVisible(visible bool) {
	w.s.mu.Lock()
	w.s.isChatVisible = visible
	w.s.mu.Unlock()
}

// AcquireKeyWindow is a no-op under Wails v3 multi-window.
func (w *WindowService) AcquireKeyWindow() {}

// ReleaseKeyWindow is a no-op under Wails v3 multi-window.
func (w *WindowService) ReleaseKeyWindow() {}

// EmitEvent emits a Wails runtime event with the given name and payload.
func (w *WindowService) EmitEvent(name string, data any) {
	w.s.app.Event.Emit(name, data)
}

// GetScreenList returns the logical resolution of all connected screens.
func (w *WindowService) GetScreenList() []ScreenInfo {
	screens := w.s.app.Screen.GetAll()
	result := make([]ScreenInfo, 0, len(screens))
	for _, s := range screens {
		result = append(result, ScreenInfo{Width: s.Size.Width, Height: s.Size.Height})
	}
	return result
}

// GetScreenSize returns the primary screen's [width, height] in pixels.
func (w *WindowService) GetScreenSize() []int {
	screens := w.s.app.Screen.GetAll()
	if len(screens) == 0 {
		return []int{1440, 900}
	}
	for _, s := range screens {
		if s.IsPrimary {
			return []int{s.Size.Width, s.Size.Height}
		}
	}
	return []int{screens[0].Size.Width, screens[0].Size.Height}
}

// GetMousePosition returns the current mouse cursor position in CSS coordinates.
// This works even when the app is not focused, enabling eye tracking while unfocused.
func (w *WindowService) GetMousePosition() MousePosition {
	var x, y float64
	if w.s.hooks.GetMouseX != nil {
		x = w.s.hooks.GetMouseX()
	}
	if w.s.hooks.GetMouseY != nil {
		y = w.s.hooks.GetMouseY()
	}
	return MousePosition{X: x, Y: y}
}

// OpenFileDialog opens a native file picker and returns the selected path.
func (w *WindowService) OpenFileDialog(title string, filters []application.FileFilter) (string, error) {
	d := w.s.app.Dialog.OpenFile().SetTitle(title)
	for _, f := range filters {
		d.AddFilter(f.DisplayName, f.Pattern)
	}
	return d.PromptForSingleSelection()
}

// OpenDirectoryDialog opens a native directory picker and returns the selected path.
func (w *WindowService) OpenDirectoryDialog(title string) (string, error) {
	return w.s.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
}

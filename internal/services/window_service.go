//go:build darwin

package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"
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

// OpenSettings shows and focuses the settings window.
func (w *WindowService) OpenSettings() {
	if win, ok := w.s.app.Window.GetByName("settings"); ok {
		win.Show()
		win.Focus()
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

// AcquireKeyWindow makes the Aiko window the key window so CSS :hover states
// work while a context menu is open and another app is frontmost.
func (w *WindowService) AcquireKeyWindow() {
	if w.s.hooks.AcquireKeyWindow != nil {
		w.s.hooks.AcquireKeyWindow()
	}
}

// ReleaseKeyWindow resigns key-window status, restoring focus to the previous app.
func (w *WindowService) ReleaseKeyWindow() {
	if w.s.hooks.ReleaseKeyWindow != nil {
		w.s.hooks.ReleaseKeyWindow()
	}
}

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

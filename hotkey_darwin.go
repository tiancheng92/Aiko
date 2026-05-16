//go:build darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// globalApp holds the Wails v3 app instance after startup, used by registerGlobalHotkey.
var globalApp *application.App

// setGlobalApp stores the app reference for use by macos.go CGO callbacks.
func setGlobalApp(app *application.App) {
	globalApp = app
}

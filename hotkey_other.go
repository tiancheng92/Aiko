//go:build !darwin

package main

import "context"

// globalAppCtx holds the Wails app context after startup, used by registerGlobalHotkey.
var globalAppCtx context.Context

// settingsServerAddr holds the address (host:port) of the settings asset server.
// Populated in startup() and used by OpenSettingsPanel.
var settingsServerAddr string

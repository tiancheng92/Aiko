//go:build darwin

package main

import "context"

// globalAppCtx holds the Wails app context after startup, used by registerGlobalHotkey.
var globalAppCtx context.Context

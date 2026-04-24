//go:build !darwin

// internal/tools/browser_other.go
package tools

import "errors"

// getBrowserURLNative is not implemented on non-macOS platforms.
func getBrowserURLNative() (string, error) {
	return "", errors.New("get_browser_url is only supported on macOS")
}

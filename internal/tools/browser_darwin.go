//go:build darwin

// internal/tools/browser_darwin.go
package tools

import (
	"fmt"
	"os/exec"
	"strings"
)

// browserScript maps a bundle-ID prefix to an AppleScript snippet that
// returns the current tab's URL as a plain string.
var browserScripts = []struct {
	app    string // display name for error messages
	script string
}{
	{
		app: "Google Chrome",
		script: `tell application "Google Chrome"
	if not running then return ""
	if (count of windows) = 0 then return ""
	get URL of active tab of front window
end tell`,
	},
	{
		app: "Arc",
		script: `tell application "Arc"
	if not running then return ""
	if (count of windows) = 0 then return ""
	get URL of active tab of front window
end tell`,
	},
	{
		app: "Safari",
		script: `tell application "Safari"
	if not running then return ""
	if (count of windows) = 0 then return ""
	get URL of current tab of front window
end tell`,
	},
	{
		app: "Brave Browser",
		script: `tell application "Brave Browser"
	if not running then return ""
	if (count of windows) = 0 then return ""
	get URL of active tab of front window
end tell`,
	},
	{
		app: "Microsoft Edge",
		script: `tell application "Microsoft Edge"
	if not running then return ""
	if (count of windows) = 0 then return ""
	get URL of active tab of front window
end tell`,
	},
	{
		app: "Firefox",
		script: `tell application "Firefox"
	if not running then return ""
	if (count of windows) = 0 then return ""
	get current URI
end tell`,
	},
}

// getBrowserURLNative runs osascript to query the frontmost browser for its
// current tab URL. It tries each supported browser in order and returns the
// first non-empty http/https URL found.
func getBrowserURLNative() (string, error) {
	var lastErr error
	for _, b := range browserScripts {
		out, err := exec.Command("osascript", "-e", b.script).Output()
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", b.app, err)
			continue
		}
		url := strings.TrimSpace(string(out))
		if url == "" || url == "missing value" {
			continue
		}
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			return url, nil
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("no supported browser returned a URL (last error: %w)", lastErr)
	}
	return "", fmt.Errorf("no supported browser is open or has an active tab")
}

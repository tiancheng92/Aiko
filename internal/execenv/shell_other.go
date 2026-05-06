//go:build !darwin

package execenv

// loadShellEnv is a no-op on non-darwin platforms. Windows / Linux don't
// have the macOS launchd-minimal-PATH problem that motivates this package.
func loadShellEnv() map[string]string { return nil }

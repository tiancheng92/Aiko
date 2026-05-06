//go:build darwin

package execenv

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// loadShellEnv probes the user's login shell to capture their real environment
// (PATH, NVM_DIR, GOPATH, etc.) so subprocesses launched from an .app bundle
// inherit the same setup as a terminal session.
//
// Uses `$SHELL -ilc env` — both login (-l) and interactive (-i), because
// nvm/asdf shims are typically registered in .zshrc/.bashrc (interactive)
// rather than .zprofile (login).
//
// 3-second timeout: guards against a broken .zshrc that runs `read` or makes
// network calls. Returns nil on any failure — callers fall back to hardcoded
// candidateDirs.
func loadShellEnv() map[string]string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", "env").Output()
	if err != nil {
		return nil
	}
	return parseEnvOutput(out)
}

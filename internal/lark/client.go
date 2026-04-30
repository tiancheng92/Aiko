// internal/lark/client.go
package lark

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Client wraps lark-cli subprocess calls.
type Client struct {
	// CLIPath is the path to the lark-cli executable. If empty, "lark-cli" is used.
	CLIPath string
}

// NewClient creates a Client. cliPath may be empty to use PATH resolution.
func NewClient(cliPath string) *Client {
	if cliPath == "" {
		cliPath = "lark-cli"
	}
	return &Client{CLIPath: cliPath}
}

// Run executes lark-cli with the given arguments and returns stdout.
// stderr is captured and appended to the error message on failure.
//
// The error message only includes the subcommand (first arg) and an arg count
// rather than the full args slice — lark-cli args can contain access tokens,
// message bodies, and other values we don't want in logs or UI toasts.
func (c *Client) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.CLIPath, args...)
	cmd.Env = append(os.Environ(), "PATH="+augmentedPATH())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		sub := "?"
		if len(args) > 0 {
			sub = args[0]
		}
		return "", fmt.Errorf("lark-cli %s (%d args): %s", sub, len(args), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Status returns the output of `lark-cli auth status`.
// Returns an error if lark-cli is not installed or not authenticated.
func (c *Client) Status(ctx context.Context) (string, error) {
	return c.Run(ctx, "auth", "status")
}

// candidateDirs lists directories where npm/node package managers commonly
// install global binaries on macOS. These are not in the minimal PATH that
// macOS uses when launching a .app bundle from Finder/Dock.
var candidateDirs = []string{
	"/usr/local/bin",
	"/opt/homebrew/bin",
	"/opt/homebrew/sbin",
}

// augmentedPATH returns a PATH value that prepends common Node.js and npm
// binary directories to the current PATH. This is required because .app
// bundles launched from Finder/Dock start with a minimal PATH that omits
// Homebrew, nvm, and npm global bin dirs — causing lark-cli's
// `#!/usr/bin/env node` shebang to fail with "node: No such file or directory".
func augmentedPATH() string {
	home, _ := os.UserHomeDir()
	extra := append([]string{}, candidateDirs...)
	if home != "" {
		extra = append(extra,
			filepath.Join(home, ".local/share/npm/bin"),
			filepath.Join(home, ".npm-global/bin"),
			filepath.Join(home, ".yarn/bin"),
			filepath.Join(home, "node_modules/.bin"),
		)
	}
	current := os.Getenv("PATH")
	if current != "" {
		extra = append(extra, current)
	}
	return strings.Join(extra, ":")
}

// FindCLI returns the absolute path of lark-cli, or an empty string if not
// found. It first checks $PATH (works when launched from a terminal), then
// falls back to common npm/node global bin directories and the user's home
// directory prefixes so that .app bundles launched from Finder can also locate
// the binary.
func FindCLI() string {
	if p, err := exec.LookPath("lark-cli"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	extra := append([]string{}, candidateDirs...)
	if home != "" {
		extra = append(extra,
			filepath.Join(home, ".local/share/npm/bin"),
			filepath.Join(home, ".npm-global/bin"),
			filepath.Join(home, ".yarn/bin"),
			filepath.Join(home, "node_modules/.bin"),
			filepath.Join(home, ".nvm/versions/node"),
		)
	}
	for _, dir := range extra {
		p := filepath.Join(dir, "lark-cli")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

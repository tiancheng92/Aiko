// internal/lark/client.go
package lark

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"aiko/internal/execenv"
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
	cmd.Env = execenv.AugmentedEnv()
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

// FindCLI returns the absolute path of lark-cli, or an empty string if not
// found. It searches the augmented PATH (login shell + candidate dirs +
// current PATH) so .app bundles launched from Finder can still locate it.
func FindCLI() string {
	return execenv.LookPath("lark-cli")
}

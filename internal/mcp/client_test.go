package mcp

import (
	"strings"
	"testing"
)

// TestBuildStdioEnvPrependsCommandDir ensures buildStdioEnv prepends the
// directory containing the command binary to PATH, so shebang scripts like
// /opt/homebrew/bin/npx can locate their interpreter (node) even when the
// parent process was launched by launchd with a minimal PATH.
func TestBuildStdioEnvPrependsCommandDir(t *testing.T) {
	parent := []string{
		"HOME=/Users/x",
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"FOO=bar",
	}
	env := buildStdioEnv("/opt/homebrew/bin/npx", parent)

	// All non-PATH parent vars must be preserved.
	if !containsLine(env, "HOME=/Users/x") {
		t.Errorf("expected HOME to be preserved, got %v", env)
	}
	if !containsLine(env, "FOO=bar") {
		t.Errorf("expected FOO to be preserved, got %v", env)
	}

	// PATH must exist exactly once and start with the command's directory,
	// so the interpreter lookup (`/usr/bin/env node`) finds node in the same bin dir.
	pathLines := filterPrefix(env, "PATH=")
	if len(pathLines) != 1 {
		t.Fatalf("expected exactly one PATH entry, got %d: %v", len(pathLines), pathLines)
	}
	got := strings.TrimPrefix(pathLines[0], "PATH=")
	parts := strings.Split(got, ":")
	if parts[0] != "/opt/homebrew/bin" {
		t.Errorf("expected PATH to start with /opt/homebrew/bin, got %q", got)
	}
	if !strings.Contains(got, "/usr/bin") {
		t.Errorf("expected original PATH entries preserved, got %q", got)
	}
}

// TestBuildStdioEnvAddsCommonBinDirs ensures common Homebrew / local bin
// directories are added to PATH even when the parent PATH lacks them,
// covering python, bun, and locally-installed MCP servers.
func TestBuildStdioEnvAddsCommonBinDirs(t *testing.T) {
	parent := []string{"PATH=/usr/bin:/bin"}
	env := buildStdioEnv("/usr/bin/python3", parent)

	path := strings.TrimPrefix(filterPrefix(env, "PATH=")[0], "PATH=")
	for _, want := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if !strings.Contains(path, want) {
			t.Errorf("expected PATH to contain %q, got %q", want, path)
		}
	}
}

// TestBuildStdioEnvNoDuplicates ensures we don't duplicate dirs that are
// already present in the parent PATH.
func TestBuildStdioEnvNoDuplicates(t *testing.T) {
	parent := []string{"PATH=/opt/homebrew/bin:/usr/bin"}
	env := buildStdioEnv("/opt/homebrew/bin/npx", parent)

	path := strings.TrimPrefix(filterPrefix(env, "PATH=")[0], "PATH=")
	count := strings.Count(":"+path+":", ":/opt/homebrew/bin:")
	if count != 1 {
		t.Errorf("expected /opt/homebrew/bin to appear once, got %d in %q", count, path)
	}
}

// TestBuildStdioEnvEmptyCommand handles the edge case where cfg.Command is
// empty or not an absolute path (e.g. "npx" relying on PATH lookup).
// It should still return a valid env with augmented PATH and not panic.
func TestBuildStdioEnvEmptyCommand(t *testing.T) {
	parent := []string{"PATH=/usr/bin"}

	for _, cmd := range []string{"", "npx", "python"} {
		env := buildStdioEnv(cmd, parent)
		paths := filterPrefix(env, "PATH=")
		if len(paths) != 1 {
			t.Fatalf("cmd=%q: expected one PATH entry, got %d", cmd, len(paths))
		}
		path := strings.TrimPrefix(paths[0], "PATH=")
		if !strings.Contains(path, "/usr/bin") {
			t.Errorf("cmd=%q: expected original PATH preserved, got %q", cmd, path)
		}
		if !strings.Contains(path, "/opt/homebrew/bin") {
			t.Errorf("cmd=%q: expected /opt/homebrew/bin added, got %q", cmd, path)
		}
	}
}

// containsLine reports whether lines contains target exactly.
func containsLine(lines []string, target string) bool {
	for _, l := range lines {
		if l == target {
			return true
		}
	}
	return false
}

// filterPrefix returns all lines that begin with prefix.
func filterPrefix(lines []string, prefix string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

// Package execenv provides a consistent environment for subprocesses launched
// by Aiko, regardless of whether the parent was started from a terminal (full
// PATH) or from Finder/Dock via launchd (minimal PATH: /usr/bin:/bin:/usr/sbin:/sbin).
//
// macOS .app bundles receive a minimal PATH from launchd that excludes
// Homebrew, nvm, asdf, npm globals, etc. This causes subprocess shebangs
// like `#!/usr/bin/env node` to fail with "node: No such file or directory".
// This package rebuilds a usable PATH from three sources:
//
//  1. User's login-shell PATH (probed once via `$SHELL -ilc env`, cached).
//  2. Hardcoded candidateDirs (Homebrew, /usr/local, etc.).
//  3. The current process PATH.
//
// All failures (shell probe timeout, missing $SHELL, non-darwin platform)
// degrade silently to the remaining sources.
package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// candidateDirs are user-space bin directories macOS launchd omits.
var candidateDirs = []string{
	"/opt/homebrew/bin",
	"/opt/homebrew/sbin",
	"/usr/local/bin",
	"/usr/local/sbin",
}

// homeCandidateDirs returns $HOME-based dirs used by npm/yarn/pipx/cargo.
// Returns nil when $HOME is unset.
func homeCandidateDirs() []string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".local/bin"),
		filepath.Join(home, ".local/share/npm/bin"),
		filepath.Join(home, ".npm-global/bin"),
		filepath.Join(home, ".yarn/bin"),
		filepath.Join(home, ".cargo/bin"),
		filepath.Join(home, "node_modules/.bin"),
	}
}

// parseEnvOutput parses the output of env(1) into KEY=VALUE pairs.
// Lines without '=' are skipped. Values may contain further '='.
func parseEnvOutput(b []byte) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		out[line[:i]] = line[i+1:]
	}
	return out
}

// mergePaths joins sources with ':' and deduplicates entries, preserving
// first occurrence. Empty entries and empty sources are dropped.
func mergePaths(sources ...string) string {
	seen := make(map[string]struct{})
	var out []string
	for _, src := range sources {
		if src == "" {
			continue
		}
		for _, entry := range strings.Split(src, ":") {
			if entry == "" {
				continue
			}
			if _, ok := seen[entry]; ok {
				continue
			}
			seen[entry] = struct{}{}
			out = append(out, entry)
		}
	}
	return strings.Join(out, ":")
}

var (
	shellEnvOnce sync.Once
	shellEnv     map[string]string // guarded by Once; nil on any failure
)

// getShellEnv returns the cached login-shell env. First call runs
// loadShellEnv (platform-specific) once; subsequent calls return the same
// result. Failure is also cached — a broken .zshrc doesn't cause repeated
// 3-second probes.
func getShellEnv() map[string]string {
	shellEnvOnce.Do(func() { shellEnv = loadShellEnv() })
	return shellEnv
}

// AugmentedPATH returns a PATH value for subprocesses. Sources in priority:
//  1. Login-shell PATH (if probed successfully).
//  2. candidateDirs + homeCandidateDirs.
//  3. os.Getenv("PATH").
//
// Deduplicated, first occurrence wins.
func AugmentedPATH() string {
	var shellPath string
	if env := getShellEnv(); env != nil {
		shellPath = env["PATH"]
	}
	return mergePaths(
		shellPath,
		strings.Join(candidateDirs, ":"),
		strings.Join(homeCandidateDirs(), ":"),
		os.Getenv("PATH"),
	)
}

// AugmentedEnv returns an environment slice for cmd.Env. It merges
// os.Environ() over the login-shell env (os.Environ wins — HOME and TMPDIR
// from launchd/Wails are authoritative), then forces PATH = AugmentedPATH().
func AugmentedEnv() []string {
	base := make(map[string]string)
	for k, v := range getShellEnv() {
		base[k] = v
	}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			base[kv[:i]] = kv[i+1:]
		}
	}
	base["PATH"] = AugmentedPATH()

	out := make([]string, 0, len(base))
	for k, v := range base {
		out = append(out, k+"="+v)
	}
	return out
}

// isExecutableFile reports whether path refers to an existing regular
// (non-directory) file with at least one exec bit set.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// lookPathIn searches the provided paths for name and returns the first
// executable file match. If name contains '/', paths is ignored and name
// itself is checked. Empty entries in paths are skipped. Returns "" on miss.
func lookPathIn(name string, paths []string) string {
	if strings.ContainsRune(name, '/') {
		if isExecutableFile(name) {
			return name
		}
		return ""
	}
	for _, dir := range paths {
		if dir == "" {
			continue
		}
		full := filepath.Join(dir, name)
		if isExecutableFile(full) {
			return full
		}
	}
	return ""
}

// LookPath searches AugmentedPATH() for name and returns the absolute path
// if found and executable. Returns "" on miss. Unlike exec.LookPath, it
// does not rely on the current process PATH.
func LookPath(name string) string {
	return lookPathIn(name, strings.Split(AugmentedPATH(), ":"))
}

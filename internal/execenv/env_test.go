package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergePathsOrder ensures sources are concatenated in input order.
func TestMergePathsOrder(t *testing.T) {
	got := mergePaths("/a:/b", "/c:/d", "/e")
	want := "/a:/b:/c:/d:/e"
	if got != want {
		t.Errorf("mergePaths = %q, want %q", got, want)
	}
}

// TestMergePathsDeduplicates ensures duplicate dirs keep their first position.
func TestMergePathsDeduplicates(t *testing.T) {
	got := mergePaths("/a:/b:/c", "/b:/d", "/a:/e")
	want := "/a:/b:/c:/d:/e"
	if got != want {
		t.Errorf("mergePaths = %q, want %q", got, want)
	}
}

// TestMergePathsSkipsEmpty ensures empty sources and empty entries are dropped.
func TestMergePathsSkipsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		want    string
	}{
		{"empty source", []string{"", "/a:/b"}, "/a:/b"},
		{"all empty", []string{"", "", ""}, ""},
		{"empty entry in middle", []string{"/a::/b"}, "/a:/b"},
		{"trailing colon", []string{"/a:/b:"}, "/a:/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mergePaths(c.sources...); got != c.want {
				t.Errorf("mergePaths(%v) = %q, want %q", c.sources, got, c.want)
			}
		})
	}
}

// TestParseEnvOutputBasic parses plain KEY=VALUE lines.
func TestParseEnvOutputBasic(t *testing.T) {
	in := []byte("FOO=bar\nBAZ=qux\n")
	got := parseEnvOutput(in)
	if got["FOO"] != "bar" || got["BAZ"] != "qux" {
		t.Errorf("parseEnvOutput = %v", got)
	}
}

// TestParseEnvOutputSkipsInvalidLines drops lines without '='.
func TestParseEnvOutputSkipsInvalidLines(t *testing.T) {
	in := []byte("FOO=bar\ngarbage line\nBAZ=qux\n\n")
	got := parseEnvOutput(in)
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(got), got)
	}
}

// TestParseEnvOutputHandlesValuesWithEquals preserves '=' within values.
func TestParseEnvOutputHandlesValuesWithEquals(t *testing.T) {
	in := []byte("FOO=a=b=c\n")
	got := parseEnvOutput(in)
	if got["FOO"] != "a=b=c" {
		t.Errorf("parseEnvOutput[FOO] = %q, want %q", got["FOO"], "a=b=c")
	}
}

// TestHomeCandidateDirsRespectsHOME returns $HOME-based paths.
func TestHomeCandidateDirsRespectsHOME(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got := homeCandidateDirs()
	if len(got) == 0 {
		t.Fatalf("expected at least one dir, got none")
	}
	for _, dir := range got {
		if !strings.HasPrefix(dir, tmp) {
			t.Errorf("dir %q does not start with HOME %q", dir, tmp)
		}
	}
}

// TestHomeCandidateDirsEmptyHOME returns nil when HOME is unset.
func TestHomeCandidateDirsEmptyHOME(t *testing.T) {
	t.Setenv("HOME", "")
	got := homeCandidateDirs()
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// writeExec is a test helper that writes an executable file with mode 0o755.
func writeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// TestLookPathInAbsolutePath returns the path when it points to an executable file.
func TestLookPathInAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tool")
	writeExec(t, exe)
	if got := lookPathIn(exe, nil); got != exe {
		t.Errorf("lookPathIn(%q, nil) = %q, want %q", exe, got, exe)
	}
}

// TestLookPathInAbsolutePathToDir returns "" when the path is a directory.
func TestLookPathInAbsolutePathToDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := lookPathIn(sub, nil); got != "" {
		t.Errorf("lookPathIn(%q, nil) = %q, want \"\"", sub, got)
	}
}

// TestLookPathInAbsolutePathNonExecutable returns "" when the file lacks exec bits.
func TestLookPathInAbsolutePathNonExecutable(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-exec")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := lookPathIn(file, nil); got != "" {
		t.Errorf("lookPathIn(%q, nil) = %q, want \"\"", file, got)
	}
}

// TestLookPathInAbsolutePathMissing returns "" for a nonexistent path.
func TestLookPathInAbsolutePathMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	if got := lookPathIn(missing, nil); got != "" {
		t.Errorf("lookPathIn(%q, nil) = %q, want \"\"", missing, got)
	}
}

// TestLookPathInRelativeWithSlash accepts a path containing '/' via direct stat.
func TestLookPathInRelativeWithSlash(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	exe := filepath.Join(sub, "tool")
	writeExec(t, exe)
	if got := lookPathIn(exe, nil); got != exe {
		t.Errorf("lookPathIn(%q, nil) = %q, want %q", exe, got, exe)
	}
}

// TestLookPathInBareNameHit returns the full path when a bare name exists in paths.
func TestLookPathInBareNameHit(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "mytool")
	writeExec(t, exe)
	if got := lookPathIn("mytool", []string{dir}); got != exe {
		t.Errorf("lookPathIn(\"mytool\", [%q]) = %q, want %q", dir, got, exe)
	}
}

// TestLookPathInBareNameMissesDirs skips matches that are directories.
func TestLookPathInBareNameMissesDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "mytool"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := lookPathIn("mytool", []string{dir}); got != "" {
		t.Errorf("lookPathIn(\"mytool\", [%q]) = %q, want \"\"", dir, got)
	}
}

// TestLookPathInBareNameMissesNonExec skips matches without exec bits.
func TestLookPathInBareNameMissesNonExec(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mytool"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := lookPathIn("mytool", []string{dir}); got != "" {
		t.Errorf("lookPathIn(\"mytool\", [%q]) = %q, want \"\"", dir, got)
	}
}

// TestLookPathInBareNameNoMatch returns "" when no path contains the name.
func TestLookPathInBareNameNoMatch(t *testing.T) {
	dir := t.TempDir()
	if got := lookPathIn("mytool", []string{dir}); got != "" {
		t.Errorf("lookPathIn(\"mytool\", [%q]) = %q, want \"\"", dir, got)
	}
}

// TestLookPathInSkipsEmptyDirs ignores empty entries in the paths slice.
func TestLookPathInSkipsEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "mytool")
	writeExec(t, exe)
	if got := lookPathIn("mytool", []string{"", dir, ""}); got != exe {
		t.Errorf("lookPathIn with empty dirs = %q, want %q", got, exe)
	}
}

// TestLookPathInFirstMatchWins returns the earlier-path match when two dirs have the name.
func TestLookPathInFirstMatchWins(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()
	exe1 := filepath.Join(d1, "mytool")
	exe2 := filepath.Join(d2, "mytool")
	writeExec(t, exe1)
	writeExec(t, exe2)
	if got := lookPathIn("mytool", []string{d1, d2}); got != exe1 {
		t.Errorf("lookPathIn = %q, want %q (first-match)", got, exe1)
	}
}

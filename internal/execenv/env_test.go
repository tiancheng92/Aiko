package execenv

import (
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

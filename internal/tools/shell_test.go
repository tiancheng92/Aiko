package tools

import (
	"testing"
)

func TestIsTrustedCommand(t *testing.T) {
	cases := []struct {
		command string
		trusted []string
		want    bool
	}{
		{"git status", []string{"git"}, true},
		{"gitk", []string{"git"}, false},          // 无空格边界，不匹配
		{"git", []string{"git"}, true},            // 完全匹配
		{"ls -la", []string{"ls", "cat"}, true},
		{"rm -rf /", []string{}, false},           // 空白名单
		{"rm -rf /", nil, false},                  // nil 白名单
		{" git status", []string{"git"}, true},    // 首部空白
		{"cat /etc/passwd", []string{"cat"}, true},
	}
	for _, c := range cases {
		got := isTrustedCommand(c.command, c.trusted)
		if got != c.want {
			t.Errorf("isTrustedCommand(%q, %v) = %v, want %v", c.command, c.trusted, got, c.want)
		}
	}
}

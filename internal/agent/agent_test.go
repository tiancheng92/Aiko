package agent_test

import (
	"context"
	"testing"

	"aiko/internal/agent"
)

// TestChatDirectCollectExists is a compile-time check that ChatDirectCollect
// exists and has the right signature. A real integration test would require
// a live LLM; this just ensures the method is defined.
func TestChatDirectCollectExists(t *testing.T) {
	// Verify the method signature exists on *Agent via interface satisfaction.
	type collecter interface {
		ChatDirectCollect(ctx context.Context, prompt string) (string, error)
	}
	var _ collecter = (*agent.Agent)(nil)
}

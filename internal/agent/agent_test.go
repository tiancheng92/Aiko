package agent_test

import (
	"context"
	"strings"
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

// TestAgentInterfaceCompleteness checks that Agent exposes all expected public methods.
func TestAgentInterfaceCompleteness(t *testing.T) {
	type agentIface interface {
		Chat(ctx context.Context, userInput string) <-chan agent.StreamResult
		ChatDirect(ctx context.Context, prompt string) <-chan agent.StreamResult
		ChatDirectCollect(ctx context.Context, prompt string) (string, error)
		SetSkillHint(skillName string)
	}
	var _ agentIface = (*agent.Agent)(nil)
}

// TestStreamResult_ZeroValue verifies that a zero-value StreamResult is safe to use.
func TestStreamResult_ZeroValue(t *testing.T) {
	var sr agent.StreamResult
	if sr.Done {
		t.Error("zero-value Done should be false")
	}
	if sr.Err != nil {
		t.Error("zero-value Err should be nil")
	}
	if sr.Token != "" {
		t.Error("zero-value Token should be empty")
	}
}

// TestStreamResult_DoneSignal verifies that a Done terminal signal has no error or token.
func TestStreamResult_DoneSignal(t *testing.T) {
	done := agent.StreamResult{Done: true}
	if done.Err != nil {
		t.Error("Done result should have nil Err")
	}
	if done.Token != "" {
		t.Error("Done result should have empty Token")
	}
}

// TestStreamResult_ErrorSignal verifies that an error result is not Done.
func TestStreamResult_ErrorSignal(t *testing.T) {
	errResult := agent.StreamResult{Err: context.Canceled}
	if errResult.Done {
		t.Error("error result should not be Done")
	}
	if errResult.Err == nil {
		t.Error("error result should have non-nil Err")
	}
}

// TestStreamResult_TokenCarriesText verifies a token result contains text.
func TestStreamResult_TokenCarriesText(t *testing.T) {
	tok := agent.StreamResult{Token: "hello"}
	if tok.Token == "" {
		t.Error("token result should have non-empty Token")
	}
	if tok.Done {
		t.Error("token result should not be Done")
	}
	if tok.Err != nil {
		t.Error("token result should have nil Err")
	}
}

// TestEmotionParser_PlainText verifies that plain text without emotion tags
// passes through as-is with no emotion set. The parser buffers short leading
// text to check for a tag prefix; once flushed at stream end the full text is returned.
func TestEmotionParser_PlainText(t *testing.T) {
	ep := agent.NewEmotionParser()
	// Feed a short plain token — may be buffered pending tag detection.
	_, emotion, _ := ep.Feed("hello world")
	if emotion != "" {
		t.Errorf("expected empty emotion, got %q", emotion)
	}
	// Flush must return any remaining buffered content.
	tail := ep.Flush()
	if tail != "hello world" {
		t.Errorf("Flush: expected %q, got %q", "hello world", tail)
	}
}

// TestEmotionParser_ExtractsEmotion verifies that a well-formed emotion tag
// is stripped from the text output and returned as a separate emotion/intensity.
func TestEmotionParser_ExtractsEmotion(t *testing.T) {
	ep := agent.NewEmotionParser()
	text, emotion, intensity := ep.Feed("[情绪:joy/0.8]\n你好！")
	if strings.Contains(text, "情绪") {
		t.Errorf("emotion tag leaked into text: %q", text)
	}
	if emotion != "joy" {
		t.Errorf("expected emotion %q, got %q", "joy", emotion)
	}
	if intensity < 0.7 || intensity > 0.9 {
		t.Errorf("expected intensity ~0.8, got %f", intensity)
	}
}

// TestEmotionParser_FlushReturnsRemainder verifies that Flush returns any
// buffered text that hadn't been emitted yet and does not panic.
func TestEmotionParser_FlushReturnsRemainder(t *testing.T) {
	ep := agent.NewEmotionParser()
	text, _, _ := ep.Feed("[情绪:joy") // partial tag — must be buffered
	if text != "" {
		t.Errorf("partial tag should be buffered, not emitted: got %q", text)
	}
	tail := ep.Flush()
	if tail != "[情绪:joy" {
		t.Errorf("Flush: got %q, want %q", tail, "[情绪:joy")
	}
}

// TestEmotionParser_FlushOnEmptyIsNoOp verifies that Flush on a fresh parser
// returns an empty string without panicking.
func TestEmotionParser_FlushOnEmptyIsNoOp(t *testing.T) {
	ep := agent.NewEmotionParser()
	tail := ep.Flush()
	if tail != "" {
		t.Errorf("Flush on empty parser should return empty string, got %q", tail)
	}
}

// TestEmotionParser_MultipleTokensNoTag verifies that multiple tokens without
// any emotion tag all pass through as text.
func TestEmotionParser_MultipleTokensNoTag(t *testing.T) {
	ep := agent.NewEmotionParser()
	words := []string{"the ", "quick ", "brown ", "fox"}
	var collected strings.Builder
	for _, w := range words {
		text, _, _ := ep.Feed(w)
		collected.WriteString(text)
	}
	collected.WriteString(ep.Flush())
	if collected.String() != "the quick brown fox" {
		t.Errorf("expected full text, got %q", collected.String())
	}
}

package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"aiko/internal/memory"
)

// estimateTokens approximates the token count for a slice of messages.
// Uses len(content)/4 as a conservative estimate safe for both English and CJK.
func estimateTokens(msgs []memory.Message) int {
	total := 0
	for i := range msgs {
		total += len(msgs[i].Content) / 4
	}
	return total
}

// checkAndSummarize is called at the start of buildContext. When unmigrated
// messages exceed MaxContextTokens, all of them are summarised and migrated
// so the upcoming LLM call receives a compact context.
// Errors are non-fatal: logged and silently skipped so the user is never blocked.
func (a *Agent) checkAndSummarize(ctx context.Context) {
	if a.shortMem == nil || a.summaryStore == nil || a.chatModel == nil {
		return
	}
	limit := a.cfg.MaxContextTokens
	if limit <= 0 {
		return
	}

	msgs, err := a.shortMem.OldestUnmigratedN(10000) // effectively "all"
	if err != nil {
		log.Warn().Err(err).Msg("checkAndSummarize: load unmigrated messages failed")
		return
	}
	if estimateTokens(msgs) <= limit {
		return
	}

	if err := a.runSummary(ctx, msgs); err != nil {
		log.Warn().Err(err).Msg("checkAndSummarize: runSummary failed")
	}
}

// runSummary compresses msgs into a rolling LLM summary and migrates them to
// long-term memory. Steps 1 (longMem store) and 2 (LLM summarise) run concurrently.
// MarkMigrated is called only if both succeed.
func (a *Agent) runSummary(ctx context.Context, msgs []memory.Message) error {
	if len(msgs) == 0 {
		return nil
	}

	block := memory.FormatBlock(msgs)
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}

	prevSummary, err := a.summaryStore.Get()
	if err != nil {
		log.Warn().Err(err).Msg("runSummary: get prev summary failed, using empty")
		prevSummary = ""
	}

	var newSummary string

	g, gctx := errgroup.WithContext(ctx)

	// Step 1: persist to long-term vector memory.
	g.Go(func() error {
		if a.longMem == nil {
			return nil
		}
		if err := a.longMem.Store(gctx, block); err != nil {
			return fmt.Errorf("store long-term memory: %w", err)
		}
		return nil
	})

	// Step 2: LLM summarisation.
	g.Go(func() error {
		prompt := buildSummaryPrompt(prevSummary, block)
		resp, err := a.chatModel.Generate(gctx, prompt)
		if err != nil {
			return fmt.Errorf("summary LLM call: %w", err)
		}
		if resp != nil {
			newSummary = resp.Content
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return err
	}

	if newSummary != "" {
		if err := a.summaryStore.Set(newSummary); err != nil {
			return fmt.Errorf("save summary: %w", err)
		}
	}

	return a.shortMem.MarkMigrated(ids)
}

// buildSummaryPrompt constructs the message slice for the summarisation LLM call.
func buildSummaryPrompt(prevSummary, conversationBlock string) []*schema.Message {
	var userContent string
	if prevSummary != "" {
		userContent = "[Previous summary]\n" + prevSummary + "\n[End of previous summary]\n\n"
	}
	userContent += "[New conversation to incorporate]\n" + conversationBlock + "[End of new conversation]\n\nOutput only the updated summary, no preamble."

	return []*schema.Message{
		{
			Role:    schema.System,
			Content: "You are a conversation memory manager. Compress the provided conversation history into a concise summary. Preserve key facts, decisions, and context. Write in third-person narrative. Be brief.",
		},
		{
			Role:    schema.User,
			Content: userContent,
		},
	}
}

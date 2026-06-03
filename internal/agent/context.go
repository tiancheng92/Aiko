package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"golang.org/x/sync/errgroup"

	"aiko/internal/bytesconv"
	"aiko/internal/knowledge"
	"aiko/internal/llm"
)

// userProfileCache holds a recently-read USER.md to avoid redundant disk reads on every turn.
var userProfileCache struct {
	sync.Mutex
	content   string
	expiresAt time.Time
}

// readUserProfile returns the cached USER.md content, refreshing from disk every 30 seconds.
func readUserProfile(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	userProfileCache.Lock()
	defer userProfileCache.Unlock()
	if time.Now().Before(userProfileCache.expiresAt) {
		return userProfileCache.content
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "USER.md"))
	if err != nil && !os.IsNotExist(err) {
		log.Warn().Err(err).Msg("read USER.md failed")
	}
	userProfileCache.content = bytesconv.BytesToString(data)
	userProfileCache.expiresAt = time.Now().Add(30 * time.Second)
	return userProfileCache.content
}

// gatherContextSources fetches context inputs concurrently: user profile,
// long-term memory search results, knowledge base results, and recent
// short-term messages. Errors from individual sources are logged and
// treated as empty (non-fatal) to avoid blocking the chat turn.
func (a *Agent) gatherContextSources(ctx context.Context, userInput string, useKnowledge, useMemory bool) (
	profile string,
	memResults []string,
	knowledgeResults []knowledge.SearchResult,
	recentMsgs []*schema.Message,
	summaryText string,
	err error,
) {
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		profile = readUserProfile(a.dataDir)
		return nil
	})

	g.Go(func() error {
		if a.longMem == nil || !useMemory {
			return nil
		}
		res, err := a.longMem.Search(gctx, userInput, 5)
		if err != nil {
			log.Warn().Err(err).Msg("longMem.Search failed")
			return nil
		}
		memResults = res
		return nil
	})

	g.Go(func() error {
		if a.knowledgeSt == nil || !useKnowledge {
			return nil
		}
		results, err := a.knowledgeSt.Search(gctx, userInput, 3)
		if err != nil {
			log.Warn().Err(err).Msg("knowledgeSt.Search failed")
			return nil
		}
		knowledgeResults = results
		return nil
	})

	g.Go(func() error {
		if a.shortMem == nil {
			return nil
		}
		msgs, err := a.shortMem.RecentMessages(a.cfg.ShortTermLimit)
		if err != nil {
			log.Warn().Err(err).Msg("shortMem.RecentMessages error")
			return nil
		}
		recentMsgs = msgs
		return nil
	})

	g.Go(func() error {
		if a.summaryStore == nil {
			return nil
		}
		s, err := a.summaryStore.Get()
		if err != nil {
			log.Warn().Err(err).Msg("summaryStore.Get failed")
			return nil
		}
		summaryText = s
		return nil
	})

	err = g.Wait()
	return
}

// ctxBufPool recycles strings.Builder instances used to assemble the per-turn
// system context, reusing the underlying buffer across conversation turns.
var ctxBufPool = sync.Pool{New: func() any { return new(strings.Builder) }}

// buildContext fetches user profile, long-term memories, knowledge base results,
// and recent short-term history concurrently, then returns a message list ready for
// runner.Run. Errors from individual sources are logged and skipped — a partial context
// is better than no response.
func (a *Agent) buildContext(ctx context.Context, userInput string, useKnowledge, useMemory bool) ([]adk.Message, error) {
	a.checkAndSummarize(ctx)
	profile, memResults, knowledgeResults, recentMsgs, summaryText, err := a.gatherContextSources(ctx, userInput, useKnowledge, useMemory)
	if err != nil {
		return nil, err
	}

	var msgs []adk.Message

	// --- Layer 1: static context (rarely changes → high cache hit rate) ---
	// User profile is stable across turns; keeping it separate from the
	// dynamic memories/knowledge layer preserves the cache prefix.
	// Current time and location intentionally omitted — get_current_time and
	// get_location tools provide them on demand.
	ctxBuf := ctxBufPool.Get().(*strings.Builder)
	ctxBuf.Reset()
	defer func() {
		ctxBuf.Reset()
		ctxBufPool.Put(ctxBuf)
	}()
	if profile != "" {
		ctxBuf.WriteString("User Profile:\n")
		ctxBuf.WriteString(profile)
	}
	if ctxBuf.Len() > 0 {
		msgs = append(msgs,
			&schema.Message{Role: schema.User, Content: ctxBuf.String()},
			&schema.Message{Role: schema.Assistant, Content: "Understood."},
		)
	}

	// --- Layer 1b: rolling summary (compressed earlier turns) ---
	if summaryText != "" {
		summary := "[Conversation summary — compressed history from earlier turns:]\n" +
			summaryText + "\n[End of summary]"
		msgs = append(msgs,
			&schema.Message{Role: schema.User, Content: summary},
			&schema.Message{Role: schema.Assistant, Content: "Understood."},
		)
	}

	// Mark the last static-prefix message for prompt caching. The system
	// prompt (set at agent construction) + USER.md + summary are sent on
	// every turn and rarely change — caching them saves 50-90% token cost
	// on repeat prefix sends via OpenRouter / Anthropic-compatible APIs.
	if len(msgs) > 0 {
		llm.EnablePromptCaching(msgs[len(msgs)-1])
	}

	// --- History messages ---
	// Placed before dynamic context so the shared history prefix between
	// consecutive turns stays cacheable. If dynamic context came first, it
	// would invalidate the cache for all following history messages.
	for _, m := range recentMsgs {
		msgs = append(msgs, m)
	}

	// --- Layer 2: dynamic context (query-dependent → may miss cache) ---
	// Positioned just before the current user message (appended by caller)
	// so retrieval is still query-aware but cache impact is minimized.
	ctxBuf.Reset()
	if len(memResults) > 0 {
		ctxBuf.WriteString("[Long-term memories — retrieved by semantic similarity; may be outdated. Use as background context, not as absolute truth.]\n")
		for _, r := range memResults {
			ctxBuf.WriteString(r)
			ctxBuf.WriteByte('\n')
		}
		ctxBuf.WriteString("[End of long-term memories]\n")
	}
	if len(knowledgeResults) > 0 {
		ctxBuf.WriteString("\n[Knowledge base results — retrieved by semantic similarity from user-imported documents:]\n")
		for _, r := range knowledgeResults {
			ctxBuf.WriteString("- [")
			ctxBuf.WriteString(r.Source)
			ctxBuf.WriteString("] ")
			ctxBuf.WriteString(r.Content)
			ctxBuf.WriteByte('\n')
		}
		ctxBuf.WriteString("[End of knowledge base results]\n")
	}
	if ctxBuf.Len() > 0 {
		msgs = append(msgs, &schema.Message{Role: schema.User, Content: ctxBuf.String()})
	}

	return msgs, nil
}

// collapseBlankLines normalises blank lines in s: removes leading blank lines
// and collapses runs of 3+ consecutive newlines to exactly two newlines (one
// blank line), so assistant replies stored in the DB stay compact.
func collapseBlankLines(s string) string {
	// Replace 3+ consecutive newlines with exactly 2.
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimLeft(s, "\n")
}

// persistAndMigrate saves user and assistant messages to SQLite, then checks
// whether the total message count exceeds ShortTermLimit. If so, the oldest
// excess messages are migrated to long-term vector memory.
// skipSave skips the SQLite inserts when flushFn already persisted the turn
// (e.g. check_and_update pre-restart flush), preventing duplicate history entries.
func (a *Agent) persistAndMigrate(ctx context.Context, userInput string, userImages []string, userFiles []string, assistantReply string, thinkingContent string, assistantImages []string, toolCallCount int, skipSave bool) {
	if a.shortMem == nil {
		return
	}

	// Increment the turn counter on every completed conversation turn so the
	// self-growth nudge fires at the correct interval regardless of whether
	// short-term memory overflow has occurred.
	a.turnCount.Add(1)

	if !skipSave {
		if _, err := a.shortMem.AddWithImagesAndFiles("user", userInput, userImages, userFiles); err != nil {
			log.Warn().Err(err).Msg("short memory: add user message")
			return
		}
		// Strip the leading emotion tag before persisting so it never appears in
		// chat history or long-term memory.
		if _, _, stripped, ok := parseBehaviorTag(assistantReply); ok {
			assistantReply = stripped
		}
		assistantReply = collapseBlankLines(assistantReply)
		if _, err := a.shortMem.AddFull("assistant", assistantReply, thinkingContent, assistantImages, nil); err != nil {
			log.Warn().Err(err).Msg("short memory: add assistant message")
			return
		}
	}

	// Trigger async self-growth reflection if warranted.
	if trigger, hints := a.shouldReflect(userInput, toolCallCount); trigger {
		go a.reflect(ctx, userInput, assistantReply, hints)
	}

	limit := a.cfg.ShortTermLimit
	if limit <= 0 {
		limit = 30
	}

	count, err := a.shortMem.CountUnmigrated()
	if err != nil {
		log.Error().Err(err).Msg("count unmigrated messages failed")
		return
	}

	excess := count - limit
	if excess <= 0 {
		return
	}

	oldest, err := a.shortMem.OldestUnmigratedN(excess)
	if err != nil {
		log.Error().Err(err).Msg("get oldest unmigrated messages failed")
		return
	}
	if len(oldest) == 0 {
		return
	}

	// Summarise and migrate the excess messages. runSummary stores to long-term
	// memory and updates the rolling summary concurrently, then marks migrated.
	if err := a.runSummary(ctx, oldest); err != nil {
		log.Error().Err(err).Msg("persistAndMigrate: runSummary failed")
	}
}

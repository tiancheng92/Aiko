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
	"aiko/internal/memory"
	"aiko/internal/tools/base"
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

// gatherContextSources fetches the four context inputs concurrently:
// user profile, long-term memory search results, recent short-term messages,
// and current location. Errors from individual sources are logged and treated
// as empty (non-fatal) to avoid blocking the chat turn.
func (a *Agent) gatherContextSources(ctx context.Context, userInput string, useKnowledge, useMemory bool) (
	profile string,
	memResult memory.MemorySearchResult,
	recentMsgs []*schema.Message,
	location string,
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
		res, err := a.longMem.SearchSplit(gctx, userInput, 5)
		if err != nil {
			log.Warn().Err(err).Msg("longMem.SearchSplit failed")
			return nil
		}
		memResult = res
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
		location = cachedLocation()
		return nil
	})

	err = g.Wait()
	return
}

// ctxBufPool recycles strings.Builder instances used to assemble the per-turn
// system context, reusing the underlying buffer across conversation turns.
var ctxBufPool = sync.Pool{New: func() any { return new(strings.Builder) }}

// buildContext fetches user profile, long-term memories (summaries and raws separately),
// and recent short-term history concurrently, then returns a message list ready for
// runner.Run. Errors from individual sources are logged and skipped — a partial context
// is better than no response.
func (a *Agent) buildContext(ctx context.Context, userInput string, useKnowledge, useMemory bool) ([]adk.Message, error) {
	// Propagate useKnowledge to tools (e.g. search_knowledge) via context.
	ctx = context.WithValue(ctx, base.UseKnowledgeKey{}, useKnowledge)
	profile, memResult, recentMsgs, location, err := a.gatherContextSources(ctx, userInput, useKnowledge, useMemory)
	if err != nil {
		return nil, err
	}

	var msgs []adk.Message

	// Build context pair (user + assistant "Understood.") — always includes current time.
	ctxBuf := ctxBufPool.Get().(*strings.Builder)
	ctxBuf.Reset()
	defer func() {
		ctxBuf.Reset()
		ctxBufPool.Put(ctxBuf)
	}()
	ctxBuf.WriteString("Current time: ")
	ctxBuf.WriteString(time.Now().Format("2006-01-02 15:04:05 CST"))
	ctxBuf.WriteByte('\n')
	if location != "" {
		ctxBuf.WriteString("Location: ")
		ctxBuf.WriteString(location)
		ctxBuf.WriteByte('\n')
	}
	if profile != "" {
		ctxBuf.WriteString("\nUser Profile:\n")
		ctxBuf.WriteString(profile)
	}
	if len(memResult.Summaries) > 0 || len(memResult.Raws) > 0 {
		ctxBuf.WriteString("\n[Long-term memories — retrieved by semantic similarity; may be outdated. Use as background context, not as absolute truth.]\n")
		if len(memResult.Summaries) > 0 {
			ctxBuf.WriteString("Relevant memory summaries:\n")
			for _, s := range memResult.Summaries {
				ctxBuf.WriteString("- ")
				ctxBuf.WriteString(s)
				ctxBuf.WriteByte('\n')
			}
		}
		if len(memResult.Raws) > 0 {
			ctxBuf.WriteString("Relevant memory details:\n")
			for _, r := range memResult.Raws {
				ctxBuf.WriteString(r)
				ctxBuf.WriteByte('\n')
			}
		}
		ctxBuf.WriteString("[End of long-term memories]\n")
	}
	if ctxBuf.Len() > 0 {
		msgs = append(msgs,
			&schema.Message{Role: schema.User, Content: ctxBuf.String()},
			&schema.Message{Role: schema.Assistant, Content: "Understood."},
		)
	}

	for _, m := range recentMsgs {
		msgs = append(msgs, m)
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
		if _, _, stripped, ok := parseEmotionTag(assistantReply); ok {
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

	count, err := a.shortMem.Count()
	if err != nil {
		log.Error().Err(err).Msg("count messages failed")
		return
	}

	excess := count - limit
	if excess <= 0 {
		return
	}

	oldest, err := a.shortMem.OldestN(excess)
	if err != nil {
		log.Error().Err(err).Msg("get oldest messages failed")
		return
	}
	if len(oldest) == 0 {
		return
	}

	// Store the block in long-term memory (only if available).
	if a.longMem != nil {
		block := memory.FormatBlock(oldest)
		if err := a.longMem.Store(ctx, block); err != nil {
			log.Error().Err(err).Msg("store long-term memory failed")
			// Don't return — still delete from short-term.
		}
	}

	// Delete the migrated messages from short-term store.
	ids := make([]int64, len(oldest))
	for i, m := range oldest {
		ids[i] = m.ID
	}
	if err := a.shortMem.DeleteByIDs(ids); err != nil {
		log.Error().Err(err).Msg("delete migrated messages failed")
	}
}

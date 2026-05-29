# Memory Summary System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a LLM-powered rolling summary layer that compresses short-term conversation history when token limits are exceeded, keeping the agent context-aware across long conversations.

**Architecture:** A new `SummaryStore` (single-row SQLite table) stores the rolling summary. `buildContext` calls `checkAndSummarize` before assembling the prompt — if unmigrated messages exceed `MaxContextTokens` all are summarised+migrated. The summary is fetched concurrently with other context sources and injected between User Profile and short-term history.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), eino (`schema.Message`, `model.ToolCallingChatModel`), Vue 3 + i18n (zh-CN / en / ja / ko)

---

### Task 1: DB schema — `summary` table + `max_context_tokens` setting

**Files:**
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: Add `summary` table to `migrate()`**

In `internal/db/sqlite.go`, inside the big `db.Exec(...)` call in `migrate()`, append after the last `CREATE TABLE IF NOT EXISTS` block:

```sql
CREATE TABLE IF NOT EXISTS summary (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    content    TEXT    NOT NULL DEFAULT '',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 2: Add v12 patch for `max_context_tokens`**

In the `patches` slice in `migrate()`, append at the end:

```go
// v12: per-conversation context token limit for early summarisation.
`ALTER TABLE settings ADD COLUMN max_context_tokens INTEGER NOT NULL DEFAULT 10000`,
```

- [ ] **Step 3: Verify Go compilation**

```bash
go build ./internal/db/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/db/sqlite.go
git commit -m "feat(db): add summary table and max_context_tokens setting"
```

---

### Task 2: `SummaryStore` — data layer

**Files:**
- Create: `internal/memory/summary.go`
- Create: `internal/memory/summary_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/memory/summary_test.go`:

```go
package memory_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"aiko/internal/memory"
)

func newTestSummaryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE summary (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			content    TEXT    NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSummaryStore_GetEmpty(t *testing.T) {
	s := memory.NewSummaryStore(newTestSummaryDB(t))
	got, err := s.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestSummaryStore_SetAndGet(t *testing.T) {
	s := memory.NewSummaryStore(newTestSummaryDB(t))
	if err := s.Set("hello summary"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "hello summary" {
		t.Errorf("expected %q, got %q", "hello summary", got)
	}
}

func TestSummaryStore_SetOverwrites(t *testing.T) {
	s := memory.NewSummaryStore(newTestSummaryDB(t))
	_ = s.Set("first")
	_ = s.Set("second")
	got, err := s.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("expected %q, got %q", "second", got)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/memory/... -run TestSummaryStore -v
```
Expected: compile error — `memory.NewSummaryStore` undefined.

- [ ] **Step 3: Implement `SummaryStore`**

Create `internal/memory/summary.go`:

```go
package memory

import (
	"database/sql"
	"fmt"
)

// SummaryStore persists the single rolling conversation summary to SQLite.
type SummaryStore struct{ db *sql.DB }

// NewSummaryStore returns a SummaryStore backed by db.
func NewSummaryStore(db *sql.DB) *SummaryStore { return &SummaryStore{db: db} }

// Get returns the current summary text, or "" if no summary has been written yet.
func (s *SummaryStore) Get() (string, error) {
	var content string
	err := s.db.QueryRow(`SELECT content FROM summary WHERE id = 1`).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("summary get: %w", err)
	}
	return content, nil
}

// Set overwrites the summary with text. Uses UPSERT to maintain the single row.
func (s *SummaryStore) Set(text string) error {
	_, err := s.db.Exec(
		`INSERT INTO summary(id, content, updated_at) VALUES(1, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		text,
	)
	if err != nil {
		return fmt.Errorf("summary set: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/memory/... -run TestSummaryStore -v
```
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/summary.go internal/memory/summary_test.go
git commit -m "feat(memory): add SummaryStore for rolling conversation summary"
```

---

### Task 3: Config — `MaxContextTokens` field

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add field to `Config` struct**

In `internal/config/config.go`, add after `ShortTermLimit int`:

```go
// MaxContextTokens is the estimated token threshold that triggers early summarisation
// before sending context to the LLM. 0 disables token-based triggering.
// Default: 10000.
MaxContextTokens int
```

- [ ] **Step 2: Load the field in `Load()`**

After `cfg.ShortTermLimit = parseInt(m["short_term_limit"], 30)`, add:

```go
cfg.MaxContextTokens = parseInt(m["max_context_tokens"], 10000)
```

- [ ] **Step 3: Save the field in `Save()`**

In the `pairs` map in `Save()`, add:

```go
"max_context_tokens": strconv.Itoa(cfg.MaxContextTokens),
```

- [ ] **Step 4: Verify Go compilation**

```bash
go build ./internal/config/...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add MaxContextTokens setting"
```

---

### Task 4: Agent — `runSummary` and `checkAndSummarize`

**Files:**
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/summary.go`

- [ ] **Step 1: Store `chatModel` and `summaryStore` on `Agent`**

In `internal/agent/agent.go`, add two fields to the `Agent` struct (after `emitEvent`):

```go
summaryStore *memory.SummaryStore         // nil means summary disabled
chatModel    model.ToolCallingChatModel   // retained for summarisation calls
```

- [ ] **Step 2: Extend `New()` signature and body**

In `internal/agent/agent.go`, add `summaryStore *memory.SummaryStore` as a new parameter to `New()` between `longMem` and `knowledgeSt`:

```go
func New(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	shortMem *memory.ShortStore,
	longMem *memory.LongStore,
	summaryStore *memory.SummaryStore,
	knowledgeSt *knowledge.Store,
	tools []tool.BaseTool,
	cfg *config.Config,
	mw middleware.Middleware,
	skillMW adk.ChatModelAgentMiddleware,
	dataDir string,
	pendingConfirms *sync.Map,
	emitEvent func(event string, data ...any),
) (*Agent, error) {
```

In the `return &Agent{...}` block inside `New()`, add:

```go
summaryStore: summaryStore,
chatModel:    chatModel,
```

- [ ] **Step 3: Create `internal/agent/summary.go`**

```go
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
```

- [ ] **Step 4: Verify Go compilation**

```bash
go build ./internal/agent/...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/summary.go
git commit -m "feat(agent): add runSummary and checkAndSummarize with token-limit trigger"
```

---

### Task 5: `buildContext` — inject summary + call `checkAndSummarize`

**Files:**
- Modify: `internal/agent/context.go`

- [ ] **Step 1: Add `summaryText` to `gatherContextSources`**

In `gatherContextSources`, change the return signature to include `summaryText string`:

```go
func (a *Agent) gatherContextSources(ctx context.Context, userInput string, useKnowledge, useMemory bool) (
	profile string,
	memResults []string,
	knowledgeResults []knowledge.SearchResult,
	recentMsgs []*schema.Message,
	summaryText string,
	err error,
) {
```

Add a new concurrent goroutine in the `errgroup` block (after the `shortMem` goroutine):

```go
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
```

- [ ] **Step 2: Update `buildContext` to call `checkAndSummarize` first**

At the very beginning of `buildContext`, before calling `gatherContextSources`, add:

```go
a.checkAndSummarize(ctx)
```

- [ ] **Step 3: Update `buildContext` to unpack `summaryText` and insert summary block**

Update the `gatherContextSources` call to capture `summaryText`:

```go
profile, memResults, knowledgeResults, recentMsgs, summaryText, err := a.gatherContextSources(ctx, userInput, useKnowledge, useMemory)
```

After the User Profile `msgs = append(msgs, ...)` block and before the history messages loop, insert:

```go
// --- Layer 1b: rolling summary (compressed earlier turns) ---
if summaryText != "" {
	summary := "[Conversation summary — compressed history from earlier turns:]\n" +
		summaryText + "\n[End of summary]"
	msgs = append(msgs,
		&schema.Message{Role: schema.User, Content: summary},
		&schema.Message{Role: schema.Assistant, Content: "Got it."},
	)
}
```

- [ ] **Step 4: Verify Go compilation**

```bash
go build ./internal/agent/...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/context.go
git commit -m "feat(agent): inject rolling summary into LLM context"
```

---

### Task 6: `persistAndMigrate` — trigger summary on turn-limit overflow

**Files:**
- Modify: `internal/agent/context.go`

- [ ] **Step 1: Replace raw `longMem.Store` + `MarkMigrated` block with `runSummary`**

In `persistAndMigrate` (in `internal/agent/context.go`), find the block starting at `if a.longMem != nil {` through `a.shortMem.MarkMigrated(ids)`. Replace it entirely with:

```go
// Summarise and migrate the excess messages. runSummary stores to long-term
// memory and updates the rolling summary concurrently, then marks migrated.
if err := a.runSummary(ctx, oldest); err != nil {
	log.Error().Err(err).Msg("persistAndMigrate: runSummary failed")
}
```

(The `ids` local variable that was built below can be removed since `runSummary` builds its own.)

- [ ] **Step 2: Verify Go compilation**

```bash
go build ./internal/agent/...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/context.go
git commit -m "feat(agent): unify turn-limit migration with runSummary"
```

---

### Task 7: Wire `summaryStore` and `chatModel` in `app.go`

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Construct `summaryStore` in `buildNewAgent`**

In `app.go`, find the `buildNewAgent` function. After the existing memory store setup, add:

```go
summaryStore := memory.NewSummaryStore(a.sqlDB)
```

- [ ] **Step 2: Pass `summaryStore` and `chatModel` to `agent.New()`**

Update the `agent.New(...)` call to include the new parameters (between `longMem` and `knowledgeSt`):

```go
return agent.New(ctx, chatModel, a.shortMem, longMem, summaryStore, knowledgeSt, allTools, a.cfg, mw, skillMW, dataDir,
	&a.pendingConfirms,
	func(event string, data ...any) {
		wailsruntime.EventsEmit(a.ctx, event, data...)
	},
)
```

- [ ] **Step 3: Verify full project builds**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add app.go
git commit -m "feat(app): wire summaryStore and chatModel into agent"
```

---

### Task 8: Frontend — `MaxContextTokens` setting input

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`
- Modify: `frontend/src/locales/zh-CN.json`
- Modify: `frontend/src/locales/en.json`
- Modify: `frontend/src/locales/ja.json`
- Modify: `frontend/src/locales/ko.json`

- [ ] **Step 1: Add `MaxContextTokens` to the `cfg` ref default**

In `SettingsWindow.vue`, in the `cfg` ref initialisation (around line 77), add:

```js
MaxContextTokens: 10000,
```

- [ ] **Step 2: Add the settings row in the template**

In `SettingsWindow.vue`, immediately after the closing `</div>` of the `shortTermLimit` settings-row (after line 1504), insert:

```html
<div class="settings-row">
  <div class="row-body">
    <div class="row-title">{{ $t('settings.ai.maxContextTokens') }}</div>
    <div class="row-desc">{{ $t('settings.ai.maxContextTokensDesc') }}</div>
  </div>
  <input type="number" v-model.number="cfg.MaxContextTokens" min="0" step="1000" style="width:72px;text-align:center" />
</div>
```

- [ ] **Step 3: Add i18n keys to `zh-CN.json`**

After `"shortTermLimitDesc"` line (line 179), add:

```json
"maxContextTokens": "上下文 Token 上限",
"maxContextTokensDesc": "超过此 token 数时提前压缩对话历史（0 = 禁用）",
```

- [ ] **Step 4: Add i18n keys to `en.json`**

After `"shortTermLimitDesc"` line, add:

```json
"maxContextTokens": "Context Token Limit",
"maxContextTokensDesc": "Compress conversation history when context exceeds this token count (0 = disabled)",
```

- [ ] **Step 5: Add i18n keys to `ja.json`**

After `"shortTermLimitDesc"` line, add:

```json
"maxContextTokens": "コンテキストトークン上限",
"maxContextTokensDesc": "コンテキストがこのトークン数を超えた場合、会話履歴を圧縮します（0 = 無効）",
```

- [ ] **Step 6: Add i18n keys to `ko.json`**

After `"shortTermLimitDesc"` line, add:

```json
"maxContextTokens": "컨텍스트 토큰 한도",
"maxContextTokensDesc": "컨텍스트가 이 토큰 수를 초과하면 대화 기록을 압축합니다 (0 = 비활성화)",
```

- [ ] **Step 7: Verify the frontend builds**

```bash
cd frontend && yarn build
```
Expected: build succeeds, no TypeScript/lint errors.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue \
        frontend/src/locales/zh-CN.json \
        frontend/src/locales/en.json \
        frontend/src/locales/ja.json \
        frontend/src/locales/ko.json
git commit -m "feat(frontend): add MaxContextTokens setting input"
```

---

### Task 9: End-to-end smoke test

**Files:** (no file changes)

- [ ] **Step 1: Build and run the app**

```bash
make run
```
Expected: app starts, no crash on startup.

- [ ] **Step 2: Verify setting saves and loads**

Open Settings → 对话 → 记忆与成长. Confirm "上下文 Token 上限" input appears below "短期记忆轮数" with default value 10000. Change it to 500, close Settings, re-open — confirm 500 is still shown.

- [ ] **Step 3: Verify summary triggers**

Set "上下文 Token 上限" to a very small value (e.g. 100) and send a few messages until the total content exceeds ~400 characters. On the next send, `checkAndSummarize` should fire. Confirm in the app log:
- No crash or error dialog
- The next response still makes contextual sense (summary was injected)

- [ ] **Step 4: Run all Go tests**

```bash
go test ./...
```
Expected: all tests pass.

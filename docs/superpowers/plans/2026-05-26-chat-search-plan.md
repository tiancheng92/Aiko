# Chat History Full-Text Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add full-text search to chat history with server-side FTS5 indexing, frontend three-state machine (normal/search/jump), and jump-to-message navigation.

**Architecture:** SQLite FTS5 content-sync virtual table indexes the messages.content column. Two new Go methods (Search, GetNewestToID) exposed as Wails bindings. Frontend adds a search button in ChatBubble title bar that communicates with ChatPanel via ref method calls; ChatPanel manages a three-state machine with snapshot save/restore and IntersectionObserver toggling.

**Tech Stack:** Go 1.26, modernc.org/sqlite (FTS5), Vue 3 Composition API, Wails v2 bindings

---

### Task 1: Add FTS5 virtual table migration

**Files:**
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: Add FTS5 migration SQL to migrate()**

In `internal/db/sqlite.go`, add a new migration patch after the existing patches (after line 149):

```go
// v12: FTS5 full-text search index on messages.content (content-sync mode).
if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content, content=messages, content_rowid=id)`); err != nil {
    if !isDuplicateColumnErr(err) {
        return fmt.Errorf("patch fts5: %w", err)
    }
}
```

Note: `isDuplicateColumnErr` won't match FTS5 "already exists" errors (different error message). Since `IF NOT EXISTS` handles this for virtual tables, `db.Exec` returns nil when the table already exists — no error to suppress. The `isDuplicateColumnErr` guard is harmless defensive code.

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles cleanly

- [ ] **Step 3: Commit**

```bash
git add internal/db/sqlite.go
git commit -m "feat(db): add FTS5 virtual table for messages full-text search"
```

---

### Task 2: Add Search and GetNewestToID to ShortStore

**Files:**
- Modify: `internal/memory/short.go`
- Modify: `internal/memory/short_test.go`

- [ ] **Step 1: Write tests in short_test.go**

Add after existing tests:

```go
func TestSearch_FindsMatches(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	s.Add("user", "hello world")
	s.Add("assistant", "hi there")
	s.Add("user", "world tour")

	results, err := s.Search("world")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// newest first
	if results[0].Content != "world tour" {
		t.Errorf("msg[0]: want 'world tour', got %q", results[0].Content)
	}
	if results[1].Content != "hello world" {
		t.Errorf("msg[1]: want 'hello world', got %q", results[1].Content)
	}
}

func TestSearch_NoResults(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	s.Add("user", "hello")

	results, err := s.Search("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	s.Add("user", "hello")

	results, err := s.Search("")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestGetNewestToID_IncludesTarget(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	var ids []int64
	for i := range 25 {
		id, err := s.Add("user", fmt.Sprintf("message %d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// Target is message 5 (the 6th message, early in history)
	msgs, err := s.GetNewestToID(ids[5], 10)
	if err != nil {
		t.Fatal(err)
	}
	// Should include target and everything newer
	if len(msgs) < 20 {
		t.Errorf("expected at least 20 messages, got %d", len(msgs))
	}
	found := false
	for _, m := range msgs {
		if m.ID == ids[5] {
			found = true
			break
		}
	}
	if !found {
		t.Error("target ID not found in results")
	}
}

func TestGetNewestToID_TargetIsRecent(t *testing.T) {
	s := newTestShortStoreWithFTS(t)
	var ids []int64
	for i := range 5 {
		id, err := s.Add("user", fmt.Sprintf("msg %d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	// Target is the newest message — should return just one page
	msgs, err := s.GetNewestToID(ids[4], 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}

func newTestShortStoreWithFTS(t *testing.T) *memory.ShortStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		thinking_content TEXT NOT NULL DEFAULT '',
		images TEXT NOT NULL DEFAULT '',
		files TEXT NOT NULL DEFAULT '',
		migrated_to_long INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE VIRTUAL TABLE messages_fts USING fts5(content, content=messages, content_rowid=id)`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return memory.NewShortStore(db)
}
```

Add `"fmt"` to imports in short_test.go.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/ -v -run "TestSearch|TestGetNewestToID"`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement Search() in short.go**

Add after `CountUnmigrated` (after line 167):

```go
// Search returns all messages whose content matches the FTS5 query, newest first.
// Returns empty slice (not error) for empty query.
func (s *ShortStore) Search(query string) ([]Message, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT m.id, m.role, m.content, m.thinking_content, m.images, m.files, m.migrated_to_long, m.created_at
		FROM messages m
		JOIN messages_fts fts ON m.id = fts.rowid
		WHERE messages_fts MATCH ?
		ORDER BY m.id DESC`, query)
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}
```

- [ ] **Step 4: Implement GetNewestToID() in short.go**

Add after Search:

```go
// GetNewestToID loads messages from the newest backwards, paginating by pageSize,
// until targetID is included. Returns all loaded messages in chronological order.
func (s *ShortStore) GetNewestToID(targetID int64, pageSize int) ([]Message, error) {
	var all []Message

	// First fetch: get most recent page.
	page, err := s.Recent(pageSize)
	if err != nil {
		return nil, fmt.Errorf("get newest to id: first page: %w", err)
	}
	if len(page) == 0 {
		return nil, nil
	}

	for {
		// Append in chronological order (page is already chronological from Recent/BeforeID).
		all = append(page, all...)

		// Check if target is in this page.
		found := false
		for _, m := range page {
			if m.ID == targetID {
				found = true
				break
			}
		}
		if found {
			break
		}

		// Fetch next older page.
		oldestID := page[0].ID
		page, err = s.BeforeID(oldestID, pageSize)
		if err != nil {
			return nil, fmt.Errorf("get newest to id: page before %d: %w", oldestID, err)
		}
		if len(page) == 0 {
			break // reached end, target not found (shouldn't happen with valid targetID)
		}
	}

	return all, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/memory/ -v -run "TestSearch|TestGetNewestToID"`
Expected: PASS

- [ ] **Step 6: Run all existing memory tests**

Run: `go test ./internal/memory/ -v`
Expected: all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/memory/short.go internal/memory/short_test.go
git commit -m "feat(memory): add Search and GetNewestToID for FTS5 chat search"
```

---

### Task 3: Add Wails bindings in app_chat.go

**Files:**
- Modify: `app_chat.go`

- [ ] **Step 1: Add SearchMessages binding**

Add after `GetMessagesBeforeID` (after line 331):

```go
// SearchMessages returns all messages whose content matches the FTS5 query.
// Returns empty slice for empty query.
func (a *App) SearchMessages(query string) ([]memory.Message, error) {
	return a.shortMem.Search(query)
}
```

- [ ] **Step 2: Add GetMessagesFromNewestToID binding**

Add after SearchMessages:

```go
// GetMessagesFromNewestToID loads messages from newest backwards until the page
// containing targetID is reached. Used for jump-to-message after search.
func (a *App) GetMessagesFromNewestToID(targetID int64) ([]memory.Message, error) {
	return a.shortMem.GetNewestToID(targetID, 10)
}
```

- [ ] **Step 3: Regenerate Wails bindings**

Run: `wails generate module`
Expected: generates TypeScript bindings for the two new methods in `frontend/src/wailsjs/go/main/App.js` and `.d.ts`

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: compiles cleanly

- [ ] **Step 5: Commit**

```bash
git add app_chat.go frontend/src/wailsjs/
git commit -m "feat(chat): add SearchMessages and GetMessagesFromNewestToID Wails bindings"
```

---

### Task 4: Add search button to ChatBubble title bar

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue`

- [ ] **Step 1: Add search icon button in template**

Insert between the fullscreen button and close button in the title bar (between lines 466 and 467):

```html
<button
  class="icon-btn search-btn"
  :title="$t('chatBubble.search')"
  :aria-label="$t('chatBubble.search')"
  @click="chatPanelRef?.enterSearch()"
>
  <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
    <circle cx="11" cy="11" r="8"/>
    <line x1="21" y1="21" x2="16.65" y2="16.65"/>
  </svg>
</button>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/ChatBubble.vue
git commit -m "feat(frontend): add search button to ChatBubble title bar"
```

---

### Task 5: Implement search state machine in ChatPanel

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Add imports for new Wails bindings**

Update the import line (line 3) to include `SearchMessages` and `GetMessagesFromNewestToID`:

```js
import { SendMessage, SendMessageWithImages, SendMessageWithFiles, GetMessages, GetMessagesBeforeID, SearchMessages, GetMessagesFromNewestToID, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend, StopGeneration, SpeakText, StopTTS, GetConfig, SaveConfig, RegenerateLastReply, GetSoundsEnabled, ReadClipboard } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: Add search state variables**

Add after `allLoaded` (after line 46):

```js
/** searchQuery is the current search input text. */
const searchQuery = ref('')
/** isSearching is true when the search bar is visible and active. */
const isSearching = ref(false)
/** searchResults holds messages returned from FTS5 search, or null when not searching. */
const searchResults = ref(null)
/** jumpTargetId is the message ID to scroll to after loading context. */
const jumpTargetId = ref(null)
/** searchSnapshot saves normal state before entering search for restore on exit. */
let searchSnapshot = null
/** searchDebounceTimer holds the active debounce timer for search input. */
let searchDebounceTimer = null
```

- [ ] **Step 3: Add enterSearch, exitSearch, doSearch, jumpToMessage functions**

Add after `loadOlderMessages` function (after line 566):

```js
/** enterSearch saves current message state and activates search mode. */
function enterSearch() {
  if (isStreaming.value) return
  searchSnapshot = {
    messages: [...messages.value],
    oldestLoadedID,
    allLoaded: allLoaded.value,
  }
  isSearching.value = true
  searchQuery.value = ''
  searchResults.value = null
  // Disable infinite-scroll observer while searching.
  sentinelObserver?.disconnect()
}

/** exitSearch restores the pre-search message list and scrolls to bottom. */
function exitSearch() {
  if (!searchSnapshot) return
  isSearching.value = false
  searchQuery.value = ''
  searchResults.value = null
  jumpTargetId.value = null
  clearTimeout(searchDebounceTimer)
  messages.value = searchSnapshot.messages
  oldestLoadedID = searchSnapshot.oldestLoadedID
  allLoaded.value = searchSnapshot.allLoaded
  searchSnapshot = null
  // Re-enable observer and scroll to bottom.
  nextTick(() => {
    const sentinel = document.getElementById('msg-load-sentinel')
    if (sentinel && sentinelObserver) sentinelObserver.observe(sentinel)
    scrollToBottom()
  })
}

/** doSearch calls the backend FTS5 search and updates results. */
async function doSearch(query) {
  const q = query.trim()
  if (!q) {
    searchResults.value = null
    return
  }
  try {
    const results = await SearchMessages(q)
    searchResults.value = (results || []).map(mapMsg)
  } catch (e) {
    console.warn('search failed:', e)
    searchResults.value = []
  }
}

/** onSearchInput handles debounced search input. */
function onSearchInput(e) {
  searchQuery.value = e.target.value
  clearTimeout(searchDebounceTimer)
  searchDebounceTimer = setTimeout(() => doSearch(searchQuery.value), 300)
}

/** onSearchKeydown handles Escape key in search input. */
function onSearchKeydown(e) {
  if (e.key === 'Escape') exitSearch()
}

/** jumpToMessage loads context from newest down to the page containing targetID, then scrolls to it. */
async function jumpToMessage(targetID) {
  if (isStreaming.value) return
  exitSearch()
  loadingHistory.value = true
  try {
    const msgs = await GetMessagesFromNewestToID(targetID)
    if (!msgs || msgs.length === 0) return
    suppressAnimation.value = true
    messages.value = msgs.map(mapMsg)
    if (msgs.length > 0) oldestLoadedID = msgs[0].ID
    allLoaded.value = false

    await nextTick()
    await new Promise(resolve => requestAnimationFrame(resolve))
    suppressAnimation.value = false

    // Re-enable observer.
    const sentinel = document.getElementById('msg-load-sentinel')
    if (sentinel && sentinelObserver) sentinelObserver.observe(sentinel)

    // Scroll to target message and flash highlight.
    const el = document.querySelector(`[data-msg-key="id:${targetID}"]`)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.classList.add('jump-flash')
      setTimeout(() => el.classList.remove('jump-flash'), 2000)
    }
  } catch (e) {
    console.warn('jump to message failed:', e)
  } finally {
    loadingHistory.value = false
  }
}
```

- [ ] **Step 4: Add search bar template in ChatPanel**

Add after the `chat-panel` opening div (find `<div class="chat-panel"` and add right after):

```html
<!-- Search bar — slides down when searching -->
<div v-if="isSearching" class="search-bar">
  <div class="search-input-wrap">
    <svg class="search-input-icon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
    </svg>
    <input
      class="search-input"
      :placeholder="$t('chat.searchPlaceholder')"
      :value="searchQuery"
      @input="onSearchInput"
      @keydown="onSearchKeydown"
      ref="searchInputEl"
    />
    <span v-if="searchResults" class="search-count">{{ searchResults.length }} {{ $t('chat.searchMatches') }}</span>
    <button class="search-close-btn" @click="exitSearch" :aria-label="$t('chat.searchClose')">
      <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
      </svg>
    </button>
  </div>
</div>
```

- [ ] **Step 5: Modify message list rendering for search mode**

In the message list template, the `v-for` over messages needs to use search results when searching. Modify the messages list to use a computed:

Add this computed after the search functions:

```js
/** displayMessages returns search results when searching, else the normal message list. */
const displayMessages = computed(() => {
  if (isSearching.value && searchResults.value) return searchResults.value
  return messages.value
})
```

And add a `searchMatchMap` computed for quick lookup of which messages match:

```js
/** searchMatchIds is a Set of message IDs that match the search query. Used to dim non-matching messages. */
const searchMatchIds = computed(() => {
  if (!isSearching.value || !searchResults.value) return null
  return new Set(searchResults.value.map(m => m.id))
})
```

Then in the template, wrap each message bubble in a div with conditional dimming class. Add `:class="{ 'search-dimmed': searchMatchIds && !searchMatchIds.has(m.id) && !m.isInfo }"` to the message wrapper element. Also add `@click="searchMatchIds && searchMatchIds.has(m.id) && jumpToMessage(m.id)"` to make search results clickable.

- [ ] **Step 6: Add highlight function**

Add a function to highlight matching keywords in message content:

```js
/** highlightMatches wraps occurrences of query terms in <mark> tags. */
function highlightMatches(text, query) {
  if (!query || !query.trim()) return text
  const escaped = query.trim().replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(`(${escaped})`, 'gi')
  return text.replace(re, '<mark class="search-highlight">$1</mark>')
}
```

- [ ] **Step 7: Add search bar CSS styles**

Add at end of `<style scoped>` block:

```css
.search-bar {
  padding: 8px 12px;
  border-bottom: 1px solid var(--lg-border-subtle);
  animation: search-slide-down 0.15s ease-out;
}
@keyframes search-slide-down {
  from { opacity: 0; transform: translateY(-8px); }
  to { opacity: 1; transform: translateY(0); }
}
.search-input-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--lg-surface-raised);
  border-radius: 8px;
  padding: 6px 10px;
}
.search-input-icon {
  color: var(--lg-text-muted);
  flex-shrink: 0;
}
.search-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  color: var(--lg-text);
  font-size: 13px;
}
.search-input::placeholder {
  color: var(--lg-text-muted);
}
.search-count {
  font-size: 11px;
  color: var(--lg-text-muted);
  white-space: nowrap;
}
.search-close-btn {
  background: none;
  border: none;
  color: var(--lg-text-muted);
  cursor: pointer;
  padding: 2px;
  border-radius: 4px;
  flex-shrink: 0;
}
.search-close-btn:hover {
  color: var(--lg-text);
  background: var(--lg-surface-hover);
}
.search-highlight {
  background: rgba(240, 180, 41, 0.25);
  color: #f0b429;
  border-radius: 2px;
  padding: 0 1px;
}
.search-dimmed {
  opacity: 0.35;
}
@keyframes jump-flash {
  0%, 100% { background: transparent; }
  50% { background: rgba(240, 180, 41, 0.12); }
}
:deep(.jump-flash) {
  animation: jump-flash 0.5s ease-in-out 4;
}
```

- [ ] **Step 8: Expose enterSearch via defineExpose**

At the bottom of `<script setup>`, find the existing `defineExpose({ focusInput, scrollToBottom })` (it may not exist in ChatPanel — check). Add or update:

```js
defineExpose({ enterSearch, focusInput, scrollToBottom })
```

(If `focusInput` and `scrollToBottom` aren't already exposed, only expose what exists.)

- [ ] **Step 9: Focus search input when entering search mode**

Add to `enterSearch()` after setting `isSearching.value = true`:

```js
nextTick(() => searchInputEl.value?.focus())
```

Add `const searchInputEl = ref(null)` to the ref declarations.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): implement chat search state machine with FTS5 backend"
```

---

### Task 6: Add i18n strings

**Files:**
- Modify: `frontend/src/locales/zh-CN.json`
- Modify: `frontend/src/locales/en.json`

- [ ] **Step 1: Add Chinese strings**

In `zh-CN.json`, under `chat` key, add:

```json
"searchPlaceholder": "搜索聊天记录...",
"searchMatches": "条匹配",
"searchClose": "关闭搜索",
"searchNoResults": "未找到匹配的消息"
```

And under `chatBubble` key, add:

```json
"search": "搜索聊天记录"
```

- [ ] **Step 2: Add English strings**

In `en.json`, under `chat` key, add:

```json
"searchPlaceholder": "Search chat history...",
"searchMatches": "matches",
"searchClose": "Close search",
"searchNoResults": "No matching messages found"
```

And under `chatBubble` key, add:

```json
"search": "Search chat history"
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/locales/
git commit -m "feat(i18n): add search-related locale strings"
```

---

### Task 7: Integration test — build and verify

**Files:** none (verification only)

- [ ] **Step 1: Build the project**

Run: `make build`
Expected: builds successfully with no errors

- [ ] **Step 2: Run all Go tests**

Run: `go test ./...`
Expected: all tests PASS

- [ ] **Step 3: Manual verification checklist**

Launch the app with `make run`, then:
- [ ] Open chat, click 🔍 — search bar slides down
- [ ] Type a keyword — matching messages highlighted, non-matching dimmed
- [ ] Press Escape — search closes, message list restored, scrolled to bottom
- [ ] Search → click a result — jumps to message context, target message flashes
- [ ] Search while streaming — search button should not respond (disabled)
- [ ] Empty search query — no results, search bar stays open
- [ ] Close search with ✕ button — same as Escape behavior

- [ ] **Step 4: Commit (if any final tweaks needed)**

```bash
git commit -m "chore: final verification of chat search feature"
```

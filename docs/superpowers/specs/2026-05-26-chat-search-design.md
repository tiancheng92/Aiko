# Chat History Full-Text Search

## Overview

Add full-text search to the chat panel. Backend uses SQLite FTS5; frontend implements a three-state machine (normal/search/jump) with state snapshot save/restore. Search results are returned in full (no pagination). Clicking a result jumps to that message's position in the normal lazy-loaded timeline.

## Search Scope

Content column only. Not searching thinking_content or attachment filenames.

## Backend

### FTS5 Virtual Table (`internal/db/sqlite.go`)

Add migration in `migrate()`:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts
USING fts5(content, content=messages, content_rowid=id)
```

Uses FTS5 content-sync mode — the FTS index automatically stays in sync with the messages table. No manual triggers needed. Only the `content` column is indexed.

### ShortStore Methods (`internal/memory/short.go`)

```go
// Search returns all messages whose content matches the FTS5 query, newest first.
func (s *ShortStore) Search(query string) ([]Message, error)

// GetNewestToID loads messages from the newest backwards until targetID is
// included, paginating by pageSize each step. Returns all loaded messages
// in chronological order.
func (s *ShortStore) GetNewestToID(targetID int64, pageSize int) ([]Message, error)
```

`Search` — runs `SELECT messages.* FROM messages JOIN messages_fts ON messages.id = messages_fts.rowid WHERE messages_fts MATCH ? ORDER BY messages.id DESC`, uses existing `scanMessage`.

`GetNewestToID` — loops: fetch `BeforeID(currentOldest, pageSize)`, prepend, repeat until `targetID` is in the loaded set. Falls back to `Recent(pageSize)` on first fetch if no marker.

### Wails Bindings (`app_chat.go`)

```go
// SearchMessages returns all messages matching the FTS5 query.
func (a *App) SearchMessages(query string) ([]memory.Message, error)

// GetMessagesFromNewestToID loads messages from newest down to the page
// containing targetID. Used for jump-to-message navigation after search.
func (a *App) GetMessagesFromNewestToID(targetID int64) ([]memory.Message, error)
```

`GetMessagesFromNewestToID` calls `shortMem.GetNewestToID(targetID, 10)`.

## Frontend

### Search Bar UI

- Title bar right side: search icon button (🔍)
- Click → search bar slides down below title bar
- Input field with debounce 300ms before calling `SearchMessages`
- Result count indicator (e.g. "3 matches")
- Clear button (✕) to exit search
- Escape key also exits search

### State Machine

Three states managed in ChatPanel:

**Normal** — standard lazy-load behavior. `messages` is the paginated list. IntersectionObserver active.

**Searching** — entered on search icon click.

1. Save snapshot: `{ messages: [...messages.value], oldestLoadedID, allLoaded }`
2. Disable IntersectionObserver
3. On query input (debounced 300ms): call `SearchMessages(query)` → replace `messages.value` with results
4. Search results rendered inline: matching text highlighted (`<mark>`), non-matching messages greyed out and collapsed
5. Clear query or click ✕/Escape → restore snapshot, re-enable Observer, scroll to bottom
6. Click a search result → enter Jump state

**Jump** — entered by clicking a search result.

1. Exit search state
2. Call `GetMessagesFromNewestToID(targetID)` → replace `messages.value`
3. Re-enable IntersectionObserver (user can scroll up to load more)
4. Scroll to the target message element
5. Flash highlight on the target message (CSS animation, 2 seconds)
6. Transition to Normal

### Snapshot Save/Restore

```js
let searchSnapshot = null  // { messages, oldestLoadedID, allLoaded }

function enterSearch() {
  searchSnapshot = {
    messages: [...messages.value],
    oldestLoadedID,
    allLoaded: allLoaded.value,
  }
  // disable observer
}

function exitSearch() {
  messages.value = searchSnapshot.messages
  oldestLoadedID = searchSnapshot.oldestLoadedID
  allLoaded.value = searchSnapshot.allLoaded
  searchSnapshot = null
  // re-enable observer
  scrollToBottom()
}
```

### Keyword Highlighting

Search results in Search state: wrap matching substrings in `<mark class="search-highlight">`. Non-matching messages get `opacity: 0.35` and reduced height via the existing collapse mechanism.

### Jump Highlight Animation

```css
@keyframes search-flash {
  0%, 100% { background: transparent; }
  50% { background: rgba(240, 180, 41, 0.15); }
}
.jump-flash { animation: search-flash 0.5s ease-in-out 4; }
```

## Files Changed

| File | Change |
|---|---|
| `internal/db/sqlite.go` | Add FTS5 virtual table migration |
| `internal/memory/short.go` | Add `Search()`, `GetNewestToID()` methods |
| `app_chat.go` | Add `SearchMessages()`, `GetMessagesFromNewestToID()` bindings |
| `frontend/src/components/ChatPanel.vue` | Search bar UI, state machine, snapshot save/restore, jump logic, highlight styles |

## Edge Cases

- **Empty search query**: restore normal view immediately
- **No results**: show "no results" placeholder in message list
- **FTS5 special characters**: escape user input before passing to MATCH (FTS5 query syntax has operators like AND/OR/NOT, `*`, `"`)
- **Search while streaming**: disable search button during active streaming
- **Very old target message**: `GetNewestToID` may need many round-trips; acceptable since this is a user-initiated action (not automatic)
- **Concurrent search**: debounce 300ms prevents rapid-fire queries; if a new query arrives before previous resolves, ignore stale response

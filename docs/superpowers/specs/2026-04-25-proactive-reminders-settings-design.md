# Proactive Reminders Settings Design

**Goal:** Expose the `proactive_items` queue in the Settings UI so users can view and cancel pending reminders. Simultaneously clean up leftover design debt: remove morning/evening greeting cron jobs, drop the `fired` column (replace mark-as-fired with delete-on-fire), and hide `ChatDirect`/`ChatDirectCollect` from Wails bindings.

**Date:** 2026-04-25

---

## Overview

`proactive_items` is a "pending queue": every row is a future reminder that has not yet fired. On fire, the row is deleted. Users see the live queue in Settings → 提醒事项 and can delete any entry to cancel it.

---

## Data Model Changes

### Drop `fired` column

`fired BOOLEAN DEFAULT FALSE` is removed. The table is rebuilt via migration (SQLite lacks reliable `DROP COLUMN` on older versions):

```sql
CREATE TABLE IF NOT EXISTS proactive_items_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    trigger_at  DATETIME NOT NULL,
    prompt      TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO proactive_items_new (id, trigger_at, prompt, created_at)
    SELECT id, trigger_at, prompt, created_at FROM proactive_items;
DROP TABLE proactive_items;
ALTER TABLE proactive_items_new RENAME TO proactive_items;
```

This migration runs in `internal/db/sqlite.go`'s `migrate()` function, appended after the existing `proactive_items` creation block.

---

## Backend Changes

### `internal/proactive/store.go`

**Store interface** — replace `MarkFired` with `Delete`, add `List`:

```go
type Store interface {
    Insert(ctx context.Context, triggerAt time.Time, prompt string) error
    DueItems(ctx context.Context, now time.Time) ([]Item, error)
    Delete(ctx context.Context, id int64) error
    List(ctx context.Context) ([]Item, error)
}
```

`Item` struct — remove `Fired bool` field:

```go
type Item struct {
    ID        int64
    TriggerAt time.Time
    Prompt    string
    CreatedAt time.Time
}
```

**`ProactiveStore` implementations:**

- `Delete(ctx, id)`: `DELETE FROM proactive_items WHERE id = ?` — idempotent (no error if row not found)
- `List(ctx)`: `SELECT id, trigger_at, prompt, created_at FROM proactive_items ORDER BY trigger_at ASC`

### `internal/proactive/engine.go`

**Remove greeting cron jobs entirely** — delete from `Start()`:
- `greetingMorningPrompt` constant
- `greetingEveningPrompt` constant
- Both `cron.AddFunc(...)` calls for morning and evening greetings

**`Poll()` — replace `MarkFired` with `Delete`:**

```go
func (e *ProactiveEngine) Poll(ctx context.Context) {
    items, err := e.store.DueItems(ctx, time.Now().UTC())
    if err != nil { return }
    for _, item := range items {
        // Delete first (prevent double-fire on slow Fire())
        if err := e.store.Delete(ctx, item.ID); err != nil { continue }
        if err := e.Fire(ctx, item.Prompt); err != nil {
            e.app.EmitEvent("notification:show", map[string]string{
                "message": "提醒触发失败：" + truncate(item.Prompt, 30),
            })
        }
    }
}
```

`truncate(s string, n int) string` — returns first `n` runes of `s`, appending `…` if truncated.

### `app.go`

**Remove from Wails bindings** — Wails exposes all exported methods on the registered struct. `ChatDirect` and `ChatDirectCollect` must remain exported (capital) to satisfy `AppInterface`. Instead, add a compile-time guard: wrap them in a private helper and have the exported version delegate to it, OR simply accept that they remain technically callable from the frontend but document them as internal. Since the frontend never calls them and there is no sensitive data path, **leave them as-is** — this is not a security issue, only a cleanliness concern. Remove this item from scope.

**New Wails-bound methods:**

```go
// ListProactiveItems returns all pending proactive reminders ordered by trigger time.
func (a *App) ListProactiveItems() ([]proactive.Item, error) {
    a.mu.RLock()
    pe := a.proactiveEngine
    a.mu.RUnlock()
    if pe == nil {
        return nil, nil
    }
    return pe.Store().List(context.Background())
}

// DeleteProactiveItem cancels a pending proactive reminder by ID.
func (a *App) DeleteProactiveItem(id int64) error {
    a.mu.RLock()
    pe := a.proactiveEngine
    a.mu.RUnlock()
    if pe == nil {
        return nil
    }
    return pe.Store().Delete(context.Background(), id)
}
```

`ProactiveEngine` needs a `Store() Store` accessor method added in `engine.go`.

---

## Frontend Changes

### `SettingsWindow.vue`

**New tab button** (after the `sms` tab button):

```html
<button :class="{ active: activeTab === 'proactive' }" @click="activeTab = 'proactive'">提醒事项</button>
```

**New tab pane:**

```html
<div v-if="activeTab === 'proactive'" class="tab-pane">
  <div class="section-header">
    <h3>提醒事项</h3>
    <button class="btn-small" @click="loadProactiveItems">刷新</button>
  </div>

  <div v-if="proactiveError" class="form-error">{{ proactiveError }}</div>

  <div v-if="proactiveItems.length === 0 && !proactiveError" class="empty-hint">
    暂无待触发的提醒事项
  </div>

  <div v-for="item in proactiveItems" :key="item.ID" class="proactive-row">
    <div class="proactive-info">
      <span class="proactive-time">{{ formatProactiveTime(item.TriggerAt) }}</span>
      <span class="proactive-prompt">{{ truncatePrompt(item.Prompt, 60) }}</span>
    </div>
    <button class="btn-small btn-danger" @click="deleteProactiveItem(item.ID)">删除</button>
  </div>
</div>
```

**Script additions** (`<script setup>`):

```js
import { ListProactiveItems, DeleteProactiveItem } from '../../wailsjs/go/main/App'

const proactiveItems = ref([])
const proactiveError = ref('')

async function loadProactiveItems() {
  try {
    proactiveError.value = ''
    proactiveItems.value = await ListProactiveItems() ?? []
  } catch (e) {
    proactiveError.value = '加载失败'
  }
}

async function deleteProactiveItem(id) {
  proactiveItems.value = proactiveItems.value.filter(i => i.ID !== id)  // optimistic
  try {
    await DeleteProactiveItem(id)
  } catch (e) {
    await loadProactiveItems()  // rollback
  }
}

function formatProactiveTime(t) {
  return new Date(t).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function truncatePrompt(s, n) {
  return s.length > n ? s.slice(0, n) + '…' : s
}

watch(() => activeTab.value, v => { if (v === 'proactive') loadProactiveItems() })
```

**CSS** (consistent with `cron-row` style):

```css
.proactive-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  background: rgba(255,255,255,0.04);
  border-radius: 8px;
  margin-bottom: 8px;
}
.proactive-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
  min-width: 0;
}
.proactive-time {
  font-size: 12px;
  color: #a5b4fc;
  font-variant-numeric: tabular-nums;
}
.proactive-prompt {
  font-size: 13px;
  color: rgba(255,255,255,0.8);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.btn-danger {
  color: #f87171;
  border-color: rgba(248,113,113,0.3);
}
.btn-danger:hover {
  background: rgba(248,113,113,0.15);
}
```

---

## What Is Not Changed

- `schedule_followup` tool logic — unchanged (Insert still works the same)
- `DueItems` query — unchanged
- `ProactiveEngine` cron poll interval — unchanged (`* * * * *`)
- Permission seeding for `schedule_followup` — unchanged
- `chat:proactive:start` frontend handling — unchanged

---

## Testing

- `TestStoreDeleteItem`: insert row → delete → DueItems returns empty
- `TestStoreList`: insert 2 rows → List returns both ordered by trigger_at
- `TestPollDeletesOnFire`: mock store, Poll calls Delete (not MarkFired) after Fire succeeds
- `TestPollDeletesOnFireFailure`: mock store where Fire fails → Delete still called, notification emitted
- `TestMigrateDropsFiredColumn`: open DB with old schema (with fired column) → migrate → confirm fired column gone

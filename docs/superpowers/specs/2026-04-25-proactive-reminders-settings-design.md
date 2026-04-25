# 提醒事项设置界面设计

**目标：** 在设置界面新增「提醒事项」tab，展示 `proactive_items` 待触发队列，支持删除单条。同步清理技术债：移除晨/晚问候、将触发后标记改为触发后删除、去掉 `fired` 字段。

**日期：** 2026-04-25

---

## 核心设计思路

`proactive_items` 是一个**待触发队列**：表里的每一行都是尚未触发的提醒。触发后直接删除该行，不标记状态。用户在设置界面看到的就是真实的待办列表，无需过滤 `fired` 状态。

---

## 数据模型变更

### 删除 `fired` 字段

通过重建表的方式删除（SQLite 旧版本不可靠支持 `DROP COLUMN`）：

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

迁移脚本追加到 `internal/db/sqlite.go` 的 `migrate()` 函数末尾。

---

## 后端变更

### `internal/proactive/store.go`

**Store 接口** — 将 `MarkFired` 替换为 `Delete`，新增 `List`：

```go
type Store interface {
    Insert(ctx context.Context, triggerAt time.Time, prompt string) error
    DueItems(ctx context.Context, now time.Time) ([]Item, error)
    Delete(ctx context.Context, id int64) error
    List(ctx context.Context) ([]Item, error)
}
```

**Item 结构体** — 删除 `Fired bool` 字段：

```go
type Item struct {
    ID        int64
    TriggerAt time.Time
    Prompt    string
    CreatedAt time.Time
}
```

**ProactiveStore 新增实现：**

- `Delete(ctx, id)`：`DELETE FROM proactive_items WHERE id = ?`，幂等（行不存在不报错）
- `List(ctx)`：`SELECT id, trigger_at, prompt, created_at FROM proactive_items ORDER BY trigger_at ASC`

### `internal/proactive/engine.go`

**彻底删除晨/晚问候：**
- 删除 `greetingMorningPrompt` 常量
- 删除 `greetingEveningPrompt` 常量
- 删除 `Start()` 中注册早晚问候的两个 `cron.AddFunc(...)` 调用

**`Poll()` — 触发后删除（不再标记 fired）：**

```go
func (e *ProactiveEngine) Poll(ctx context.Context) {
    items, err := e.store.DueItems(ctx, time.Now().UTC())
    if err != nil { return }
    for _, item := range items {
        // 先删除，防止 Fire 慢时重复触发
        if err := e.store.Delete(ctx, item.ID); err != nil { continue }
        if err := e.Fire(ctx, item.Prompt); err != nil {
            e.app.EmitEvent("notification:show", map[string]string{
                "message": "提醒触发失败：" + truncate(item.Prompt, 30),
            })
        }
    }
}
```

`truncate(s string, n int) string`：返回前 n 个 rune，超出时追加 `…`。

**新增 `Store()` 访问器**，供 `app.go` 调用：

```go
func (e *ProactiveEngine) Store() Store { return e.store }
```

### `app.go`

新增两个 Wails 绑定方法：

```go
// ListProactiveItems 返回所有待触发的提醒事项，按触发时间升序排列。
func (a *App) ListProactiveItems() ([]proactive.Item, error) {
    a.mu.RLock()
    pe := a.proactiveEngine
    a.mu.RUnlock()
    if pe == nil {
        return nil, nil
    }
    return pe.Store().List(context.Background())
}

// DeleteProactiveItem 取消指定 ID 的提醒事项。
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

> **关于 ChatDirect / ChatDirectCollect：** Wails 通过注册整个 struct 暴露所有导出方法，而这两个方法必须保持大写以实现 `AppInterface` 接口，无法通过改名隐藏。前端实际上不会调用它们，也不存在安全风险，因此**保持现状，不在本次范围内处理**。

---

## 前端变更

### `SettingsWindow.vue`

**新增 tab 按钮**（放在 `sms` tab 按钮之后）：

```html
<button :class="{ active: activeTab === 'proactive' }" @click="activeTab = 'proactive'">提醒事项</button>
```

**新增 tab 内容面板：**

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

**`<script setup>` 新增逻辑：**

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
  proactiveItems.value = proactiveItems.value.filter(i => i.ID !== id)  // 乐观更新
  try {
    await DeleteProactiveItem(id)
  } catch (e) {
    await loadProactiveItems()  // 失败则回滚
  }
}

function formatProactiveTime(t) {
  return new Date(t).toLocaleString('zh-CN', {
    month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit'
  })
}

function truncatePrompt(s, n) {
  return s.length > n ? s.slice(0, n) + '…' : s
}

// 切换到提醒事项 tab 时自动加载
watch(() => activeTab.value, v => { if (v === 'proactive') loadProactiveItems() })
```

**新增 CSS**（与 `cron-row` 风格一致）：

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

## 不变的部分

- `schedule_followup` 工具逻辑（Insert 不变）
- `DueItems` 查询逻辑不变
- `ProactiveEngine` 的 cron poll 间隔不变（每分钟）
- `schedule_followup` 的权限行 seeding 不变
- `chat:proactive:start` 前端处理逻辑不变

---

## 测试

- `TestStoreDelete`：插入一行 → 删除 → `DueItems` 返回空
- `TestStoreList`：插入 2 行 → `List` 按 `trigger_at` 升序返回
- `TestPollDeletesAfterFire`：mock store，Poll 成功触发后调用 `Delete`（不再调用 `MarkFired`）
- `TestPollDeletesOnFireFailure`：mock store，Fire 失败时仍调用 `Delete`，并 emit `notification:show`
- `TestMigrateDropsFiredColumn`：对含 `fired` 列的旧 DB 执行迁移，确认迁移后 `fired` 列不存在

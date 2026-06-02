# Claude Code 面板增强设计

**日期**：2026-06-02  
**范围**：`ClaudeStatusPanel.vue` + `internal/claudecco/server.go`（最小改动）

---

## 目标

提升 Claude Code 面板的信息密度和会话管理能力，具体新增：

1. **实时耗时计时器** — 显示每个会话已运行多久
2. **工具参数副标题** — 当前工具的关键输入参数（如命令内容、文件路径）
3. **工具调用计数** — 本次会话共触发了多少次工具
4. **× 手动关闭按钮** — idle 会话可手动清除

---

## 后端改动（`internal/claudecco/server.go`）

### 变更：`sessionSnapshot` 加一个字段

```go
type sessionSnapshot struct {
    ID            string `json:"id"`
    Name          string `json:"name"`
    CWD           string `json:"cwd"`
    State         string `json:"state"`
    ToolName      string `json:"toolName,omitempty"`
    HookEventName string `json:"hookEventName,omitempty"`
    ToolInput     string `json:"toolInput,omitempty"`  // 新增
}
```

### 变更：`emitStatus` 填充 `ToolInput`

在构建 `snap` 时，将 `input.ToolInput`（`any` 类型）序列化为 JSON，截断至 120 字符后填入 `snap.ToolInput`：

```go
if id == input.SessionID && input.ToolInput != nil {
    snap.ToolName = input.ToolName
    snap.HookEventName = input.HookEventName
    if b, err := json.Marshal(input.ToolInput); err == nil {
        s := string(b)
        if len([]rune(s)) > 120 {
            s = string([]rune(s)[:120]) + "…"
        }
        snap.ToolInput = s
    }
}
```

**不改动**：`sessionInfo`、事件路由、transcript 解析。

---

## 前端改动（`ClaudeStatusPanel.vue`）

### 数据结构

前端维护两个响应式 Map，key 为 `session.id`：

```js
// 首次进入 thinking 状态的时间戳（ms）
const sessionStartTimes = new Map()   // id → number

// 每个 session 累计触发的工具次数
const sessionToolCounts = new Map()   // id → number

// 手动关闭的 session id 集合
const dismissed = new Set()
```

### 耗时计时器

- 每次收到 `claudecco:status` 事件，遍历 sessions：
  - 若 state 变为 `thinking` 且该 id 不在 `sessionStartTimes` 中，记录 `Date.now()`
  - 若 state 变为 `idle` 或 `error`，**不清除**时间戳（保留最终耗时显示）
- `ref(0)` 的 `tick` 每秒自增（`setInterval`），驱动计算属性重新计算
- 格式：`< 60s` 显示 `Xs`，`≥ 60s` 显示 `Xm Ys`，`≥ 1h` 显示 `Xh Ym`

```js
const tick = ref(0)
let timer = null

onMounted(() => { timer = setInterval(() => tick.value++, 1000) })
onUnmounted(() => { clearInterval(timer); timer = null })

function elapsedLabel(id) {
  tick.value // 触发响应式依赖
  const start = sessionStartTimes.get(id)
  if (!start) return ''
  const sec = Math.floor((Date.now() - start) / 1000)
  if (sec < 60) return `${sec}s`
  const m = Math.floor(sec / 60), s = sec % 60
  if (m < 60) return `${m}m ${s}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}
```

### 工具调用计数

- 每次收到事件，遍历 sessions：若 state 为 `thinking` 且 `toolName` 非空，将该 id 的计数 +1
- 注意：同一次 tool 调用可能多次推送（PreToolUse → PermissionRequest 等），需去重：记录上次 toolName + 状态变化时才计数（或直接用 HookEventName === 'PreToolUse' 才计数）

实现方案：只在 `hookEventName === 'PreToolUse'` 时计数，避免重复：

```js
function onStatus(data) {
  const incoming = data.sessions || []
  for (const s of incoming) {
    if (s.state === 'thinking') {
      if (!sessionStartTimes.has(s.id)) {
        sessionStartTimes.set(s.id, Date.now())
      }
      if (s.hookEventName === 'PreToolUse') {
        sessionToolCounts.set(s.id, (sessionToolCounts.get(s.id) || 0) + 1)
      }
    }
  }
  sessions.value = incoming
}
```

### 工具参数解析（`ToolInput` 副标题）

`ToolInput` 是截断后的 JSON 字符串，前端尝试解析，按优先级取第一个有意义的值：

```js
function toolInputLabel(raw) {
  if (!raw) return ''
  try {
    const obj = JSON.parse(raw)
    const key = ['command', 'cmd', 'file_path', 'path', 'query', 'url', 'skill', 'prompt']
      .find(k => obj[k] && typeof obj[k] === 'string')
    if (key) {
      let val = obj[key]
      if (val.length > 60) val = val.slice(0, 60) + '…'
      return val
    }
  } catch {}
  // fallback: 原始字符串截断
  return raw.length > 60 ? raw.slice(0, 60) + '…' : raw
}
```

### × 关闭按钮

- idle 状态的行右侧显示 × 按钮（仅 hover 时完全可见，平时半透明）
- 点击后 `dismissed.add(id)`，`sessions` 计算属性过滤掉已 dismissed 的项
- `dismissed` 仅存 `reactive(new Set())`，不持久化（组件卸载后清空）

```js
const dismissed = reactive(new Set())

const visibleSessions = computed(() =>
  sessions.value.filter(s => !dismissed.has(s.id))
)

function dismiss(id) {
  dismissed.add(id)
  // 清理计时器数据
  sessionStartTimes.delete(id)
  sessionToolCounts.delete(id)
}
```

### 模板变更（行结构）

```
[状态点] [会话名]            [×12 计数] [2m34s 耗时] [idle/thinking]
         [工具副标题: git status]        [tool badge]
```

- 耗时在 `thinking` 时滚动更新，`idle` 后固定显示最终值
- × 按钮仅 idle 状态出现，hover 时显示，平时 `opacity: 0.3`
- 计数 `0` 时不显示

---

## 不做的事

- Token 统计（无数据源）
- Transcript 解析
- 统计持久化
- 会话历史折叠

---

## 文件清单

| 文件 | 改动类型 |
|---|---|
| `internal/claudecco/server.go` | 新增 `ToolInput` 字段 + `emitStatus` 填充逻辑 |
| `frontend/src/components/ClaudeStatusPanel.vue` | 新增计时器、计数、副标题、× 按钮 |
| `frontend/src/locales/zh-CN.json` | 无需改动（新元素用代码内联或已有 key）|

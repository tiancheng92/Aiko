# Tavily 搜索 API 集成设计

## 目标

当配置了 `TavilyAPIKey` 时，使用 Tavily Search 作为 `web_search` 的主搜索后端。未设置 key 时自动退回 DuckDuckGo。新增可选的日期/时效过滤参数，供 LLM Agent 按需传入。

## 决策

- **后端选择**：运行时分支——`Cfg.TavilyAPIKey != ""` 时走 Tavily；否则走 DDG。
- **可选日期参数**：`time_range`、`start_date`、`end_date` 在 DDG 路径下静默忽略。
- **输出格式**：不变——`[N] title — url\n    snippet`；若 Tavily 返回 `answer` 则在前面追加 `[Summary]` 块。
- **num_results 默认值**：从 5 改为 10。

---

## 第一节：Config 变更

### 1.1 新增字段（`internal/config/config.go`）

```go
TavilyAPIKey string // 可选；为空时退回 DuckDuckGo
```

紧随 `JinaAPIKey` 之后添加，遵循相同的 `Load()` / `Save()` 模式：

```go
// Load()
cfg.TavilyAPIKey = m["tavily_api_key"]

// Save()
"tavily_api_key": cfg.TavilyAPIKey,
```

---

## 第二节：`web_search` 工具变更

### 2.1 工具参数（更新后）

| 参数 | 类型 | 默认值 | 最大值 | 说明 |
|---|---|---|---|---|
| `query` | string | — | — | 搜索关键词（必填）|
| `num_results` | int | **10** | 10 | 返回结果数量 |
| `time_range` | string | — | — | `"day"` / `"week"` / `"month"` / `"year"`（仅 Tavily）|
| `start_date` | string | — | — | 起始日期过滤，格式 `"YYYY-MM-DD"`（仅 Tavily）|
| `end_date` | string | — | — | 结束日期过滤，格式 `"YYYY-MM-DD"`（仅 Tavily）|

### 2.2 工具描述更新

工具描述字符串补充可选参数说明：

```
可选参数（仅 Tavily 生效，DuckDuckGo 模式下忽略）：
- time_range: 按时效过滤 — "day"、"week"、"month"、"year"
- start_date / end_date: 日期范围，格式 "YYYY-MM-DD"
```

### 2.3 `WebSearchTool` 结构体

新增 `Cfg *config.Config` 字段（与 `WebFetchTool` 保持一致）：

```go
type WebSearchTool struct {
    Cfg *config.Config
}
```

在 `registry.go` 中从 `All()` 迁移至 `AllContextual()`。

### 2.4 运行时后端选择

```
InvokableRun(ctx, args)
  ├─ 解析：query, num_results, time_range, start_date, end_date
  ├─ if Cfg != nil && Cfg.TavilyAPIKey != ""
  │     → tavilySearch(ctx, query, numResults, key, timeRange, startDate, endDate)
  └─ else
        → 已有 DDG 路径（不变）
```

### 2.5 `tavilySearch` 辅助函数

```
POST https://api.tavily.com/search
Content-Type: application/json
超时：15s

请求体：
{
  "api_key":        "<key>",
  "query":          "<query>",
  "max_results":    <n>,
  "search_depth":   "advanced",
  "include_answer": true,
  // 仅非空时传入：
  "time_range":     "<time_range>",
  "start_date":     "<start_date>",
  "end_date":       "<end_date>"
}
```

响应 JSON 结构：
```json
{
  "answer": "...",         // 可选合成摘要
  "results": [
    {
      "title":   "...",
      "url":     "...",
      "content": "..."     // 摘要片段
    }
  ]
}
```

处理流程：
1. 解码 JSON 响应
2. 对每条结果 URL 调用 `normalizeURL` + 去重（与 DDG 路径一致）
3. 截取至 `numResults` 条
4. 格式化输出

### 2.6 输出格式

与 DDG 保持相同的紧凑格式。若 `answer` 非空，在前面追加摘要块：

```
[Summary]
<answer 文本>

[1] Title — https://example.com
    摘要文本...

[2] Title — https://...
    摘要文本...
```

---

## 第三节：设置 UI

### 3.1 新增输入项（`SettingsWindow.vue`）

在「Web 工具」区域的「Jina API Key」输入项正下方添加密码输入框：

```html
<div class="setting-item">
  <label>Tavily API Key（可选）</label>
  <input type="password" v-model="cfg.TavilyAPIKey" placeholder="留空则使用 DuckDuckGo" />
</div>
```

---

## 第四节：文件变更清单

只涉及四个文件：

```
internal/config/config.go                    ← TavilyAPIKey 字段 + Load/Save
internal/tools/web_tools.go                  ← WebSearchTool.Cfg、tavilySearch、参数变更
internal/tools/registry.go                   ← WebSearchTool 迁移至 AllContextual
frontend/src/components/SettingsWindow.vue   ← Tavily API Key 输入框
```

`web_tools.go` 中新增辅助函数：

```go
func tavilySearch(ctx context.Context, query string, numResults int, apiKey, timeRange, startDate, endDate string) ([]searchResult, error)
```

### 不变部分

- DDG 解析路径（`parseDDGResults`、`extractDDGResult`、`normalizeURL`）——不做任何修改
- `web_fetch` 相关代码——不涉及
- 工具权限系统——不涉及

---

## 范围外

- 将 `search_depth` 作为用户可配置参数（硬编码为 `"advanced"`）
- Tavily 响应缓存
- Brave Search / SerpAPI 集成
- 由 Agent 按查询动态选择后端

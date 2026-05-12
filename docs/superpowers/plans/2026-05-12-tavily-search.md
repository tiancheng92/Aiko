# Tavily Search API Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Tavily Search as the primary `web_search` backend, with DuckDuckGo as automatic fallback when no API key is configured.

**Architecture:** `WebSearchTool` gains a `Cfg *config.Config` field and moves to `AllContextual()`. In `InvokableRun`, if `Cfg.TavilyAPIKey != ""` it calls `tavilySearch()`; otherwise the existing DDG path runs unchanged. Three new optional params (`time_range`, `start_date`, `end_date`) pass through to Tavily's POST body; DDG ignores them.

**Tech Stack:** Go `net/http`, `encoding/json`; Tavily REST API (`https://api.tavily.com/search`); existing `normalizeURL` deduplication.

---

## File Map

| File | Change |
|---|---|
| `internal/config/config.go` | Add `TavilyAPIKey` field + `Load`/`Save` entries |
| `internal/tools/web_tools.go` | Add `Cfg` to `WebSearchTool`; update `Info`; update `InvokableRun`; add `tavilySearch` helper |
| `internal/tools/registry.go` | Move `WebSearchTool` from `All()` to `AllContextual()`; add `&WebSearchTool{}` to `AllPermissionDeclarations` ctxPrototypes |
| `frontend/src/components/SettingsWindow.vue` | Add Tavily API Key input below existing Jina input |

---

### Task 1: Add TavilyAPIKey to Config

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add the struct field**

In `config.go`, after line 44 (`JinaAPIKey string ...`), add:

```go
TavilyAPIKey string // optional; empty = DuckDuckGo fallback
```

- [ ] **Step 2: Add Load() entry**

In `Load()`, after `cfg.JinaAPIKey = m["jina_api_key"]` (line 110), add:

```go
cfg.TavilyAPIKey = m["tavily_api_key"]
```

- [ ] **Step 3: Add Save() entry**

In `Save()`, in the `pairs` map after `"jina_api_key": cfg.JinaAPIKey,` (line 148), add:

```go
"tavily_api_key": cfg.TavilyAPIKey,
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./internal/config/...
```

Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add TavilyAPIKey optional field"
```

---

### Task 2: Add Tavily API Key Input to Settings UI

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: Add the input below the Jina key input**

After line 1569 (the `<p id="jina-hint" ...>` element), add a new `form-row` div inside the same `settings-section`:

```html
              <div class="form-row" style="margin-top:8px">
                <label for="tavily-api-key-input">Tavily API Key</label>
                <input
                  id="tavily-api-key-input"
                  v-model="cfg.TavilyAPIKey"
                  type="password"
                  placeholder="tvly-xxxxxxxxxxxx（留空则使用 DuckDuckGo）"
                  spellcheck="false"
                  autocorrect="off"
                  autocomplete="off"
                  class="short-input"
                  aria-describedby="tavily-hint"
                />
              </div>
              <p id="tavily-hint" class="section-hint" style="margin-top:8px">可选，设置后 web_search 使用 Tavily（支持时效过滤），留空自动退回 DuckDuckGo</p>
```

The full "Web 工具" section should look like:

```html
            <div class="settings-section" style="margin-top:12px">
              <h3 class="section-title">Web 工具</h3>
              <div class="form-row">
                <label for="jina-api-key-input">Jina API Key</label>
                <input
                  id="jina-api-key-input"
                  v-model="cfg.JinaAPIKey"
                  type="password"
                  placeholder="jina_xxxxxxxxxxxx（留空即可）"
                  spellcheck="false"
                  autocorrect="off"
                  autocomplete="off"
                  class="short-input"
                  aria-describedby="jina-hint"
                />
              </div>
              <p id="jina-hint" class="section-hint" style="margin-top:8px">可选，提升 web_fetch 抓取额度（留空使用免费模式）</p>
              <div class="form-row" style="margin-top:8px">
                <label for="tavily-api-key-input">Tavily API Key</label>
                <input
                  id="tavily-api-key-input"
                  v-model="cfg.TavilyAPIKey"
                  type="password"
                  placeholder="tvly-xxxxxxxxxxxx（留空则使用 DuckDuckGo）"
                  spellcheck="false"
                  autocorrect="off"
                  autocomplete="off"
                  class="short-input"
                  aria-describedby="tavily-hint"
                />
              </div>
              <p id="tavily-hint" class="section-hint" style="margin-top:8px">可选，设置后 web_search 使用 Tavily（支持时效过滤），留空自动退回 DuckDuckGo</p>
            </div>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(settings): add Tavily API Key input to Web 工具 section"
```

---

### Task 3: Implement tavilySearch and update WebSearchTool

**Files:**
- Modify: `internal/tools/web_tools.go`

- [ ] **Step 1: Add `encoding/json` import**

The file already imports `net/http`, `context`, `fmt`, `io`, `strings`, `time`. Add `encoding/json` and `bytes` to the import block:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"golang.org/x/net/html"

	"aiko/internal/config"
)
```

- [ ] **Step 2: Update `WebSearchTool` struct and `Info`**

Replace the current `WebSearchTool` struct and `Info` method (lines 38–58):

```go
// WebSearchTool searches the web via Tavily (when API key is configured) or DuckDuckGo.
type WebSearchTool struct {
	Cfg *config.Config // optional; nil or empty TavilyAPIKey = DuckDuckGo fallback
}

func (t *WebSearchTool) Name() string                { return "web_search" }
func (t *WebSearchTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for web_search.
func (t *WebSearchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "搜索互联网，返回结果标题、URL 和摘要。配置 Tavily API Key 后使用 Tavily（更准确，支持时效过滤）；未配置时自动退回 DuckDuckGo。若已知具体页面 URL，直接用 web_fetch 更精准。\n可选参数（仅 Tavily 生效）：time_range 支持 \"day\"、\"week\"、\"month\"、\"year\"；start_date / end_date 格式为 \"YYYY-MM-DD\"。",
		map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "搜索词或自然语言问题",
				Required: true,
			},
			"num_results": {
				Type: schema.Integer,
				Desc: "返回结果数量，默认 10，最多 10",
			},
			"time_range": {
				Type: schema.String,
				Desc: "时效过滤：\"day\"、\"week\"、\"month\"、\"year\"（仅 Tavily）",
			},
			"start_date": {
				Type: schema.String,
				Desc: "起始日期，格式 \"YYYY-MM-DD\"（仅 Tavily）",
			},
			"end_date": {
				Type: schema.String,
				Desc: "结束日期，格式 \"YYYY-MM-DD\"（仅 Tavily）",
			},
		},
	), nil
}
```

- [ ] **Step 3: Update `InvokableRun` to parse new params and branch on API key**

Replace the current `InvokableRun` for `WebSearchTool` (lines 62–110):

```go
// InvokableRun routes to Tavily (if key configured) or DuckDuckGo.
func (t *WebSearchTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	query, _ := args["query"].(string)
	if query == "" {
		return "请提供搜索词", nil
	}
	numResults := 10
	if n, ok := args["num_results"].(float64); ok && n > 0 {
		numResults = int(n)
	}
	if numResults > 10 {
		numResults = 10
	}
	timeRange, _ := args["time_range"].(string)
	startDate, _ := args["start_date"].(string)
	endDate, _ := args["end_date"].(string)

	// time_range and (start_date / end_date) are mutually exclusive.
	if timeRange != "" && (startDate != "" || endDate != "") {
		return "参数错误：time_range 与 start_date / end_date 不能同时使用，请二选一", nil
	}

	// Use Tavily when API key is configured; fall back to DuckDuckGo otherwise.
	if t.Cfg != nil && t.Cfg.TavilyAPIKey != "" {
		return tavilySearch(ctx, query, numResults, t.Cfg.TavilyAPIKey, timeRange, startDate, endDate)
	}
	return ddgSearch(ctx, query, numResults)
}
```

- [ ] **Step 4: Extract DDG path into `ddgSearch` helper**

The current DDG logic in `InvokableRun` becomes a standalone function. Replace the old `InvokableRun` body's DDG block (the HTTP request + `parseDDGResults` call + formatting loop) with this named function added after `InvokableRun`:

```go
// ddgSearch performs a DuckDuckGo HTML search and returns formatted results.
func ddgSearch(ctx context.Context, query string, numResults int) (string, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	reqCtx, cancel := context.WithTimeout(ctx, webTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("DNT", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read search response: %w", err)
	}

	results := parseDDGResults(string(body), numResults)
	if len(results) == 0 {
		return "未找到搜索结果", nil
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s — %s\n    %s\n\n", i+1, r.title, r.url, r.snippet)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
```

- [ ] **Step 5: Add `tavilySearch` helper**

Add this function after `ddgSearch`:

```go
// tavilyResult is the JSON shape of a single Tavily search result.
type tavilyResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// tavilyResponse is the top-level Tavily API response.
type tavilyResponse struct {
	Answer  string         `json:"answer"`
	Results []tavilyResult `json:"results"`
}

// tavilySearch queries the Tavily Search API and returns formatted results.
// timeRange, startDate, endDate are optional — empty string omits them from the request.
func tavilySearch(ctx context.Context, query string, numResults int, apiKey, timeRange, startDate, endDate string) (string, error) {
	body := map[string]any{
		"api_key":        apiKey,
		"query":          query,
		"max_results":    numResults,
		"search_depth":   "advanced",
		"include_answer": true,
	}
	if timeRange != "" {
		body["time_range"] = timeRange
	}
	if startDate != "" {
		body["start_date"] = startDate
	}
	if endDate != "" {
		body["end_date"] = endDate
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal tavily request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, webTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build tavily request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Sprintf("Tavily 请求失败（%d）: %s", resp.StatusCode, string(b)), nil
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read tavily response: %w", err)
	}

	var tr tavilyResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("parse tavily response: %w", err)
	}

	// Deduplicate and normalise URLs.
	seen := map[string]bool{}
	var results []tavilyResult
	for _, r := range tr.Results {
		norm := normalizeURL(r.URL)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		r.URL = norm
		results = append(results, r)
		if len(results) >= numResults {
			break
		}
	}

	if len(results) == 0 {
		return "未找到搜索结果", nil
	}

	var sb strings.Builder
	// Prepend synthesised answer if Tavily returned one.
	if answer := strings.TrimSpace(tr.Answer); answer != "" {
		fmt.Fprintf(&sb, "[Summary]\n%s\n\n", answer)
	}
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s — %s\n    %s\n\n", i+1, r.Title, r.URL, strings.TrimSpace(r.Content))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
```

- [ ] **Step 6: Verify compilation**

```bash
go build ./internal/tools/...
```

Expected: no output (success).

- [ ] **Step 7: Commit**

```bash
git add internal/tools/web_tools.go
git commit -m "feat(web-search): add Tavily primary backend with DDG fallback, date filter params"
```

---

### Task 4: Move WebSearchTool to AllContextual in registry

**Files:**
- Modify: `internal/tools/registry.go`

- [ ] **Step 1: Remove `&WebSearchTool{}` from `All()`**

In `All()` (around line 121), delete:

```go
		&WebSearchTool{},
```

- [ ] **Step 2: Add `&WebSearchTool{Cfg: cfg}` to `AllContextual()`**

In `AllContextual()`, in the `contextTools` slice (around line 234), add at the top:

```go
		&WebSearchTool{Cfg: cfg},
```

Full updated slice start:

```go
	contextTools := []Tool{
		&WebSearchTool{Cfg: cfg},
		&WebFetchTool{Cfg: cfg},
		...
	}
```

- [ ] **Step 3: Add `&WebSearchTool{}` to `AllPermissionDeclarations` ctxPrototypes**

In `AllPermissionDeclarations()`, in the `ctxPrototypes` slice (around line 177), add at the top:

```go
		&WebSearchTool{},
		&WebFetchTool{},
		...
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add internal/tools/registry.go
git commit -m "refactor(registry): move WebSearchTool to AllContextual for Cfg injection"
```

---

### Task 5: Manual smoke test

- [ ] **Step 1: Build and run**

```bash
make run
```

- [ ] **Step 2: Test DDG fallback (no Tavily key)**

Open Settings → 执行安全 tab → Web 工具 section. Confirm "Tavily API Key" input is visible. Leave it empty.

Ask the agent: `搜索 "golang context timeout"`. Confirm results appear in `[N] title — url\n    snippet` format.

- [ ] **Step 3: Test Tavily (with key)**

Enter a valid Tavily API key in Settings. Ask the agent:

```
web_search query="今日科技新闻" time_range="day"
```

Confirm results come from Tavily (answer summary block appears if Tavily returns one).

- [ ] **Step 4: Test date filter**

Ask:

```
web_search query="AI 新闻" start_date="2026-05-11" end_date="2026-05-12"
```

Confirm Tavily returns results within the date range.

- [ ] **Step 5: Final commit (if any fixups needed)**

```bash
git add -p
git commit -m "fix(web-search): <describe fixup>"
```

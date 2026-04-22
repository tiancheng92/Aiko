# 三期网络搜索工具 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 添加 `web_search`（DuckDuckGo HTML 接口）和 `web_fetch`（HTTP GET + HTML → 纯文本）两个 AI 工具。

**Architecture:** 两个工具实现 `internaltools.Tool` 接口，放在 `internal/tools/web_tools.go`；`web_search` 解析 DuckDuckGo 的 HTML 结果页（无需 API key）；`web_fetch` 下载指定 URL 并将 HTML 转为纯文本返回给 LLM；两者都是 `PermProtected` 级别，需用户在设置中开启。

**Tech Stack:** Go `net/http`（标准库）、`golang.org/x/net/html`（已在 go.mod）、`internal/tools` Tool 接口

---

## 文件结构

| 操作 | 文件 | 说明 |
|---|---|---|
| Create | `internal/tools/web_tools.go` | WebSearchTool、WebFetchTool 实现 |
| Modify | `internal/tools/registry.go` | `All()` 中追加两个工具 |

---

### Task 1: 实现 `web_search` 工具

**Files:**
- Create: `internal/tools/web_tools.go`

- [ ] **Step 1: 创建文件，实现 WebSearchTool**

```go
// internal/tools/web_tools.go
package tools

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
    "time"

    "golang.org/x/net/html"
)

const (
    webTimeout   = 15 * time.Second
    maxBodyBytes = 1 << 20 // 1 MiB
)

// WebSearchTool searches the web via DuckDuckGo HTML endpoint.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) Description() string {
    return `使用 DuckDuckGo 搜索互联网，返回摘要结果列表。参数 JSON: {"query":"<搜索词>","num_results":5}`
}
func (t *WebSearchTool) Permission() PermissionLevel { return PermProtected }

// Execute queries DuckDuckGo HTML search and returns formatted results.
func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) ToolResult {
    query, _ := args["query"].(string)
    if query == "" {
        return ToolResult{Content: "请提供搜索词"}
    }
    numResults := 5
    if n, ok := args["num_results"].(float64); ok && n > 0 {
        numResults = int(n)
    }
    if numResults > 10 {
        numResults = 10
    }

    searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
    reqCtx, cancel := context.WithTimeout(ctx, webTimeout)
    defer cancel()

    req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, searchURL, nil)
    if err != nil {
        return ToolResult{Error: fmt.Errorf("build search request: %w", err)}
    }
    req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; desktop-pet/1.0)")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return ToolResult{Error: fmt.Errorf("search request failed: %w", err)}
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
    if err != nil {
        return ToolResult{Error: fmt.Errorf("read search response: %w", err)}
    }

    results := parseDDGResults(string(body), numResults)
    if len(results) == 0 {
        return ToolResult{Content: "未找到搜索结果"}
    }

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("DuckDuckGo 搜索 \"%s\" 的结果：\n\n", query))
    for i, r := range results {
        sb.WriteString(fmt.Sprintf("%d. **%s**\n   %s\n   %s\n\n", i+1, r.title, r.snippet, r.url))
    }
    return ToolResult{Content: sb.String()}
}

type ddgResult struct {
    title   string
    snippet string
    url     string
}

// parseDDGResults extracts search results from DuckDuckGo's HTML response.
// DDG HTML format: results are in <div class="result"> containing
// <a class="result__a"> for title/url and <a class="result__snippet"> for snippet.
func parseDDGResults(body string, max int) []ddgResult {
    doc, err := html.Parse(strings.NewReader(body))
    if err != nil {
        return nil
    }

    var results []ddgResult
    var walk func(*html.Node)
    walk = func(n *html.Node) {
        if len(results) >= max {
            return
        }
        if n.Type == html.ElementNode && n.Data == "div" {
            for _, a := range n.Attr {
                if a.Key == "class" && strings.Contains(a.Val, "result__body") {
                    r := extractDDGResult(n)
                    if r.title != "" {
                        results = append(results, r)
                    }
                    return
                }
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            walk(c)
        }
    }
    walk(doc)
    return results
}

// extractDDGResult extracts title, url, and snippet from a DDG result node.
func extractDDGResult(n *html.Node) ddgResult {
    var r ddgResult
    var walk func(*html.Node)
    walk = func(n *html.Node) {
        if n.Type == html.ElementNode && n.Data == "a" {
            cls := attrVal(n, "class")
            href := attrVal(n, "href")
            text := textContent(n)
            if strings.Contains(cls, "result__a") {
                r.title = text
                r.url = href
            } else if strings.Contains(cls, "result__snippet") {
                r.snippet = text
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            walk(c)
        }
    }
    walk(n)
    return r
}

// attrVal returns the value of a named HTML attribute, or "".
func attrVal(n *html.Node, key string) string {
    for _, a := range n.Attr {
        if a.Key == key {
            return a.Val
        }
    }
    return ""
}

// textContent returns all text node content within an HTML subtree.
func textContent(n *html.Node) string {
    if n.Type == html.TextNode {
        return strings.TrimSpace(n.Data)
    }
    var sb strings.Builder
    for c := n.FirstChild; c != nil; c = c.NextSibling {
        sb.WriteString(textContent(c))
        sb.WriteString(" ")
    }
    return strings.TrimSpace(sb.String())
}
```

- [ ] **Step 2: 实现 WebFetchTool（追加到同文件末尾）**

```go
// WebFetchTool fetches a URL and returns its content as plain text.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) Description() string {
    return `抓取指定 URL 的网页内容，返回去除 HTML 标签后的纯文本。参数 JSON: {"url":"<完整URL>","max_chars":3000}`
}
func (t *WebFetchTool) Permission() PermissionLevel { return PermProtected }

// Execute fetches the given URL and returns stripped plain text.
func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) ToolResult {
    targetURL, _ := args["url"].(string)
    if targetURL == "" {
        return ToolResult{Content: "请提供 URL"}
    }
    if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
        targetURL = "https://" + targetURL
    }
    maxChars := 3000
    if m, ok := args["max_chars"].(float64); ok && m > 0 {
        maxChars = int(m)
    }
    if maxChars > 8000 {
        maxChars = 8000
    }

    reqCtx, cancel := context.WithTimeout(ctx, webTimeout)
    defer cancel()

    req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
    if err != nil {
        return ToolResult{Error: fmt.Errorf("build fetch request: %w", err)}
    }
    req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; desktop-pet/1.0)")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return ToolResult{Error: fmt.Errorf("fetch failed: %w", err)}
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
    if err != nil {
        return ToolResult{Error: fmt.Errorf("read response: %w", err)}
    }

    text := htmlToText(string(body))
    if len([]rune(text)) > maxChars {
        text = string([]rune(text)[:maxChars]) + "\n...(已截断)"
    }
    if text == "" {
        return ToolResult{Content: fmt.Sprintf("无法从 %s 提取文本内容", targetURL)}
    }
    return ToolResult{Content: fmt.Sprintf("URL: %s\n\n%s", targetURL, text)}
}

// htmlToText converts HTML to plain text by stripping tags.
func htmlToText(body string) string {
    doc, err := html.Parse(strings.NewReader(body))
    if err != nil {
        return body
    }
    var sb strings.Builder
    var walk func(*html.Node)
    walk = func(n *html.Node) {
        // Skip script/style nodes entirely.
        if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
            return
        }
        if n.Type == html.TextNode {
            t := strings.TrimSpace(n.Data)
            if t != "" {
                sb.WriteString(t)
                sb.WriteString(" ")
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            walk(c)
        }
    }
    walk(doc)
    return strings.Join(strings.Fields(sb.String()), " ")
}
```

- [ ] **Step 3: 在 `registry.go` 的 `All()` 中追加两个工具**

```go
func All() []Tool {
    return []Tool{
        &GetCurrentTimeTool{},
        &GetTimezoneTool{},
        &FormatTimeTool{},
        &GetOSInfoTool{},
        &GetHardwareInfoTool{},
        &GetNetworkStatusTool{},
        &WebSearchTool{},
        &WebFetchTool{},
    }
}
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 5: Commit**

```bash
git add internal/tools/web_tools.go internal/tools/registry.go
git commit -m "feat: add web_search (DuckDuckGo) and web_fetch tools"
```

---

## Self-Review

**Spec coverage:**
- ✅ #2 web_search — Task 1 (WebSearchTool)
- ✅ #2 web_fetch — Task 1 (WebFetchTool)
- ✅ 搜索引擎 DuckDuckGo — 使用 `html.duckduckgo.com/html/`，无需 API key

**Placeholder scan:** 无 TBD / TODO。

**Type consistency:** `WebSearchTool` / `WebFetchTool` 均实现 `Tool` 接口，`All()` 返回 `[]Tool`，一致。

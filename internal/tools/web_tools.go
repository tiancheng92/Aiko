// internal/tools/web_tools.go
package tools

import (
	"context"
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
)

const (
	webTimeout      = 15 * time.Second
	fetchTimeout    = 30 * time.Second
	maxBodyBytes    = 2 << 20 // 2 MiB
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	defaultMaxChars = 8000
)

// Pre-compiled regexes for better performance.
var (
	reScript     = regexp.MustCompile(`(?i)<script[\s\S]*?</script>`)
	reStyle      = regexp.MustCompile(`(?i)<style[\s\S]*?</style>`)
	reTags       = regexp.MustCompile(`<[^>]*>`)
	reWhitespace = regexp.MustCompile(`[^\S\n]+`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)

// WebSearchTool searches the web via DuckDuckGo HTML endpoint.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string                { return "web_search" }
func (t *WebSearchTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for web_search.
func (t *WebSearchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "使用 DuckDuckGo 搜索互联网，返回相关结果摘要",
		map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "搜索词",
				Required: true,
			},
			"num_results": {
				Type: schema.Integer,
				Desc: "返回结果数量，默认 5，最多 10",
			},
		},
	), nil
}

// InvokableRun queries DuckDuckGo HTML search and returns formatted results.
func (t *WebSearchTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	query, _ := args["query"].(string)
	if query == "" {
		return "请提供搜索词", nil
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
	fmt.Fprintf(&sb, "DuckDuckGo 搜索 \"%s\" 的结果：\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. **%s**\n   %s\n   %s\n\n", i+1, r.title, r.snippet, r.url)
	}
	return sb.String(), nil
}

type ddgResult struct {
	title   string
	snippet string
	url     string
}

// parseDDGResults extracts search results from DuckDuckGo's HTML response.
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

// WebFetchTool fetches a URL and returns its content as plain text.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string                { return "web_fetch" }
func (t *WebFetchTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for web_fetch.
func (t *WebFetchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "抓取指定 URL 的网页内容，返回去除 HTML 标签后的纯文本",
		map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "要抓取的完整 URL",
				Required: true,
			},
			"max_chars": {
				Type: schema.Integer,
				Desc: "返回文本的最大字符数，默认 8000，最多 50000",
			},
		},
	), nil
}

// InvokableRun fetches the given URL and returns stripped plain text.
func (t *WebFetchTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	targetURL, _ := args["url"].(string)
	if targetURL == "" {
		return "请提供 URL", nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}
	maxChars := defaultMaxChars
	if m, ok := args["max_chars"].(float64); ok && m > 0 {
		maxChars = int(m)
	}
	if maxChars > 50000 {
		maxChars = 50000
	}

	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("build fetch request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("DNT", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	text := htmlToText(string(body))
	if len([]rune(text)) > maxChars {
		text = string([]rune(text)[:maxChars]) + "\n...(已截断)"
	}
	if text == "" {
		return fmt.Sprintf("无法从 %s 提取文本内容", targetURL), nil
	}
	return fmt.Sprintf("URL: %s\n\n%s", targetURL, text), nil
}

// htmlToText converts HTML to plain text using regex pipeline.
func htmlToText(body string) string {
	text := reScript.ReplaceAllString(body, "")
	text = reStyle.ReplaceAllString(text, "")
	text = reTags.ReplaceAllString(text, " ")
	text = reWhitespace.ReplaceAllString(text, " ")
	text = reBlankLines.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

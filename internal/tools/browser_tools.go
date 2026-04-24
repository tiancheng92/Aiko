// internal/tools/browser_tools.go
package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GetBrowserURLTool reads the URL currently displayed in the frontmost browser
// window using the macOS Accessibility API, then fetches and summarises the
// page content via WebFetchTool.
type GetBrowserURLTool struct{}

// Name returns the tool identifier.
func (t *GetBrowserURLTool) Name() string { return "get_browser_url" }

// Permission declares this tool as public (no extra approval needed).
func (t *GetBrowserURLTool) Permission() PermissionLevel { return PermPublic }

// Info returns eino tool metadata.
func (t *GetBrowserURLTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: "获取用户当前浏览器（Chrome/Safari/Arc 等）正在访问的页面 URL，并抓取该页面的正文内容供 AI 分析或总结。无需任何参数。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// InvokableRun reads the browser URL via Accessibility API, then delegates to
// WebFetchTool to retrieve the page content.
func (t *GetBrowserURLTool) InvokableRun(ctx context.Context, _ string, opts ...tool.Option) (string, error) {
	url, err := getBrowserURLNative()
	if err != nil {
		return fmt.Sprintf("无法获取浏览器 URL：%s\n\n请确认已在「系统设置 → 隐私与安全性 → 辅助功能」中授权 Aiko，且浏览器当前有打开的标签页。", err.Error()), nil
	}

	fetcher := &WebFetchTool{}
	input := fmt.Sprintf(`{"url":%q,"prompt":"请提取并返回该页面的主要正文内容，保留标题和重要结构。"}`, url)
	content, err := fetcher.InvokableRun(ctx, input, opts...)
	if err != nil {
		return fmt.Sprintf("已获取到 URL：%s\n\n但抓取页面内容时出错：%s", url, err.Error()), nil
	}
	return fmt.Sprintf("**当前页面 URL**：%s\n\n**页面内容**：\n%s", url, content), nil
}

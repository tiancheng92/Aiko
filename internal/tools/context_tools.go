// internal/tools/context_tools.go
package tools

import (
	"context"
	"fmt"
	"strings"

	"desktop-pet/internal/knowledge"
)

// SearchKnowledgeTool searches the knowledge base for relevant document chunks.
type SearchKnowledgeTool struct {
	KnowledgeSt *knowledge.Store
}

// Name returns the tool name.
func (t *SearchKnowledgeTool) Name() string { return "search_knowledge" }

// Description returns the tool description for the AI.
func (t *SearchKnowledgeTool) Description() string {
	return `搜索已导入的知识库文档，返回与查询最相关的段落。参数 JSON: {"query":"<搜索词>"}`
}

// Permission returns the permission level required to run this tool.
func (t *SearchKnowledgeTool) Permission() PermissionLevel { return PermPublic }

// Execute searches the knowledge store for the given query string.
func (t *SearchKnowledgeTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	if t.KnowledgeSt == nil {
		return ToolResult{Content: "知识库未启用（需配置 Embedding 模型并导入文档）"}
	}
	query, _ := args["query"].(string)
	if query == "" {
		return ToolResult{Content: "请提供搜索词"}
	}
	results, err := t.KnowledgeSt.Search(ctx, query, 5)
	if err != nil {
		return ToolResult{Error: fmt.Errorf("search knowledge: %w", err)}
	}
	if len(results) == 0 {
		return ToolResult{Content: "知识库中未找到相关内容"}
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 条相关知识库内容：\n\n", len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("--- 片段 %d ---\n%s\n\n", i+1, r))
	}
	return ToolResult{Content: sb.String()}
}

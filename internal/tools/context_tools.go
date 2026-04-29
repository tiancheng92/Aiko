// internal/tools/context_tools.go
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/knowledge"
)

// SearchKnowledgeTool searches the knowledge base for relevant document chunks.
type SearchKnowledgeTool struct {
	KnowledgeSt *knowledge.Store
}

func (t *SearchKnowledgeTool) Name() string             { return "search_knowledge" }
func (t *SearchKnowledgeTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for search_knowledge.
func (t *SearchKnowledgeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "搜索已导入的知识库文档，返回与查询最相关的段落",
		map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "搜索词或问题",
				Required: true,
			},
		},
	), nil
}

// InvokableRun searches the knowledge store for the given query string.
func (t *SearchKnowledgeTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	if t.KnowledgeSt == nil {
		return "知识库未启用（需配置 Embedding 模型并导入文档）", nil
	}
	args := parseArgs(input)
	query, _ := args["query"].(string)
	if query == "" {
		return "请提供搜索词", nil
	}
	results, err := t.KnowledgeSt.Search(ctx, query, 5)
	if err != nil {
		return "", fmt.Errorf("search knowledge: %w", err)
	}
	if len(results) == 0 {
		return "知识库中未找到相关内容", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "找到 %d 条相关知识库内容：\n\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&sb, "--- 片段 %d [来源: %s, 相似度: %.2f] ---\n%s\n\n", i+1, r.Source, r.Similarity, r.Content)
	}
	return sb.String(), nil
}

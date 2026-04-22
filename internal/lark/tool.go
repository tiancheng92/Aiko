// internal/lark/tool.go
package lark

import (
	"context"
	"fmt"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Tool implements the eino InvokableTool interface for lark-cli subprocess calls.
// It is injected with a Client at startup.
type Tool struct {
	Client *Client
}

// Name returns the tool identifier.
func (t *Tool) Name() string { return "lark" }

// Info returns the eino tool schema.
func (t *Tool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: "操作飞书：发消息、查日历、读文档等。通过 lark-cli 子进程执行，需提前完成 `lark-cli auth login`。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"args": {
				Type:     schema.String,
				Desc:     `lark-cli 命令参数，空格分隔，例如 "im +messages-send --chat-id oc_xxx --text Hello" 或 "calendar +agenda"`,
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun parses args and delegates to lark-cli.
func (t *Tool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var params struct {
		Args string `json:"args"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if params.Args == "" {
		return "请提供 lark-cli 命令参数", nil
	}
	parts := splitArgs(params.Args)
	// Always append --format json for structured output.
	parts = append(parts, "--format", "json")
	return t.Client.Run(ctx, parts...)
}

// splitArgs splits a shell-like argument string, respecting quoted strings.
func splitArgs(s string) []string {
	var args []string
	var cur []byte
	inQ := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			inQ = !inQ
		case c == ' ' && !inQ:
			if len(cur) > 0 {
				args = append(args, string(cur))
				cur = cur[:0]
			}
		default:
			cur = append(cur, c)
		}
	}
	if len(cur) > 0 {
		args = append(args, string(cur))
	}
	return args
}

// internal/mcp/client.go
package mcp

import (
	"context"
	json "github.com/bytedance/sonic"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpgo "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// LoadTools connects to all enabled MCP servers and returns their tools as eino BaseTool slice.
// Servers that fail to connect are logged and skipped (non-fatal).
func LoadTools(ctx context.Context, store *ServerStore) []tool.BaseTool {
	cfgs, err := store.List(ctx)
	if err != nil {
		slog.Error("failed to list mcp_servers", "err", err)
		return nil
	}

	var tools []tool.BaseTool
	for _, cfg := range cfgs {
		if !cfg.Enabled {
			continue
		}
		serverTools, err := connectAndDiscover(ctx, cfg)
		if err != nil {
			slog.Warn("mcp server connect failed, skipping", "server", cfg.Name, "err", err)
			continue
		}
		tools = append(tools, serverTools...)
		slog.Info("mcp server connected", "server", cfg.Name, "tools", len(serverTools))
	}
	return tools
}

// connectAndDiscover opens a connection to one MCP server and returns its tools.
func connectAndDiscover(ctx context.Context, cfg ServerConfig) ([]tool.BaseTool, error) {
	var client *mcpgo.Client
	var err error

	switch cfg.Transport {
	case "stdio":
		if cfg.Command == "" {
			return nil, fmt.Errorf("stdio transport requires a command")
		}
		args := append([]string{cfg.Command}, cfg.Args...)
		client, err = mcpgo.NewStdioMCPClient(args[0], nil, args[1:]...)
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("sse transport requires a url")
		}
		var opts []transport.ClientOption
		if len(cfg.Headers) > 0 {
			opts = append(opts, mcpgo.WithHeaders(cfg.Headers))
		}
		client, err = mcpgo.NewSSEMCPClient(cfg.URL, opts...)
	case "http":
		if cfg.URL == "" {
			return nil, fmt.Errorf("http transport requires a url")
		}
		var opts []transport.StreamableHTTPCOption
		if len(cfg.Headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
		}
		client, err = mcpgo.NewStreamableHttpClient(cfg.URL, opts...)
	default:
		return nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
	if err != nil {
		return nil, fmt.Errorf("create mcp client: %w", err)
	}

	// Start the transport connection before initialization.
	// NewStdioMCPClient auto-starts for backward compatibility; SSE and HTTP do not.
	if cfg.Transport != "stdio" {
		if err := client.Start(ctx); err != nil {
			return nil, fmt.Errorf("mcp transport start: %w", err)
		}
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "Desktop Pet",
		Version: "1.0.0",
	}
	_, err = client.Initialize(ctx, initRequest)
	if err != nil {
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}

	resp, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("mcp list tools: %w", err)
	}

	result := make([]tool.BaseTool, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		result = append(result, &mcpToolAdapter{
			client:     client,
			serverName: cfg.Name,
			toolDef:    t,
		})
	}
	return result, nil
}

// mcpToolAdapter wraps an MCP tool as an eino tool.BaseTool.
type mcpToolAdapter struct {
	client     *mcpgo.Client
	serverName string
	toolDef    mcp.Tool
}

// qualifiedName returns "{serverName}__{toolName}" to avoid collisions.
func (a *mcpToolAdapter) qualifiedName() string {
	return a.serverName + "__" + a.toolDef.Name
}

// Info returns the tool's schema information for eino.
func (a *mcpToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	desc := a.toolDef.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %q from server %q", a.toolDef.Name, a.serverName)
	}
	return &schema.ToolInfo{
		Name: a.qualifiedName(),
		Desc: fmt.Sprintf("[MCP:%s] %s", a.serverName, desc),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"args": {
				Desc:     "JSON object with tool arguments",
				Required: false,
				Type:     schema.String,
			},
		}),
	}, nil
}

// InvokableRun calls the MCP tool with the given input JSON and returns the result.
func (a *mcpToolAdapter) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	// Parse the input JSON into a map.
	var args map[string]any
	if strings.TrimSpace(input) != "" && input != "{}" {
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", fmt.Errorf("parse mcp tool args: %w", err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = a.toolDef.Name
	req.Params.Arguments = args

	resp, err := a.client.CallTool(ctx, req)
	if err != nil {
		return "", fmt.Errorf("mcp call tool %q: %w", a.toolDef.Name, err)
	}

	// Collect text content from the response.
	var sb strings.Builder
	for _, c := range resp.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	if resp.IsError {
		return fmt.Sprintf("MCP tool %q returned error: %s", a.toolDef.Name, sb.String()), nil
	}
	return sb.String(), nil
}
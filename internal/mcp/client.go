// internal/mcp/client.go
package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpgo "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// geminiNameRe matches characters that Gemini does not allow in function names.
// Gemini allows: a-z, A-Z, 0-9, _, ., :, -  (max 128 chars).
var geminiNameRe = regexp.MustCompile(`[^a-zA-Z0-9_.:\-]`)

// sanitizeName converts a raw tool name into one acceptable by Gemini's API.
func sanitizeName(raw string) string {
	s := geminiNameRe.ReplaceAllString(raw, "_")
	if len(s) > 0 && s[0] >= '0' && s[0] <= '9' {
		s = "_" + s
	}
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

// LoadToolsAsync connects to all enabled MCP servers concurrently and calls done when finished.
// Each server runs in its own goroutine; servers that fail are logged and skipped.
// If perServerTimeout > 0, each server gets an individual deadline.
// The done callback is always called exactly once, even if no servers are configured.
// The caller owns the returned closers and must Close them when the tool set is replaced.
func LoadToolsAsync(ctx context.Context, store *ServerStore, perServerTimeout time.Duration, done func([]tool.BaseTool, []io.Closer)) {
	go func() {
		cfgs, err := store.List(ctx)
		if err != nil {
			slog.Error("LoadToolsAsync: failed to list mcp_servers", "err", err)
			done(nil, nil)
			return
		}

		var mu sync.Mutex
		var tools []tool.BaseTool
		var closers []io.Closer

		var wg sync.WaitGroup
		for _, cfg := range cfgs {
			if !cfg.Enabled {
				continue
			}
			wg.Add(1)
			go func(cfg ServerConfig) {
				defer wg.Done()
				sctx := ctx
				if perServerTimeout > 0 {
					var cancel context.CancelFunc
					sctx, cancel = context.WithTimeout(ctx, perServerTimeout)
					defer cancel()
				}
				serverTools, client, err := connectAndDiscover(sctx, cfg)
				if err != nil {
					slog.Warn("mcp server connect failed, skipping", "server", cfg.Name, "err", err)
					return
				}
				slog.Info("mcp server connected", "server", cfg.Name, "tools", len(serverTools))
				mu.Lock()
				tools = append(tools, serverTools...)
				if client != nil {
					closers = append(closers, client)
				}
				mu.Unlock()
			}(cfg)
		}
		wg.Wait()
		done(tools, closers)
	}()
}

// LoadTools connects to all enabled MCP servers and returns their tools as eino BaseTool slice.
// Servers that fail to connect are logged and skipped (non-fatal).
// The returned closers own the underlying MCP client connections; the caller
// must Close them when the tool set is replaced (e.g. on config change) or the
// process exits, otherwise stdio subprocesses and sockets leak.
func LoadTools(ctx context.Context, store *ServerStore) ([]tool.BaseTool, []io.Closer) {
	cfgs, err := store.List(ctx)
	if err != nil {
		slog.Error("failed to list mcp_servers", "err", err)
		return nil, nil
	}

	var tools []tool.BaseTool
	var closers []io.Closer
	for _, cfg := range cfgs {
		if !cfg.Enabled {
			continue
		}
		serverTools, client, err := connectAndDiscover(ctx, cfg)
		if err != nil {
			slog.Warn("mcp server connect failed, skipping", "server", cfg.Name, "err", err)
			continue
		}
		tools = append(tools, serverTools...)
		if client != nil {
			closers = append(closers, client)
		}
		slog.Info("mcp server connected", "server", cfg.Name, "tools", len(serverTools))
	}
	return tools, closers
}

// connectAndDiscover opens a connection to one MCP server and returns its tools.
// The returned *mcpgo.Client must be closed by the caller when it is no longer
// needed; on any error from this function the client (if created) is closed here.
func connectAndDiscover(ctx context.Context, cfg ServerConfig) ([]tool.BaseTool, *mcpgo.Client, error) {
	var client *mcpgo.Client
	var err error

	switch cfg.Transport {
	case "stdio":
		if cfg.Command == "" {
			return nil, nil, fmt.Errorf("stdio transport requires a command")
		}
		args := append([]string{cfg.Command}, cfg.Args...)
		client, err = mcpgo.NewStdioMCPClient(args[0], nil, args[1:]...)
	case "sse":
		if cfg.URL == "" {
			return nil, nil, fmt.Errorf("sse transport requires a url")
		}
		var opts []transport.ClientOption
		if len(cfg.Headers) > 0 {
			opts = append(opts, mcpgo.WithHeaders(cfg.Headers))
		}
		client, err = mcpgo.NewSSEMCPClient(cfg.URL, opts...)
	case "http":
		if cfg.URL == "" {
			return nil, nil, fmt.Errorf("http transport requires a url")
		}
		var opts []transport.StreamableHTTPCOption
		if len(cfg.Headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
		}
		client, err = mcpgo.NewStreamableHttpClient(cfg.URL, opts...)
	default:
		return nil, nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("create mcp client: %w", err)
	}

	// Start the transport connection before initialization.
	// NewStdioMCPClient auto-starts for backward compatibility; SSE and HTTP do not.
	if cfg.Transport != "stdio" {
		if err := client.Start(ctx); err != nil {
			_ = client.Close()
			return nil, nil, fmt.Errorf("mcp transport start: %w", err)
		}
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "Desktop Pet",
		Version: "1.0.0",
	}
	if _, err := client.Initialize(ctx, initRequest); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("mcp initialize: %w", err)
	}

	resp, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("mcp list tools: %w", err)
	}

	result := make([]tool.BaseTool, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		result = append(result, &mcpToolAdapter{
			client:     client,
			serverName: cfg.Name,
			toolDef:    t,
		})
	}
	return result, client, nil
}

// mcpToolAdapter wraps an MCP tool as an eino tool.BaseTool.
type mcpToolAdapter struct {
	client     *mcpgo.Client
	serverName string
	toolDef    mcp.Tool
}

// qualifiedName returns a sanitized "{serverName}__{toolName}" that is safe
// for all LLM APIs including Gemini (alphanumeric, _, ., :, - only, max 128).
func (a *mcpToolAdapter) qualifiedName() string {
	return sanitizeName(a.serverName + "__" + a.toolDef.Name)
}

// Info returns the tool's schema information for eino.
// It converts the MCP tool's true inputSchema properties so the LLM generates
// correctly-shaped calls (e.g. {"url":"..."}) rather than a wrapped {"args":"..."}.
func (a *mcpToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	desc := a.toolDef.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %q from server %q", a.toolDef.Name, a.serverName)
	}
	return &schema.ToolInfo{
		Name:        a.qualifiedName(),
		Desc:        fmt.Sprintf("[MCP:%s] %s", a.serverName, desc),
		ParamsOneOf: schema.NewParamsOneOfByParams(mcpSchemaToEinoParams(a.toolDef.InputSchema)),
	}, nil
}

// mcpSchemaToEinoParams converts the MCP tool's inputSchema to an eino ParameterInfo map.
// Falls back to a single generic "args" string parameter when the schema has no properties.
func mcpSchemaToEinoParams(is mcp.ToolInputSchema) map[string]*schema.ParameterInfo {
	if len(is.Properties) == 0 {
		return map[string]*schema.ParameterInfo{
			"args": {Desc: "JSON object with tool arguments", Type: schema.String},
		}
	}
	required := make(map[string]bool, len(is.Required))
	for _, r := range is.Required {
		required[r] = true
	}
	params := make(map[string]*schema.ParameterInfo, len(is.Properties))
	for name, prop := range is.Properties {
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		params[name] = jsonSchemaPropToEinoParam(propMap, required[name])
	}
	if len(params) == 0 {
		params["args"] = &schema.ParameterInfo{Desc: "JSON object with tool arguments", Type: schema.String}
	}
	return params
}

// jsonSchemaPropToEinoParam converts a single JSON Schema property definition to eino ParameterInfo.
func jsonSchemaPropToEinoParam(prop map[string]any, required bool) *schema.ParameterInfo {
	info := &schema.ParameterInfo{Required: required}
	if desc, ok := prop["description"].(string); ok {
		info.Desc = desc
	}
	switch prop["type"] {
	case "integer":
		info.Type = schema.Integer
	case "number":
		info.Type = schema.Number
	case "boolean":
		info.Type = schema.Boolean
	case "array":
		info.Type = schema.Array
		// Gemini requires items to be present for array parameters.
		// Extract from the "items" sub-schema if available; default to string.
		if itemsProp, ok := prop["items"].(map[string]any); ok {
			info.ElemInfo = jsonSchemaPropToEinoParam(itemsProp, false)
		} else {
			info.ElemInfo = &schema.ParameterInfo{Type: schema.String}
		}
	case "object":
		info.Type = schema.Object
	default:
		// "string" and anything unrecognised → string
		info.Type = schema.String
	}
	if enum, ok := prop["enum"].([]any); ok && info.Type == schema.String {
		for _, e := range enum {
			if es, ok := e.(string); ok {
				info.Enum = append(info.Enum, es)
			}
		}
	}
	return info
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
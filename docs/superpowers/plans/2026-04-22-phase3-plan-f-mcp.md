# 三期 MCP 接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 允许用户在设置界面配置外部 MCP server（stdio 或 SSE），app 启动时自动连接并将 MCP server 提供的工具注册为 AI 可调用工具。

**Architecture:**
使用 `github.com/mark3labs/mcp-go` 客户端库连接外部 MCP server。每个 MCP server 配置保存在 SQLite `mcp_servers` 表中；`internal/mcp/` 包负责连接管理和工具适配；适配后的工具实现 eino `tool.BaseTool` 接口，在 `initLLMComponents` 中与内置工具合并后传入 agent。

**Tech Stack:** Go `github.com/mark3labs/mcp-go`（新增依赖）、SQLite（已有）、eino `tool.BaseTool`（已有）

---

## 文件结构

| 操作 | 文件 | 说明 |
|---|---|---|
| Modify | `internal/db/sqlite.go` | 添加 mcp_servers 表 |
| Create | `internal/mcp/client.go` | MCPServer 配置结构、连接逻辑、工具适配 |
| Create | `internal/mcp/store.go` | MCPServerStore — CRUD 操作封装 |
| Modify | `app.go` | 暴露 CRUD API、initLLMComponents 中加载 MCP 工具 |
| Modify | `frontend/src/components/SettingsWindow.vue` | MCP server 管理 UI（列表 + 增删） |

---

### Task 1: DB migration — mcp_servers 表

**Files:**
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: 在 `migrate()` 中追加 mcp_servers 表**

在现有 `migrate` 函数的建表 SQL 末尾追加（紧接现有最后一个 `CREATE TABLE` 之后）：

```sql
CREATE TABLE IF NOT EXISTS mcp_servers (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,   -- 用户起的名字，同时作为工具前缀
    transport   TEXT NOT NULL,          -- "stdio" or "sse"
    command     TEXT,                   -- stdio: 可执行文件路径（如 "/usr/local/bin/mcp-server"）
    args        TEXT,                   -- stdio: JSON 数组，如 ["--flag","value"]
    url         TEXT,                   -- sse: HTTP endpoint
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/db/sqlite.go
git commit -m "feat: add mcp_servers metadata table"
```

---

### Task 2: 添加 mcp-go 依赖

**Files:**
- Modify: `go.mod` / `go.sum`（通过 `go get` 自动更新）

- [ ] **Step 1: 安装 mcp-go 客户端库**

```bash
go get github.com/mark3labs/mcp-go@latest
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add mcp-go client dependency"
```

---

### Task 3: MCPServerStore — 配置 CRUD

**Files:**
- Create: `internal/mcp/store.go`

- [ ] **Step 1: 创建 `internal/mcp/store.go`**

```go
// internal/mcp/store.go
package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ServerConfig holds the persisted configuration for one MCP server.
type ServerConfig struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Transport string    `json:"transport"` // "stdio" | "sse"
	Command   string    `json:"command"`   // stdio only
	Args      []string  `json:"args"`      // stdio only
	URL       string    `json:"url"`       // sse only
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ServerStore manages MCP server configurations in SQLite.
type ServerStore struct {
	db *sql.DB
}

// NewServerStore creates a ServerStore backed by the given SQLite database.
func NewServerStore(db *sql.DB) *ServerStore {
	return &ServerStore{db: db}
}

// List returns all configured MCP servers ordered by creation time.
func (s *ServerStore) List(ctx context.Context) ([]ServerConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, transport, COALESCE(command,''), COALESCE(args,'[]'),
		        COALESCE(url,''), enabled, created_at FROM mcp_servers ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list mcp_servers: %w", err)
	}
	defer rows.Close()

	var cfgs []ServerConfig
	for rows.Next() {
		var c ServerConfig
		var argsJSON string
		if err := rows.Scan(&c.ID, &c.Name, &c.Transport, &c.Command,
			&argsJSON, &c.URL, &c.Enabled, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan mcp_server row: %w", err)
		}
		_ = json.Unmarshal([]byte(argsJSON), &c.Args)
		cfgs = append(cfgs, c)
	}
	return cfgs, rows.Err()
}

// Add inserts a new MCP server configuration.
func (s *ServerStore) Add(ctx context.Context, c ServerConfig) (ServerConfig, error) {
	argsJSON, _ := json.Marshal(c.Args)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_servers(name, transport, command, args, url, enabled) VALUES(?,?,?,?,?,?)`,
		c.Name, c.Transport, c.Command, string(argsJSON), c.URL, c.Enabled)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("insert mcp_server: %w", err)
	}
	c.ID, _ = res.LastInsertId()
	return c, nil
}

// Update modifies an existing MCP server configuration by ID.
func (s *ServerStore) Update(ctx context.Context, c ServerConfig) error {
	argsJSON, _ := json.Marshal(c.Args)
	_, err := s.db.ExecContext(ctx,
		`UPDATE mcp_servers SET name=?, transport=?, command=?, args=?, url=?, enabled=? WHERE id=?`,
		c.Name, c.Transport, c.Command, string(argsJSON), c.URL, c.Enabled, c.ID)
	if err != nil {
		return fmt.Errorf("update mcp_server: %w", err)
	}
	return nil
}

// Delete removes an MCP server configuration by ID.
func (s *ServerStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete mcp_server: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/mcp/store.go
git commit -m "feat: add MCPServerStore for CRUD on mcp_servers table"
```

---

### Task 4: MCP 客户端 — 连接 + 工具适配

**Files:**
- Create: `internal/mcp/client.go`

- [ ] **Step 1: 创建 `internal/mcp/client.go`**

```go
// internal/mcp/client.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpgo "github.com/mark3labs/mcp-go/client"
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
		client, err = mcpgo.NewStdioMCPClient(cfg.Command, cfg.Args...)
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("sse transport requires a url")
		}
		client, err = mcpgo.NewSSEMCPClient(cfg.URL)
	default:
		return nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
	if err != nil {
		return nil, fmt.Errorf("create mcp client: %w", err)
	}

	if err := client.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
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
```

- [ ] **Step 2: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 3: Commit**

```bash
git add internal/mcp/client.go
git commit -m "feat: add MCP client with stdio/SSE transport and eino tool adapter"
```

---

### Task 5: 在 `app.go` 中暴露 CRUD API 并加载 MCP 工具

**Files:**
- Modify: `app.go`

- [ ] **Step 1: 添加 `mcpStore` 字段和初始化**

在 `App` struct 中加入（紧跟 `knowledgeSt` 字段之后）：

```go
mcpStore    *mcp.ServerStore
```

在 `import` 中加入：

```go
"desktop-pet/internal/mcp"
```

在 `startup` 函数的 `a.permStore = ...` 之后（SQLite 已初始化后）加入：

```go
a.mcpStore = mcp.NewServerStore(a.sqlDB)
```

- [ ] **Step 2: 添加对外 API 方法**

在 `app.go` 末尾追加以下四个方法：

```go
// ListMCPServers returns all configured MCP server entries.
func (a *App) ListMCPServers() ([]mcp.ServerConfig, error) {
	return a.mcpStore.List(a.ctx)
}

// AddMCPServer adds a new MCP server configuration.
func (a *App) AddMCPServer(cfg mcp.ServerConfig) (mcp.ServerConfig, error) {
	return a.mcpStore.Add(a.ctx, cfg)
}

// UpdateMCPServer updates an existing MCP server configuration by ID.
func (a *App) UpdateMCPServer(cfg mcp.ServerConfig) error {
	return a.mcpStore.Update(a.ctx, cfg)
}

// DeleteMCPServer removes an MCP server configuration by ID.
func (a *App) DeleteMCPServer(id int64) error {
	return a.mcpStore.Delete(a.ctx, id)
}
```

- [ ] **Step 3: 在 `initLLMComponents` 中加载 MCP 工具**

在现有 `allTools := append(builtinTools, skillTools...)` 所在区域，替换为：

```go
builtinTools := internaltools.AllEino(a.permStore)
skillTools, err := skill.LoadAll(a.cfg.SkillsDir)
if err != nil {
    return fmt.Errorf("load skills: %w", err)
}
mcpTools := mcp.LoadTools(ctx, a.mcpStore)
allTools := append(builtinTools, skillTools...)
allTools = append(allTools, mcpTools...)
```

- [ ] **Step 4: 验证编译**

```bash
go build ./...
```

Expected: 无输出。

- [ ] **Step 5: Commit**

```bash
git add app.go
git commit -m "feat: expose MCP server CRUD API and load MCP tools in initLLMComponents"
```

---

### Task 6: 设置界面 MCP 管理 UI

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: 在 `<script setup>` 中添加 MCP 相关状态和函数**

在 `import` 区域加入：

```js
import { ListMCPServers, AddMCPServer, UpdateMCPServer, DeleteMCPServer } from '../../wailsjs/go/main/App'
```

在现有 ref 声明区域末尾加入：

```js
// MCP servers
const mcpServers = ref([])
const showMCPForm = ref(false)
const mcpForm = ref({ id: 0, name: '', transport: 'stdio', command: '', args: '', url: '', enabled: true })
const mcpFormError = ref('')

/** fetchMCPServers loads the MCP server list from the backend. */
async function fetchMCPServers() {
  try {
    mcpServers.value = await ListMCPServers() || []
  } catch (e) {
    console.error('fetchMCPServers:', e)
  }
}

/** openMCPForm opens the add-server form with empty fields. */
function openMCPForm() {
  mcpForm.value = { id: 0, name: '', transport: 'stdio', command: '', args: '', url: '', enabled: true }
  mcpFormError.value = ''
  showMCPForm.value = true
}

/** saveMCPServer adds or updates an MCP server. */
async function saveMCPServer() {
  mcpFormError.value = ''
  const cfg = {
    ...mcpForm.value,
    args: mcpForm.value.args ? mcpForm.value.args.split(' ').filter(Boolean) : [],
  }
  try {
    if (cfg.id === 0) {
      await AddMCPServer(cfg)
    } else {
      await UpdateMCPServer(cfg)
    }
    showMCPForm.value = false
    await fetchMCPServers()
  } catch (e) {
    mcpFormError.value = String(e)
  }
}

/** deleteMCPServer removes an MCP server by ID. */
async function deleteMCPServer(id) {
  try {
    await DeleteMCPServer(id)
    await fetchMCPServers()
  } catch (e) {
    console.error('deleteMCPServer:', e)
  }
}

/** toggleMCPServer toggles the enabled state of an MCP server. */
async function toggleMCPServer(srv) {
  try {
    await UpdateMCPServer({ ...srv, enabled: !srv.enabled })
    await fetchMCPServers()
  } catch (e) {
    console.error('toggleMCPServer:', e)
  }
}
```

在 `onMounted` 中追加：

```js
await fetchMCPServers()
```

- [ ] **Step 2: 在 `<template>` 中添加 MCP 管理面板**

在现有最后一个设置 section（如 "工具权限"）之后，`</div>` 关闭主容器之前，插入：

```html
<!-- MCP Servers Section -->
<div class="section">
  <div class="section-header">
    <h3>MCP 服务器</h3>
    <button class="btn-small" @click="openMCPForm">+ 添加</button>
  </div>

  <div v-if="mcpServers.length === 0" class="empty-hint">
    暂无 MCP 服务器，点击"添加"接入外部工具
  </div>

  <div v-for="srv in mcpServers" :key="srv.id" class="mcp-row">
    <div class="mcp-info">
      <span class="mcp-name">{{ srv.name }}</span>
      <span class="mcp-transport">{{ srv.transport }}</span>
      <span class="mcp-endpoint">{{ srv.transport === 'stdio' ? srv.command : srv.url }}</span>
    </div>
    <div class="mcp-actions">
      <button class="btn-toggle" :class="{ active: srv.enabled }" @click="toggleMCPServer(srv)">
        {{ srv.enabled ? '已启用' : '已禁用' }}
      </button>
      <button class="btn-danger-small" @click="deleteMCPServer(srv.id)">删除</button>
    </div>
  </div>

  <!-- Add/Edit Form -->
  <div v-if="showMCPForm" class="mcp-form">
    <div class="form-row">
      <label>名称</label>
      <input v-model="mcpForm.name" placeholder="my-server" />
    </div>
    <div class="form-row">
      <label>传输方式</label>
      <select v-model="mcpForm.transport">
        <option value="stdio">stdio</option>
        <option value="sse">SSE</option>
      </select>
    </div>
    <template v-if="mcpForm.transport === 'stdio'">
      <div class="form-row">
        <label>命令</label>
        <input v-model="mcpForm.command" placeholder="/usr/local/bin/mcp-server" />
      </div>
      <div class="form-row">
        <label>参数（空格分隔）</label>
        <input v-model="mcpForm.args" placeholder="--flag value" />
      </div>
    </template>
    <template v-else>
      <div class="form-row">
        <label>URL</label>
        <input v-model="mcpForm.url" placeholder="http://localhost:8080/sse" />
      </div>
    </template>
    <div v-if="mcpFormError" class="form-error">{{ mcpFormError }}</div>
    <div class="form-buttons">
      <button class="btn-primary" @click="saveMCPServer">保存</button>
      <button class="btn-secondary" @click="showMCPForm = false">取消</button>
    </div>
  </div>
</div>
```

- [ ] **Step 3: 在 `<style>` 中追加 MCP 相关样式**

在 `<style scoped>` 末尾追加：

```css
.mcp-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid rgba(255,255,255,0.08);
  gap: 8px;
}
.mcp-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}
.mcp-name {
  font-weight: 600;
  font-size: 13px;
}
.mcp-transport {
  font-size: 11px;
  opacity: 0.6;
  text-transform: uppercase;
}
.mcp-endpoint {
  font-size: 11px;
  opacity: 0.5;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mcp-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
.btn-toggle {
  font-size: 12px;
  padding: 3px 8px;
  border-radius: 4px;
  border: 1px solid rgba(255,255,255,0.2);
  background: rgba(255,255,255,0.05);
  color: inherit;
  cursor: pointer;
  opacity: 0.6;
}
.btn-toggle.active {
  opacity: 1;
  border-color: rgba(100,200,100,0.5);
  background: rgba(100,200,100,0.1);
  color: #6dc96d;
}
.btn-danger-small {
  font-size: 12px;
  padding: 3px 8px;
  border-radius: 4px;
  border: 1px solid rgba(255,80,80,0.3);
  background: rgba(255,80,80,0.08);
  color: #ff6b6b;
  cursor: pointer;
}
.mcp-form {
  margin-top: 12px;
  padding: 12px;
  background: rgba(255,255,255,0.05);
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.1);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.form-error {
  color: #ff6b6b;
  font-size: 12px;
}
.form-buttons {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
.empty-hint {
  font-size: 12px;
  opacity: 0.5;
  padding: 8px 0;
}
```

- [ ] **Step 4: 验证编译和前端构建**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat: add MCP server management UI in settings"
```

---

## Self-Review

**Spec coverage:**
- ✅ #9 支持外部 MCP 接入 — Tasks 1-6（配置持久化、连接管理、工具发现、UI 管理）
- ✅ stdio 传输 — `mcpgo.NewStdioMCPClient`
- ✅ SSE 传输 — `mcpgo.NewSSEMCPClient`
- ✅ 工具发现后注册到 agent — `initLLMComponents` 中 `mcp.LoadTools`
- ✅ 连接失败不影响启动 — `LoadTools` 内部 skip + warn

**Placeholder scan:** 无 TBD / TODO。

**Type consistency:** `mcp.ServerConfig` 在 `store.go` 定义，`app.go` 的四个 API 方法和 `client.go` 的 `LoadTools` 均接受/返回 `mcp.ServerConfig`，一致。`mcpToolAdapter` 实现了 eino `tool.BaseTool` 接口（`Info` + `InvokableRun`），与 `builtinTools` 和 `skillTools` 的类型一致，可直接 `append` 后传入 agent。

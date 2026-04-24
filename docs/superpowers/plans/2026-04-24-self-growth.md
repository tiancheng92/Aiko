# Self-Growth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Aiko agent 具备主动沉淀知识的自我成长能力——通过 Nudge 机制定期提示保存记忆、维护用户画像文件、自动生成可复用 skill 文件。

**Architecture:** 新增三个 growth 工具（`save_memory` / `update_user_profile` / `save_skill`），注入 `AllContextual`；在 `agent.go` 中追加 `turnCount` 轮次计数和 nudge 逻辑，同时在 `buildHistoryPrefix` 中读取 `~/.aiko/USER.md` 注入 context；`initLLMComponents` 将 `~/.aiko/auto-skills` 追加到 skill 加载目录。定时任务走 `ChatDirect`，不触发 nudge，完全隔离。

**Tech Stack:** Go, eino ADK, chromem-go (LongStore), SQLite, os (file I/O)

---

## File Map

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/tools/growth_tools.go` | **新建** | 三个 growth 工具实现 |
| `internal/tools/registry.go` | **修改** | `AllContextual` 增加 `longMem`/`dataDir` 参数，注册三个工具 |
| `internal/agent/agent.go` | **修改** | `turnCount`/`nudgeInterval` 字段；nudge 注入；USER.md 读取；`dataDir` 字段 |
| `internal/config/config.go` | **修改** | 新增 `NudgeInterval int` 字段，加载/保存逻辑 |
| `app.go` | **修改** | 权限注册、`AllContextual` 调用、`agent.New` 传 `dataDir`、auto-skills 目录 |

---

## Task 1: config.Config 新增 NudgeInterval

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: 在 Config 结构体中添加字段**

在 `ShortTermLimit int` 行之后添加：

```go
NudgeInterval  int      // 每隔多少轮触发一次 self-growth nudge，0 表示使用默认值 5
```

- [ ] **Step 2: 在 Load() 中解析字段**

在 `cfg.ShortTermLimit = parseInt(m["short_term_limit"], 30)` 行之后添加：

```go
cfg.NudgeInterval = parseInt(m["nudge_interval"], 5)
if cfg.NudgeInterval <= 0 {
    cfg.NudgeInterval = 5
}
```

- [ ] **Step 3: 在 Save() 的 pairs map 中持久化字段**

在 `"short_term_limit": strconv.Itoa(cfg.ShortTermLimit),` 行之后添加：

```go
"nudge_interval": strconv.Itoa(cfg.NudgeInterval),
```

- [ ] **Step 4: 编译验证**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./internal/config/...
```

Expected: 无错误输出

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add NudgeInterval field for self-growth nudge"
```

---

## Task 2: 新建 growth_tools.go（三个工具）

**Files:**
- Create: `internal/tools/growth_tools.go`

- [ ] **Step 1: 新建文件，实现 SaveMemoryTool**

```go
// internal/tools/growth_tools.go
package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/memory"
)

// SaveMemoryTool saves a single concrete fact or preference to long-term memory.
type SaveMemoryTool struct {
	LongMem *memory.LongStore
}

// Name returns the tool's stable identifier.
func (t *SaveMemoryTool) Name() string { return "save_memory" }

// Permission returns the required permission level.
func (t *SaveMemoryTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for save_memory.
func (t *SaveMemoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"保存单条具体事实、偏好或结论到长期记忆（一两句话）。不用摘要整段对话——对话历史由系统自动处理。",
		map[string]*schema.ParameterInfo{
			"content": {
				Type:     schema.String,
				Desc:     "要长期记住的具体事实、偏好或结论（一两句话）",
				Required: true,
			},
		},
	), nil
}

// InvokableRun stores the given content into the long-term memory store.
func (t *SaveMemoryTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	if t.LongMem == nil {
		return "长期记忆未启用（需配置 Embedding 模型）", nil
	}
	args := parseArgs(input)
	content, _ := args["content"].(string)
	if content == "" {
		return "请提供要保存的内容", nil
	}
	if err := t.LongMem.Store(ctx, content); err != nil {
		return "", fmt.Errorf("save memory: %w", err)
	}
	return fmt.Sprintf("已保存到长期记忆：%s", content), nil
}
```

- [ ] **Step 2: 追加 UpdateUserProfileTool**

在同文件末尾追加：

```go
// UpdateUserProfileTool updates a key-value entry in ~/.aiko/USER.md.
type UpdateUserProfileTool struct {
	DataDir string
}

// Name returns the tool's stable identifier.
func (t *UpdateUserProfileTool) Name() string { return "update_user_profile" }

// Permission returns the required permission level.
func (t *UpdateUserProfileTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for update_user_profile.
func (t *UpdateUserProfileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"更新用户画像中的某个条目（习惯、偏好、背景信息）。已存在的 key 会被覆盖，否则追加。",
		map[string]*schema.ParameterInfo{
			"key": {
				Type:     schema.String,
				Desc:     "画像条目的键名，如 preferred_language、coding_style",
				Required: true,
			},
			"value": {
				Type:     schema.String,
				Desc:     "条目的值",
				Required: true,
			},
		},
	), nil
}

// InvokableRun reads ~/.aiko/USER.md, updates or appends the key-value line, and writes back atomically.
func (t *UpdateUserProfileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	key, _ := args["key"].(string)
	value, _ := args["value"].(string)
	if key == "" {
		return "请提供 key", nil
	}

	profilePath := userProfilePath(t.DataDir)
	updated, err := upsertProfileLine(profilePath, key, value)
	if err != nil {
		return "", fmt.Errorf("update user profile: %w", err)
	}
	if updated {
		return fmt.Sprintf("已更新用户画像：%s = %s", key, value), nil
	}
	return fmt.Sprintf("已追加用户画像：%s = %s", key, value), nil
}
```

- [ ] **Step 3: 追加 SaveSkillTool**

在同文件末尾追加：

```go
// SaveSkillTool writes a reusable skill file to ~/.aiko/auto-skills/<name>/SKILL.md.
type SaveSkillTool struct {
	DataDir string
}

// Name returns the tool's stable identifier.
func (t *SaveSkillTool) Name() string { return "save_skill" }

// Permission returns the required permission level.
func (t *SaveSkillTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for save_skill.
func (t *SaveSkillTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"将当前解决的问题模式保存为可复用的技能文件。已存在的同名技能会被更新（自我改进）。",
		map[string]*schema.ParameterInfo{
			"name": {
				Type:     schema.String,
				Desc:     "技能的唯一标识名（英文小写，用连字符分隔，如 fix-go-import-cycle）",
				Required: true,
			},
			"description": {
				Type:     schema.String,
				Desc:     "技能的一句话描述",
				Required: true,
			},
			"content": {
				Type:     schema.String,
				Desc:     "技能的详细内容（Markdown 格式，说明何时使用及具体步骤）",
				Required: true,
			},
		},
	), nil
}

// InvokableRun creates or overwrites ~/.aiko/auto-skills/<name>/SKILL.md.
func (t *SaveSkillTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	content, _ := args["content"].(string)
	if name == "" {
		return "请提供技能名称", nil
	}

	skillPath, err := writeSkillFile(t.DataDir, name, description, content)
	if err != nil {
		return "", fmt.Errorf("save skill: %w", err)
	}
	return fmt.Sprintf("已保存技能文件：%s", skillPath), nil
}
```

- [ ] **Step 4: 追加 helper 函数**

在同文件末尾追加（注意添加所需 import：`os`, `path/filepath`, `strings`）：

先将文件顶部 import 块改为：

```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/memory"
)
```

然后在文件末尾追加 helper 函数：

```go
// userProfilePath returns the path to ~/.aiko/USER.md.
func userProfilePath(dataDir string) string {
	return filepath.Join(dataDir, "USER.md")
}

// upsertProfileLine reads the profile file, replaces the line starting with
// "- <key>:" if found, otherwise appends it. Returns true if the key existed.
// Writes atomically via a temp file + rename.
func upsertProfileLine(path, key, value string) (updated bool, err error) {
	existing, readErr := os.ReadFile(path)
	var lines []string
	if readErr == nil {
		lines = strings.Split(string(existing), "\n")
	}

	prefix := fmt.Sprintf("- %s:", key)
	newLine := fmt.Sprintf("- %s: %s", key, value)
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = newLine
			found = true
			break
		}
	}
	if !found {
		// Remove any trailing empty line before appending.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, newLine, "")
	}

	data := []byte(strings.Join(lines, "\n"))
	if err := atomicWrite(path, data); err != nil {
		return false, err
	}
	return found, nil
}

// writeSkillFile creates ~/.aiko/auto-skills/<name>/SKILL.md with frontmatter.
// Returns the path of the written file.
func writeSkillFile(dataDir, name, description, content string) (string, error) {
	dir := filepath.Join(dataDir, "auto-skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir auto-skills: %w", err)
	}
	skillPath := filepath.Join(dir, "SKILL.md")
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, description, content)
	if err := atomicWrite(skillPath, []byte(body)); err != nil {
		return "", err
	}
	return skillPath, nil
}

// atomicWrite writes data to path via a temp file + rename for atomicity.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: 编译验证**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./internal/tools/...
```

Expected: 无错误输出

- [ ] **Step 6: Commit**

```bash
git add internal/tools/growth_tools.go
git commit -m "feat(tools): add save_memory, update_user_profile, save_skill growth tools"
```

---

## Task 3: 注册 growth 工具到 registry.go

**Files:**
- Modify: `internal/tools/registry.go`

- [ ] **Step 1: 修改 AllContextual 签名，追加 growth 工具**

将 `AllContextual` 函数替换为：

```go
// AllContextual returns tools that require runtime dependencies injected at startup.
func AllContextual(
	permStore *PermissionStore,
	knowledgeSt *knowledge.Store,
	sched *scheduler.Scheduler,
	longMem *memory.LongStore,
	dataDir string,
) []tool.BaseTool {
	contextTools := []Tool{
		&SearchKnowledgeTool{KnowledgeSt: knowledgeSt},
		&CronTool{Scheduler: sched},
		&SaveMemoryTool{LongMem: longMem},
		&UpdateUserProfileTool{DataDir: dataDir},
		&SaveSkillTool{DataDir: dataDir},
	}
	result := make([]tool.BaseTool, len(contextTools))
	for i, t := range contextTools {
		result[i] = ToEino(t, permStore)
	}
	return result
}
```

- [ ] **Step 2: 添加 memory import**

在 import 块中添加 `"aiko/internal/memory"`：

```go
import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/knowledge"
	"aiko/internal/memory"
	"aiko/internal/scheduler"
)
```

- [ ] **Step 3: 编译验证（此时 app.go 会报错，但 tools 包本身应通过）**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./internal/tools/...
```

Expected: 无错误输出

- [ ] **Step 4: Commit**

```bash
git add internal/tools/registry.go
git commit -m "feat(tools): register growth tools in AllContextual"
```

---

## Task 4: 修改 agent.go（turnCount、nudge、USER.md、dataDir）

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: 在 Agent 结构体中添加新字段**

将 `Agent` 结构体替换为：

```go
// Agent wraps an eino ReAct agent with short/long-term memory integration.
type Agent struct {
	runner        *adk.Runner
	shortMem      *memory.ShortStore
	longMem       *memory.LongStore
	cfg           *config.Config
	dataDir       string // ~/.aiko 数据目录，用于读取 USER.md
	turnCount     int    // 已完成的对话轮次（重启从 0 开始）
	nudgeInterval int    // 每隔多少轮触发 self-growth nudge
}
```

- [ ] **Step 2: 修改 New 函数签名，传入 dataDir**

将 `New` 函数签名的参数列表中，在 `skillMW adk.ChatModelAgentMiddleware,` 之后添加 `dataDir string,`：

```go
func New(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	shortMem *memory.ShortStore,
	longMem *memory.LongStore,
	tools []tool.BaseTool,
	cfg *config.Config,
	mw middleware.Middleware,
	skillMW adk.ChatModelAgentMiddleware,
	dataDir string,
) (*Agent, error) {
```

- [ ] **Step 3: 修改 New 函数末尾的 return，设置新字段**

将 return 语句替换为：

```go
ni := cfg.NudgeInterval
if ni <= 0 {
    ni = 5
}
return &Agent{
    runner:        runner,
    shortMem:      shortMem,
    longMem:       longMem,
    cfg:           cfg,
    dataDir:       dataDir,
    nudgeInterval: ni,
}, nil
```

- [ ] **Step 4: 在 persistAndMigrate 末尾递增 turnCount**

在 `persistAndMigrate` 函数的最后一个 `if err := a.shortMem.DeleteByIDs(ids); ...` 块之后，以及在函数末尾之前，追加：

```go
a.turnCount++
```

注意：`turnCount` 在 goroutine 中递增，但 `persistAndMigrate` 本身已经是异步调用（`go a.persistAndMigrate(...)`），且 `turnCount` 只在单个 goroutine（每次 Chat 调用的尾部 goroutine）中递增，不存在并发写，无需加锁。

- [ ] **Step 5: 修改 buildHistoryPrefix，注入 USER.md 和 nudge**

将 `buildHistoryPrefix` 整个函数替换为：

```go
// buildHistoryPrefix returns recent conversation history as a formatted string,
// prepended with USER.md profile (if available) and a self-growth nudge (if due).
// Returns empty string if no history exists or an error occurs.
// userInput is used as the semantic query for long-term memory retrieval.
func (a *Agent) buildHistoryPrefix(ctx context.Context, userInput string) (string, error) {
	// Read USER.md for user profile injection.
	var profileSection string
	if a.dataDir != "" {
		profilePath := filepath.Join(a.dataDir, "USER.md")
		if data, err := os.ReadFile(profilePath); err == nil && len(data) > 0 {
			profileSection = "User Profile:\n" + string(data) + "\n"
		} else if err != nil && !os.IsNotExist(err) {
			slog.Warn("read USER.md failed", "err", err)
		}
	}

	if a.shortMem == nil {
		return profileSection, nil
	}

	// Inject relevant long-term memories if available.
	var longMemContext string
	if a.longMem != nil {
		results, err := a.longMem.Search(ctx, userInput, 3)
		if err == nil && len(results) > 0 {
			var lmb strings.Builder
			lmb.WriteString("Relevant past context:\n")
			for _, r := range results {
				lmb.WriteString(r)
				lmb.WriteByte('\n')
			}
			longMemContext = lmb.String()
		}
	}

	recent, err := a.shortMem.Recent(a.cfg.ShortTermLimit)
	if err != nil {
		slog.Warn("short memory Recent error", "err", err)
		recent = nil
	}

	// Assemble history section.
	var histSection string
	if len(recent) > 0 {
		histStr := memory.FormatBlock(recent)
		if longMemContext != "" {
			histSection = longMemContext + "\nRecent conversation:\n" + histStr
		} else {
			histSection = "Recent conversation:\n" + histStr
		}
	} else if longMemContext != "" {
		histSection = longMemContext
	}

	// Append self-growth nudge if due.
	var nudgeSection string
	if a.nudgeInterval > 0 && a.turnCount > 0 && a.turnCount%a.nudgeInterval == 0 {
		nudgeSection = `
[SELF-GROWTH NUDGE]
请在本次回复前，回顾刚才的对话，考虑是否需要：
1. 调用 save_memory 保存一条具体事实或偏好（一两句话，不需要摘要对话）
2. 调用 update_user_profile 更新用户画像（发现了新的习惯/偏好/背景信息）
3. 调用 save_skill 将本次解决的问题模式提炼为可复用技能
如果都不需要，直接回复即可，无需解释。
`
	}

	result := profileSection + histSection + nudgeSection
	return result, nil
}
```

- [ ] **Step 6: 在文件顶部 import 中添加 os 和 path/filepath**

将 import 块更新为：

```go
import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	localbk "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/agent/middleware"
	"aiko/internal/config"
	"aiko/internal/memory"
)
```

- [ ] **Step 7: 编译验证（此时 app.go 还报错，agent 包本身应通过）**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./internal/agent/...
```

Expected: 无错误输出

- [ ] **Step 8: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): add turnCount nudge, USER.md injection, and dataDir support"
```

---

## Task 5: 修改 app.go（权限注册 + AllContextual + agent.New + skillsDirs）

**Files:**
- Modify: `app.go`

- [ ] **Step 1: 注册三个新工具的权限行**

在 `startup` 函数中，找到以下代码块：

```go
for _, t := range []internaltools.Tool{
    &internaltools.SearchKnowledgeTool{},
    &internaltools.CronTool{},
} {
    _ = a.permStore.EnsureRow(toolsCtx, t)
}
```

替换为：

```go
for _, t := range []internaltools.Tool{
    &internaltools.SearchKnowledgeTool{},
    &internaltools.CronTool{},
    &internaltools.SaveMemoryTool{},
    &internaltools.UpdateUserProfileTool{},
    &internaltools.SaveSkillTool{},
} {
    _ = a.permStore.EnsureRow(toolsCtx, t)
}
```

- [ ] **Step 2: 修改 AllContextual 调用，传入 longMem 和 dataDir**

在 `initLLMComponents` 中，找到：

```go
contextTools := internaltools.AllContextual(a.permStore, knowledgeSt, sched)
```

替换为：

```go
contextTools := internaltools.AllContextual(a.permStore, knowledgeSt, sched, longMem, dataDir)
```

注意：`dataDir` 变量需要在 `initLLMComponents` 中可用。在函数开头添加：

```go
home, err := os.UserHomeDir()
if err != nil {
    return fmt.Errorf("get home dir: %w", err)
}
dataDir := filepath.Join(home, ".aiko")
```

- [ ] **Step 3: 修改 agent.New 调用，传入 dataDir**

找到：

```go
newAgent, err := agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW)
```

替换为：

```go
newAgent, err := agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW, dataDir)
```

- [ ] **Step 4: 追加 auto-skills 到 skillsDirs**

找到：

```go
skillMW, err := skill.NewMiddleware(ctx, a.cfg.SkillsDirs)
```

替换为：

```go
autoSkillsDir := filepath.Join(dataDir, "auto-skills")
skillDirs := append(append([]string{}, a.cfg.SkillsDirs...), autoSkillsDir)
skillMW, err := skill.NewMiddleware(ctx, skillDirs)
```

（用 `append([]string{}, ...)` 避免修改原 slice）

- [ ] **Step 5: 确认 os/filepath import 已在 app.go 中存在**

```bash
grep '"os"\|"path/filepath"' /Users/xutiancheng/code/self/Aiko/app.go
```

Expected: 两行都能匹配到（app.go 已有这两个 import）

- [ ] **Step 6: 全量编译**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: 无错误输出

- [ ] **Step 7: Commit**

```bash
git add app.go
git commit -m "feat(app): wire growth tools, dataDir to agent, auto-skills skill dir"
```

---

## Task 6: 集成验证

**Files:** 无新文件，验证整体功能

- [ ] **Step 1: 启动开发模式**

```bash
cd /Users/xutiancheng/code/self/Aiko && wails dev
```

Expected: 应用正常启动，无 panic，日志无 ERROR

- [ ] **Step 2: 验证工具权限行已创建**

启动后检查 SQLite：

```bash
sqlite3 ~/.aiko/aiko.db "SELECT tool_name, granted FROM tool_permissions WHERE tool_name IN ('save_memory','update_user_profile','save_skill');"
```

Expected:
```
save_memory|0
save_skill|0
update_user_profile|0
```

（三行均存在，默认未授权；因为是 PermPublic，权限检查会直接放行）

- [ ] **Step 3: 验证 PermPublic 工具无需授权即可运行**

查看 `PermissionStore.IsGranted` 逻辑——public 工具直接返回 true，无需 DB 行为 granted=1。确认代码：

```bash
grep -A 10 "func.*IsGranted" /Users/xutiancheng/code/self/Aiko/internal/tools/permission.go
```

Expected: 应有 `PermPublic` 直接返回 true 的逻辑

- [ ] **Step 4: 手动触发 nudge 验证（调低 NudgeInterval）**

通过设置界面（或直接 SQL）将 nudge_interval 设为 1：

```bash
sqlite3 ~/.aiko/aiko.db "INSERT INTO settings(key,value) VALUES('nudge_interval','1') ON CONFLICT(key) DO UPDATE SET value='1';"
```

重启应用，发两条消息后，第二条回复前应触发 nudge（agent 可能调用 growth 工具或直接回复）。

- [ ] **Step 5: 验证 save_skill 写文件**

在聊天中输入：
```
请调用 save_skill 工具，保存一个测试技能，name=test-skill，description=测试技能，content=这是测试内容
```

Expected:
```bash
ls ~/.aiko/auto-skills/test-skill/SKILL.md
cat ~/.aiko/auto-skills/test-skill/SKILL.md
```

应看到 frontmatter + 内容

- [ ] **Step 6: 验证 update_user_profile 写文件**

在聊天中输入：
```
请调用 update_user_profile 工具，key=preferred_language，value=Go
```

Expected:
```bash
cat ~/.aiko/USER.md
```

应看到 `- preferred_language: Go`

- [ ] **Step 7: 恢复 nudge_interval**

```bash
sqlite3 ~/.aiko/aiko.db "UPDATE settings SET value='5' WHERE key='nudge_interval';"
```

- [ ] **Step 8: 最终 Commit**

```bash
git add -p  # 确认无意外变更
git commit -m "chore: verify self-growth integration complete"
```

---

## 变更摘要

| 特性 | 实现方式 |
|------|---------|
| 记忆整合 Nudge | `agent.turnCount % nudgeInterval == 0` 时在 history prefix 末尾追加提示 |
| 用户画像 USER.md | `buildHistoryPrefix` 读取 `~/.aiko/USER.md` 注入 context 前缀 |
| Skill 自动生成 | `save_skill` 写 `~/.aiko/auto-skills/<name>/SKILL.md`，auto-skills 目录加入 skillsDirs |
| 定时任务隔离 | `ChatDirect` 不调用 `persistAndMigrate`，`turnCount` 不递增，nudge 不触发 |

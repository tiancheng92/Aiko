# Aiko Self-Growth Design Spec

**Date**: 2026-04-24  
**Status**: Approved  
**Scope**: 三个优先级由高到低的自我成长特性，参考 hermes-agent 的 learning loop 设计

---

## 1. 背景与目标

当前 Aiko 每次会话结束后信息即丢失（仅自动迁移原始对话块到向量记忆）。目标是让 agent 具备主动沉淀知识的能力：

1. **记忆整合 Nudge**（高）：定期提示 agent 主动保存具体事实/偏好
2. **用户画像 USER.md**（高）：跨会话积累用户个性化信息并注入 context
3. **Skill 自动生成**（中）：agent 将可复用解法提炼为 skill 文件

---

## 2. 架构总览

```
internal/tools/
  growth_tools.go        # 新增：save_memory / update_user_profile / save_skill

internal/agent/agent.go  # 修改：turnCount 字段；nudge 注入；USER.md 读取注入

internal/config/config.go # 修改：添加 NudgeInterval int 字段（默认 5）

internal/db/sqlite.go    # 无变更（复用现有表）

app.go                   # 修改：注册新工具权限行；AllContextual 传入 growth 依赖；
                         #        initLLMComponents 追加 auto-skills 到 skillsDirs
```

**数据目录**：
- `~/.aiko/USER.md` — 用户画像文件
- `~/.aiko/auto-skills/<name>/SKILL.md` — agent 自动生成的 skill 文件

---

## 3. 三个新工具（internal/tools/growth_tools.go）

### 3.1 save_memory

| 字段 | 值 |
|------|-----|
| name | `save_memory` |
| permission | `PermPublic` |
| param | `content string` (required) |
| description | 保存单条具体事实、偏好或结论（一两句话）。不用摘要整段对话——对话历史由系统自动处理。 |

**实现**：调用注入的 `*memory.LongStore`，`Store(ctx, content)`。

### 3.2 update_user_profile

| 字段 | 值 |
|------|-----|
| name | `update_user_profile` |
| permission | `PermPublic` |
| params | `key string` (required), `value string` (required) |
| description | 更新用户画像中的某个条目（习惯、偏好、背景信息）。已存在的 key 会被覆盖，否则追加。 |

**实现**：
- 读取 `~/.aiko/USER.md`（不存在则视为空）
- 逐行扫描，匹配 `- <key>:` 前缀
  - 找到则替换该行为 `- <key>: <value>`
  - 未找到则追加 `- <key>: <value>\n`
- 原子写回文件

### 3.3 save_skill

| 字段 | 值 |
|------|-----|
| name | `save_skill` |
| permission | `PermPublic` |
| params | `name string` (required), `description string` (required), `content string` (required) |
| description | 将当前解决的问题模式保存为可复用的技能文件。已存在的同名技能会被更新（自我改进）。 |

**实现**：
- `dir = ~/.aiko/auto-skills/<name>/`，`os.MkdirAll`
- 写 `dir/SKILL.md`，格式：
  ```markdown
  ---
  name: <name>
  description: <description>
  ---

  <content>
  ```
- 返回确认消息，包含文件路径

**依赖注入**：三个工具通过 `GrowthTools` 结构体持有 `*memory.LongStore` 和 `dataDir string`，通过 `AllContextual` 传入。

---

## 4. Agent Nudge 机制（internal/agent/agent.go）

### 4.1 turn 计数

`Agent` 结构体新增字段：
```go
turnCount     int   // 已完成的对话轮次，每次 persistAndMigrate 后递增
nudgeInterval int   // 从 cfg.NudgeInterval 读取，默认 5
```

`persistAndMigrate` 末尾：`a.turnCount++`

### 4.2 nudge 注入位置

在 `buildHistoryPrefix` 返回前，当 `a.turnCount > 0 && a.turnCount % a.nudgeInterval == 0` 时，在结果末尾追加：

```
[SELF-GROWTH NUDGE]
请在本次回复前，回顾刚才的对话，考虑是否需要：
1. 调用 save_memory 保存一条具体事实或偏好（一两句话，不需要摘要对话）
2. 调用 update_user_profile 更新用户画像（发现了新的习惯/偏好/背景信息）
3. 调用 save_skill 将本次解决的问题模式提炼为可复用技能
如果都不需要，直接回复即可，无需解释。
```

nudge 在 history 上下文内发出，agent 有完整对话背景来判断。

> **注意**：定时任务通过 `ChatDirect` 触发，`persistAndMigrate` 不被调用，`turnCount` 不递增，nudge 不触发，USER.md 不注入。定时任务对话完全游离于自我成长流程之外，这是有意为之。

### 4.3 USER.md 注入

在 `buildHistoryPrefix` 开头读取 `~/.aiko/USER.md`：
- 文件不存在 → 跳过
- 读取失败 → `slog.Warn`，跳过，不中断
- 有内容 → 在返回的 prefix 最前面插入：
  ```
  User Profile:
  <内容>
  
  ```

`dataDir` 通过 `Agent` 新增字段 `dataDir string` 注入（从 `cfg` 或构造参数传入）。

---

## 5. config.Config 变更

`internal/config/config.go` 新增字段：
```go
NudgeInterval int `json:"nudge_interval"` // 默认 0，读取时若 <= 0 则用 5
```

加载时：`if cfg.NudgeInterval <= 0 { cfg.NudgeInterval = 5 }`

---

## 6. app.go 变更

### 6.1 startup — 注册新工具权限行

新工具加入 `EnsureRow` 循环（类似 `SearchKnowledgeTool`）：
```go
&internaltools.SaveMemoryTool{}
&internaltools.UpdateUserProfileTool{}
&internaltools.SaveSkillTool{}
```

### 6.2 AllContextual — 传入 growth 工具

`AllContextual` 签名增加 `longMem *memory.LongStore, dataDir string`，追加三个 growth 工具实例。

### 6.3 initLLMComponents — 追加 auto-skills 目录

```go
skillsDirs = append(skillsDirs, filepath.Join(dataDir, "auto-skills"))
```

### 6.4 Agent.New — 传入 dataDir

`agent.New` 增加 `dataDir string` 参数，存入 `Agent.dataDir`。

---

## 7. 错误处理

- `save_memory` 失败：工具返回错误字符串，不 panic
- `update_user_profile` 写文件失败：返回错误字符串，原文件不被破坏（先写 tmp 再 rename）
- `save_skill` 写文件失败：返回错误字符串
- USER.md 读取失败：slog.Warn，静默跳过，不影响对话
- nudge 注入：仅追加字符串，不可能失败

---

## 8. 不在本次范围内

- FTS5 全文搜索历史会话（低优先级，单独迭代）
- RL 轨迹训练（长期，跳过）
- 前端设置页面暴露 NudgeInterval（可后续迭代）

---

## 9. 文件变更清单

| 文件 | 变更类型 |
|------|---------|
| `internal/tools/growth_tools.go` | **新建** |
| `internal/tools/registry.go` | **修改**：AllContextual 增加参数和工具 |
| `internal/agent/agent.go` | **修改**：turnCount、nudge、USER.md 注入、dataDir |
| `internal/config/config.go` | **修改**：NudgeInterval 字段 |
| `app.go` | **修改**：权限注册、AllContextual 调用、skillsDirs |

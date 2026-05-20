# Tool Use Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 system prompt 中注入强制性工具使用策略，减少 LLM 跳过工具调用或复用历史工具结果导致的幻觉。

**Architecture:** 在 `internal/agent/agent.go` 的 `buildAgentRunner` 函数中，新增一个 `toolPolicyPrompt` 常量，拼接到 `cfg.SystemPrompt` 和 `emotionPromptSuffix` 之间。常量按 4 类工具（实时数据、用户存储数据、文件与网络内容、执行与写入）分组列出强制规则，并加一条禁止复用历史工具结果的通用规则。

**Tech Stack:** Go 1.26、eino ReAct Agent、`internal/agent/agent.go`

---

### Task 1: 新增 toolPolicyPrompt 常量并修改拼接逻辑

**Files:**
- Modify: `internal/agent/agent.go:26-28`（在 `emotionPromptSuffix` 常量之后新增 `toolPolicyPrompt`）
- Modify: `internal/agent/agent.go:146`（修改 `systemPrompt` 拼接）

- [ ] **Step 1: 在 `emotionPromptSuffix` 常量之后新增 `toolPolicyPrompt` 常量**

在 [internal/agent/agent.go](internal/agent/agent.go) 第 28 行（`emotionPromptSuffix` 常量末尾）之后插入：

```go
// toolPolicyPrompt is injected between the user-configured system prompt and the
// emotion suffix to enforce strict tool-call discipline and reduce hallucinations.
const toolPolicyPrompt = `

【工具使用强制规则】
以下规则优先级高于一切。违反即为错误，任何情况下都不得绕过。

1. 实时数据 — 下列数据随时变化，每次必须调用对应工具获取最新值，禁止凭记忆或推测回答：
   get_current_time / get_timezone / get_system_stats / get_network_status /
   get_location / get_weather / get_exchange_rate / get_browser_url /
   read_clipboard / take_screenshot

2. 用户存储数据 — 下列数据存储在用户系统或应用中，内容在调用前未知，禁止臆测，必须调用工具读取后才能引用：
   get_reminders / get_mails / get_mail_content / get_calendar_events /
   list_running_apps / get_os_info / get_hardware_info /
   search_memory / list_skills / search_knowledge

3. 文件与网络内容 — 下列内容在获取前完全未知，不得引用未经工具读取过的内容：
   list_directory / read_file / read_image / web_search / web_fetch
   （读取文件前先用 list_directory 确认路径存在）

4. 执行与写入 — 结果不可预测，必须实际调用工具执行后，依据工具返回的真实输出进行报告，禁止提前猜测或伪造结果：
   execute_shell / execute_code / write_file / delete_file / move_file /
   make_directory / write_clipboard / control_app / create_calendar_event /
   complete_reminder / cron / save_memory / save_skill / update_user_profile /
   save_image / check_and_update

5. 禁止复用历史工具结果 — 对话历史中出现的工具调用结果是过去某一时刻的快照，不代表当前状态。
   每轮对话如需工具数据，必须重新调用工具；严禁直接引用或复用历史消息中的工具返回值作为当前答案。

通用原则：工具调用失败时，如实报告错误原因，不得补充推断或虚构结果。`
```

- [ ] **Step 2: 修改 `buildAgentRunner` 中的 `systemPrompt` 拼接**

在 [internal/agent/agent.go:146](internal/agent/agent.go#L146) 找到：

```go
systemPrompt := cfg.SystemPrompt + emotionPromptSuffix
```

替换为：

```go
systemPrompt := cfg.SystemPrompt + toolPolicyPrompt + emotionPromptSuffix
```

- [ ] **Step 3: 编译验证**

```bash
go build ./...
```

预期：无编译错误，无输出。

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): inject tool-use policy prompt to reduce hallucinations"
```

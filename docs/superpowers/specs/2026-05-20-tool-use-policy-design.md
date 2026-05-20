# Tool Use Policy — 减少 LLM 幻觉设计文档

**日期**：2026-05-20  
**问题**：Agent 应该调用工具时不调用，改从对话历史或历史工具结果中伪造答案  
**方案**：在 system prompt 中注入强制性工具使用策略（Approach A）

---

## 背景

Aiko 的 eino ReAct Agent 存在以下两种幻觉模式：

1. **跳过工具调用**：对需要实时数据或执行操作的问题，直接从上下文记忆中推断答案
2. **复用历史工具结果**：看到短期记忆中存有前几轮的工具返回值，将其当作当前结果直接复用，表面上像调用了工具，实际没有

这两种模式都会产生虚假但看起来合理的回答。

---

## 设计决策

**不修改架构**，仅在 `agent.go` 的 `buildAgentRunner` 中注入一段强约束 prompt。

拼接顺序从：
```
cfg.SystemPrompt + emotionPromptSuffix
```
改为：
```
cfg.SystemPrompt + toolPolicyPrompt + emotionPromptSuffix
```

---

## toolPolicyPrompt 内容结构

共 5 条规则，按工具类别组织：

### 规则 1：实时数据类

**适用工具**：`get_current_time` / `get_timezone` / `get_system_stats` / `get_network_status` / `get_location` / `get_weather` / `get_exchange_rate` / `get_browser_url` / `read_clipboard`

**规则**：上述数据随时变化，禁止凭记忆或推测回答，每次必须调用对应工具获取最新值。

### 规则 2：用户数据类

**适用工具**：`get_reminders` / `get_mails` / `get_mail_content` / `get_calendar_events` / `list_running_apps` / `get_os_info` / `get_hardware_info`

**规则**：上述数据存储在用户系统中，内容未知，禁止臆测，必须调用工具读取后才能引用。

### 规则 3：文件与知识类

**适用工具**：`list_directory` / `read_file` / `read_image` / `search_knowledge` / `search_memory` / `list_skills`

**规则**：文件或知识库的内容在读取之前完全未知，不得引用未经工具读取过的文件内容或知识条目。读取文件前先用 `list_directory` 确认路径存在。

### 规则 4：执行结果类

**适用工具**：`execute_shell` / `execute_code` / `write_file` / `delete_file` / `move_file` / `make_directory`

**规则**：执行结果不可预测，禁止在调用工具前猜测或伪造结果。必须实际执行后，依据工具返回的真实输出进行报告。

### 规则 5：历史工具结果禁止复用

**规则**：对话历史中出现的工具调用结果是过去某一时刻的快照，不代表当前状态。每轮对话如需工具数据，必须重新调用工具，严禁直接复用或参考历史消息中的工具返回值作为当前答案。

### 通用原则

工具调用失败时，如实报告错误原因，不得补充推断或虚构结果。

---

## 改动范围

| 文件 | 改动内容 |
|------|---------|
| `internal/agent/agent.go` | 新增 `toolPolicyPrompt` 常量；修改 `buildAgentRunner` 中的 `systemPrompt` 拼接逻辑 |

其余文件（工具文件、memory、context.go 等）**不改动**。

---

## 局限性

- 依赖模型遵守 prompt 约束，强模型（GPT-4o、Claude 3.5+）效果好，弱模型可能忽略
- 不能从物理上阻止模型"看到"历史工具结果（思路 Y 可做到，但超出本次范围）
- 若幻觉问题持续，后续可叠加思路 Y（对实时数据类工具结果在历史中脱敏）

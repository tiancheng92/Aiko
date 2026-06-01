# Claude Code → Aiko 宠物状态同步

**日期**: 2026-06-01
**状态**: 设计完成

## 概述

Aiko 通过内置 HTTP server 接收 Claude Code hook 事件，同步宠物状态：Claude 思考时宠物进入 thinking 状态，Claude 完成时宠物恢复 idle 并弹出气泡通知。

## 架构

```
Claude Code (终端)                    Aiko (桌面宠物)
─────────────                        ──────────────
PreToolUse hook ──HTTP POST──→  POST /event  →  pet thinking (debounce 3s)
Stop hook        ──HTTP POST──→  POST /event  →  pet idle + notification bubble
```

- Claude Code 通过 `http` 类型 hook 向 Aiko 发送事件
- Aiko 内置轻量 HTTP server，仅监听 `127.0.0.1`（不暴露到网络）
- 用户在 Aiko 设置界面启用功能并配置端口，复制 hook 配置到 `~/.claude/settings.json`

## 组件

### 1. HTTP Server — `internal/claudecco/server.go`

- 监听 `127.0.0.1:<port>`，单 endpoint `POST /event`
- 接收 JSON body：

```json
// PreToolUse 触发
{ "event": "thinking" }

// Stop 触发
{ "event": "done", "summary": "Claude Code 已完成" }
```

- `thinking` → `EventsEmit("pet:state:change", "thinking")`，带 3 秒 debounce（连续 PreToolUse 不反复切换状态）
- `done` → `EventsEmit("pet:state:change", "idle")` + `EventsEmit("notification:show", {title: "Claude Code", message: summary, durationSecs: cfg.ClaudeCodeNotificationSecs})`
- 所有收到的事件以 debug 级别日志输出（`log.Debug().Str("event", ...).Msg("claudecco: received event")`）
- 未知 event 返回 400，method 非 POST 返回 405
- Graceful shutdown 通过 context 控制

### 2. 配置扩展 — `internal/config/config.go`

Config 结构体新增字段：

```go
ClaudeCodeEnabled          bool `json:"claude_code_enabled"`           // 默认 false
ClaudeCodePort             int  `json:"claude_code_port"`              // 默认 9876
ClaudeCodeNotificationSecs int  `json:"claude_code_notification_secs"` // 默认 15，气泡持续时间（秒）
```

对应数据库迁移新增列。

### 3. 生命周期集成 — `app.go`

- `startup()`: 若 `ClaudeCodeEnabled` 为 true，启动 HTTP server
- `shutdown()`: 停止 HTTP server
- `SaveConfig()` 中检测 `ClaudeCodeEnabled` / `ClaudeCodePort` 变更 → 重启 server

### 4. 设置界面 — `SettingsWindow.vue`

新增 "Claude Code" 标签页或区块，包含：
- 开关：启用/禁用状态同步
- 端口号输入框（默认 9876）
- 气泡持续时间输入框（默认 15 秒）
- **Hook 配置展示区**：动态生成 JSON 片段（填入当前端口号），用户可复制到 `~/.claude/settings.json`

生成的 hook 配置示例（端口 9876）：

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "http",
            "url": "http://127.0.0.1:9876/event",
            "method": "POST",
            "body": "{\"event\":\"thinking\"}"
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "http",
            "url": "http://127.0.0.1:9876/event",
            "method": "POST",
            "body": "{\"event\":\"done\",\"summary\":\"Claude Code 已完成\"}"
          }
        ]
      }
    ]
  }
}
```

- 展示区包含"复制"按钮，点击复制到剪贴板
- 提示文字说明：将此配置合并到 `~/.claude/settings.json` 的 `"hooks"` 字段中

### 5. 国际化 — `locales/*.json`

新增 key（zh-CN）：

```json
{
  "claudeCode": {
    "title": "Claude Code 同步",
    "enabled": "启用状态同步",
    "port": "监听端口",
    "notificationSecs": "气泡持续时间（秒）",
    "hookConfig": "Hook 配置",
    "hookConfigHint": "将以下配置合并到 ~/.claude/settings.json 的 \"hooks\" 字段中",
    "copied": "已复制到剪贴板"
  }
}
```

## 数据流

### thinking 流程

```
Claude Code 执行工具
  → PreToolUse hook 触发
  → HTTP POST {"event":"thinking"} → Aiko :9876/event
  → debounce 3s: 若已有 pending timer 则重置
  → EventsEmit("pet:state:change", "thinking")
  → usePetState 更新 petState = "thinking"
  → Live2DPet / VRMPet 播放 thinking 动画
```

### done 流程

```
Claude Code 完成回复
  → Stop hook 触发
  → HTTP POST {"event":"done","summary":"..."} → Aiko :9876/event
  → 取消 pending thinking debounce
  → EventsEmit("pet:state:change", "idle")
  → EventsEmit("notification:show", {title:"Claude Code", message:"...", durationSecs: N})
  → NotificationBubble 显示气泡（N 秒后自动消失，N 来自配置）
```

## 边界情况

- **端口冲突**: 启动 HTTP server 失败时记录 warn 日志，不阻断主流程
- **重复 thinking**: debounce 3 秒，连续工具调用只在第一次触发状态切换
- **快速 done**: 若 done 到达时 debounce 尚未触发（工具在 3 秒内完成），取消 pending debounce，状态不变
- **禁用后**: 关闭 HTTP server，但用户需自行清理 hook 配置（提示文字说明）
- **未启用时接收请求**: server 不启动，连接被拒绝 → Claude Code hook 静默失败（无影响）
- **通知更新**: 新 done 事件到达时，替换当前气泡内容并重置计时器（不叠加多个气泡）

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/claudecco/server.go` | 新增 | HTTP server 实现 |
| `internal/config/config.go` | 修改 | 新增 `ClaudeCodeEnabled`、`ClaudeCodePort`、`ClaudeCodeNotificationSecs` |
| `internal/db/migrations.go` | 修改 | 新字段 schema 迁移 |
| `app.go` | 修改 | startup/shutdown 管理 server 生命周期 |
| `app_config.go` | 修改 | SaveConfig 中检测并重启 server |
| `frontend/src/components/SettingsWindow.vue` | 修改 | 新增 Claude Code 设置区块 |
| `frontend/src/components/NotificationBubble.vue` | 修改 | 支持 `durationSecs` 字段；新通知替换当前内容并重置计时器，不叠加 |
| `frontend/src/locales/zh-CN.json` | 修改 | 新增中文文案 |
| `frontend/src/locales/en.json` | 修改 | 新增英文文案 |
| `frontend/src/locales/ja.json` | 修改 | 新增日文文案 |
| `frontend/src/locales/ko.json` | 修改 | 新增韩文文案 |

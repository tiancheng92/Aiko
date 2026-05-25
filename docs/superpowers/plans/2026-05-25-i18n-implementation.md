# Multi-Language UI Support — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add i18n support (zh-CN / en / ja / ko) to the Aiko frontend using vue-i18n, with language preference persisted in Go config.

**Architecture:** vue-i18n with JSON locale files per language. Language detection runs in `main.js` bootstrap — check `config.Language` first, fall back to `navigator.language`, fall back to `en`. Manual override via SettingsWindow dropdown writes to `SaveConfig({Language})`. Go `Config` struct gets a `Language string` field; DB migration adds the column.

**Tech Stack:** vue-i18n (Vue 3), Go config + SQLite settings table, Wails bindings

---

### Task 1: Add Language field to Go Config struct

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add Language field to Config struct and Load/Save methods**

In `internal/config/config.go`:

Add field to Config struct (after `RenderBackend` or at end of fields):

```go
// Language is the UI language preference. Empty string means "follow system".
// Valid values: "zh-CN", "en", "ja", "ko".
Language string
```

In `Load()`, add to the `m` map parsing (after `render_backend` line):

```go
cfg.Language = m["language"]
```

In `Save()`, add to the `pairs` map:

```go
"language": cfg.Language,
```

- [ ] **Step 2: Add test for Language round-trip and default**

In `internal/config/config_test.go`, add two test functions:

```go
// TestConfigLanguage_RoundTrip tests that Language round-trips through Save/Load.
func TestConfigLanguage_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	cfg := &config.Config{Language: "ja"}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Language != "ja" {
		t.Errorf("Language: got %q, want %q", loaded.Language, "ja")
	}
}

// TestConfigLanguage_Default tests that Language defaults to empty string.
func TestConfigLanguage_Default(t *testing.T) {
	db := newTestDB(t)
	store := config.NewStore(db)

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Language != "" {
		t.Errorf("Language default: got %q, want %q", loaded.Language, "")
	}
}
```

- [ ] **Step 3: Run Go tests to verify round-trip**

```bash
go test ./internal/config/ -v -run "TestConfigLanguage"
```

Expected: PASS for both tests.

- [ ] **Step 4: Run all existing config tests to verify no regressions**

```bash
go test ./internal/config/ -v
```

Expected: ALL PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add Language field for UI i18n preference"
```

---

### Task 2: Add DB migration for language column

**Files:**
- Modify: `internal/db/sqlite.go`

- [ ] **Step 1: Add language column patch**

In `internal/db/sqlite.go`, add to the `patches` slice (before the closing `}` of the slice):

```go
// v10: UI language preference (empty = follow system).
`ALTER TABLE settings ADD COLUMN language TEXT NOT NULL DEFAULT ''`,
```

Update the comment above the existing last patch (v9: model_profiles supports_vision) to indicate v10 follows.

- [ ] **Step 2: Build to verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Run Go tests**

```bash
go test ./internal/db/ -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/db/sqlite.go
git commit -m "feat(db): add language column migration for i18n support"
```

---

### Task 3: Install vue-i18n dependency

**Files:**
- Modify: `frontend/package.json` (via yarn)
- Modify: `frontend/yarn.lock` (via yarn)

- [ ] **Step 1: Install vue-i18n**

```bash
cd frontend && yarn add vue-i18n
```

- [ ] **Step 2: Verify installation**

```bash
node -e "require('vue-i18n')" 2>/dev/null && echo "OK" || cd frontend && node -e "require('vue-i18n')" && echo "OK"
```

Expected: "OK" printed.

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json frontend/yarn.lock
git commit -m "chore(frontend): add vue-i18n dependency"
```

---

### Task 4: Create locale JSON files

**Files:**
- Create: `frontend/src/locales/zh-CN.json`
- Create: `frontend/src/locales/en.json`
- Create: `frontend/src/locales/ja.json`
- Create: `frontend/src/locales/ko.json`

- [ ] **Step 1: Create zh-CN.json (source of truth — extracted from existing Chinese text)**

```json
{
  "settings": {
    "title": "设置",
    "tabs": {
      "general": "通用",
      "appearance": "外观",
      "model": "模型",
      "ai": "对话",
      "tools": "工具",
      "knowledge": "知识库",
      "automation": "自动化",
      "lark": "飞书",
      "sms": "短信",
      "about": "关于"
    },
    "language": {
      "label": "语言",
      "followSystem": "跟随系统"
    },
    "general": {
      "system": "系统",
      "autoLaunch": "登录时自动启动",
      "autoLaunchDesc": "开机登录 macOS 后自动运行 Aiko",
      "theme": "界面主题",
      "style": "风格",
      "styleLiquidGlass": "液态玻璃",
      "styleFrosted": "毛玻璃",
      "styleDesc": "液态玻璃：近透明折射光影；毛玻璃：经典深色质感",
      "shortcuts": "快捷键",
      "shortcutOptionDouble": "双击 Option — 显示 / 隐藏聊天框",
      "shortcutOptionHold": "按住 Option 1 秒 — 开始语音输入",
      "shortcutOptionRelease": "松开 Option — 停止录音，等待识别",
      "shortcutEnter": "发送消息",
      "shortcutCmdEnter": "消息框内换行",
      "shortcutCmdV": "粘贴图片到消息框（支持截图直接粘贴）",
      "shortcutDrag": "拖动悬浮球 — 重新定位桌宠",
      "shortcutRightClick": "右键聊天框 — 导出记录、清空历史、打开设置"
    },
    "appearance": {
      "petModel": "桌宠模型",
      "renderMode": "渲染模式",
      "vrmModel": "VRM 模型",
      "vrmDelete": "删除",
      "vrmImport": "导入 VRM 文件",
      "vrmUserImported": "用户导入",
      "vrmBuiltin": "内置",
      "vrmDeleteModel": "删除此模型",
      "petSize": "宠物尺寸",
      "petSizeDesc": "像素大小，0 表示自动",
      "chatSize": "聊天框尺寸",
      "chatWidth": "宽度",
      "chatHeight": "高度",
      "resetBall": "重置悬浮球位置",
      "resetBallDesc": "将悬浮球移回默认位置",
      "voice": "语音与音效",
      "voiceAutoSend": "语音识别后自动发送",
      "soundsEnabled": "聊天音效",
      "ttsAutoPlay": "AI 回复后自动朗读",
      "ttsSummarizeThreshold": "摘要字数阈值",
      "ttsSummarizeThresholdDesc": "超过此字数的消息生成摘要后朗读，0 表示禁用摘要",
      "avatar": "头像设置",
      "aiAvatar": "AI 头像",
      "userAvatar": "用户头像",
      "resetAvatar": "还原",
      "change": "更换"
    },
    "model": {
      "title": "模型配置",
      "addProfile": "新增配置",
      "noProfile": "未配置模型，请点击下方按钮添加",
      "activate": "激活",
      "activateDesc": "已激活",
      "activated": "已激活",
      "edit": "编辑",
      "delete": "删除",
      "profileName": "配置名称",
      "provider": "Provider",
      "baseURL": "Base URL",
      "apiKey": "API Key",
      "model": "Model",
      "embeddingModel": "Embedding Model",
      "embeddingDim": "Embedding Dimension",
      "embeddingInherit": "Embedding 继承 LLM 配置",
      "embeddingProvider": "Embedding Provider",
      "embeddingBaseURL": "Embedding Base URL",
      "embeddingAPIKey": "Embedding API Key",
      "ttsModelDir": "TTS 模型目录",
      "ttsVoice": "TTS 声线",
      "ttsSpeed": "TTS 语速",
      "ttsBackend": "TTS 后端",
      "supportsVision": "支持图片输入",
      "saveProfile": "保存",
      "cancel": "取消",
      "fetchOpenRouter": "获取 OpenRouter 模型列表"
    },
    "ai": {
      "systemPrompt": "系统提示词",
      "systemPromptDesc": "设定 AI 的行为和个性",
      "shortTermLimit": "短期记忆轮数",
      "shortTermLimitDesc": "保留最近 N 轮对话作为上下文",
      "nudgeInterval": "自我成长提示间隔",
      "nudgeIntervalDesc": "每隔 N 轮触发一次记忆沉淀提示，0 使用默认值",
      "skillsDirs": "技能目录",
      "skillsDirsDesc": "每行一个目录路径，存放 YAML 技能文件",
      "thinkingLevel": "思考等级",
      "default": "默认",
      "off": "关闭",
      "low": "低",
      "medium": "中",
      "high": "高"
    },
    "tools": {
      "subTabs": {
        "permissions": "权限",
        "mcp": "MCP",
        "settings": "工具设置"
      },
      "permissions": {
        "title": "内置工具权限",
        "level": "权限级别",
        "granted": "已授权",
        "grantedAt": "授权时间",
        "lastUsed": "最近使用",
        "grant": "授权",
        "revoke": "撤销",
        "confirmPrompt": "每次弹出确认框",
        "autoApprove": "自动批准",
        "disabled": "禁用"
      },
      "mcp": {
        "addServer": "添加 MCP 服务器",
        "editServer": "编辑 MCP 服务器",
        "name": "名称",
        "transport": "传输方式",
        "command": "命令",
        "args": "参数",
        "url": "URL",
        "headers": "Headers (JSON)",
        "enabled": "启用",
        "save": "保存",
        "cancel": "取消",
        "delete": "删除",
        "noServers": "暂无 MCP 服务器"
      },
      "settings": {
        "shellTimeout": "Shell 超时 (秒)",
        "shellTimeoutDesc": "execute_shell 工具的超时时间，默认 30",
        "codeTimeout": "代码超时 (秒)",
        "codeTimeoutDesc": "execute_code 工具的超时时间，默认 60",
        "allowedPaths": "文件系统白名单",
        "allowedPathsDesc": "每行一个目录路径，空表示禁止所有路径",
        "addPath": "添加",
        "trustedCommands": "免确认命令",
        "trustedCommandsDesc": "每行一个命令前缀，匹配的命令无需弹窗确认"
      }
    },
    "knowledge": {
      "import": "导入知识",
      "importDesc": "导入文档、网页或文本内容到知识库",
      "importUrl": "URL",
      "importFile": "文件",
      "importText": "文本",
      "source": "来源",
      "sources": "知识源",
      "noSources": "暂无知识源",
      "deleteSource": "删除",
      "jinaAPIKey": "Jina API Key",
      "jinaAPIKeyDesc": "留空使用 Jina 免费额度",
      "tavilyAPIKey": "Tavily API Key",
      "tavilyAPIKeyDesc": "留空使用 DuckDuckGo 后备搜索",
      "progress": "导入中...",
      "done": "导入完成",
      "error": "导入失败"
    },
    "automation": {
      "subTabs": {
        "cron": "定时任务",
        "proactive": "待触发"
      },
      "cron": {
        "add": "新增任务",
        "edit": "编辑任务",
        "name": "名称",
        "description": "描述",
        "schedule": "Cron 表达式",
        "prompt": "提示词",
        "enabled": "启用",
        "runNow": "立即执行",
        "delete": "删除",
        "save": "保存",
        "cancel": "取消",
        "noJobs": "暂无定时任务",
        "saveToMemory": "结果存入长期记忆",
        "notify": "发送系统通知"
      },
      "proactive": {
        "noItems": "无待触发项",
        "delete": "删除"
      }
    },
    "lark": {
      "status": "飞书状态",
      "installed": "已安装",
      "notInstalled": "未安装",
      "installPrompt": "请先安装 lark-cli",
      "command": "执行命令",
      "run": "运行"
    },
    "sms": {
      "watcher": "短信监听",
      "start": "启动",
      "stop": "停止",
      "status": "状态",
      "running": "运行中",
      "stopped": "已停止"
    },
    "about": {
      "version": "版本",
      "currentVersion": "当前版本",
      "checkUpdate": "检查更新",
      "upToDate": "已是最新版本",
      "newVersion": "发现新版本",
      "installing": "正在安装更新...",
      "releaseNotes": "更新日志",
      "github": "GitHub"
    },
    "save": "保存",
    "saving": "保存中...",
    "saved": "已保存",
    "search": "搜索设置..."
  },
  "chat": {
    "placeholder": "发消息...",
    "send": "发送",
    "sendHint": "⌘↩ 发送 · ↩ 换行",
    "stop": "停止",
    "stopAriaLabel": "停止生成",
    "sendAriaLabel": "发送",
    "sendTitle": "发送 (⌘Enter)",
    "thinkingLevel": "思考等级",
    "useKnowledge": "本次是否检索知识库",
    "useMemory": "本次是否检索长期记忆",
    "knowledgeChip": "知识库",
    "memoryChip": "记忆",
    "markdownMode": "Markdown 编辑模式",
    "attachFile": "附加文件",
    "clearHistory": "清空历史",
    "clearConfirm": "确认清空所有聊天记录？此操作不可撤销。",
    "clearConfirmOk": "确认清空",
    "clearConfirmCancel": "取消",
    "export": "导出记录",
    "exportFailed": "导出失败",
    "thinkingLabel": {
      "default": "默认",
      "off": "无思考",
      "low": "低思考",
      "medium": "中思考",
      "high": "高思考"
    },
    "copy": "复制",
    "copied": "已复制",
    "speak": "朗读",
    "stopSpeak": "停止朗读"
  },
  "chatBubble": {
    "fullscreen": "全屏",
    "exitFullscreen": "退出全屏",
    "close": "关闭"
  },
  "toolConfirm": {
    "title": "工具执行确认",
    "shellWarning": "Shell 命令可修改系统文件、执行任意操作，请确认安全后再执行。",
    "codeWarning": "代码将在本地环境中执行，请确认代码安全后再执行。",
    "command": "命令",
    "workingDir": "工作目录",
    "confirm": "确认执行",
    "cancel": "取消",
    "editBeforeRun": "编辑后再执行"
  },
  "notification": {
    "close": "关闭通知"
  },
  "execution": {
    "running": "执行中...",
    "done": "执行完成"
  }
}
```

- [ ] **Step 2: Create en.json**

```json
{
  "settings": {
    "title": "Settings",
    "tabs": {
      "general": "General",
      "appearance": "Appearance",
      "model": "Model",
      "ai": "Chat",
      "tools": "Tools",
      "knowledge": "Knowledge",
      "automation": "Automation",
      "lark": "Lark",
      "sms": "SMS",
      "about": "About"
    },
    "language": {
      "label": "Language",
      "followSystem": "Follow System"
    },
    "general": {
      "system": "System",
      "autoLaunch": "Launch at login",
      "autoLaunchDesc": "Automatically start Aiko after logging into macOS",
      "theme": "Theme",
      "style": "Style",
      "styleLiquidGlass": "Liquid Glass",
      "styleFrosted": "Frosted Glass",
      "styleDesc": "Liquid Glass: translucent refractive; Frosted Glass: classic dark textured",
      "shortcuts": "Shortcuts",
      "shortcutOptionDouble": "Double-tap Option — Show/Hide chat bubble",
      "shortcutOptionHold": "Hold Option 1s — Start voice input",
      "shortcutOptionRelease": "Release Option — Stop recording, wait for recognition",
      "shortcutEnter": "Send message",
      "shortcutCmdEnter": "New line in message box",
      "shortcutCmdV": "Paste image to message box (supports screenshot paste)",
      "shortcutDrag": "Drag floating ball — Reposition pet",
      "shortcutRightClick": "Right-click chat bubble — Export, clear history, open settings"
    },
    "appearance": {
      "petModel": "Pet Model",
      "renderMode": "Render Mode",
      "vrmModel": "VRM Model",
      "vrmDelete": "Delete",
      "vrmImport": "Import VRM File",
      "vrmUserImported": "User Imported",
      "vrmBuiltin": "Built-in",
      "vrmDeleteModel": "Delete this model",
      "petSize": "Pet Size",
      "petSizeDesc": "Size in pixels, 0 for auto",
      "chatSize": "Chat Bubble Size",
      "chatWidth": "Width",
      "chatHeight": "Height",
      "resetBall": "Reset Floating Ball Position",
      "resetBallDesc": "Move the floating ball back to default position",
      "voice": "Voice & Sound",
      "voiceAutoSend": "Auto-send after voice recognition",
      "soundsEnabled": "Chat sound effects",
      "ttsAutoPlay": "Auto-read AI replies",
      "ttsSummarizeThreshold": "Summary word threshold",
      "ttsSummarizeThresholdDesc": "Summarize messages longer than this before reading, 0 to disable",
      "avatar": "Avatar Settings",
      "aiAvatar": "AI Avatar",
      "userAvatar": "User Avatar",
      "resetAvatar": "Reset",
      "change": "Change"
    },
    "model": {
      "title": "Model Profiles",
      "addProfile": "Add Profile",
      "noProfile": "No model configured. Click the button below to add one.",
      "activate": "Activate",
      "activateDesc": "Activate",
      "activated": "Activated",
      "edit": "Edit",
      "delete": "Delete",
      "profileName": "Profile Name",
      "provider": "Provider",
      "baseURL": "Base URL",
      "apiKey": "API Key",
      "model": "Model",
      "embeddingModel": "Embedding Model",
      "embeddingDim": "Embedding Dimension",
      "embeddingInherit": "Embedding inherits LLM config",
      "embeddingProvider": "Embedding Provider",
      "embeddingBaseURL": "Embedding Base URL",
      "embeddingAPIKey": "Embedding API Key",
      "ttsModelDir": "TTS Model Directory",
      "ttsVoice": "TTS Voice",
      "ttsSpeed": "TTS Speed",
      "ttsBackend": "TTS Backend",
      "supportsVision": "Supports Vision",
      "saveProfile": "Save",
      "cancel": "Cancel",
      "fetchOpenRouter": "Fetch OpenRouter Models"
    },
    "ai": {
      "systemPrompt": "System Prompt",
      "systemPromptDesc": "Define the AI's behavior and personality",
      "shortTermLimit": "Short-term Memory Rounds",
      "shortTermLimitDesc": "Keep the most recent N conversation rounds as context",
      "nudgeInterval": "Self-growth Nudge Interval",
      "nudgeIntervalDesc": "Trigger memory consolidation every N rounds, 0 for default",
      "skillsDirs": "Skills Directories",
      "skillsDirsDesc": "One directory path per line, containing YAML skill files",
      "thinkingLevel": "Thinking Level",
      "default": "Default",
      "off": "Off",
      "low": "Low",
      "medium": "Medium",
      "high": "High"
    },
    "tools": {
      "subTabs": {
        "permissions": "Permissions",
        "mcp": "MCP",
        "settings": "Tool Settings"
      },
      "permissions": {
        "title": "Built-in Tool Permissions",
        "level": "Permission Level",
        "granted": "Granted",
        "grantedAt": "Granted At",
        "lastUsed": "Last Used",
        "grant": "Grant",
        "revoke": "Revoke",
        "confirmPrompt": "Confirm each time",
        "autoApprove": "Auto-approve",
        "disabled": "Disabled"
      },
      "mcp": {
        "addServer": "Add MCP Server",
        "editServer": "Edit MCP Server",
        "name": "Name",
        "transport": "Transport",
        "command": "Command",
        "args": "Arguments",
        "url": "URL",
        "headers": "Headers (JSON)",
        "enabled": "Enabled",
        "save": "Save",
        "cancel": "Cancel",
        "delete": "Delete",
        "noServers": "No MCP servers"
      },
      "settings": {
        "shellTimeout": "Shell Timeout (seconds)",
        "shellTimeoutDesc": "Timeout for execute_shell tool, default 30",
        "codeTimeout": "Code Timeout (seconds)",
        "codeTimeoutDesc": "Timeout for execute_code tool, default 60",
        "allowedPaths": "Filesystem Whitelist",
        "allowedPathsDesc": "One directory path per line, empty denies all paths",
        "addPath": "Add",
        "trustedCommands": "Trusted Commands",
        "trustedCommandsDesc": "One command prefix per line, matching commands skip confirmation"
      }
    },
    "knowledge": {
      "import": "Import Knowledge",
      "importDesc": "Import documents, web pages, or text into the knowledge base",
      "importUrl": "URL",
      "importFile": "File",
      "importText": "Text",
      "source": "Source",
      "sources": "Knowledge Sources",
      "noSources": "No knowledge sources",
      "deleteSource": "Delete",
      "jinaAPIKey": "Jina API Key",
      "jinaAPIKeyDesc": "Leave empty to use Jina free tier",
      "tavilyAPIKey": "Tavily API Key",
      "tavilyAPIKeyDesc": "Leave empty to use DuckDuckGo fallback",
      "progress": "Importing...",
      "done": "Import complete",
      "error": "Import failed"
    },
    "automation": {
      "subTabs": {
        "cron": "Cron Jobs",
        "proactive": "Pending"
      },
      "cron": {
        "add": "Add Job",
        "edit": "Edit Job",
        "name": "Name",
        "description": "Description",
        "schedule": "Cron Expression",
        "prompt": "Prompt",
        "enabled": "Enabled",
        "runNow": "Run Now",
        "delete": "Delete",
        "save": "Save",
        "cancel": "Cancel",
        "noJobs": "No cron jobs",
        "saveToMemory": "Save result to long-term memory",
        "notify": "Send system notification"
      },
      "proactive": {
        "noItems": "No pending items",
        "delete": "Delete"
      }
    },
    "lark": {
      "status": "Lark Status",
      "installed": "Installed",
      "notInstalled": "Not Installed",
      "installPrompt": "Please install lark-cli first",
      "command": "Run Command",
      "run": "Run"
    },
    "sms": {
      "watcher": "SMS Watcher",
      "start": "Start",
      "stop": "Stop",
      "status": "Status",
      "running": "Running",
      "stopped": "Stopped"
    },
    "about": {
      "version": "Version",
      "currentVersion": "Current Version",
      "checkUpdate": "Check for Updates",
      "upToDate": "Up to date",
      "newVersion": "New version available",
      "installing": "Installing update...",
      "releaseNotes": "Release Notes",
      "github": "GitHub"
    },
    "save": "Save",
    "saving": "Saving...",
    "saved": "Saved",
    "search": "Search settings..."
  },
  "chat": {
    "placeholder": "Type a message...",
    "send": "Send",
    "sendHint": "⌘↩ Send · ↩ New line",
    "stop": "Stop",
    "stopAriaLabel": "Stop generating",
    "sendAriaLabel": "Send",
    "sendTitle": "Send (⌘Enter)",
    "thinkingLevel": "Thinking Level",
    "useKnowledge": "Search knowledge base for this query",
    "useMemory": "Search long-term memory for this query",
    "knowledgeChip": "Knowledge",
    "memoryChip": "Memory",
    "markdownMode": "Markdown Edit Mode",
    "attachFile": "Attach File",
    "clearHistory": "Clear History",
    "clearConfirm": "Clear all chat history? This cannot be undone.",
    "clearConfirmOk": "Clear",
    "clearConfirmCancel": "Cancel",
    "export": "Export History",
    "exportFailed": "Export failed",
    "thinkingLabel": {
      "default": "Default",
      "off": "No Thinking",
      "low": "Low Thinking",
      "medium": "Medium Thinking",
      "high": "High Thinking"
    },
    "copy": "Copy",
    "copied": "Copied",
    "speak": "Read Aloud",
    "stopSpeak": "Stop Reading"
  },
  "chatBubble": {
    "fullscreen": "Fullscreen",
    "exitFullscreen": "Exit Fullscreen",
    "close": "Close"
  },
  "toolConfirm": {
    "title": "Tool Execution Confirmation",
    "shellWarning": "Shell commands can modify system files and execute arbitrary operations. Please verify safety before proceeding.",
    "codeWarning": "Code will be executed in the local environment. Please verify safety before proceeding.",
    "command": "Command",
    "workingDir": "Working Directory",
    "confirm": "Confirm Execution",
    "cancel": "Cancel",
    "editBeforeRun": "Edit before running"
  },
  "notification": {
    "close": "Dismiss"
  },
  "execution": {
    "running": "Running...",
    "done": "Complete"
  }
}
```

- [ ] **Step 3: Create ja.json**

```json
{
  "settings": {
    "title": "設定",
    "tabs": {
      "general": "一般",
      "appearance": "外観",
      "model": "モデル",
      "ai": "会話",
      "tools": "ツール",
      "knowledge": "ナレッジ",
      "automation": "自動化",
      "lark": "Lark",
      "sms": "SMS",
      "about": "について"
    },
    "language": {
      "label": "言語",
      "followSystem": "システムに従う"
    },
    "general": {
      "system": "システム",
      "autoLaunch": "ログイン時に起動",
      "autoLaunchDesc": "macOSにログイン後、自動的にAikoを起動します",
      "theme": "テーマ",
      "style": "スタイル",
      "styleLiquidGlass": "リキッドガラス",
      "styleFrosted": "すりガラス",
      "styleDesc": "リキッドガラス：半透明の屈折光彩；すりガラス：クラシックなダークテクスチャ",
      "shortcuts": "ショートカット",
      "shortcutOptionDouble": "Optionダブルタップ — チャットバブルを表示/非表示",
      "shortcutOptionHold": "Option長押し1秒 — 音声入力を開始",
      "shortcutOptionRelease": "Optionを離す — 録音停止、認識待ち",
      "shortcutEnter": "メッセージを送信",
      "shortcutCmdEnter": "メッセージボックス内で改行",
      "shortcutCmdV": "画像をメッセージボックスに貼り付け（スクリーンショット対応）",
      "shortcutDrag": "フローティングボールをドラッグ — ペットを再配置",
      "shortcutRightClick": "チャットバブルを右クリック — 履歴エクスポート、クリア、設定を開く"
    },
    "appearance": {
      "petModel": "ペットモデル",
      "renderMode": "レンダリングモード",
      "vrmModel": "VRMモデル",
      "vrmDelete": "削除",
      "vrmImport": "VRMファイルをインポート",
      "vrmUserImported": "ユーザーインポート",
      "vrmBuiltin": "内蔵",
      "vrmDeleteModel": "このモデルを削除",
      "petSize": "ペットサイズ",
      "petSizeDesc": "ピクセルサイズ、0は自動",
      "chatSize": "チャットバブルサイズ",
      "chatWidth": "幅",
      "chatHeight": "高さ",
      "resetBall": "フローティングボールの位置をリセット",
      "resetBallDesc": "フローティングボールをデフォルト位置に戻す",
      "voice": "音声とサウンド",
      "voiceAutoSend": "音声認識後に自動送信",
      "soundsEnabled": "チャット効果音",
      "ttsAutoPlay": "AI返信後に自動読み上げ",
      "ttsSummarizeThreshold": "要約文字数閾値",
      "ttsSummarizeThresholdDesc": "この文字数を超えるメッセージは要約後に読み上げ、0は無効",
      "avatar": "アバター設定",
      "aiAvatar": "AIアバター",
      "userAvatar": "ユーザーアバター",
      "resetAvatar": "リセット",
      "change": "変更"
    },
    "model": {
      "title": "モデルプロファイル",
      "addProfile": "プロファイル追加",
      "noProfile": "モデルが設定されていません。下のボタンをクリックして追加してください。",
      "activate": "有効化",
      "activateDesc": "有効化",
      "activated": "有効済み",
      "edit": "編集",
      "delete": "削除",
      "profileName": "プロファイル名",
      "provider": "プロバイダー",
      "baseURL": "ベースURL",
      "apiKey": "APIキー",
      "model": "モデル",
      "embeddingModel": "埋め込みモデル",
      "embeddingDim": "埋め込み次元",
      "embeddingInherit": "埋め込みはLLM設定を継承",
      "embeddingProvider": "埋め込みプロバイダー",
      "embeddingBaseURL": "埋め込みベースURL",
      "embeddingAPIKey": "埋め込みAPIキー",
      "ttsModelDir": "TTSモデルディレクトリ",
      "ttsVoice": "TTS音声",
      "ttsSpeed": "TTS速度",
      "ttsBackend": "TTSバックエンド",
      "supportsVision": "画像入力対応",
      "saveProfile": "保存",
      "cancel": "キャンセル",
      "fetchOpenRouter": "OpenRouterモデル一覧を取得"
    },
    "ai": {
      "systemPrompt": "システムプロンプト",
      "systemPromptDesc": "AIの動作と個性を定義します",
      "shortTermLimit": "短期記憶ラウンド数",
      "shortTermLimitDesc": "直近Nラウンドの会話をコンテキストとして保持",
      "nudgeInterval": "自己成長ナッジ間隔",
      "nudgeIntervalDesc": "Nラウンドごとに記憶定着プロンプトをトリガー、0はデフォルト",
      "skillsDirs": "スキルディレクトリ",
      "skillsDirsDesc": "1行に1つのディレクトリパス、YAMLスキルファイルを含む",
      "thinkingLevel": "思考レベル",
      "default": "デフォルト",
      "off": "オフ",
      "low": "低",
      "medium": "中",
      "high": "高"
    },
    "tools": {
      "subTabs": {
        "permissions": "権限",
        "mcp": "MCP",
        "settings": "ツール設定"
      },
      "permissions": {
        "title": "内蔵ツール権限",
        "level": "権限レベル",
        "granted": "許可済み",
        "grantedAt": "許可日時",
        "lastUsed": "最終使用",
        "grant": "許可",
        "revoke": "取り消し",
        "confirmPrompt": "毎回確認",
        "autoApprove": "自動承認",
        "disabled": "無効"
      },
      "mcp": {
        "addServer": "MCPサーバー追加",
        "editServer": "MCPサーバー編集",
        "name": "名前",
        "transport": "転送方式",
        "command": "コマンド",
        "args": "引数",
        "url": "URL",
        "headers": "ヘッダー (JSON)",
        "enabled": "有効",
        "save": "保存",
        "cancel": "キャンセル",
        "delete": "削除",
        "noServers": "MCPサーバーがありません"
      },
      "settings": {
        "shellTimeout": "シェルタイムアウト (秒)",
        "shellTimeoutDesc": "execute_shellツールのタイムアウト、デフォルト30",
        "codeTimeout": "コードタイムアウト (秒)",
        "codeTimeoutDesc": "execute_codeツールのタイムアウト、デフォルト60",
        "allowedPaths": "ファイルシステムホワイトリスト",
        "allowedPathsDesc": "1行に1つのディレクトリパス、空はすべてのパスを拒否",
        "addPath": "追加",
        "trustedCommands": "信頼済みコマンド",
        "trustedCommandsDesc": "1行に1つのコマンドプレフィックス、一致するコマンドは確認不要"
      }
    },
    "knowledge": {
      "import": "ナレッジインポート",
      "importDesc": "ドキュメント、Webページ、またはテキストをナレッジベースにインポート",
      "importUrl": "URL",
      "importFile": "ファイル",
      "importText": "テキスト",
      "source": "ソース",
      "sources": "ナレッジソース",
      "noSources": "ナレッジソースがありません",
      "deleteSource": "削除",
      "jinaAPIKey": "Jina APIキー",
      "jinaAPIKeyDesc": "空のままにするとJina無料枠を使用",
      "tavilyAPIKey": "Tavily APIキー",
      "tavilyAPIKeyDesc": "空のままにするとDuckDuckGoフォールバックを使用",
      "progress": "インポート中...",
      "done": "インポート完了",
      "error": "インポート失敗"
    },
    "automation": {
      "subTabs": {
        "cron": "Cronジョブ",
        "proactive": "保留中"
      },
      "cron": {
        "add": "ジョブ追加",
        "edit": "ジョブ編集",
        "name": "名前",
        "description": "説明",
        "schedule": "Cron式",
        "prompt": "プロンプト",
        "enabled": "有効",
        "runNow": "今すぐ実行",
        "delete": "削除",
        "save": "保存",
        "cancel": "キャンセル",
        "noJobs": "Cronジョブがありません",
        "saveToMemory": "結果を長期記憶に保存",
        "notify": "システム通知を送信"
      },
      "proactive": {
        "noItems": "保留中のアイテムはありません",
        "delete": "削除"
      }
    },
    "lark": {
      "status": "Larkステータス",
      "installed": "インストール済み",
      "notInstalled": "未インストール",
      "installPrompt": "lark-cliを先にインストールしてください",
      "command": "コマンド実行",
      "run": "実行"
    },
    "sms": {
      "watcher": "SMS監視",
      "start": "開始",
      "stop": "停止",
      "status": "状態",
      "running": "実行中",
      "stopped": "停止中"
    },
    "about": {
      "version": "バージョン",
      "currentVersion": "現在のバージョン",
      "checkUpdate": "アップデートを確認",
      "upToDate": "最新です",
      "newVersion": "新しいバージョンがあります",
      "installing": "アップデートをインストール中...",
      "releaseNotes": "リリースノート",
      "github": "GitHub"
    },
    "save": "保存",
    "saving": "保存中...",
    "saved": "保存済み",
    "search": "設定を検索..."
  },
  "chat": {
    "placeholder": "メッセージを入力...",
    "send": "送信",
    "sendHint": "⌘↩ 送信 · ↩ 改行",
    "stop": "停止",
    "stopAriaLabel": "生成を停止",
    "sendAriaLabel": "送信",
    "sendTitle": "送信 (⌘Enter)",
    "thinkingLevel": "思考レベル",
    "useKnowledge": "このクエリでナレッジベースを検索",
    "useMemory": "このクエリで長期記憶を検索",
    "knowledgeChip": "ナレッジ",
    "memoryChip": "記憶",
    "markdownMode": "Markdown編集モード",
    "attachFile": "ファイル添付",
    "clearHistory": "履歴をクリア",
    "clearConfirm": "すべてのチャット履歴を消去しますか？この操作は元に戻せません。",
    "clearConfirmOk": "クリア",
    "clearConfirmCancel": "キャンセル",
    "export": "履歴をエクスポート",
    "exportFailed": "エクスポート失敗",
    "thinkingLabel": {
      "default": "デフォルト",
      "off": "思考なし",
      "low": "低思考",
      "medium": "中思考",
      "high": "高思考"
    },
    "copy": "コピー",
    "copied": "コピー済み",
    "speak": "読み上げ",
    "stopSpeak": "読み上げ停止"
  },
  "chatBubble": {
    "fullscreen": "フルスクリーン",
    "exitFullscreen": "フルスクリーン解除",
    "close": "閉じる"
  },
  "toolConfirm": {
    "title": "ツール実行確認",
    "shellWarning": "シェルコマンドはシステムファイルを変更し、任意の操作を実行できます。実行前に安全性を確認してください。",
    "codeWarning": "コードはローカル環境で実行されます。実行前に安全性を確認してください。",
    "command": "コマンド",
    "workingDir": "作業ディレクトリ",
    "confirm": "実行を確認",
    "cancel": "キャンセル",
    "editBeforeRun": "編集してから実行"
  },
  "notification": {
    "close": "閉じる"
  },
  "execution": {
    "running": "実行中...",
    "done": "完了"
  }
}
```

- [ ] **Step 4: Create ko.json**

```json
{
  "settings": {
    "title": "설정",
    "tabs": {
      "general": "일반",
      "appearance": "외관",
      "model": "모델",
      "ai": "대화",
      "tools": "도구",
      "knowledge": "지식",
      "automation": "자동화",
      "lark": "Lark",
      "sms": "SMS",
      "about": "정보"
    },
    "language": {
      "label": "언어",
      "followSystem": "시스템 따르기"
    },
    "general": {
      "system": "시스템",
      "autoLaunch": "로그인 시 자동 시작",
      "autoLaunchDesc": "macOS 로그인 후 자동으로 Aiko 실행",
      "theme": "테마",
      "style": "스타일",
      "styleLiquidGlass": "리퀴드 글라스",
      "styleFrosted": "프로스트 글라스",
      "styleDesc": "리퀴드 글라스: 반투명 굴절 광채; 프로스트 글라스: 클래식 다크 텍스처",
      "shortcuts": "단축키",
      "shortcutOptionDouble": "Option 더블 탭 — 채팅 버블 표시/숨기기",
      "shortcutOptionHold": "Option 1초 길게 누르기 — 음성 입력 시작",
      "shortcutOptionRelease": "Option 놓기 — 녹음 중지, 인식 대기",
      "shortcutEnter": "메시지 전송",
      "shortcutCmdEnter": "메시지 상자에서 줄바꿈",
      "shortcutCmdV": "메시지 상자에 이미지 붙여넣기 (스크린샷 지원)",
      "shortcutDrag": "플로팅 볼 드래그 — 펫 위치 변경",
      "shortcutRightClick": "채팅 버블 우클릭 — 기록 내보내기, 지우기, 설정 열기"
    },
    "appearance": {
      "petModel": "펫 모델",
      "renderMode": "렌더링 모드",
      "vrmModel": "VRM 모델",
      "vrmDelete": "삭제",
      "vrmImport": "VRM 파일 가져오기",
      "vrmUserImported": "사용자 가져오기",
      "vrmBuiltin": "내장",
      "vrmDeleteModel": "이 모델 삭제",
      "petSize": "펫 크기",
      "petSizeDesc": "픽셀 크기, 0은 자동",
      "chatSize": "채팅 버블 크기",
      "chatWidth": "너비",
      "chatHeight": "높이",
      "resetBall": "플로팅 볼 위치 초기화",
      "resetBallDesc": "플로팅 볼을 기본 위치로 이동",
      "voice": "음성 및 사운드",
      "voiceAutoSend": "음성 인식 후 자동 전송",
      "soundsEnabled": "채팅 효과음",
      "ttsAutoPlay": "AI 응답 후 자동 읽기",
      "ttsSummarizeThreshold": "요약 글자 수 임계값",
      "ttsSummarizeThresholdDesc": "이 글자 수를 초과하는 메시지는 요약 후 읽기, 0은 비활성화",
      "avatar": "아바타 설정",
      "aiAvatar": "AI 아바타",
      "userAvatar": "사용자 아바타",
      "resetAvatar": "초기화",
      "change": "변경"
    },
    "model": {
      "title": "모델 프로필",
      "addProfile": "프로필 추가",
      "noProfile": "모델이 구성되지 않았습니다. 아래 버튼을 클릭하여 추가하세요.",
      "activate": "활성화",
      "activateDesc": "활성화",
      "activated": "활성화됨",
      "edit": "편집",
      "delete": "삭제",
      "profileName": "프로필 이름",
      "provider": "제공자",
      "baseURL": "기본 URL",
      "apiKey": "API 키",
      "model": "모델",
      "embeddingModel": "임베딩 모델",
      "embeddingDim": "임베딩 차원",
      "embeddingInherit": "임베딩이 LLM 설정 상속",
      "embeddingProvider": "임베딩 제공자",
      "embeddingBaseURL": "임베딩 기본 URL",
      "embeddingAPIKey": "임베딩 API 키",
      "ttsModelDir": "TTS 모델 디렉토리",
      "ttsVoice": "TTS 음성",
      "ttsSpeed": "TTS 속도",
      "ttsBackend": "TTS 백엔드",
      "supportsVision": "이미지 입력 지원",
      "saveProfile": "저장",
      "cancel": "취소",
      "fetchOpenRouter": "OpenRouter 모델 목록 가져오기"
    },
    "ai": {
      "systemPrompt": "시스템 프롬프트",
      "systemPromptDesc": "AI의 동작과 성격을 정의합니다",
      "shortTermLimit": "단기 기억 라운드 수",
      "shortTermLimitDesc": "최근 N 라운드의 대화를 컨텍스트로 유지",
      "nudgeInterval": "자기 성장 알림 간격",
      "nudgeIntervalDesc": "N 라운드마다 기억 통합 프롬프트 트리거, 0은 기본값",
      "skillsDirs": "스킬 디렉토리",
      "skillsDirsDesc": "한 줄에 하나의 디렉토리 경로, YAML 스킬 파일 포함",
      "thinkingLevel": "사고 레벨",
      "default": "기본",
      "off": "끄기",
      "low": "낮음",
      "medium": "중간",
      "high": "높음"
    },
    "tools": {
      "subTabs": {
        "permissions": "권한",
        "mcp": "MCP",
        "settings": "도구 설정"
      },
      "permissions": {
        "title": "내장 도구 권한",
        "level": "권한 레벨",
        "granted": "허용됨",
        "grantedAt": "허용 시간",
        "lastUsed": "마지막 사용",
        "grant": "허용",
        "revoke": "취소",
        "confirmPrompt": "매번 확인",
        "autoApprove": "자동 승인",
        "disabled": "비활성화"
      },
      "mcp": {
        "addServer": "MCP 서버 추가",
        "editServer": "MCP 서버 편집",
        "name": "이름",
        "transport": "전송 방식",
        "command": "명령어",
        "args": "인수",
        "url": "URL",
        "headers": "헤더 (JSON)",
        "enabled": "활성화",
        "save": "저장",
        "cancel": "취소",
        "delete": "삭제",
        "noServers": "MCP 서버 없음"
      },
      "settings": {
        "shellTimeout": "셸 타임아웃 (초)",
        "shellTimeoutDesc": "execute_shell 도구의 타임아웃, 기본값 30",
        "codeTimeout": "코드 타임아웃 (초)",
        "codeTimeoutDesc": "execute_code 도구의 타임아웃, 기본값 60",
        "allowedPaths": "파일 시스템 화이트리스트",
        "allowedPathsDesc": "한 줄에 하나의 디렉토리 경로, 비워두면 모든 경로 거부",
        "addPath": "추가",
        "trustedCommands": "신뢰된 명령어",
        "trustedCommandsDesc": "한 줄에 하나의 명령어 접두사, 일치하는 명령어는 확인 불필요"
      }
    },
    "knowledge": {
      "import": "지식 가져오기",
      "importDesc": "문서, 웹 페이지 또는 텍스트를 지식 베이스로 가져오기",
      "importUrl": "URL",
      "importFile": "파일",
      "importText": "텍스트",
      "source": "출처",
      "sources": "지식 소스",
      "noSources": "지식 소스 없음",
      "deleteSource": "삭제",
      "jinaAPIKey": "Jina API 키",
      "jinaAPIKeyDesc": "비워두면 Jina 무료 티어 사용",
      "tavilyAPIKey": "Tavily API 키",
      "tavilyAPIKeyDesc": "비워두면 DuckDuckGo 폴백 사용",
      "progress": "가져오는 중...",
      "done": "가져오기 완료",
      "error": "가져오기 실패"
    },
    "automation": {
      "subTabs": {
        "cron": "Cron 작업",
        "proactive": "대기 중"
      },
      "cron": {
        "add": "작업 추가",
        "edit": "작업 편집",
        "name": "이름",
        "description": "설명",
        "schedule": "Cron 표현식",
        "prompt": "프롬프트",
        "enabled": "활성화",
        "runNow": "지금 실행",
        "delete": "삭제",
        "save": "저장",
        "cancel": "취소",
        "noJobs": "Cron 작업 없음",
        "saveToMemory": "결과를 장기 기억에 저장",
        "notify": "시스템 알림 전송"
      },
      "proactive": {
        "noItems": "대기 중인 항목 없음",
        "delete": "삭제"
      }
    },
    "lark": {
      "status": "Lark 상태",
      "installed": "설치됨",
      "notInstalled": "설치되지 않음",
      "installPrompt": "lark-cli를 먼저 설치하세요",
      "command": "명령 실행",
      "run": "실행"
    },
    "sms": {
      "watcher": "SMS 감시",
      "start": "시작",
      "stop": "중지",
      "status": "상태",
      "running": "실행 중",
      "stopped": "중지됨"
    },
    "about": {
      "version": "버전",
      "currentVersion": "현재 버전",
      "checkUpdate": "업데이트 확인",
      "upToDate": "최신 버전",
      "newVersion": "새 버전 있음",
      "installing": "업데이트 설치 중...",
      "releaseNotes": "릴리스 노트",
      "github": "GitHub"
    },
    "save": "저장",
    "saving": "저장 중...",
    "saved": "저장됨",
    "search": "설정 검색..."
  },
  "chat": {
    "placeholder": "메시지 입력...",
    "send": "전송",
    "sendHint": "⌘↩ 전송 · ↩ 줄바꿈",
    "stop": "중지",
    "stopAriaLabel": "생성 중지",
    "sendAriaLabel": "전송",
    "sendTitle": "전송 (⌘Enter)",
    "thinkingLevel": "사고 레벨",
    "useKnowledge": "이 쿼리에 대해 지식 베이스 검색",
    "useMemory": "이 쿼리에 대해 장기 기억 검색",
    "knowledgeChip": "지식",
    "memoryChip": "기억",
    "markdownMode": "Markdown 편집 모드",
    "attachFile": "파일 첨부",
    "clearHistory": "기록 지우기",
    "clearConfirm": "모든 채팅 기록을 삭제하시겠습니까? 이 작업은 취소할 수 없습니다.",
    "clearConfirmOk": "지우기",
    "clearConfirmCancel": "취소",
    "export": "기록 내보내기",
    "exportFailed": "내보내기 실패",
    "thinkingLabel": {
      "default": "기본",
      "off": "사고 없음",
      "low": "낮은 사고",
      "medium": "중간 사고",
      "high": "높은 사고"
    },
    "copy": "복사",
    "copied": "복사됨",
    "speak": "읽기",
    "stopSpeak": "읽기 중지"
  },
  "chatBubble": {
    "fullscreen": "전체 화면",
    "exitFullscreen": "전체 화면 종료",
    "close": "닫기"
  },
  "toolConfirm": {
    "title": "도구 실행 확인",
    "shellWarning": "셸 명령은 시스템 파일을 수정하고 임의의 작업을 실행할 수 있습니다. 실행 전에 안전을 확인하세요.",
    "codeWarning": "코드가 로컬 환경에서 실행됩니다. 실행 전에 안전을 확인하세요.",
    "command": "명령",
    "workingDir": "작업 디렉토리",
    "confirm": "실행 확인",
    "cancel": "취소",
    "editBeforeRun": "편집 후 실행"
  },
  "notification": {
    "close": "닫기"
  },
  "execution": {
    "running": "실행 중...",
    "done": "완료"
  }
}
```

- [ ] **Step 5: Verify JSON validity**

```bash
for f in frontend/src/locales/*.json; do node -e "JSON.parse(require('fs').readFileSync('$f','utf8'))" && echo "$f: OK"; done
```

Expected: all 4 files print "OK".

- [ ] **Step 6: Verify all 4 files have identical keys**

```bash
node -e "
const fs = require('fs');
const dir = 'frontend/src/locales/';
const files = ['zh-CN.json', 'en.json', 'ja.json', 'ko.json'];
function keys(obj, prefix) {
  return Object.entries(obj).flatMap(([k,v]) =>
    typeof v === 'object' && !Array.isArray(v) ? keys(v, prefix + k + '.') : [prefix + k]
  ).sort();
}
const ref = keys(JSON.parse(fs.readFileSync(dir + files[0], 'utf8')), '');
for (let i = 1; i < files.length; i++) {
  const k = keys(JSON.parse(fs.readFileSync(dir + files[i], 'utf8')), '');
  const missing = ref.filter(x => !k.includes(x));
  const extra = k.filter(x => !ref.includes(x));
  if (missing.length) console.log(files[i] + ' MISSING:', missing);
  if (extra.length) console.log(files[i] + ' EXTRA:', extra);
  if (!missing.length && !extra.length) console.log(files[i] + ': keys match');
}
"
```

Expected: all 3 non-reference files print "keys match".

- [ ] **Step 7: Commit**

```bash
git add frontend/src/locales/
git commit -m "feat(frontend): add i18n locale files for zh-CN, en, ja, ko"
```

---

### Task 5: Create i18n initialization module

**Files:**
- Create: `frontend/src/locales/index.js`

- [ ] **Step 1: Create locales/index.js**

```js
/** i18n initialization — detects locale and creates vue-i18n instance. */
import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN.json'
import en from './en.json'
import ja from './ja.json'
import ko from './ko.json'

const SUPPORTED = ['zh-CN', 'en', 'ja', 'ko']
const FALLBACK = 'en'

const messages = { 'zh-CN': zhCN, en, ja, ko }

/**
 * detectLocale resolves the UI language from config preference and system locale.
 * @param {string} configLanguage - value from Go config.Language, empty means follow system
 * @returns {string} a supported locale code
 */
export function detectLocale(configLanguage) {
  if (configLanguage && SUPPORTED.includes(configLanguage)) return configLanguage
  const sys = navigator.language       // e.g. "zh-Hans-CN"
  const short = sys.slice(0, 2)        // "zh"
  return SUPPORTED.find(l => l.startsWith(short)) || FALLBACK
}

/**
 * createI18nInstance creates a vue-i18n instance for the given locale.
 * @param {string} locale - one of SUPPORTED
 * @returns {import('vue-i18n').I18n}
 */
export function createI18nInstance(locale) {
  return createI18n({
    legacy: false,
    locale,
    fallbackLocale: FALLBACK,
    messages,
  })
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/locales/index.js
git commit -m "feat(frontend): add i18n initialization module with locale detection"
```

---

### Task 6: Update main.js bootstrap

**Files:**
- Modify: `frontend/src/main.js`

- [ ] **Step 1: Rewrite main.js to initialize i18n**

Replace the entire content of `frontend/src/main.js`:

```js
import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './style.css'
import { GetConfig } from '../wailsjs/go/main/App'
import { detectLocale, createI18nInstance } from './locales/index.js'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'frosted'
  const locale = detectLocale(cfg.Language || '')
  const i18n = createI18nInstance(locale)
  createApp(App).use(i18n).mount('#app')
}).catch(() => {
  document.documentElement.dataset.theme = 'frosted'
  const locale = detectLocale('')
  const i18n = createI18nInstance(locale)
  createApp(App).use(i18n).mount('#app')
})
```

- [ ] **Step 2: Build frontend to verify**

```bash
cd frontend && yarn build
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/main.js
git commit -m "feat(frontend): wire up vue-i18n in main.js bootstrap"
```

---

### Task 7: Add language selector to SettingsWindow — logic

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

This task adds only the language switching logic (computed, handler, config sync). The template changes are in Task 8.

- [ ] **Step 1: Add import for useI18n and define language options**

At the top of `<script setup>`, add after the existing imports:

```js
import { useI18n } from 'vue-i18n'
```

After `const confirm = useConfirm()`, add:

```js
const { locale, t } = useI18n()

const LANG_OPTIONS = [
  { value: 'zh-CN', label: '中文' },
  { value: 'en',    label: 'English' },
  { value: 'ja',    label: '日本語' },
  { value: 'ko',    label: '한국어' },
  { value: '',      label: '跟随系统' },
]

/** selectedLang: currently displayed language value in the dropdown. */
const selectedLang = computed({
  get: () => cfg.value.Language || '',
  set: (val) => {
    cfg.value.Language = val
    const resolved = val || detectSystemLocale()
    locale.value = resolved
    debouncedSave(true)
  },
})

/** detectSystemLocale returns the system language if supported, else 'en'. */
function detectSystemLocale() {
  const sys = navigator.language
  const short = sys.slice(0, 2)
  return ['zh-CN', 'en', 'ja', 'ko'].find(l => l.startsWith(short)) || 'en'
}
```

- [ ] **Step 2: Add Language to cfg ref defaults**

In the `cfg` ref declaration, add `Language: ''`:

```js
const cfg = ref({
  // ...existing fields...
  Language: '',
})
```

- [ ] **Step 3: Build to verify compilation**

```bash
cd frontend && yarn build
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(frontend): add language selector logic to SettingsWindow"
```

---

### Task 8: Add language selector UI + convert SettingsWindow template to use $t()

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: Insert language selector into general tab template**

After line 1484 (`<div v-if="activeTab === 'general'" class="tab-pane">`), insert the language selector as the first section (before the "系统" section):

```html
          <!-- 语言 -->
          <div class="group-label">{{ $t('settings.language.label') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.language.label') }}</div>
                <div class="row-desc">{{ $t('settings.general.languageDesc') }}</div>
              </div>
              <div class="row-ctrl">
                <select v-model="selectedLang" class="vrm-select">
                  <option v-for="opt in LANG_OPTIONS" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                  </option>
                </select>
              </div>
            </div>
          </div>
```

- [ ] **Step 2: Convert tab labels to use $t()**

In the `tabMeta` array (around line 253), change all `label` to use computed text. Since the tabMeta is outside `<script setup>` reactivity scope for `t()`, instead convert the **template** tab rendering to use `$t()`.

Find the tab rendering template section (search for `v-for="tab in filteredTabs"`) and change the label rendering:

```html
<!-- BEFORE: -->
{{ tab.label }}

<!-- AFTER: -->
{{ $t('settings.tabs.' + tab.id) }}
```

And update the tabMeta array: remove the `label` field (or keep it as a fallback — the `$t()` call will override). Since the keys match `tab.id`, just change the template.

- [ ] **Step 3: Convert group labels and text in general tab**

Replace Chinese hardcoded strings in the general tab template:

```
"系统"      → {{ $t('settings.general.system') }}
"登录时自动启动"  → {{ $t('settings.general.autoLaunch') }}
"开机登录 macOS 后自动运行 Aiko" → {{ $t('settings.general.autoLaunchDesc') }}
"界面主题"     → {{ $t('settings.general.theme') }}
"风格"       → {{ $t('settings.general.style') }}
"液态玻璃：近透明折射光影；毛玻璃：经典深色质感" → {{ $t('settings.general.styleDesc') }}
"液态玻璃"     → {{ $t('settings.general.styleLiquidGlass') }}
"毛玻璃"      → {{ $t('settings.general.styleFrosted') }}
"快捷键"      → {{ $t('settings.general.shortcuts') }}
(and all shortcut description strings similarly)
```

- [ ] **Step 4: Convert all other tab templates to use $t()**

Apply the same pattern to every tab in SettingsWindow:
- `appearance` tab: all group labels, row titles, row descriptions, button text
- `model` tab: all labels, buttons, placeholder text
- `ai` tab: all labels, descriptions
- `tools` tab + sub-tabs: all labels, buttons, table headers
- `knowledge` tab: all labels, buttons
- `automation` tab + sub-tabs: all labels, buttons, table headers
- `lark` tab: all labels, status text, buttons
- `sms` tab: all labels, status text, buttons
- `about` tab: all labels, version text, button text

Search text → `$t('settings.xxx.yyy')` mapping as defined in the locale JSON structure from Task 4.

- [ ] **Step 5: Build to verify**

```bash
cd frontend && yarn build
```

Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(frontend): convert SettingsWindow to use i18n $t() for all UI strings"
```

---

### Task 9: Convert ChatPanel to use $t()

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Add useI18n import**

At the top of `<script setup>`, add:

```js
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
```

- [ ] **Step 2: Update thinkingLevelLabel computed to use i18n**

Find the `thinkingLevelLabel` computed and replace hardcoded labels:

```js
const thinkingLevelLabel = computed(() => {
  const key = {
    default: 'default',
    off: 'off',
    low: 'low',
    medium: 'medium',
    high: 'high',
  }[thinkingLevel.value] || 'default'
  return t('chat.thinkingLabel.' + key)
})
```

- [ ] **Step 3: Replace template strings**

Replace these template strings:
```
placeholder="发消息..."       → :placeholder="$t('chat.placeholder')"
"⏹ 停止"                     → {{ $t('chat.stop') }}
aria-label="停止生成"         → :aria-label="$t('chat.stopAriaLabel')"
aria-label="发送"             → :aria-label="$t('chat.sendAriaLabel')"
title="发送 (⌘Enter)"         → :title="$t('chat.sendTitle')"
title="思考等级"              → :title="$t('chat.thinkingLevel')"
title="本次是否检索知识库"     → :title="$t('chat.useKnowledge')"
title="本次是否检索长期记忆"   → :title="$t('chat.useMemory')"
"知识库"                      → {{ $t('chat.knowledgeChip') }}
"记忆"                        → {{ $t('chat.memoryChip') }}
title="Markdown 编辑模式"      → :title="$t('chat.markdownMode')"
title="附加文件"               → :title="$t('chat.attachFile')"
"⌘↩ 发送 · ↩ 换行"            → {{ $t('chat.sendHint') }}
"确认清空"                    → {{ $t('chat.clearConfirmOk') }}
"取消"                        → {{ $t('chat.clearConfirmCancel') }}
"确认清空所有聊天记录？此操作不可撤销。" → {{ $t('chat.clearConfirm') }}
"已复制"                      → {{ $t('chat.copied') }}
"复制"                        → {{ $t('chat.copy') }}
"朗读"                        → {{ $t('chat.speak') }}
"停止朗读"                    → {{ $t('chat.stopSpeak') }}
```

- [ ] **Step 4: Build to verify**

```bash
cd frontend && yarn build
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): convert ChatPanel to use i18n $t() for all UI strings"
```

---

### Task 10: Convert ChatBubble to use $t()

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue`

- [ ] **Step 1: Add useI18n to script setup, replace template strings**

```js
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
```

Replace in template:
```
:title="isFullscreen ? '退出全屏' : '全屏'"  → :title="isFullscreen ? $t('chatBubble.exitFullscreen') : $t('chatBubble.fullscreen')"
aria-label="关闭"                              → :aria-label="$t('chatBubble.close')"
```

- [ ] **Step 2: Build, commit**

```bash
cd frontend && yarn build
git add frontend/src/components/ChatBubble.vue
git commit -m "feat(frontend): convert ChatBubble to use i18n $t()"
```

---

### Task 11: Convert ToolConfirmModal, NotificationBubble, ExecutionProgress, ContextMenu

**Files:**
- Modify: `frontend/src/components/ToolConfirmModal.vue`
- Modify: `frontend/src/components/NotificationBubble.vue`
- Modify: `frontend/src/components/ExecutionProgress.vue`
- Modify: `frontend/src/components/ContextMenu.vue`

- [ ] **Step 1: Convert ToolConfirmModal.vue**

Add `import { useI18n } from 'vue-i18n'; const { t } = useI18n()`.

Replace hardcoded strings:
```
'Shell 命令可修改系统文件...'  → t('toolConfirm.shellWarning')
其他工具类型警告              → t('toolConfirm.codeWarning')
标题                          → t('toolConfirm.title')
"确认执行"                    → t('toolConfirm.confirm')
"取消"                        → t('toolConfirm.cancel')
"编辑后再执行"                → t('toolConfirm.editBeforeRun')
```

- [ ] **Step 2: Convert NotificationBubble.vue**

```js
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
```

Replace: `aria-label="关闭通知"` → `:aria-label="$t('notification.close')"`

- [ ] **Step 3: Convert ExecutionProgress.vue**

```js
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
```

Replace status text:
```
"执行中..."  → {{ $t('execution.running') }}
"执行完成"   → {{ $t('execution.done') }}
```

- [ ] **Step 4: Convert ContextMenu.vue**

```js
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
```

Replace Chinese menu item labels. Check the component for any hardcoded text and wrap with `$t()`.

- [ ] **Step 5: Build and commit**

```bash
cd frontend && yarn build
git add frontend/src/components/ToolConfirmModal.vue frontend/src/components/NotificationBubble.vue frontend/src/components/ExecutionProgress.vue frontend/src/components/ContextMenu.vue
git commit -m "feat(frontend): convert remaining components to use i18n $t()"
```

---

### Task 12: Regenerate Wails bindings and verify build

**Files:**
- Auto-modified: `frontend/wailsjs/` (by wails generate)

- [ ] **Step 1: Regenerate Wails bindings**

```bash
wails generate module
```

This picks up the new `Language` field in the Go Config struct and regenerates the TypeScript bindings.

- [ ] **Step 2: Full Go build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Frontend build**

```bash
cd frontend && yarn build
```

Expected: no errors.

- [ ] **Step 4: Full Wails build**

```bash
make build
```

Expected: binary produced successfully.

- [ ] **Step 5: Commit**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate Wails bindings with Language field"
```

---

### Task 13: Manual smoke test

- [ ] **Step 1: Launch the app**

```bash
make run
```

- [ ] **Step 2: Verify default language detection**

Open Settings → General tab. The language dropdown should reflect system language (or "跟随系统" if never changed).

- [ ] **Step 3: Switch language and verify immediate UI update**

Select English → Settings UI should switch to English immediately.
Select 日本語 → Settings UI should switch to Japanese.
Select 한국어 → Settings UI should switch to Korean.
Select 中文 → Settings UI should switch back to Chinese.
Select 跟随系统 → Should revert to system-detected language.

- [ ] **Step 4: Verify persistence**

Close settings, quit app, re-launch. Language preference should be preserved.

- [ ] **Step 5: Verify ChatPanel translations**

Open chat bubble → placeholder text, toolbar tooltips should be in selected language.

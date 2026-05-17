# 子项目 4：拆分 app.go 为按领域聚焦的文件

**日期：** 2026-05-17  
**范围：** `app.go` → 11 个文件，均在 `package main`  
**目标：** 将 2758 行的 `app.go` 拆分为按业务领域聚焦的文件，每个文件职责单一、易于阅读和修改。不改变任何行为。

---

## 背景

完成前三个子项目的重构后，`app.go` 仍有 ~2758 行、109 个函数。各领域（聊天、配置、VRM、TTS、MCP、Cron…）混杂在一个文件中，打开文件时需要大量滚动，修改一个领域时难以定位相关代码。

Go 支持同一 package 的多个源文件共享一个 struct，因此可以零成本拆分——编译结果完全相同。

---

## 文件拆分方案

所有文件都在 `package main`，导入列表各自独立（只 import 该文件用到的包）。

| 目标文件 | 职责 | 迁移的函数/类型 |
|----------|------|----------------|
| `app.go` | `App` 结构体、生命周期（startup/domReady/shutdown）、Agent 构建核心 | `App` struct、`NewApp`、`startup`、`domReady`、`shutdown`、`buildAgent`、`initLLMComponents`、`rebuildAgentTools` |
| `app_chat.go` | 聊天消息发送与历史管理 | `streamChat`、`SendMessage`、`RegenerateLastReply`、`SendMessageWithImages`、`SendMessageWithFiles`、`parseDataURL`、`GetMessages`、`GetMessagesBeforeID`、`ClearChatHistory`、`StopGeneration`、`ChatDirect`、`ChatDirectCollect`、`ExportChatHistory`、`formatChatError`、`FileAttachment`（类型） |
| `app_config.go` | 全局配置 CRUD、Model Profile、头像 | `GetConfig`、`SaveConfig`、`SetAvatar`、`ResetAvatar`、`ListModelProfiles`、`SaveModelProfile`、`DeleteModelProfile`、`ActivateModelProfile`、`MissingRequiredConfig`、`GetAvailableModels`、`ListLLMModels`、`ListOpenRouterModels` |
| `app_tts.go` | TTS 语音合成与设置 | `SpeakText`、`StopTTS`、`GetKokoroTTSVoices`、`GetTTSAutoPlay`、`SetTTSAutoPlay`、`SetupKokoroTTS`、`stripNonSpeech`、`isEmojiRune` |
| `app_vrm.go` | VRM 3D 宠物模型管理 | `VRMModelInfo`（类型）、`ListVRMModels`、`GetVRMPath`、`ImportVRMFile`、`DeleteVRMModel` |
| `app_knowledge.go` | 知识库导入与管理 | `ImportKnowledge`、`ListKnowledgeSources`、`DeleteKnowledgeSource` |
| `app_mcp.go` | MCP 服务器配置管理 | `ListMCPServers`、`AddMCPServer`、`UpdateMCPServer`、`DeleteMCPServer` |
| `app_cron.go` | Cron 定时任务与主动消息 | `ListCronJobs`、`CreateCronJob`、`UpdateCronJob`、`DeleteCronJob`、`SetCronJobEnabled`、`RunCronJobNow`、`ListProactiveItems`、`DeleteProactiveItem` |
| `app_layout.go` | 屏幕布局、位置、尺寸 | `ScreenInfo`（类型）、`MousePosition`（类型）、`GetBallPosition`、`SaveBallPosition`、`ResetBallPosition`、`GetScreenList`、`startScreenWatcher`、`GetPetSize`、`SavePetSize`、`GetChatSize`、`SaveChatSize`、`GetMousePosition`、`GetScreenSize` |
| `app_system.go` | 系统工具、更新、窗口控制、工具权限 | `AcquireKeyWindow`、`ReleaseKeyWindow`、`EmitEvent`、`SetChatVisible`、`IsChatVisible`、`LinkPreview`（类型）、`FetchLinkPreview`、`extractMeta`、`extractAttrValue`、`htmlUnescape`、`urlJoin`、`GetAutoLaunch`、`SetAutoLaunch`、`LarkStatus`、`LarkRunCommand`、`PingLLM`、`ConfirmToolExecution`、`KillToolExecution`、`GetToolPermissions`、`SetToolPermission`、`OpenFileDialog`、`OpenDirectoryDialog`、`GetVersion`、`CheckUpdate`、`InstallUpdate`、`UpdateInfo`（类型）、`ensureAikoCert`、`downloadFileWithProgress`、`downloadFile`、`run` |
| `app_voice.go` | 语音输入（VoiceAutoSend/Sounds）、短信监听 、首次启动 | `GetVoiceAutoSend`、`SetVoiceAutoSend`、`GetSoundsEnabled`、`SetSoundsEnabled`、`startSMSWatcher`、`StartSMSWatcher`、`StopSMSWatcher`、`IsSMSWatcherRunning`、`IsFirstLaunch`、`MarkWelcomeShown` |

---

## 约束

- 所有文件保持 `package main`——拆分后编译行为与拆分前完全相同
- 每个新文件包含独立的 `import` 块，只导入该文件用到的包
- 不新增任何 public API、不修改任何函数签名
- `App` struct 和 `NewApp` 留在 `app.go`
- 分拆操作是纯机械移动，不修改任何函数体

---

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `app.go` | 修改（删除） | 删除迁移到新文件的函数；保留 struct、lifecycle、agent 核心 |
| `app_chat.go` | 新建 | 聊天相关方法 |
| `app_config.go` | 新建 | 配置相关方法 |
| `app_tts.go` | 新建 | TTS 相关方法 |
| `app_vrm.go` | 新建 | VRM 相关方法 |
| `app_knowledge.go` | 新建 | 知识库相关方法 |
| `app_mcp.go` | 新建 | MCP 相关方法 |
| `app_cron.go` | 新建 | Cron/定时任务相关方法 |
| `app_layout.go` | 新建 | 布局/屏幕相关方法 |
| `app_system.go` | 新建 | 系统/更新/工具权限相关方法 |
| `app_voice.go` | 新建 | 语音/短信相关方法 |

## 测试策略

- 无新增测试（纯机械移动，零行为变更）
- 验证：`go build ./...` 和 `go build -race ./...`
- 验证：`go vet ./...`
- 人工验证：`make run` 启动确认功能正常

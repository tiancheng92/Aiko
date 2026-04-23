# Per-Screen Pet & Chat Configuration

**Date:** 2026-04-23  
**Status:** Approved

## Overview

当鼠标移动到不同显示器时，Aiko 窗口跟随迁移，并自动加载该屏幕对应的宠物位置、宠物大小、聊天框大小配置。每块屏幕独立保存一套完整配置，key 以分辨率 `{W}x{H}` 区分。

---

## 1. 数据存储

使用现有 `settings` 表（key/value），扩展两个新 key 格式：

| Key | 示例 | 含义 |
|---|---|---|
| `ball_pos_{W}x{H}` | `1200,900` | 宠物 CSS 坐标（已有） |
| `pet_size_{W}x{H}` | `300` | 宠物高度 px；`0` = 自动计算 |
| `chat_size_{W}x{H}` | `420,600` | 聊天框 width,height px；`0,0` = 自动 |

首次进入某屏时对应 key 不存在，fallback 到当前全局默认值。

---

## 2. Go 后端

### 2.1 新增字段

`App` struct 增加：

```go
activeScreen wailsruntime.Screen // 当前活跃屏幕，受 mu 保护
```

### 2.2 屏幕监测 `startScreenWatcher()`

- 在 `startup()` 中调用，独立 goroutine 运行
- 每 500ms 轮询一次：
  1. 读取鼠标 macOS 屏幕坐标（复用 `C.getMouseScreenX/Y`）
  2. 遍历 `wailsruntime.ScreenGetAll` 找到包含鼠标坐标的屏幕（判断 `screen.Bounds`）
  3. 与 `a.activeScreen` 对比，无变化跳过
  4. 检测到变化：
     - `wailsruntime.WindowSetSize(ctx, screen.Size.Width, screen.Size.Height)`
     - `wailsruntime.WindowSetPosition(ctx, screen.Bounds.X, screen.Bounds.Y)`
     - `wailsruntime.EventsEmit(ctx, "screen:changed", ScreenInfo{Width, Height})`
     - 更新 `a.activeScreen`（持有 `mu.Lock`）

### 2.3 新增数据结构

```go
// ScreenInfo holds the resolution of a screen.
type ScreenInfo struct {
    Width  int `json:"width"`
    Height int `json:"height"`
}
```

### 2.4 新增 Wails 绑定方法

| 方法 | 说明 |
|---|---|
| `GetScreenList() []ScreenInfo` | 返回所有屏幕列表，供设置页展示 |
| `GetPetSize(screenW, screenH int) int` | 读 `pet_size_{W}x{H}`，不存在返回 `0` |
| `SavePetSize(size, screenW, screenH int) error` | 写 `pet_size_{W}x{H}` |
| `GetChatSize(screenW, screenH int) []int` | 读 `chat_size_{W}x{H}`，返回 `[width, height]`，不存在返回 `[0, 0]` |
| `SaveChatSize(width, height, screenW, screenH int) error` | 写 `chat_size_{W}x{H}` |

`GetBallPosition` / `SaveBallPosition` 已有，无需改动。

---

## 3. 前端

### 3.1 活跃屏幕状态

在顶层组件（`App.vue`）维护：

```js
const activeScreen = ref({ width: 0, height: 0 })
```

- 初始化时调用 `GetScreenSize()` 填充
- 监听 `screen:changed` 事件更新，并 emit 内部事件 `screen:active:changed`

### 3.2 `Live2DPet.vue`

- 监听 `screen:active:changed`：
  - 调用 `GetPetSize(w, h)` 更新宠物高度（`0` 时走原有自动计算逻辑）
  - 调用 `GetBallPosition(w, h)` 更新宠物坐标
- 拖拽结束保存位置时已携带分辨率，无需改动

### 3.3 `ChatBubble.vue`

- 监听 `screen:active:changed`：
  - 调用 `GetChatSize(w, h)` 更新聊天框宽高（`0` 时走原有默认逻辑）
- resize/拖拽结束时调用 `SaveChatSize` 持久化（替换现有存全局配置逻辑）

### 3.4 `SettingsWindow.vue`

- 宠物大小、聊天框大小保存时附带 `activeScreen` 分辨率
- 新增只读提示「当前屏幕：`{W}x{H}`」，让用户明确知道在为哪块屏配置

---

## 4. 事件总线

| 事件名 | 方向 | Payload | 含义 |
|---|---|---|---|
| `screen:changed` | backend→frontend | `{ width, height }` | 活跃屏幕变化 |
| `screen:active:changed` | frontend→frontend | `{ width, height }` | 前端内部转发，各组件订阅 |

---

## 5. 错误处理

- `ScreenGetAll` 失败：跳过本次轮询，保持现有窗口状态
- `GetPetSize` / `GetChatSize` key 不存在：返回 `0` / `[0,0]`，前端 fallback 到默认值
- 窗口迁移失败：记录 `slog.Warn`，不影响其他功能

---

## 6. 不在本次范围内

- 窗口跨屏拖拽检测（用鼠标轮询代替）
- 设置页面新增「复制当前屏配置到其他屏」功能
- Windows/Linux 支持

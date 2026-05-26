# 番茄钟功能设计

## 概述

为 Aiko 桌面宠物添加番茄钟功能，通过悬浮计时面板、宠物状态联动、系统通知，提供专注工作流支持。

## 架构

### 后端

**新增文件：`internal/pomodoro/engine.go`**

番茄钟引擎，管理倒计时生命周期和状态机。模式对标 `internal/proactive/engine.go`。

```
Engine 结构:
  mu            sync.Mutex
  done          chan struct{}
  wg            sync.WaitGroup
  timer         *time.Timer        // 倒计时定时器
  state         State              // idle | running | paused
  phase         Phase              // focus | short_break | long_break
  remaining     time.Duration      // 当前阶段剩余秒数
  currentRound  int                // 当前轮次 (1-based)
  cfg           Config             // 时长配置

  onTick       func(TickPayload)   // 每秒回调 → emit Wails 事件
  onPhaseChange func(PhasePayload) // 阶段切换回调 → emit 事件 + 通知
  onStateChange func(StatePayload) // 状态变更回调
```

**Config 结构：**
```go
type Config struct {
    FocusDuration        int // 分钟，默认 25
    ShortBreakDuration   int // 分钟，默认 5
    LongBreakDuration    int // 分钟，默认 15
    RoundsBeforeLongBreak int // 默认 4
}
```

**新增文件：`app_pomodoro.go`** — Wails 绑定方法：

| 方法 | 说明 |
|---|---|
| `StartPomodoro()` | 开始当前阶段倒计时 |
| `PausePomodoro()` | 暂停倒计时 |
| `ResumePomodoro()` | 恢复倒计时 |
| `StopPomodoro()` | 结束计时，重置状态 |
| `GetPomodoroStatus()` | 返回 `{state, phase, remaining, currentRound, cfg}` |

**Wails 事件（backend → frontend）：**

| 事件 | Payload | 频率 |
|---|---|---|
| `pomodoro:tick` | `{remaining: int, phase: string, round: int}` | 每秒 |
| `pomodoro:phase:changed` | `{phase: string, message: string}` | 阶段切换时 |
| `pomodoro:state:changed` | `{state: string}` | 状态变更时 |

### 前端

**新增文件：`frontend/src/components/PomodoroPanel.vue`**

悬浮计时面板组件：
- glassmorphism 背景 + 圆角卡片（与 ChatBubble 风格一致）
- 左侧：conic-gradient 环形进度圈 + 居中倒计时 + 阶段标签
- 右侧上：轮次信息 + 进度点（实心=已完成，空心=待完成）
- 右侧下：暂停/继续 + 结束 按钮（垂直排列）
- 颜色随阶段切换：专注=红，短休息=绿，长休息=蓝
- 定位：相对宠物位置偏上方，`position: fixed`
- 进入/退出动画：Vue `<Transition>` + spring 物理动画

状态机（面板内部）：

```
未开始 → [点击开始] → 运行中（start 按钮变成暂停）
暂停   → [点击继续] → 运行中
运行中 → [点击暂停] → 暂停
运行中 → [点击结束] → 面板关闭
暂停   → [点击结束] → 面板关闭
倒计时到0         → 自动进入下一阶段（focus→break / break→focus）
                   最后长休息结束 → 面板保留显示"完成"，点击结束关闭
```

**修改文件：`frontend/src/components/ContextMenu.vue`**
- 新增"番茄钟"菜单项
- 面板打开时 `disabled`
- 面板关闭时恢复可用

**修改文件：`frontend/src/App.vue`**
- 管理 `pomodoroPanelOpen` 和 `pomodoroRunning` 状态
- 协调互斥：pomodoroRunning → 关闭 ChatBubble；chat streaming → 不自动开始番茄钟
- 监听 `pomodoro:state:changed` 事件更新 `pomodoroRunning`

**修改文件：`frontend/src/components/SettingsWindow.vue`**
- 侧边栏新增"番茄钟"标签页（`iconSvg` 用番茄/时钟图标）
- 四个数字输入字段：专注时长、短休息时长、长休息时长、长休息间隔
- 遵循现有 `settings-group` / `settings-row` 布局模式
- 修改通过 `SaveConfig` 持久化

### 数据存储

在 `settings` 表新增 key-value 配置项（无需新建表）：

| Key | 默认值 | 说明 |
|---|---|---|
| `pomodoro.focus_duration` | `25` | 专注时长（分钟） |
| `pomodoro.short_break_duration` | `5` | 短休息时长（分钟） |
| `pomodoro.long_break_duration` | `15` | 长休息时长（分钟） |
| `pomodoro.rounds_before_long_break` | `4` | 几轮后触发长休息 |

## 交互流程

```
右键宠物 → 点击"番茄钟" → PomodoroPanel 出现在宠物上方
  - 右键菜单"番茄钟" disabled
  - 面板显示：环形进度 100% + "准备开始" + 开始按钮

点击"开始" → Engine.Start()
  - 每秒 emit pomodoro:tick → 前端更新进度圈和倒计时
  - pet:state:change → "focusing"（宠物专注表情/动作）
  - 点击宠物 → 不打开聊天（互斥生效）

点击"暂停" → Engine.Pause()
  - 停止倒计时，保留剩余时间
  - pet:state:change → "idle"
  - 聊天可正常打开（暂停态不互斥）

点击"继续" → Engine.Resume()
  - 恢复倒计时
  - 重新互斥

点击"结束" → Engine.Stop()
  - 面板关闭，状态重置为 idle
  - 右键菜单"番茄钟"恢复

阶段自动切换（倒计时到 0）：
  - emit pomodoro:phase:changed {phase, message}
  - macOS 系统通知（notify.System）
  - 宠物说话通过 notification:show 显示为应用内气泡（因运行中聊天框关闭，无法用聊天消息）
  - 自动开始下一阶段倒计时
  - focus→short_break (绿色) → focus (红色) → ... → long_break (蓝色)
  - 第 N 轮长休息结束后 → 自动停止，面板保留显示"完成 ✓"
```

## 互斥规则

| 番茄钟状态 | 聊天框 | 结果 |
|---|---|---|
| 运行中 | 点击宠物 | 不打开聊天 |
| 运行中 | 聊天已打开 | 自动关闭聊天 |
| 暂停 | 点击宠物 | 正常打开聊天 |
| 未开始 | 点击宠物 | 正常打开聊天 |
| 任意 | LLM 流式响应中 | 番茄钟面板可打开但不自动开始 |

## 宠物行为

| 阶段 | 宠物状态 | 阶段切换时说的话 |
|---|---|---|
| 开始专注 | `focusing` | "开始专注！25 分钟后休息~" |
| 专注→短休息 | `resting` | "休息一下！5 分钟后继续。" |
| 短休息→专注 | `focusing` | "第 N 轮开始，加油！" |
| 专注→长休息 | `resting` | "已经完成 N 轮了，好好休息 15 分钟吧!" |
| 长休息→完成 | `idle` | "全部完成！今天效率很高！🎉" |

## 宠物状态定义

在 `usePetState.js` 中新增两个状态值：
- `focusing` — 专注中，可用于驱动 Live2D/VRM 的认真表情
- `resting` — 休息中，放松表情

## 注意事项

- 倒计时基于 wall-clock 时间戳：记录阶段结束时间点，tick 时计算 remaining = endTime − now。系统休眠醒来后自动修正——若已过结束时间则触发阶段切换，否则继续剩余倒计时。避免 monotonic clock 在休眠期间暂停导致计时偏差
- `app.go` 的 `shutdown(ctx)` 中需加入 `pomodoroEngine.Stop()`
- 番茄钟引擎在 `startup()` 中初始化（无需等待 LLM 组件，功能独立）
- `GetPomodoroStatus()` 返回 Config 含时长参数，前端据此渲染初始状态
- 若 Live2D/VRM 不支持 focusing/resting 表情，回退到 idle 状态（不报错）

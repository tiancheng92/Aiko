# VRM 行为系统重构设计

## 目标

将 VRM 宠物的情绪/动作驱动系统从「硬编码状态机 + 情绪标签仅驱动 blendshape」升级为「LLM 行为标签统一驱动动画 + blendshape」，同时移除 Live2D 的情绪标签支持（模型无素材）。

## 标签协议

```
[表现:emotion]
[表现:emotion,动作:action]
```

- **emotion**（必填）：joy, sad, surprised, angry, neutral
- **action**（可选）：wave, nod, celebrate, surprised_react
- 标签必须在回复第一行，换行后写正文
- 标签从显示文本中剔除
- 匹配失败或 buffer 超 60 字节 → 原文透传

### 示例

```
[表现:joy,动作:wave]
太棒了！我一直在等你！
```
→ 先播 wave（一次性）→ 持续 celebrate + blendshape happy=0.7

```
[表现:sad]
对不起，我查不到这个信息...
```
→ sad.vrma（持续）+ blendshape sad=0.7

```
[表现:neutral]
让我想想...
```
→ 清除 blendshape，回退 petState fallback

## 动画系统设计

### 两层模型

| 类型 | 语义 | 播放模式 | 生命周期 |
|---|---|---|---|
| emotion | 回复的情绪基调 | 循环（loop） | 持续到下一个行为标签或 petState→idle |
| action | 一次性手势 | 单次（once） | 播完自动回退 |

### 动画优先级

```
action（一次性） > emotion（持续） > petState（fallback）
```

### emotion → 动画映射

| emotion | 动画文件 | blendshape | 权重 |
|---|---|---|---|
| joy | celebrate.vrma | happy | 0.7 |
| sad | sad.vrma | sad | 0.7 |
| surprised | surprised_react.vrma | surprised | 0.7 |
| angry | angry.vrma | angry | 0.7 |
| neutral | 回退 petState fallback | 清除所有 | 0 |

### action → 动画映射

| action | 动画文件 | 播放时长 |
|---|---|---|
| wave | wave_big.vrma | ~3s（once） |
| nod | nod.vrma | ~2s（once） |
| surprised_react | surprised_react.vrma | ~3s（once） |
| celebrate | celebrate.vrma | ~2s（once） |

### petState fallback 动画

| 状态 | 动画 | 触发条件 |
|---|---|---|
| idle | waiting.vrma | 无活跃 emotion |
| thinking | waiting.vrma | 无活跃 emotion |
| speaking | hand_talk.vrma + mouth blendshape | 无活跃 emotion |
| listening | curious.vrma | 无活跃 emotion |
| error | embarrassed.vrma + sad blendshape | 始终 |

### chat:behavior 到达时

1. 如有 action → playAnimation(action, loop=false)，等待播完
2. 如 emotion ≠ neutral → 切换持续动画 + 设置 blendshape 0.7
3. 如 emotion = neutral → 清除 blendshape，切换到 petState fallback

### petState 变化时

1. idle → 清除 emotion 状态 + blendshape，切回 waiting
2. speaking → 驱动 mouth blendshape；如有活跃 emotion 保持当前动画
3. error → sad blendshape + embarrassed
4. 其他 → 如无活跃 emotion，切换 fallback 动画

## 后端改动

### 文件：`internal/agent/agent.go`

- `emotionPromptSuffix` → `behaviorPromptSuffix`
- system prompt 改为要求输出 `[表现:emotion]` 或 `[表现:emotion,动作:action]`

### 文件：`internal/agent/emotion.go`（重命名 behavior.go）

- `EmotionParser` → `BehaviorParser`
- 正则为 `^\[表现:(\w+)(?:,动作:(\w+))?\]\n?`
- `Feed()` 返回 `(text, emotion, action)`
- buffer 上限 60 字节

### 文件：`app_chat.go`

- emit `chat:behavior` {emotion, action} 替代 `chat:emotion`

### 文件：`internal/agent/emotion_test.go`（重命名 behavior_test.go）

- 测试用例更新为新格式

### 文件：`internal/agent/context.go`

- 情绪标签剥离逻辑适配新格式

## 前端改动

### 文件：`frontend/src/components/VRMPet.vue`

- 移除 `useEmotionEvents`，监听 `chat:behavior`（通过新 composable `useBehaviorEvents`）
- 新增 `applyBehavior({emotion, action})` 替代 `applyEmotion`
- `setState` 瘦身：移除 thinking/speaking 的 blendshape 保留逻辑
- `applyStateAnimation` 瘦身：移除 `_speakingEmotion` 逻辑，改用活跃 emotion
- 新增 `playAction` 方法：播放一次性动画后自动回退
- 移除 `EMOTION_SPEAKING_ANIMS`，新增 `EMOTION_ANIMS`（对所有状态生效）
- 新增 `ACTION_ANIMS` 映射表

### 文件：`frontend/src/composables/useBehaviorEvents.js`（新增）

- 类似 `useEmotionEvents`，监听 `chat:behavior` 事件

### 文件：`frontend/src/components/Live2DPet.vue`

- 回退 `useEmotionEvents` 相关改动（import、EMOTION_EXPRESSION_KEYWORDS、applyEmotionToExpression、订阅调用）
- `watch(petState)` 恢复硬编码 expression（星星眼、爱心等）

## 不变的部分

- pomodoro 面板位置修复（已独立完成）
- Live2D 渲染管线
- VRM 渲染管线（tick、headIK、mouthAnim、emotionBlend 等子模块）
- IDLE 自发动作系统（scheduleIdleVariety）
- petState 状态机本身（后端逻辑不变）
- 全局鼠标跟踪
- 点击穿透

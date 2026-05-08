# VRM Pet —— 3D 桌宠渲染后端（与 Live2D 双轨共存）

**Date:** 2026-05-08
**Status:** Draft → Ready for implementation planning
**Scope:** 在现有 Live2D 渲染后端之外，新增 VRM (three.js + three-vrm) 3D 渲染后端；两者通过配置项切换，共享位置/尺寸/状态/鼠标追踪逻辑。

---

## 背景与动机

当前 `Live2DPet.vue` (418 行) 已覆盖了渲染、拖拽、状态驱动、全局鼠标追踪、模型热切换。但 Live2D 有几个天花板：

- 表情/动作是模型作者预烘焙的有限集合，情绪表达单薄（硬切换，无过渡）。
- 2D 平面，头/身体不能真正转向；只能 `focus(x,y)` 做眼球微移。
- 模型库同质化（二次元风），资产文件夹式分发，体积大。
- TTS 播放时宠物不会"动嘴"。

VRM 作为开放 3D 标准：ARKit 52 blendshape 可加权混合、骨骼 IK 可转头转身、VRoid Hub 有大量免费商用模型、单文件 `.vrm` 发布。

**目标：** 在不删除 Live2D 的前提下，引入 VRM 渲染后端作为"可选升级"，并借机加入 Live2D 做不到的几个创意功能：LLM 情绪 → blendshape 加权混合、TTS 期间嘴巴张合动画、IK 头追鼠标 + 拖拽转身、用户 VRM 拖拽导入热载。

---

## 非目标

- **不**删除 Live2D 相关代码（保留双轨）。
- **不**做真正的音素级 viseme（只做正弦波张合嘴巴）。
- **不**做完整骨骼 IK 库（只操作 head/neck 两块骨骼）。
- **不**做 VRMA 动画文件加载（idle/motion 全部程序化）。
- **不**在本次变更中做 Windows/Linux 跨平台适配。

---

## 架构

### 组件结构

```
frontend/
├── public/vrm/                       # 内置 .vrm demo 模型（1-2 个，CC0/商用友好）
├── src/
│   ├── components/
│   │   ├── VRMPet.vue                # 新增：three.js + three-vrm 渲染
│   │   └── Live2DPet.vue             # 保持不变
│   ├── composables/
│   │   ├── usePetRenderer.js         # 新增：双端 imperative API 约定（文档）
│   │   ├── useVRMModel.js            # 新增：VRM 列表 + 当前选中，仿 useModelPath
│   │   └── useEmotionEvents.js       # 新增:订阅 chat:emotion → petRef.applyEmotion
│   └── App.vue                       # 改：根据 cfg.RenderBackend 挂载对应组件

internal/
├── config/                           # 改：Config 增加 RenderBackend / VRMModel
├── db/                               # 改：migration 注入新字段默认值
├── agent/                            # 改：system prompt 增加情绪前缀要求；流式剥离
app.go                                # 改：新增 VRM 相关 Wails 绑定方法
macos.go                              # 改：hitTest 选择器追加 .vrm-pet
```

### 组件协议（`usePetRenderer.js`）

两个宠物组件都通过 `defineExpose` 暴露统一 imperative API，父组件调用时不关心底层是 2D 还是 3D：

| 方法 | 参数 | Live2D 实现 | VRM 实现 |
|---|---|---|---|
| `setState(state)` | idle/thinking/speaking/listening/error | 切换预制表情 + motion | 更新状态标志，驱动内部子系统 |
| `focusGlobal(x, y)` | 全局屏幕坐标 | 换算后调用 `live2dModel.focus` | IK head + neck 欧拉角 |
| `speak(level)` | 0.0-1.0（当前版本忽略） | no-op | 当前版本忽略 level，speaking 状态靠正弦波 |
| `applyEmotion({emotion, intensity})` | emotion ∈ {joy,sad,surprised,angry,neutral}, intensity ∈ [0,1] | 降级为切对应表情 | blendshape 加权 lerp |
| `playMotion(name)` | motion 名 | `live2dModel.motion(name)` | 当前版本 no-op（未来扩展 VRMA）|
| `setSize(n)` | 像素 | resize canvas + scale model | resize renderer + camera aspect |
| `getPosition()` | — | 返回 `pos.value` | 返回自己的 `pos.value` |

---

## 数据流

### 流 1：petState 驱动（现有，两端共用）

```
Go: a.emitPetState("speaking")
  → Wails event "pet:state:change"
  → App.vue 监听 → petRef.setState("speaking")
  → VRMPet: 进入 speaking 分支；启动嘴巴正弦张合
    Live2DPet: 切换表情 + motion（现有逻辑不变）
```

### 流 2：LLM 情绪 → blendshape（新增）

```
Agent 回复开始
  → LLM 首行输出 [情绪:joy/0.8]\n
  → Go agent.go 流式 token 处理：
      维护 emotionParseState { parsing: bool, buffer: string }
      每个 token 到达，若 parsing：追加 buffer
        - 检测到 ]\n → parseEmotionTag(buffer) →
            emit Wails "chat:emotion" {emotion, intensity}
            ]\n 之后的剩余内容走正常 chat:token emit
            标记 parsing=false
        - buffer 超过 30 字符仍未见 ] → 放弃解析，
            整个 buffer 作为 chat:token emit，parsing=false
  → 前端 useEmotionEvents.js 收到 chat:emotion
  → petRef.applyEmotion({emotion, intensity})
  → VRMPet: 设置 targetWeights，每帧 lerp（~250ms 过渡到位）
```

**System prompt 追加：**
> 在每条回复的第一行必须输出情绪标签，格式严格为 `[情绪:emotion/intensity]`，其中 emotion ∈ {joy, sad, surprised, angry, neutral}，intensity ∈ [0.0, 1.0]。然后换行写正文。示例：`[情绪:joy/0.7]\n你好！很高兴见到你。`

### 流 3：speaking 状态嘴巴张合（新增，VRM 独占）

```
petState=speaking 到达
  → VRMPet 内部 mouthPhase=0，每帧 mouthPhase += dt * 8
  → openValue = (sin(mouthPhase) + 1) * 0.3
  → vrm.expressionManager.setValue('aa', openValue)
petState ≠ speaking
  → openValue lerp 到 0
```

---

## VRMPet 组件内部设计

### 依赖
```
yarn add three @pixiv/three-vrm @pixiv/three-vrm-springbone
```

### 渲染循环
```js
const scene = new THREE.Scene()
const camera = new THREE.PerspectiveCamera(30, 1, 0.1, 20)
camera.position.set(0, 1.3, 2.0)
const renderer = new THREE.WebGLRenderer({ canvas, alpha: true, antialias: true })
scene.add(new THREE.DirectionalLight(0xffffff, 1.0),
          new THREE.AmbientLight(0xffffff, 0.4))

// 加载 VRM
const loader = new GLTFLoader()
loader.register(p => new VRMLoaderPlugin(p))
const gltf = await loader.loadAsync(vrmPath)
vrm = gltf.userData.vrm
VRMUtils.removeUnnecessaryVertices(gltf.scene)
VRMUtils.removeUnnecessaryJoints(gltf.scene)
vrm.scene.rotation.y = Math.PI
scene.add(vrm.scene)

// 每帧
function tick() {
  const dt = clock.getDelta()
  updateHeadIK(dt)       // 头/脖 欧拉角追鼠标
  updateMouthAnim(dt)    // speaking 正弦
  updateEmotionBlend(dt) // lerp blendshape 权重
  updateIdleMicro(dt)    // 无交互时微动作
  updateBlink(dt)        // 眨眼
  vrm.update(dt)
  renderer.render(scene, camera)
  rafId = requestAnimationFrame(tick)
}
```

### 子系统

**IK 头追鼠标 + 拖拽转身：**
- 用 `vrm.humanoid.getNormalizedBoneNode('head'|'neck')` 直接设欧拉角
- `focusGlobal(mx, my)`：屏幕坐标 → 相对宠物中心偏移 → clamp 到头 ±60°、脖 ±30°
- 拖拽中：head/neck 继续追鼠标 + `vrm.scene.rotation.y` 额外补偿 ±20°（身体轻微跟随）
- 松手后 lerp 回 0

**情绪 blendshape 混合：**
- 映射表：`{joy:'happy', sad:'sad', surprised:'surprised', angry:'angry', neutral:null}`
- `applyEmotion({emotion, intensity})`：设置 `targetWeights[mapped] = intensity`，其余 = 0
- 每帧 `current = lerp(current, target, dt*4)`（约 250ms 过渡）

**嘴巴张合：**
- speaking 状态：`mouthPhase += dt * 8`，`openValue = (sin(phase)+1)*0.3`，set `aa` blendshape
- 非 speaking：lerp 到 0

**自主微动作：**
- 每 15-40 秒随机触发一次：轻微歪头（脖子 ±5° 2s 内回正）、呼吸幅度增强 2 个周期
- `setTimeout` 调度，`onScopeDispose` 清理

**眨眼：**
- 每 3-6 秒一次，`blink` expression：100ms 关 + 50ms 睁

**拖拽导入 VRM：**
- 容器层监听 `dragover`/`drop`
- 校验扩展名 `.vrm` + MIME
- `FileReader` → ArrayBuffer → base64 → `ImportVRMFile(name, base64)`
- Go 侧写入 `~/.aiko/vrm-models/{name}.vrm`，校验 glTF magic `glTF`
- 成功后触发 `config:vrm:model:changed` → 热载入

### 状态 → 行为映射

| petState | VRM 行为 |
|---|---|
| idle | 随机微动作 + 眨眼；情绪衰减到 neutral |
| thinking | 轻微点头循环 ±3°；情绪保持前一个值 |
| speaking | 嘴巴正弦张合；情绪由 `chat:emotion` 驱动 |
| listening | 头前倾 5°；情绪 neutral |
| error | 情绪 sad intensity=0.6；头低 5° |

---

## Go 后端改动

### Config（`internal/config/`）
```go
type Config struct {
    // ... 现有字段
    RenderBackend string `json:"render_backend"` // "live2d" | "vrm"，默认 "live2d"
    VRMModel      string `json:"vrm_model"`      // 当前 VRM 文件名
}
```
- DB migration：在 `internal/db/` 加一步，为现有 settings 行注入默认值 `live2d` / 空字符串。

### Wails 绑定（`app.go`）
```go
type VRMModelInfo struct {
    Name   string `json:"name"`
    URL    string `json:"url"`     // 前端 fetch 用
    Source string `json:"source"`  // "builtin" | "user"
    SizeKB int    `json:"size_kb"`
}

// ListVRMModels returns built-in and user-imported .vrm files.
func (a *App) ListVRMModels() ([]VRMModelInfo, error)

// GetVRMPath returns the URL for a given model name.
func (a *App) GetVRMPath(name string) (string, error)

// ImportVRMFile writes a base64-encoded .vrm to ~/.aiko/vrm-models/.
// Validates glTF magic header "glTF" before writing.
func (a *App) ImportVRMFile(name string, base64Data string) error

// DeleteVRMModel removes a user-imported .vrm.
func (a *App) DeleteVRMModel(name string) error
```

- 内置模型：`frontend/public/vrm/*.vrm`，通过 Wails asset server 暴露
- 用户模型：`~/.aiko/vrm-models/*.vrm`，通过 asset handler 暴露成 `/user-vrm/{name}`（实现细节对齐现有本地文件访问约定）
- **禁用 `time.Time` 作为 Wails 绑定返回字段**（遵循 CLAUDE.md 规范）
- 字段读写需持 `a.mu`（config 相关）

### Agent 情绪标签解析（`internal/agent/`）
- System prompt 追加情绪前缀要求（见上文数据流 2）
- 在流式 token pipeline 中插入解析层（放在 emit `chat:token` 之前）：
  ```go
  // parseEmotionTag extracts an emotion tag from the message prefix.
  // Matches: ^\[情绪:(\w+)/([\d.]+)\]\n?
  // Returns (emotion, intensity, remainingText, ok).
  func parseEmotionTag(s string) (string, float64, string, bool)
  ```
- Per-chat state：`{parsing bool, buffer string}`；首次进入该 session 或新的 assistant 回复开始时 reset 为 `{parsing: true, buffer: ""}`。
- Flush 保证：buffer 超过 30 字符、或 agent 流结束、或检测到明确非情绪前缀字符，立即把 buffer 全量 emit 为 `chat:token`，置 `parsing=false`。

### macos.go
hitTest 选择器追加 `.vrm-pet`：
```
.live2d-pet,.vrm-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.lightbox,.tool-confirm-modal,.execution-progress
```

### 不改动
- 鼠标全局轮询 `GetMousePosition`（VRMPet 复用）
- 位置/尺寸持久化 `GetBallPosition/SaveBallPosition/GetPetSize`（两个后端共用同一 key）
- `petState` emit 逻辑

---

## 错误处理

| 场景 | 处理 |
|---|---|
| VRM 文件损坏 | `loader.loadAsync` reject → catch → fallback Live2D + 通知提示 |
| 用户 drop 非 .vrm | 前端校验扩展名 + MIME；Go 侧校验 glTF magic |
| 情绪标签解析失败 | 静默降级，buffer 全量作为正文 emit，不阻塞对话流 |
| 切换 backend 时资源未清理 | 切换前组件 `onUnmounted` → `renderer.dispose()` + 递归 dispose geometry/material/texture |
| WebGL context lost | 监听 `webglcontextlost` → 尝试重建；失败则降级 Live2D |
| three-vrm 版本差异 | 使用 `@pixiv/three-vrm` v3（同时支持 VRM 0.x 和 1.0） |

---

## 测试策略

### Go 单元测试
- `parseEmotionTag`：
  - 正常格式 `[情绪:joy/0.8]\n正文`
  - 无标签（走 fallback）
  - intensity 越界 0/1 之外
  - Unicode 正文
  - 多行 + 多 `[`
  - 格式错误（缺少 `]`、缺少 `/`、intensity 不是数字）
  - buffer 超 30 字符保护

### 前端手动测试清单
- 切换 `RenderBackend` 后位置/尺寸保留
- VRM 模型热重载：设置面板切换 → 立即生效
- 拖拽 .vrm 到设置窗口 → 列表出现 → 可选中
- 删除用户 VRM → 列表消失
- `speaking` 状态嘴巴动，`chat:emotion` 到达时表情平滑过渡
- 情绪前缀不泄漏到聊天框（关键）
- 窗口 blur/focus、多屏切换后 VRM 位置正确
- 点击穿透：VRM 以外区域鼠标事件穿透到桌面
- 大 VRM (20MB+) 加载不卡死
- 长时间运行（30min+）无内存/显存泄漏

---

## 风险

| 风险 | 影响 | Mitigation |
|---|---|---|
| 包体膨胀 | three + three-vrm ≈ 250KB gzip；与 pixi 共存 ≈ +30% | 可接受；双轨共存期不去 pixi |
| LLM 不遵守情绪前缀 | 标签解析失败 | 30 字符 fallback；模型侧用 few-shot 强化 |
| three-vrm 版本兼容 | VRM 0.x vs 1.0 | 用 v3，内置 demo 都测 |
| WebGL GPU 占用 | 常驻渲染耗电 | 无交互/无状态变化时降帧 30→15fps；失焦 5fps |
| 恶意 VRM | 理论上可嵌 glTF 扩展 | 不启用自定义 extension handler |
| demo 模型版权 | 商用风险 | VRoid Hub 筛 CC0/商用友好，或用 Alicia Solid 公开测试模型 |

---

## 未来（不在本次范围）

- 真正的音素级 viseme（TTS 音频 AnalyserNode → aa/ih/ou/ee/oh 加权）
- VRMA 动画文件加载支持
- 完整骨骼 IK（扩到手臂/身体跟随）
- Windows/Linux 点击穿透移植
- 完全下线 Live2D（单轨 VRM）

---

## 参考

- [@pixiv/three-vrm](https://github.com/pixiv/three-vrm)
- [VRM 规范](https://vrm.dev/en/)
- [VRoid Hub](https://hub.vroid.com/)（模型来源）

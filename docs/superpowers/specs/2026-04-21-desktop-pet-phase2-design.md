# 桌面宠物项目二期功能设计文档

## 项目概述

桌面宠物项目二期功能开发，采用渐进式增强方案，在现有Wails+Vue+Live2D架构基础上逐步添加功能，确保向后兼容性和代码质量。

## 第一阶段功能范围

### 核心目标
- 提升AI系统的健壮性和用户体验
- 实现独立的设置管理界面
- 增强Live2D角色的交互性和表现力
- 建立可扩展的工具生态系统

### 功能清单
1. AI中间件系统（异常处理与恢复）
2. 独立设置窗口
3. 基础AI工具集（时间、系统信息）
4. Live2D状态动画系统
5. 右键菜单基础框架

## 详细设计

### 1. AI中间件系统

#### 架构设计
- **位置**: `internal/agent/middleware/` 包
- **设计模式**: 拦截器链模式
- **核心理念**: 所有工具调用都经过中间件链处理，提供统一的异常处理、重试和恢复机制

#### 关键组件

**ErrorRecoveryMiddleware**
- 功能: 捕获工具调用异常，提供友好的错误响应
- 实现: 将技术错误转换为用户可理解的消息
- 示例: "无法获取网络信息，请检查网络连接" 而不是 "dial tcp: no such host"

**RetryMiddleware** 
- 功能: 对可重试的工具调用进行自动重试
- 策略: 指数退避重试，最多3次
- 适用场景: 网络请求、文件访问等可能暂时失败的操作

**LoggingMiddleware**
- 功能: 记录所有工具调用的详细日志
- 内容: 调用时间、参数、结果、错误信息、耗时
- 用途: 问题诊断和性能分析

#### 接口设计
```go
type Middleware interface {
    Process(ctx context.Context, req ToolRequest, next ToolHandler) (*ToolResponse, error)
}

type ToolHandler func(ctx context.Context, req ToolRequest) (*ToolResponse, error)
```

### 2. 独立设置窗口

#### 实现方案
- **前端**: 新建 `SettingsWindow.vue` 组件，完全独立于 `ChatBubble.vue`
- **后端**: 在 `App` 结构体中添加 `OpenSettingsWindow()` 方法
- **窗口管理**: 通过Wails runtime创建新的浮动窗口，支持与聊天窗口同时打开

#### 窗口特性
- 尺寸: 600x500 像素
- 特性: 可调整大小、可最小化、置顶可选
- 位置: 相对于主屏幕居中
- 生命周期: 独立管理，关闭不影响主应用

#### 设置内容分类
1. **基础配置**: LLM模型、API密钥、代理设置
2. **工具权限管理**: 各个工具的启用/禁用和权限级别
3. **Live2D配置**: 模型选择、动画设置、显示比例
4. **界面设置**: 主题、字体大小、窗口透明度

#### 数据同步
- 设置变更立即保存到SQLite数据库
- 通过Wails Events通知主窗口更新配置
- 主窗口通过 `config.Store` 重新加载配置

### 3. 基础AI工具集

#### 工具架构设计
- **包结构**: `internal/tools/` 新包，与现有 `skill` 包并行
- **统一接口**: 
```go
type Tool interface {
    Name() string
    Description() string
    Permission() PermissionLevel
    Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error)
}
```

#### 权限级别定义
- `Public`: 无需用户授权（如获取当前时间）
- `Protected`: 需要用户一次性授权（如获取系统信息）
- `Restricted`: 每次使用都需要明确同意（如获取地理位置）

#### 具体工具实现

**TimeTools 工具集**
- `GetCurrentTime`: 获取当前本地时间
- `GetTimezone`: 获取系统时区信息
- `FormatTime`: 格式化时间字符串
- 权限级别: `Public`

**SystemTools 工具集**
- `GetOSInfo`: 获取操作系统信息（名称、版本、架构）
- `GetHardwareInfo`: 获取硬件配置（CPU、内存、磁盘）
- `GetNetworkStatus`: 获取网络连接状态
- 权限级别: `Protected`

**LocationTools 工具集**
- `GetLocation`: 获取地理位置信息（需要系统权限）
- `GetTimezoneByLocation`: 根据位置获取时区
- 权限级别: `Restricted`

#### 权限管理存储
```sql
-- 新增权限管理表
CREATE TABLE tool_permissions (
    tool_name TEXT PRIMARY KEY,
    permission_granted BOOLEAN NOT NULL DEFAULT FALSE,
    granted_at TIMESTAMP,
    last_used TIMESTAMP
);
```

### 4. Live2D状态动画系统

#### 状态定义
- `idle`: 空闲状态，播放默认动画
- `thinking`: AI思考中，显示思考动画和表情
- `speaking`: AI回复中，显示说话动画
- `listening`: 用户输入中，显示专注倾听动画
- `error`: 发生错误，显示困惑或尴尬表情

#### 事件驱动架构
- **后端事件**: 通过 `wailsruntime.EventsEmit` 发送状态变化
- **事件格式**: 
```json
{
  "event": "pet:state:change",
  "data": {
    "state": "thinking",
    "message": "正在思考您的问题...",
    "duration": 0  // 0表示持续到下次状态变更
  }
}
```

#### 前端状态管理
- `Live2DPet.vue` 组件监听状态事件
- 维护状态队列，支持状态优先级
- 状态切换时平滑过渡动画

#### 动画映射表
| 状态 | Live2D动画 | 表情 | 持续时间 | 优先级 |
|------|-----------|------|----------|--------|
| thinking | Idle + 左右摆头 | 困惑表情 | 持续 | 中 |
| speaking | TapBody循环 | 开心表情 | 持续 | 高 |
| listening | 眼睛跟踪鼠标 | 专注表情 | 持续 | 中 |
| error | 摇头动画 | 尴尬表情 | 3秒 | 高 |
| idle | Idle循环 | 默认表情 | 持续 | 低 |

### 5. 右键菜单基础框架

#### 实现策略
- 使用浏览器原生 `contextmenu` 事件
- 阻止默认右键菜单，显示自定义菜单
- 菜单项通过Wails绑定调用后端方法

#### Live2D宠物右键菜单
```
┌─────────────────────┐
│ 🎭 切换表情          │
│ 👗 更换模型          │
│ ⚙️ 宠物设置          │
│ ─────────────────   │
│ 📍 固定位置          │
│ 🏃 活动范围设置      │
│ ─────────────────   │
│ 🔧 打开设置          │
│ ❌ 退出程序          │
└─────────────────────┘
```

#### ChatBubble右键菜单
```
┌─────────────────────┐
│ 📋 复制聊天内容      │
│ 💾 导出聊天记录      │
│ 🗑️ 清空聊天历史      │
│ ─────────────────   │
│ 🔧 打开设置          │
│ 📖 使用帮助          │
└─────────────────────┘
```

#### 菜单组件设计
- 创建 `ContextMenu.vue` 通用右键菜单组件
- 支持动态菜单项配置
- 支持图标、分隔线、快捷键显示
- 自动定位，避免超出屏幕边界

## 技术实现细节

### 后端模块扩展
```
internal/
├── agent/
│   ├── middleware/          # 新增：中间件系统
│   │   ├── error_recovery.go
│   │   ├── retry.go
│   │   ├── logging.go
│   │   └── chain.go
│   └── agent.go            # 修改：集成中间件
├── tools/                  # 新增：工具系统
│   ├── base.go            # 工具接口定义
│   ├── time_tools.go      # 时间相关工具
│   ├── system_tools.go    # 系统信息工具
│   ├── location_tools.go  # 地理位置工具
│   └── permission.go      # 权限管理
└── config/
    └── config.go          # 修改：添加工具权限配置
```

### 前端组件扩展
```
frontend/src/
├── components/
│   ├── SettingsWindow.vue     # 新增：独立设置窗口
│   ├── ContextMenu.vue        # 新增：右键菜单组件
│   ├── ToolPermissions.vue    # 新增：工具权限管理
│   └── Live2DPet.vue         # 修改：状态动画系统
├── composables/
│   ├── useContextMenu.js      # 新增：右键菜单逻辑
│   ├── usePetState.js        # 新增：宠物状态管理
│   └── useSettings.js        # 新增：设置管理逻辑
└── utils/
    └── eventBus.js           # 新增：组件间通信
```

### 数据库架构更新
```sql
-- 工具权限表
CREATE TABLE tool_permissions (
    tool_name TEXT PRIMARY KEY,
    permission_granted BOOLEAN NOT NULL DEFAULT FALSE,
    granted_at TIMESTAMP,
    last_used TIMESTAMP
);

-- 宠物状态日志表（可选，用于分析）
CREATE TABLE pet_state_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    state TEXT NOT NULL,
    triggered_at TIMESTAMP NOT NULL,
    duration INTEGER -- 毫秒
);
```

## 用户体验流程

### 初次使用流程
1. 用户启动应用，Live2D宠物出现
2. 首次点击宠物时，显示欢迎信息和功能介绍
3. 自动打开设置窗口，引导用户完成基础配置
4. 工具权限采用渐进式申请（首次使用时询问）

### 日常使用流程
1. 用户与宠物对话
2. 宠物根据对话状态显示相应动画
3. 需要调用工具时，检查权限并执行
4. 出现错误时，显示友好提示而不是崩溃
5. 通过右键菜单快速访问常用功能

## 后续阶段预览

### 第二阶段计划（后续迭代）
- 多Live2D模型支持
- 宠物活动范围扩展（聊天框下方走动）
- 本地后端安全沙箱（localbackend）
- 更丰富的AI工具集
- 聊天界面CSS优化
- 文本选择和复制功能

### 扩展性考虑
- 插件化工具架构，便于第三方工具接入
- 模块化的中间件系统，支持自定义中间件
- 可配置的动画系统，支持自定义动画映射
- 国际化支持框架，便于多语言扩展

## 成功标准

### 功能完整性
- [ ] AI对话过程中工具调用异常不会中断对话
- [ ] 设置窗口可以与聊天窗口同时打开并正常工作
- [ ] Live2D宠物能够根据对话状态正确显示动画
- [ ] 右键菜单在宠物和聊天框上都能正常弹出
- [ ] 基础工具（时间、系统信息）能够正确获取并显示信息

### 性能要求
- 工具调用响应时间 < 500ms
- 状态动画切换延迟 < 100ms  
- 设置窗口打开时间 < 200ms
- 右键菜单响应时间 < 50ms

### 用户体验标准
- 错误信息用户友好，无技术术语
- 界面操作直观，无需说明文档
- 设置变更立即生效，无需重启
- 所有交互都有视觉反馈
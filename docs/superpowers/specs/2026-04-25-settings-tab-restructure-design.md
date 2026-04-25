# 设置界面 Tab 结构重组设计

**日期：** 2026-04-25

---

## 目标

将现有 9 个 tab 重组为 8 个，解决三个问题：
1. 「桌宠」tab 混杂了 AI 配置与外观配置
2. 「工具权限」与「MCP服务器」分散，工具生态割裂
3. 「短信监听」tab 混入了语音和音效无关设置
4. 「定时任务」与「提醒事项」同属自动化却分开

---

## 新 Tab 结构

### 顶层导航（8 个 tab，顺序如下）

| 顺序 | key | 图标 | 标签 |
|---|---|---|---|
| 1 | `model` | 🤖 | 模型 |
| 2 | `ai` | 🧠 | AI |
| 3 | `appearance` | 🎨 | 外观与交互 |
| 4 | `tools` | 🔧 | 工具 |
| 5 | `knowledge` | 📚 | 知识库 |
| 6 | `automation` | ⏰ | 自动化 |
| 7 | `lark` | 🪶 | 飞书 |
| 8 | `sms` | 📱 | 短信监听 |

---

## 各 Tab 内容详述

### 1. 模型 `model`
内容不变：模型配置 Profile 的增删改激活。

---

### 2. AI `ai`（新增）
从原「桌宠」tab 中分离出以下配置项：
- System Prompt（textarea）
- 短期记忆轮数（1-100，number input）
- 自我成长 Nudge 间隔（轮，number input）
- Skills 目录（每行一个路径，textarea）

---

### 3. 外观与交互 `appearance`（原「桌宠」重命名+扩充）
保留原桌宠外观配置，新增从「短信监听」迁移来的交互配置：
- Live2D 模型选择（grid 按钮）
- 桌宠大小（range slider）
- 聊天框宽度（range slider）
- 聊天框高度（range slider）
- 语音消息立刻发送（toggle，从短信监听迁入）
- 聊天音效（toggle，从短信监听迁入）

---

### 4. 工具 `tools`（原「工具权限」+「MCP服务器」合并）
内部使用子 tab 切换：

#### 子 tab：MCP服务器
内容不变：MCP 服务器列表、添加/编辑/删除/启禁用。

#### 子 tab：工具权限
分两区展示：

**Public 工具区**（无需授权）
- 标题：「内置工具（无需授权）」
- 所有 `public` 级别工具名横向排列，逗号分隔，灰色小字
- 仅展示，无交互

**Protected 工具区**（需手动开启）
- 标题：「需授权工具」
- 保持现有 perm-row 行列 + toggle 开关样式

子 tab 默认显示「MCP服务器」。

---

### 5. 知识库 `knowledge`
内容不变：导入文件、进度显示、知识源列表与删除。

---

### 6. 自动化 `automation`（原「定时任务」+「提醒事项」合并）
内部使用子 tab 切换：

#### 子 tab：定时任务
内容不变：任务列表、新建/编辑/删除/启禁用/立即执行。

#### 子 tab：提醒事项
内容不变：待触发列表（按触发时间升序）、刷新按钮、删除单条。

子 tab 默认显示「定时任务」。切换到「提醒事项」时自动加载列表（现有 watch 逻辑迁移为监听子 tab 变化）。

---

### 7. 飞书 `lark`
内容不变。

---

### 8. 短信监听 `sms`
移除语音和音效设置后，仅保留：
- 短信监听开关（状态指示点 + 开启/停止按钮）
- 错误提示
- 操作指引（授权步骤说明）

---

## 实现要点

### `activeTab` 变量
- 类型从 `'model' | 'pet' | 'tools' | 'knowledge' | 'mcp' | 'cron' | 'lark' | 'sms' | 'proactive'`
- 改为 `'model' | 'ai' | 'appearance' | 'tools' | 'knowledge' | 'automation' | 'lark' | 'sms'`
- 默认值保持 `'model'`

### 子 tab 状态
新增两个 ref：
- `toolsSubTab = ref('mcp')` — `'mcp' | 'permissions'`
- `automationSubTab = ref('cron')` — `'cron' | 'proactive'`

### 提醒事项加载时机
原 `watch(activeTab, v => { if (v === 'proactive') loadProactiveItems() })` 改为：
```js
watch(automationSubTab, v => { if (v === 'proactive') loadProactiveItems() })
```

### 工具权限 Public 区渲染
```js
const publicTools = computed(() => toolPerms.filter(p => p.Level === 'public').map(p => p.ToolName).join('、'))
const protectedTools = computed(() => toolPerms.filter(p => p.Level !== 'public'))
```

### 子 tab 样式
复用现有 `.win-sidebar` / `.active` 样式思路，但改为水平小 tab bar（`display: flex; gap: 8px`），放在 tab-pane 内顶部。与顶层导航视觉区分：字号略小、无图标、使用 pill 样式。

---

## 不变的部分
- 所有 Wails 绑定方法（Go 后端无需修改）
- 各功能的具体表单、列表、操作逻辑
- CSS 变量和整体毛玻璃主题风格

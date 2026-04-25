# 设置界面 Tab 结构重组 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将设置界面从 9 个平铺 tab 重组为 8 个语义清晰的 tab，其中「工具」和「自动化」内部使用子 tab 切换。

**Architecture:** 纯前端改动，仅修改 `SettingsWindow.vue` 一个文件。将 `activeTab` 的可选值更新，新增 `toolsSubTab` 和 `automationSubTab` 两个子 tab ref，迁移内容块并添加子 tab 导航 UI。Go 后端零改动。

**Tech Stack:** Vue 3 Composition API (`<script setup>`)，Wails v2 前端。

---

## 文件结构

**唯一修改文件：**
- Modify: `frontend/src/components/SettingsWindow.vue`

改动分五个独立 task 完成，每个 task 一次 commit，保持文件可运行状态。

---

## Task 1：更新顶层 tab 状态与导航栏

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`（script 区 + nav 区）

- [ ] **Step 1：更新 `activeTab` 默认值注释，新增两个子 tab ref**

找到：
```js
const activeTab = ref('model')  // 'model' | 'pet' | 'tools' | 'knowledge'
```
替换为：
```js
const activeTab = ref('model')  // 'model' | 'ai' | 'appearance' | 'tools' | 'knowledge' | 'automation' | 'lark' | 'sms'
const toolsSubTab = ref('mcp')         // 'mcp' | 'permissions'
const automationSubTab = ref('cron')   // 'cron' | 'proactive'
```

- [ ] **Step 2：新增 computed（工具权限分区）**

在 `watch(activeTab, ...)` 行之前插入：
```js
const publicToolNames = computed(() =>
  toolPerms.value.filter(p => p.Level === 'public').map(p => p.ToolName).join('、')
)
const protectedToolPerms = computed(() =>
  toolPerms.value.filter(p => p.Level !== 'public')
)
```

- [ ] **Step 3：替换侧边栏导航 HTML**

找到整个 `<nav class="win-sidebar">...</nav>` 块，替换为：
```html
<nav class="win-sidebar">
  <button :class="{ active: activeTab === 'model' }" @click="activeTab = 'model'">
    <span class="nav-icon">🤖</span><span class="nav-label">模型</span>
  </button>
  <button :class="{ active: activeTab === 'ai' }" @click="activeTab = 'ai'">
    <span class="nav-icon">🧠</span><span class="nav-label">AI</span>
  </button>
  <button :class="{ active: activeTab === 'appearance' }" @click="activeTab = 'appearance'">
    <span class="nav-icon">🎨</span><span class="nav-label">外观与交互</span>
  </button>
  <button :class="{ active: activeTab === 'tools' }" @click="activeTab = 'tools'">
    <span class="nav-icon">🔧</span><span class="nav-label">工具</span>
  </button>
  <button :class="{ active: activeTab === 'knowledge' }" @click="activeTab = 'knowledge'">
    <span class="nav-icon">📚</span><span class="nav-label">知识库</span>
  </button>
  <button :class="{ active: activeTab === 'automation' }" @click="activeTab = 'automation'">
    <span class="nav-icon">⏰</span><span class="nav-label">自动化</span>
  </button>
  <button :class="{ active: activeTab === 'lark' }" @click="activeTab = 'lark'">
    <span class="nav-icon">🪶</span><span class="nav-label">飞书</span>
  </button>
  <button :class="{ active: activeTab === 'sms' }" @click="activeTab = 'sms'">
    <span class="nav-icon">📱</span><span class="nav-label">短信监听</span>
  </button>
</nav>
```

- [ ] **Step 4：验证页面可渲染**

运行 `cd frontend && yarn build`，确认无编译错误。此时点击侧边栏 AI / 外观与交互 / 工具 / 自动化不显示内容（tab-pane 尚未添加），其余 tab 正常显示。

- [ ] **Step 5：commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "refactor(settings): update tab keys and add sub-tab refs"
```

---

## Task 2：新增「AI」tab，拆出原桌宠 AI 配置

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1：在 `<div v-if="activeTab === 'pet'"` 块之前插入 AI tab pane**

找到：
```html
        <!-- 模型设置 -->
        <div v-if="activeTab === 'model'" class="tab-pane">
```
在**模型 tab pane 结束标签 `</div>` 之后**（即 `<!-- 工具权限 -->` 注释之前）插入：
```html
        <!-- AI 设置 -->
        <div v-if="activeTab === 'ai'" class="tab-pane">
          <label>System Prompt<textarea v-model="cfg.SystemPrompt" rows="5" /></label>
          <label>短期记忆轮数（1-100）<input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" /></label>
          <label>自我成长 Nudge 间隔（轮）<input type="number" v-model.number="cfg.NudgeInterval" min="1" max="100" /></label>
          <label>Skills 目录<span class="field-hint">每行一个路径</span><textarea v-model="cfg.SkillsDirs" rows="3" placeholder="~/.aiko/skills" /></label>
        </div>
```

- [ ] **Step 2：从原「桌宠」tab pane 删除这四项**

在原 `<div v-if="activeTab === 'pet'"` 块中，找到并删除以下四行：
```html
          <label>System Prompt<textarea v-model="cfg.SystemPrompt" rows="5" /></label>
          <label>短期记忆轮数（1-100）<input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" /></label>
          <label>自我成长 Nudge 间隔（轮）<input type="number" v-model.number="cfg.NudgeInterval" min="1" max="100" /></label>
          <label>Skills 目录<span class="field-hint">每行一个路径</span><textarea v-model="cfg.SkillsDirs" rows="3" placeholder="~/.aiko/skills" /></label>
```

- [ ] **Step 3：将原 `v-if="activeTab === 'pet'"` 改为 `v-if="activeTab === 'appearance'"`**

找到：
```html
        <div v-if="activeTab === 'pet'" class="tab-pane">
```
替换为：
```html
        <!-- 外观与交互 -->
        <div v-if="activeTab === 'appearance'" class="tab-pane">
```

- [ ] **Step 4：验证**

运行 `cd frontend && yarn build`，无错误。在 dev 环境点击「AI」tab 看到四项配置；点击「外观与交互」看到 Live2D / 大小 / 聊天框配置（暂不含语音音效，下一 task 迁入）。

- [ ] **Step 5：commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "refactor(settings): split AI config into new AI tab"
```

---

## Task 3：迁移语音与音效到「外观与交互」，清理「短信监听」

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1：在「外观与交互」tab pane 末尾追加语音和音效**

找到 `appearance` tab pane 的最后一个 `</label>` 后、`</div>` 前，追加：
```html
          <div class="settings-section-title" style="margin-top:20px">语音与音效</div>
          <div class="sms-toggle-row" style="margin-top:8px">
            <span class="sms-status-label" style="flex:1">语音消息立刻发送</span>
            <label class="voice-auto-send-switch">
              <input type="checkbox" v-model="cfg.VoiceAutoSend" @change="toggleVoiceAutoSend" />
              <span class="voice-auto-send-slider"></span>
            </label>
          </div>
          <p class="sms-desc" style="margin-top:4px">释放 Option 键后，等待转录完成并自动发送消息</p>
          <div class="sms-toggle-row" style="margin-top:16px">
            <span class="sms-status-label" style="flex:1">聊天音效</span>
            <label class="voice-auto-send-switch">
              <input type="checkbox" v-model="cfg.SoundsEnabled" @change="toggleSoundsEnabled" />
              <span class="voice-auto-send-slider"></span>
            </label>
          </div>
          <p class="sms-desc" style="margin-top:4px">发送、收到消息和出错时播放轻柔提示音</p>
```

- [ ] **Step 2：从「短信监听」tab pane 删除语音和音效部分**

在 `<div v-if="activeTab === 'sms'"` 块中，找到并删除从 `<!-- Voice auto-send toggle -->` 注释到音效 `</p>` 为止的整段（约 25 行）：
```html
          <!-- Voice auto-send toggle -->
          <div class="settings-section-title" style="margin-top:20px">语音设置</div>
          <div class="sms-toggle-row" style="margin-top:8px">
            ...（语音 toggle）...
          </div>
          <p class="sms-desc" style="margin-top:4px">释放 Option 键后，等待转录完成并自动发送消息</p>

          <!-- Sounds toggle -->
          <div class="sms-toggle-row" style="margin-top:16px">
            ...（音效 toggle）...
          </div>
          <p class="sms-desc" style="margin-top:4px">发送、收到消息和出错时播放轻柔提示音</p>
```

- [ ] **Step 3：验证**

运行 `cd frontend && yarn build`，无错误。「外观与交互」tab 末尾出现语音与音效两个 toggle；「短信监听」tab 只剩开关、错误提示和步骤指引。

- [ ] **Step 4：commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "refactor(settings): move voice/sounds to appearance tab"
```

---

## Task 4：新增「工具」tab（MCP + 权限子 tab）

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1：插入子 tab 导航 CSS**

在 `<style>` 区末尾追加：
```css
/* 子 tab 导航 */
.sub-tab-bar {
  display: flex;
  gap: 6px;
  margin-bottom: 16px;
}
.sub-tab-bar button {
  padding: 4px 14px;
  border-radius: 20px;
  border: 1px solid rgba(255,255,255,0.15);
  background: transparent;
  color: rgba(255,255,255,0.5);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
}
.sub-tab-bar button.active {
  background: rgba(255,255,255,0.12);
  color: rgba(255,255,255,0.9);
  border-color: rgba(255,255,255,0.3);
}
.public-tools {
  font-size: 12px;
  color: rgba(255,255,255,0.35);
  line-height: 1.8;
  margin-bottom: 16px;
}
.public-tools-title {
  font-size: 11px;
  color: rgba(255,255,255,0.25);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 6px;
}
.protected-tools-title {
  font-size: 11px;
  color: rgba(255,255,255,0.25);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 10px;
  margin-top: 16px;
}
```

- [ ] **Step 2：新建「工具」tab pane，替换原两个 pane**

找到：
```html
        <!-- 工具权限 -->
        <div v-if="activeTab === 'tools'" class="tab-pane">
```
（原工具权限 pane 结束于其 `</div>`，紧接着是「知识库」pane）

将原「工具权限」pane 和原「MCP」pane（`v-if="activeTab === 'mcp'"`）**全部替换**为以下单个「工具」pane：

```html
        <!-- 工具 -->
        <div v-if="activeTab === 'tools'" class="tab-pane">
          <div class="sub-tab-bar">
            <button :class="{ active: toolsSubTab === 'mcp' }" @click="toolsSubTab = 'mcp'">MCP 服务器</button>
            <button :class="{ active: toolsSubTab === 'permissions' }" @click="toolsSubTab = 'permissions'">工具权限</button>
          </div>

          <!-- MCP 子 tab -->
          <template v-if="toolsSubTab === 'mcp'">
            <div class="section-header">
              <h3>MCP 服务器</h3>
              <button class="btn-small" @click="openMCPForm">+ 添加</button>
            </div>

            <div v-if="mcpServers.length === 0" class="empty-hint">
              暂无 MCP 服务器，点击"添加"接入外部工具
            </div>

            <div v-for="srv in mcpServers" :key="srv.id" class="mcp-row">
              <div class="mcp-info">
                <span class="mcp-name">{{ srv.name }}</span>
                <span class="mcp-transport">{{ srv.transport }}</span>
                <span class="mcp-endpoint">{{ srv.transport === 'stdio' ? srv.command : srv.url }}</span>
              </div>
              <div class="mcp-actions">
                <button class="btn-toggle" :class="{ active: srv.enabled }" @click="toggleMCPServer(srv)">
                  {{ srv.enabled ? '已启用' : '已禁用' }}
                </button>
                <button class="btn-small" @click="editMCPServer(srv)">编辑</button>
                <button class="btn-danger-small" @click="deleteMCPServer(srv.id)">删除</button>
              </div>
            </div>

            <!-- Add/Edit Form（完整保留原有表单，不做任何改动） -->
            <div v-if="showMCPForm" class="mcp-form">
              <!-- 此处粘贴原 mcp-form 的完整内容，一字不改 -->
            </div>
          </template>

          <!-- 工具权限子 tab -->
          <template v-if="toolsSubTab === 'permissions'">
            <div v-if="toolPerms.length === 0" class="empty">暂无工具信息</div>
            <template v-else>
              <div class="public-tools-title">内置工具（无需授权）</div>
              <div class="public-tools">{{ publicToolNames }}</div>
              <div class="protected-tools-title">需授权工具</div>
              <div v-for="perm in protectedToolPerms" :key="perm.ToolName" class="perm-row">
                <div class="perm-info">
                  <span class="perm-name">{{ perm.ToolName }}</span>
                  <span :class="['perm-level', perm.Level]">{{ perm.Level }}</span>
                </div>
                <label class="toggle">
                  <input type="checkbox" :checked="perm.Granted" @change="togglePerm(perm)" />
                  <span class="toggle-track" />
                </label>
              </div>
            </template>
          </template>
        </div>
```

> **注意**：上面 MCP 表单 `<!-- 此处粘贴原 mcp-form 的完整内容，一字不改 -->` 注释处，需要将原 `<div v-if="showMCPForm" class="mcp-form">...</div>` 的完整内容（含所有 form-row、transport 切换、按钮）原样粘贴进来，不作修改。

- [ ] **Step 3：验证**

运行 `cd frontend && yarn build`，无错误。点击「工具」→「MCP 服务器」子 tab 看到 MCP 列表；点击「工具权限」子 tab 看到 public 工具名逗号列表 + protected 工具 toggle。

- [ ] **Step 4：commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "refactor(settings): merge MCP and tool permissions into tools tab with sub-tabs"
```

---

## Task 5：新增「自动化」tab（定时任务 + 提醒事项子 tab）

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1：更新提醒事项加载 watch**

找到：
```js
watch(activeTab, v => { if (v === 'proactive') loadProactiveItems() })
```
替换为：
```js
watch(automationSubTab, v => { if (v === 'proactive') loadProactiveItems() })
```

- [ ] **Step 2：新建「自动化」tab pane，替换原两个 pane**

找到原「定时任务」pane（`v-if="activeTab === 'cron'"`）和原「提醒事项」pane（`v-if="activeTab === 'proactive'"`），**全部替换**为以下单个「自动化」pane：

```html
        <!-- 自动化 -->
        <div v-if="activeTab === 'automation'" class="tab-pane">
          <div class="sub-tab-bar">
            <button :class="{ active: automationSubTab === 'cron' }" @click="automationSubTab = 'cron'">定时任务</button>
            <button :class="{ active: automationSubTab === 'proactive' }" @click="automationSubTab = 'proactive'">提醒事项</button>
          </div>

          <!-- 定时任务子 tab -->
          <template v-if="automationSubTab === 'cron'">
            <div class="section-header">
              <h3>定时任务</h3>
              <button class="btn-small" @click="openCronForm">+ 新建</button>
            </div>
            <!-- 此处粘贴原 cron tab pane 内的全部内容（新建表单 + 任务列表 + 内联编辑），一字不改 -->
          </template>

          <!-- 提醒事项子 tab -->
          <template v-if="automationSubTab === 'proactive'">
            <div class="section-header">
              <h3>提醒事项</h3>
              <button class="btn-small" @click="loadProactiveItems">刷新</button>
            </div>
            <div v-if="proactiveError" class="form-error">{{ proactiveError }}</div>
            <div v-if="proactiveItems.length === 0 && !proactiveError" class="empty-hint">
              暂无待触发的提醒事项
            </div>
            <div v-for="item in proactiveItems" :key="item.ID" class="proactive-row">
              <div class="proactive-info">
                <span class="proactive-time">{{ formatProactiveTime(item.TriggerAt) }}</span>
                <span class="proactive-prompt">{{ truncatePrompt(item.Prompt, 60) }}</span>
              </div>
              <button class="btn-small btn-danger" @click="deleteProactiveItem(item.ID)">删除</button>
            </div>
          </template>
        </div>
```

> **注意**：`<!-- 此处粘贴原 cron tab pane 内的全部内容 -->` 处需将原 `<div v-if="activeTab === 'cron'"` 内的所有子元素（section-header 除外，已在上方重写）原样粘贴，从新建表单 `<div v-if="showCronForm && cronForm.id === 0"` 开始到任务列表末尾 `</div>` 为止。

- [ ] **Step 3：验证**

运行 `cd frontend && yarn build`，无错误。点击「自动化」→「定时任务」看到任务列表；点击「提醒事项」触发 `loadProactiveItems` 并显示列表。

- [ ] **Step 4：最终全量验证**

```bash
cd frontend && yarn build
```

检查所有 8 个顶层 tab 均可点击并显示正确内容：
- 🤖 模型 → 模型 Profile 列表
- 🧠 AI → System Prompt、记忆轮数、Nudge、Skills
- 🎨 外观与交互 → Live2D、大小、聊天框、语音、音效
- 🔧 工具 → 子 tab：MCP服务器 / 工具权限
- 📚 知识库 → 知识源列表
- ⏰ 自动化 → 子 tab：定时任务 / 提醒事项
- 🪶 飞书 → 飞书配置
- 📱 短信监听 → 仅短信开关和说明

- [ ] **Step 5：commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "refactor(settings): merge cron and reminders into automation tab with sub-tabs"
```

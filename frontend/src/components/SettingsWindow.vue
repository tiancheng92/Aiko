<!-- frontend/src/components/SettingsWindow.vue -->
<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import {
  GetConfig, SaveConfig,
  ImportKnowledge, ListKnowledgeSources, DeleteKnowledgeSource,
  OpenFileDialog, GetToolPermissions, SetToolPermission,
  ListLLMModels,
  ListMCPServers, AddMCPServer, UpdateMCPServer, DeleteMCPServer,
  ListCronJobs, CreateCronJob, UpdateCronJob, DeleteCronJob, SetCronJobEnabled, RunCronJobNow,
  LarkStatus, LarkRunCommand,
} from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { useModelPath } from '../composables/useModelPath.js'

const emit = defineEmits(['close'])

const cfg = ref({
  LLMBaseURL: '', LLMAPIKey: '', LLMModel: '', EmbeddingModel: '',
  Live2DModel: 'hiyori',
  SystemPrompt: '', ShortTermLimit: 30, SkillsDir: '',
  EmbeddingDim: 1536,
  PetSize: 0,
  ChatWidth: 0,
  ChatHeight: 0,
})
const { availableModels, loadModels } = useModelPath()
const toolPerms = ref([])   // [{ ToolName, Level, Granted }]
const sources = ref([])
const importProgress = ref(null)
const saving = ref(false)
const statusMsg = ref('')
const activeTab = ref('model')  // 'model' | 'pet' | 'tools' | 'knowledge'

const llmModels = ref([])       // fetched from /v1/models
const fetchingModels = ref(false)

// MCP servers
const mcpServers = ref([])
const showMCPForm = ref(false)
const mcpForm = ref({ id: 0, name: '', transport: 'stdio', command: '', args: '', url: '', headers: '', enabled: true })
const mcpFormError = ref('')

// Cron jobs
const cronJobs = ref([])
const showCronForm = ref(false)
const cronForm = ref({ id: 0, name: '', description: '', schedule: '', prompt: '' })
const cronFormError = ref('')

// Lark
const larkStatus = ref('')
const larkStatusLoading = ref(false)
const larkStatusError = ref('')

// Draggable window state
const pos = ref({ x: Math.round(window.innerWidth / 2 - 300), y: Math.round(window.innerHeight / 2 - 250) })
let dragStart = null
let offProgress = null

onMounted(async () => {
  loadModels()
  const loaded = await GetConfig()
  if (loaded) Object.assign(cfg.value, loaded)
  sources.value = await ListKnowledgeSources() || []
  try { toolPerms.value = await GetToolPermissions() || [] } catch {}
  await fetchMCPServers()
  await fetchCronJobs()
  fetchLarkStatus()
  offProgress = EventsOn('knowledge:progress', (p) => { importProgress.value = p })
  // Auto-fetch model list if URL is already configured.
  if (cfg.value.LLMBaseURL) fetchLLMModels()
})

onUnmounted(() => offProgress?.())

/** fetchLLMModels calls the backend with the current form values to list available models. */
async function fetchLLMModels() {
  fetchingModels.value = true
  statusMsg.value = ''
  try {
    llmModels.value = await ListLLMModels(cfg.value.LLMBaseURL, cfg.value.LLMAPIKey) || []
    if (llmModels.value.length === 0) statusMsg.value = '未获取到模型列表'
  } catch (e) {
    statusMsg.value = '获取模型失败: ' + e
    llmModels.value = []
  } finally {
    fetchingModels.value = false
  }
}

/** previewPetSize emits a real-time size change event so the pet resizes without saving. */
function previewPetSize(e) {
  const size = Number(e.target.value)
  cfg.value.PetSize = size
  EventsEmit('config:pet:size:changed', size)
}

/** previewChatSize emits a real-time event so ChatBubble resizes without saving. */
function previewChatSize(field, e) {
  const val = Number(e.target.value)
  cfg.value[field] = val
  EventsEmit('config:chat:size:changed', { width: cfg.value.ChatWidth, height: cfg.value.ChatHeight })
}

/** resetChatSize restores default chat bubble dimensions. */
function resetChatSize() {
  cfg.value.ChatWidth  = 0
  cfg.value.ChatHeight = 0
  EventsEmit('config:chat:size:changed', { width: 0, height: 0 })
}

/** save persists configuration to the backend. */
async function save() {
  saving.value = true
  statusMsg.value = ''
  try {
    await SaveConfig(cfg.value)
    EventsEmit('config:model:changed', cfg.value.Live2DModel)
    statusMsg.value = '已保存'
  } catch (e) {
    statusMsg.value = '保存失败: ' + e
  } finally {
    saving.value = false
  }
}

/** togglePerm toggles a tool permission on/off. */
async function togglePerm(perm) {
  try {
    await SetToolPermission(perm.ToolName, !perm.Granted)
    perm.Granted = !perm.Granted
  } catch (e) {
    statusMsg.value = '权限更新失败: ' + e
  }
}

/** importFile opens a file picker and imports into knowledge base. */
async function importFile() {
  const path = await OpenFileDialog('选择文档', [{ DisplayName: '文档', Pattern: '*.txt;*.md;*.pdf;*.epub' }])
  if (!path) return
  importProgress.value = { Source: path, Total: 0, Processed: 0 }
  try {
    await ImportKnowledge(path)
    sources.value = await ListKnowledgeSources() || []
  } catch (e) {
    statusMsg.value = '导入失败: ' + e
  } finally {
    importProgress.value = null
  }
}

/** deleteSource removes a knowledge source. */
async function deleteSource(src) {
  try {
    await DeleteKnowledgeSource(src)
    sources.value = sources.value.filter(s => s !== src)
  } catch (e) {
    statusMsg.value = '删除失败: ' + e
  }
}

/** onHeaderMouseDown begins dragging the settings window. */
function onHeaderMouseDown(e) {
  dragStart = { mx: e.clientX - pos.value.x, my: e.clientY - pos.value.y }
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}

/** onMouseMove updates position during drag. */
function onMouseMove(e) {
  if (!dragStart) return
  pos.value = { x: e.clientX - dragStart.mx, y: e.clientY - dragStart.my }
}

/** onMouseUp ends the drag. */
function onMouseUp() {
  dragStart = null
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
}

/** fetchMCPServers loads the MCP server list from the backend. */
async function fetchMCPServers() {
  try {
    mcpServers.value = await ListMCPServers() || []
  } catch (e) {
    console.error('fetchMCPServers:', e)
  }
}

/** openMCPForm opens the add-server form with empty fields. */
function openMCPForm() {
  mcpForm.value = { id: 0, name: '', transport: 'stdio', command: '', args: '', url: '', headers: '', enabled: true }
  mcpFormError.value = ''
  showMCPForm.value = true
}

/** saveMCPServer adds or updates an MCP server. */
async function saveMCPServer() {
  mcpFormError.value = ''
  // Parse headers string ("Key: Value\nKey2: Value2") into map
  const headers = {}
  if (mcpForm.value.headers) {
    for (const line of mcpForm.value.headers.split('\n')) {
      const idx = line.indexOf(':')
      if (idx > 0) {
        const k = line.slice(0, idx).trim()
        const v = line.slice(idx + 1).trim()
        if (k) headers[k] = v
      }
    }
  }
  const cfg = {
    ...mcpForm.value,
    args: mcpForm.value.args ? mcpForm.value.args.split(' ').filter(Boolean) : [],
    headers,
  }
  try {
    if (cfg.id === 0) {
      await AddMCPServer(cfg)
    } else {
      await UpdateMCPServer(cfg)
    }
    showMCPForm.value = false
    await fetchMCPServers()
  } catch (e) {
    mcpFormError.value = String(e)
  }
}

/** deleteMCPServer removes an MCP server by ID. */
async function deleteMCPServer(id) {
  try {
    await DeleteMCPServer(id)
    await fetchMCPServers()
  } catch (e) {
    console.error('deleteMCPServer:', e)
  }
}

/** toggleMCPServer toggles the enabled state of an MCP server. */
async function toggleMCPServer(srv) {
  try {
    await UpdateMCPServer({ ...srv, enabled: !srv.enabled })
    await fetchMCPServers()
  } catch (e) {
    console.error('toggleMCPServer:', e)
  }
}

// ─── Cron jobs ───────────────────────────────────────────────────────────────

/** fetchCronJobs reloads the job list from the backend. */
async function fetchCronJobs() {
  try { cronJobs.value = await ListCronJobs() || [] } catch (e) { console.error('fetchCronJobs:', e) }
}

/** openCronForm opens the form to create a new job. */
function openCronForm() {
  cronForm.value = { id: 0, name: '', description: '', schedule: '', prompt: '' }
  cronFormError.value = ''
  showCronForm.value = true
}

/** editCronJob opens the form pre-filled with an existing job. */
function editCronJob(job) {
  cronForm.value = { id: job.ID, name: job.Name, description: job.Description, schedule: job.Schedule, prompt: job.Prompt }
  cronFormError.value = ''
  showCronForm.value = true
}

/** saveCronJob creates or updates a job. */
async function saveCronJob() {
  const { id, name, description, schedule, prompt } = cronForm.value
  if (!name.trim() || !schedule.trim() || !prompt.trim()) {
    cronFormError.value = '名称、Cron 表达式和触发提示词为必填项'
    return
  }
  try {
    if (id) {
      await UpdateCronJob(id, name, description, schedule, prompt)
    } else {
      await CreateCronJob(name, description, schedule, prompt)
    }
    showCronForm.value = false
    await fetchCronJobs()
  } catch (e) {
    cronFormError.value = String(e)
  }
}

/** deleteCronJob removes a job after confirmation. */
async function deleteCronJob(id) {
  try {
    await DeleteCronJob(id)
    await fetchCronJobs()
  } catch (e) {
    console.error('deleteCronJob:', e)
  }
}

/** toggleCronJob enables or disables a job. */
async function toggleCronJob(job) {
  try {
    await SetCronJobEnabled(job.ID, !job.Enabled)
    await fetchCronJobs()
  } catch (e) {
    console.error('toggleCronJob:', e)
  }
}

/** runCronJobNow fires a job immediately. */
async function runCronJobNow(id) {
  try {
    await RunCronJobNow(id)
    statusMsg.value = '已触发执行'
  } catch (e) {
    statusMsg.value = '触发失败: ' + e
  }
}

/** fetchLarkStatus checks lark-cli auth status. */
async function fetchLarkStatus() {
  larkStatusLoading.value = true
  larkStatusError.value = ''
  try {
    larkStatus.value = await LarkStatus()
  } catch (e) {
    larkStatusError.value = String(e)
    larkStatus.value = ''
  } finally {
    larkStatusLoading.value = false
  }
}
</script>

<template>
  <div class="settings-win" :style="{ left: pos.x + 'px', top: pos.y + 'px' }">
    <!-- Draggable title bar -->
    <div class="win-titlebar" @mousedown="onHeaderMouseDown">
      <span class="win-title">设置</span>
      <button class="win-close" @click="$emit('close')">✕</button>
    </div>

    <!-- Sidebar + content -->
    <div class="win-body">
      <nav class="win-sidebar">
        <button :class="{ active: activeTab === 'model' }" @click="activeTab = 'model'">🤖 模型</button>
        <button :class="{ active: activeTab === 'pet' }" @click="activeTab = 'pet'">🐾 宠物</button>
        <button :class="{ active: activeTab === 'tools' }" @click="activeTab = 'tools'">🔧 工具权限</button>
        <button :class="{ active: activeTab === 'knowledge' }" @click="activeTab = 'knowledge'">📚 知识库</button>
        <button :class="{ active: activeTab === 'mcp' }" @click="activeTab = 'mcp'">🔌 MCP服务器</button>
        <button :class="{ active: activeTab === 'cron' }" @click="activeTab = 'cron'">⏰ 定时任务</button>
        <button :class="{ active: activeTab === 'lark' }" @click="activeTab = 'lark'">🪶 飞书</button>
      </nav>

      <div class="win-content">
        <!-- 模型设置 -->
        <div v-if="activeTab === 'model'" class="tab-pane">
          <label>Base URL
            <div class="url-row">
              <input v-model="cfg.LLMBaseURL" placeholder="http://localhost:11434/v1" />
              <button class="fetch-btn" @click="fetchLLMModels" :disabled="fetchingModels || !cfg.LLMBaseURL">
                {{ fetchingModels ? '获取中...' : '获取模型' }}
              </button>
            </div>
          </label>
          <label>API Key<input v-model="cfg.LLMAPIKey" type="password" placeholder="（可选）" /></label>
          <label>Model
            <div class="select-row">
              <select v-if="llmModels.length" v-model="cfg.LLMModel">
                <option value="">-- 请选择模型 --</option>
                <option v-for="m in llmModels" :key="m" :value="m">{{ m }}</option>
              </select>
              <input v-else v-model="cfg.LLMModel" placeholder="qwen2.5:7b" />
            </div>
          </label>
          <label>Embedding Model
            <div class="select-row">
              <select v-if="llmModels.length" v-model="cfg.EmbeddingModel">
                <option value="">-- 不使用 Embedding --</option>
                <option v-for="m in llmModels" :key="m" :value="m">{{ m }}</option>
              </select>
              <input v-else v-model="cfg.EmbeddingModel" placeholder="nomic-embed-text（可选）" />
            </div>
          </label>
          <label>Embedding 维度<input type="number" v-model.number="cfg.EmbeddingDim" min="256" max="4096" /></label>
        </div>

        <!-- 宠物设置 -->
        <div v-if="activeTab === 'pet'" class="tab-pane">
          <label>Live2D 模型
            <div class="model-grid">
              <button
                v-for="m in availableModels"
                :key="m"
                :class="['model-btn', { selected: cfg.Live2DModel === m }]"
                @click="cfg.Live2DModel = m"
              >{{ m }}</button>
            </div>
          </label>
          <label>宠物大小
            <div class="size-row">
              <input
                type="range" min="100" max="600" step="10"
                :value="cfg.PetSize || 200"
                @input="previewPetSize"
              />
              <span class="size-val">{{ cfg.PetSize || '自动' }}{{ cfg.PetSize ? 'px' : '' }}</span>
            </div>
            <div class="size-hint">0 = 自动根据屏幕高度计算；拖动滑块实时预览，保存后生效</div>
            <button class="btn-reset-size" @click="cfg.PetSize = 0; EventsEmit('config:pet:size:changed', 0)">重置为自动</button>
          </label>
          <label>聊天框宽度
            <div class="size-row">
              <input
                type="range" min="300" max="800" step="10"
                :value="cfg.ChatWidth || 420"
                @input="previewChatSize('ChatWidth', $event)"
              />
              <span class="size-val">{{ cfg.ChatWidth || '默认' }}{{ cfg.ChatWidth ? 'px' : '' }}</span>
            </div>
          </label>
          <label>聊天框高度
            <div class="size-row">
              <input
                type="range" min="320" max="900" step="10"
                :value="cfg.ChatHeight || 540"
                @input="previewChatSize('ChatHeight', $event)"
              />
              <span class="size-val">{{ cfg.ChatHeight || '默认' }}{{ cfg.ChatHeight ? 'px' : '' }}</span>
            </div>
            <button class="btn-reset-size" @click="resetChatSize">重置为默认</button>
          </label>
          <label>System Prompt<textarea v-model="cfg.SystemPrompt" rows="5" /></label>
          <label>短期记忆轮数（1-100）<input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" /></label>
          <label>Skills 目录<input v-model="cfg.SkillsDir" placeholder="~/.desktop-pet/skills" /></label>
        </div>

        <!-- 工具权限 -->
        <div v-if="activeTab === 'tools'" class="tab-pane">
          <p class="hint">Public 工具无需授权；Protected 工具需要手动开启。</p>
          <div v-if="toolPerms.length === 0" class="empty">暂无工具信息</div>
          <div v-for="perm in toolPerms" :key="perm.ToolName" class="perm-row">
            <div class="perm-info">
              <span class="perm-name">{{ perm.ToolName }}</span>
              <span :class="['perm-level', perm.Level]">{{ perm.Level }}</span>
            </div>
            <label class="toggle">
              <input type="checkbox" :checked="perm.Granted" :disabled="perm.Level === 'public'" @change="togglePerm(perm)" />
              <span class="toggle-track" />
            </label>
          </div>
        </div>

        <!-- 知识库 -->
        <div v-if="activeTab === 'knowledge'" class="tab-pane">
          <button @click="importFile" :disabled="!!importProgress">导入文件</button>
          <div v-if="importProgress" class="progress">
            {{ importProgress.Source }}: {{ importProgress.Processed }}/{{ importProgress.Total }}
          </div>
          <ul v-if="sources.length">
            <li v-for="src in sources" :key="src">
              <span>{{ src }}</span>
              <button @click="deleteSource(src)">删除</button>
            </li>
          </ul>
          <p v-else class="empty">暂无知识库文件</p>
        </div>

        <!-- MCP Servers Section -->
        <div v-if="activeTab === 'mcp'" class="tab-pane">
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
              <button class="btn-danger-small" @click="deleteMCPServer(srv.id)">删除</button>
            </div>
          </div>

          <!-- Add/Edit Form -->
          <div v-if="showMCPForm" class="mcp-form">
            <div class="form-row">
              <label>名称</label>
              <input v-model="mcpForm.name" placeholder="my-server" />
            </div>
            <div class="form-row">
              <label>传输方式</label>
              <select v-model="mcpForm.transport">
                <option value="stdio">stdio</option>
                <option value="sse">SSE</option>
                <option value="http">HTTP (Streamable)</option>
              </select>
            </div>
            <template v-if="mcpForm.transport === 'stdio'">
              <div class="form-row">
                <label>命令</label>
                <input v-model="mcpForm.command" placeholder="/usr/local/bin/mcp-server" />
              </div>
              <div class="form-row">
                <label>参数（空格分隔）</label>
                <input v-model="mcpForm.args" placeholder="--flag value" />
              </div>
            </template>
            <template v-else>
              <div class="form-row">
                <label>URL</label>
                <input v-model="mcpForm.url" placeholder="http://localhost:8080/sse" />
              </div>
              <div class="form-row">
                <label>请求头（每行一个，格式：Key: Value）</label>
                <textarea v-model="mcpForm.headers" rows="3" placeholder="Authorization: Bearer xxx&#10;X-Custom: value" />
              </div>
            </template>
            <div v-if="mcpFormError" class="form-error">{{ mcpFormError }}</div>
            <div class="form-buttons">
              <button class="btn-primary" @click="saveMCPServer">保存</button>
              <button class="btn-secondary" @click="showMCPForm = false">取消</button>
            </div>
          </div>
        </div>

        <!-- 定时任务 -->
        <div v-if="activeTab === 'cron'" class="tab-pane">
          <div class="section-header">
            <h3>定时任务</h3>
            <button class="btn-small" @click="openCronForm">+ 新建</button>
          </div>

          <div v-if="cronJobs.length === 0" class="empty-hint">
            暂无定时任务，点击"新建"创建
          </div>

          <!-- New job form (id === 0) -->
          <div v-if="showCronForm && cronForm.id === 0" class="cron-form">
            <h4>新建定时任务</h4>
            <div class="form-row">
              <label>名称 *</label>
              <input v-model="cronForm.name" placeholder="每日早报" />
            </div>
            <div class="form-row">
              <label>描述</label>
              <input v-model="cronForm.description" placeholder="可选说明" />
            </div>
            <div class="form-row">
              <label>Cron 表达式 *</label>
              <input v-model="cronForm.schedule" placeholder="0 8 * * *（每天早 8 点）" />
            </div>
            <div class="form-row">
              <label>触发提示词 *</label>
              <textarea v-model="cronForm.prompt" rows="3" placeholder="触发时发送给 AI 的消息内容" />
            </div>
            <div v-if="cronFormError" class="form-error">{{ cronFormError }}</div>
            <div class="form-buttons">
              <button class="btn-primary" @click="saveCronJob">保存</button>
              <button class="btn-secondary" @click="showCronForm = false">取消</button>
            </div>
          </div>

          <div
            v-for="job in cronJobs"
            :key="job.ID"
            class="cron-row"
            :class="{ 'cron-row--editing': showCronForm && cronForm.id === job.ID }"
          >
            <!-- View mode -->
            <template v-if="!(showCronForm && cronForm.id === job.ID)">
              <div class="cron-info">
                <div class="cron-name-row">
                  <span class="cron-name">{{ job.Name }}</span>
                  <span class="cron-schedule">{{ job.Schedule }}</span>
                  <span class="cron-status" :class="job.Enabled ? 'cron-status--on' : 'cron-status--off'">
                    {{ job.Enabled ? '启用中' : '已禁用' }}
                  </span>
                </div>
                <div v-if="job.Description" class="cron-desc">{{ job.Description }}</div>
                <div class="cron-prompt">{{ job.Prompt }}</div>
                <div v-if="job.LastRun" class="cron-lastrun">上次执行：{{ new Date(job.LastRun).toLocaleString() }}</div>
              </div>
              <div class="cron-actions">
                <button class="btn-small" @click="runCronJobNow(job.ID)">执行</button>
                <button class="btn-small" @click="editCronJob(job)">编辑</button>
                <button v-if="job.Enabled" class="btn-toggle" @click="toggleCronJob(job)">禁用</button>
                <button v-else class="btn-toggle btn-toggle--enable" @click="toggleCronJob(job)">启用</button>
                <button class="btn-danger-small" @click="deleteCronJob(job.ID)">删除</button>
              </div>
            </template>

            <!-- Inline edit mode -->
            <template v-else>
              <div class="cron-edit-form">
                <div class="form-row">
                  <label>名称 *</label>
                  <input v-model="cronForm.name" placeholder="每日早报" />
                </div>
                <div class="form-row">
                  <label>描述</label>
                  <input v-model="cronForm.description" placeholder="可选说明" />
                </div>
                <div class="form-row">
                  <label>Cron 表达式 *</label>
                  <input v-model="cronForm.schedule" placeholder="0 8 * * *（每天早 8 点）" />
                </div>
                <div class="form-row">
                  <label>触发提示词 *</label>
                  <textarea v-model="cronForm.prompt" rows="3" placeholder="触发时发送给 AI 的消息内容" />
                </div>
                <div v-if="cronFormError" class="form-error">{{ cronFormError }}</div>
                <div class="form-buttons">
                  <button class="btn-primary" @click="saveCronJob">保存</button>
                  <button class="btn-secondary" @click="showCronForm = false">取消</button>
                </div>
              </div>
            </template>
          </div>

        </div>

        <!-- 飞书 lark-cli -->
        <div v-if="activeTab === 'lark'" class="tab-pane">
          <label>lark-cli 路径
            <div class="url-row">
              <input v-model="cfg.LarkCLIPath" placeholder="留空自动从 PATH 查找（lark-cli）" />
              <button class="fetch-btn" @click="fetchLarkStatus" :disabled="larkStatusLoading">
                {{ larkStatusLoading ? '检测中...' : '检测状态' }}
              </button>
            </div>
          </label>

          <div v-if="larkStatus" class="lark-status lark-status--ok">
            <pre>{{ larkStatus }}</pre>
          </div>
          <div v-else-if="larkStatusError" class="lark-status lark-status--err">{{ larkStatusError }}</div>

          <div class="section-header" style="margin-top:8px">
            <h3>快速引导</h3>
          </div>
          <div class="lark-guide">
            <div class="lark-guide-step">
              <span class="lark-step-num">1</span>
              <div class="lark-step-body">
                <div class="lark-step-title">安装 lark-cli</div>
                <code class="lark-code">npm install -g @larksuite/cli</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">2</span>
              <div class="lark-step-body">
                <div class="lark-step-title">初始化应用凭证（App ID / App Secret）</div>
                <code class="lark-code">lark-cli config init</code>
                <p class="lark-step-hint">在终端中运行，按提示填入飞书开放平台的 App ID 与 App Secret</p>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">3</span>
              <div class="lark-step-body">
                <div class="lark-step-title">登录（获取用户 Access Token）</div>
                <code class="lark-code">lark-cli auth login --recommend</code>
                <p class="lark-step-hint">浏览器扫码授权后即可以用户身份访问飞书</p>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">4</span>
              <div class="lark-step-body">
                <div class="lark-step-title">完成后点击"检测状态"验证</div>
              </div>
            </div>
          </div>

          <p class="lark-hint">
            配置完成后，AI 可通过 <code>lark</code> 工具操作飞书，例如：发消息、查日历、读文档等。<br>
            在「工具权限」中启用 <strong>lark</strong> 工具后生效。
          </p>
        </div>
      </div>
    </div>

    <!-- Footer -->
    <div class="win-footer">
      <span class="status-msg">{{ statusMsg }}</span>
      <button @click="save" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
    </div>
  </div>
</template>

<style scoped>
/* Window container */
.settings-win {
  position: fixed;
  z-index: 99990;
  width: 640px;
  height: 520px;
  background: rgba(13, 17, 28, 0.88);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
  box-shadow:
    0 24px 64px rgba(0, 0, 0, 0.8),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-size: 13px;
  color: #e5e7eb;
}

/* Title bar */
.win-titlebar {
  display: flex;
  align-items: center;
  padding: 0 16px;
  height: 44px;
  cursor: move;
  flex-shrink: 0;
  user-select: none;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.02);
}
.win-title { flex: 1; font-weight: 600; font-size: 13px; letter-spacing: 0.02em; color: rgba(255,255,255,0.85); }
.win-close {
  background: none;
  border: none;
  color: rgba(255,255,255,0.3);
  cursor: pointer;
  font-size: 14px;
  padding: 6px 8px;
  border-radius: 6px;
  transition: background 0.15s, color 0.15s;
}
.win-close:hover { background: rgba(239,68,68,0.15); color: #ef4444; }

/* Layout */
.win-body { flex: 1; display: flex; overflow: hidden; }

/* Sidebar */
.win-sidebar {
  width: 130px;
  background: rgba(255, 255, 255, 0.02);
  border-right: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  flex-direction: column;
  padding: 10px 6px;
  gap: 2px;
  flex-shrink: 0;
}
.win-sidebar button {
  background: none;
  border: none;
  color: rgba(156, 163, 175, 0.8);
  padding: 9px 12px;
  cursor: pointer;
  font-size: 12px;
  text-align: left;
  border-radius: 8px;
  transition: background 0.15s, color 0.15s;
}
.win-sidebar button:hover { background: rgba(255,255,255,0.06); color: #f9fafb; }
.win-sidebar button.active {
  background: rgba(99, 102, 241, 0.15);
  color: #a5b4fc;
  font-weight: 500;
}

/* Content */
.win-content { flex: 1; overflow-y: auto; padding: 20px; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.1) transparent; }
.win-content::-webkit-scrollbar { width: 4px; }
.win-content::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 2px; }

/* Form */
.tab-pane { display: flex; flex-direction: column; gap: 14px; }
label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: rgba(156, 163, 175, 0.8); font-weight: 500; letter-spacing: 0.01em; }
input, textarea, select {
  background: rgba(31, 41, 55, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 7px 11px;
  color: #f9fafb;
  font-size: 13px;
  outline: none;
  font-family: inherit;
  transition: border-color 0.15s;
}
input:focus, textarea:focus, select:focus { border-color: rgba(99, 102, 241, 0.6); }
input::placeholder, textarea::placeholder { color: rgba(156, 163, 175, 0.4); }
textarea { resize: vertical; }
select {
  cursor: pointer;
  appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='8' viewBox='0 0 12 8'%3E%3Cpath d='M1 1l5 5 5-5' stroke='%236b7280' stroke-width='1.5' fill='none' stroke-linecap='round'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  padding-right: 28px;
}

/* URL row with fetch button */
.url-row { display: flex; gap: 8px; align-items: center; }
.url-row input { flex: 1; }
.fetch-btn {
  background: rgba(55, 65, 81, 0.6);
  color: rgba(209, 213, 219, 0.9);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  font-size: 12px;
  white-space: nowrap;
  flex-shrink: 0;
  transition: background 0.15s, border-color 0.15s;
}
.fetch-btn:hover:not(:disabled) { background: rgba(75, 85, 99, 0.7); border-color: rgba(255,255,255,0.15); }
.fetch-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.select-row { display: flex; }
.select-row select, .select-row input { flex: 1; }

/* Tool permissions */
.hint { color: rgba(107, 114, 128, 0.8); font-size: 12px; margin: 0 0 8px; line-height: 1.5; }
.perm-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 0;
  border-bottom: 1px solid rgba(255,255,255,0.04);
}
.perm-row:last-child { border-bottom: none; }
.perm-info { display: flex; align-items: center; gap: 10px; }
.perm-name { font-size: 13px; color: #e5e7eb; }
.perm-level { font-size: 10px; padding: 2px 7px; border-radius: 20px; font-weight: 500; letter-spacing: 0.03em; }
.perm-level.public { background: rgba(6, 95, 70, 0.4); color: #6ee7b7; border: 1px solid rgba(110, 231, 183, 0.2); }
.perm-level.protected { background: rgba(124, 45, 18, 0.4); color: #fdba74; border: 1px solid rgba(253, 186, 116, 0.2); }

/* Toggle switch */
.toggle { display: flex; align-items: center; cursor: pointer; }
.toggle input { display: none; }
.toggle-track {
  width: 36px; height: 20px;
  background: rgba(55, 65, 81, 0.8);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  position: relative;
  transition: background 0.2s, border-color 0.2s;
}
.toggle input:checked ~ .toggle-track { background: rgba(99, 102, 241, 0.8); border-color: rgba(99, 102, 241, 0.4); }
.toggle-track::after {
  content: '';
  position: absolute;
  top: 2px; left: 2px;
  width: 14px; height: 14px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.2s;
  box-shadow: 0 1px 3px rgba(0,0,0,0.3);
}
.toggle input:checked ~ .toggle-track::after { transform: translateX(16px); }
.toggle input:disabled ~ .toggle-track { opacity: 0.35; cursor: not-allowed; }

/* Buttons */
button {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #fff;
  border: none;
  border-radius: 8px;
  padding: 7px 16px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: opacity 0.15s;
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.3);
}
button:hover:not(:disabled) { opacity: 0.9; }
button:disabled { opacity: 0.4; cursor: not-allowed; box-shadow: none; }

/* Knowledge list */
ul { list-style: none; padding: 0; margin-top: 4px; }
li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 7px 0;
  border-bottom: 1px solid rgba(255,255,255,0.04);
  font-size: 12px;
  color: #d1d5db;
}
li:last-child { border-bottom: none; }
li button {
  background: rgba(55, 65, 81, 0.6);
  border: 1px solid rgba(255,255,255,0.06);
  padding: 3px 10px;
  font-size: 11px;
  box-shadow: none;
}
li button:hover { background: rgba(220, 38, 38, 0.25); border-color: rgba(220, 38, 38, 0.3); color: #fca5a5; }

.empty { color: rgba(107, 114, 128, 0.6); font-size: 12px; margin-top: 6px; }
.progress { color: rgba(156, 163, 175, 0.7); font-size: 12px; margin: 6px 0; }

/* Model grid */
.model-grid { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 4px; }
.model-btn {
  background: rgba(31, 41, 55, 0.6);
  color: rgba(156, 163, 175, 0.8);
  border: 1px solid rgba(255,255,255,0.08);
  border-radius: 8px;
  padding: 6px 14px;
  cursor: pointer;
  font-size: 12px;
  transition: border-color 0.15s, color 0.15s, background 0.15s;
  box-shadow: none;
}
.model-btn:hover { border-color: rgba(99,102,241,0.5); color: #f9fafb; background: rgba(99,102,241,0.08); }
.model-btn.selected {
  background: rgba(99, 102, 241, 0.2);
  border-color: rgba(99, 102, 241, 0.5);
  color: #a5b4fc;
}

/* Footer */
.win-footer {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  border-top: 1px solid rgba(255,255,255,0.06);
  flex-shrink: 0;
  background: rgba(255,255,255,0.01);
}
.status-msg { color: rgba(107, 114, 128, 0.8); font-size: 12px; flex: 1; }

/* MCP Servers */
.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.section-header h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: #e5e7eb;
}
.btn-small {
  background: rgba(55, 65, 81, 0.6);
  color: rgba(209, 213, 219, 0.9);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  padding: 4px 10px;
  cursor: pointer;
  font-size: 11px;
  white-space: nowrap;
  transition: background 0.15s, border-color 0.15s;
  box-shadow: none;
}
.btn-small:hover { background: rgba(75, 85, 99, 0.7); border-color: rgba(255,255,255,0.15); }

.mcp-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid rgba(255,255,255,0.08);
  gap: 8px;
}
.mcp-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}
.mcp-name {
  font-weight: 600;
  font-size: 13px;
}
.mcp-transport {
  font-size: 11px;
  opacity: 0.6;
  text-transform: uppercase;
}
.mcp-endpoint {
  font-size: 11px;
  opacity: 0.5;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mcp-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
.btn-toggle {
  font-size: 12px;
  padding: 3px 8px;
  border-radius: 4px;
  border: 1px solid rgba(255,255,255,0.2);
  background: rgba(255,255,255,0.05);
  color: inherit;
  cursor: pointer;
  opacity: 0.6;
  box-shadow: none;
}
.btn-toggle.active {
  opacity: 1;
  border-color: rgba(100,200,100,0.5);
  background: rgba(100,200,100,0.1);
  color: #6dc96d;
}
.btn-danger-small {
  font-size: 12px;
  padding: 3px 8px;
  border-radius: 4px;
  border: 1px solid rgba(255,80,80,0.3);
  background: rgba(255,80,80,0.08);
  color: #ff6b6b;
  cursor: pointer;
  box-shadow: none;
}
.mcp-form {
  margin-top: 12px;
  padding: 12px;
  background: rgba(255,255,255,0.05);
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.1);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.form-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.form-row label {
  font-size: 12px;
  color: rgba(156, 163, 175, 0.8);
  font-weight: 500;
}
.form-error {
  color: #ff6b6b;
  font-size: 12px;
}
.form-buttons {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
.btn-primary {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #fff;
  border: none;
  border-radius: 6px;
  padding: 6px 14px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.3);
}
.btn-secondary {
  background: rgba(55, 65, 81, 0.6);
  color: rgba(209, 213, 219, 0.9);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  padding: 6px 14px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  box-shadow: none;
}
.empty-hint {
  font-size: 12px;
  opacity: 0.5;
  padding: 8px 0;
}

/* Pet size slider */
.size-row { display: flex; align-items: center; gap: 10px; margin-top: 2px; }
.size-row input[type=range] { flex: 1; accent-color: #6366f1; cursor: pointer; }
.size-val { font-size: 12px; color: #a5b4fc; min-width: 44px; text-align: right; font-variant-numeric: tabular-nums; }
.size-hint { font-size: 11px; color: rgba(107,114,128,0.6); margin-top: 2px; line-height: 1.4; }
.btn-reset-size {
  margin-top: 4px;
  background: rgba(55, 65, 81, 0.6);
  color: rgba(209, 213, 219, 0.8);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  padding: 3px 10px;
  font-size: 11px;
  cursor: pointer;
  box-shadow: none;
  align-self: flex-start;
}

/* Cron jobs */
.cron-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 12px;
  background: rgba(255,255,255,0.03);
  border: 1px solid rgba(255,255,255,0.07);
  border-radius: 8px;
}
.cron-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.cron-name-row { display: flex; align-items: center; gap: 8px; }
.cron-name { font-size: 13px; font-weight: 600; color: #f9fafb; }
.cron-schedule {
  font-size: 11px; color: #a5b4fc;
  background: rgba(99,102,241,0.15);
  border-radius: 4px; padding: 1px 6px;
  font-family: 'Fira Code', monospace;
}
.cron-desc { font-size: 11px; color: #9ca3af; }
.cron-prompt {
  font-size: 12px; color: #d1d5db;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.cron-lastrun { font-size: 11px; color: #6b7280; }
.cron-actions { display: flex; flex-direction: column; gap: 5px; flex-shrink: 0; }
.cron-status {
  font-size: 11px;
  border-radius: 4px;
  padding: 1px 6px;
  font-weight: 500;
}
.cron-status--on  { background: rgba(34,197,94,0.15); color: #4ade80; }
.cron-status--off { background: rgba(107,114,128,0.15); color: #9ca3af; }
.btn-toggle--enable { background: rgba(34,197,94,0.15); color: #4ade80; border-color: rgba(34,197,94,0.3); }
.btn-toggle--enable:hover { background: rgba(34,197,94,0.25); }
.cron-row--editing {
  border-color: rgba(99,102,241,0.4);
  background: rgba(99,102,241,0.06);
}
.cron-edit-form {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.cron-form {
  background: rgba(255,255,255,0.03);
  border: 1px solid rgba(255,255,255,0.08);
  border-radius: 10px;
  padding: 14px;
  display: flex; flex-direction: column; gap: 10px;
}
.cron-form h4 { margin: 0 0 4px; font-size: 13px; color: #f9fafb; }

/* Lark tab */
.lark-status {
  padding: 10px 12px;
  border-radius: 8px;
  font-size: 12px;
  font-family: 'Fira Code', monospace;
  white-space: pre-wrap;
  word-break: break-all;
}
.lark-status--ok  { background: rgba(34,197,94,0.08); border: 1px solid rgba(34,197,94,0.2); color: #4ade80; }
.lark-status--err { background: rgba(239,68,68,0.08); border: 1px solid rgba(239,68,68,0.2); color: #f87171; }
.lark-status pre { margin: 0; }
.lark-guide { display: flex; flex-direction: column; gap: 10px; }
.lark-guide-step {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  background: rgba(255,255,255,0.03);
  border: 1px solid rgba(255,255,255,0.07);
  border-radius: 8px;
}
.lark-step-num {
  flex-shrink: 0;
  width: 22px; height: 22px;
  border-radius: 50%;
  background: rgba(99,102,241,0.25);
  color: #a5b4fc;
  font-size: 12px;
  font-weight: 700;
  display: flex; align-items: center; justify-content: center;
}
.lark-step-body { display: flex; flex-direction: column; gap: 4px; }
.lark-step-title { font-size: 12px; font-weight: 600; color: #f9fafb; }
.lark-code {
  display: inline-block;
  font-family: 'Fira Code', monospace;
  font-size: 11px;
  background: rgba(0,0,0,0.35);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 4px;
  padding: 2px 8px;
  color: #e2e8f0;
  user-select: text;
}
.lark-step-hint { font-size: 11px; color: #9ca3af; margin: 0; }
.lark-hint {
  font-size: 11px;
  color: #6b7280;
  line-height: 1.6;
  padding: 8px 12px;
  background: rgba(255,255,255,0.02);
  border-radius: 6px;
}
.lark-hint code { font-family: 'Fira Code', monospace; color: #a5b4fc; }
</style>

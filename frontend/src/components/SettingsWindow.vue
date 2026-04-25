<!-- frontend/src/components/SettingsWindow.vue -->
<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import {
  GetConfig, SaveConfig,
  ImportKnowledge, ListKnowledgeSources, DeleteKnowledgeSource,
  OpenFileDialog, GetToolPermissions, SetToolPermission,
  ListLLMModels,
  ListMCPServers, AddMCPServer, UpdateMCPServer, DeleteMCPServer,
  ListCronJobs, CreateCronJob, UpdateCronJob, DeleteCronJob, SetCronJobEnabled, RunCronJobNow,
  LarkStatus, LarkRunCommand,
  ListModelProfiles, SaveModelProfile, DeleteModelProfile, ActivateModelProfile,
  ListOpenRouterModels,
  SavePetSize, SaveChatSize,
  GetPetSize, GetChatSize,
  StartSMSWatcher, StopSMSWatcher, IsSMSWatcherRunning,
  GetVoiceAutoSend, SetVoiceAutoSend,
  GetSoundsEnabled, SetSoundsEnabled,
  GetTTSVoices, SetTTSAutoPlay,
} from '../../wailsjs/go/main/App'
import { ListProactiveItems, DeleteProactiveItem } from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { useModelPath } from '../composables/useModelPath.js'

const emit = defineEmits(['close'])

const props = defineProps({
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
})

const cfg = ref({
  LLMBaseURL: '', LLMAPIKey: '', LLMModel: '', EmbeddingModel: '',
  Live2DModel: 'hiyori',
  SystemPrompt: '', ShortTermLimit: 30, NudgeInterval: 5, SkillsDirs: '',
  EmbeddingDim: 1536,
  PetSize: 0,
  ChatWidth: 0,
  ChatHeight: 0,
  ActiveProfileID: 0,
  VoiceAutoSend: false,
  SoundsEnabled: false,
})
const { availableModels, loadModels } = useModelPath()
const toolPerms = ref([])   // [{ ToolName, Level, Granted }]
const sources = ref([])
const importProgress = ref(null)
const saving = ref(false)
const statusMsg = ref('')
const activeTab = ref('model')  // 'model' | 'ai' | 'appearance' | 'tools' | 'knowledge' | 'automation' | 'lark' | 'sms'
const toolsSubTab = ref('mcp')         // 'mcp' | 'permissions'
const automationSubTab = ref('cron')   // 'cron' | 'proactive'

const llmModels = ref([])       // fetched from /v1/models
const fetchingModels = ref(false)

// Model profiles
const profiles = ref([])
const activeProfileID = ref(0)
const showProfileForm = ref(false)
const profileForm = ref({ id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, tts_model: '', tts_voice: '', tts_speed: 1.0 })
const profileFormError = ref('')
const profileModels = ref([])
const fetchingProfileModels = ref(false)
const ttsVoices = ref([])

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

// SMS watcher
const smsWatcherRunning = ref(false)
const smsWatcherLoading = ref(false)
const smsWatcherError = ref('')

// Draggable window state
const pos = ref({ x: Math.round(window.innerWidth / 2 - 300), y: Math.round(window.innerHeight / 2 - 250) })
let dragStart = null
let offProgress = null
let offScreen = null

onMounted(async () => {
  loadModels()
  const loaded = await GetConfig()
  if (loaded) {
    Object.assign(cfg.value, loaded)
    // SkillsDirs comes as []string from Go; join to newline-separated string for textarea.
    cfg.value.SkillsDirs = Array.isArray(loaded.SkillsDirs)
      ? loaded.SkillsDirs.join('\n')
      : (loaded.SkillsDirs || '')
  }
  sources.value = await ListKnowledgeSources() || []
  // Override PetSize / ChatWidth / ChatHeight with per-screen saved values so the
  // settings UI shows the config that is actually active for the current screen.
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0) {
    try {
      const petSize = await GetPetSize(sw, sh)
      if (petSize > 0) cfg.value.PetSize = petSize
    } catch (e) { console.warn('SettingsWindow: GetPetSize failed', e) }
    try {
      const [cw, ch] = await GetChatSize(sw, sh)
      if (cw > 0) cfg.value.ChatWidth = cw
      if (ch > 0) cfg.value.ChatHeight = ch
    } catch (e) { console.warn('SettingsWindow: GetChatSize failed', e) }
  }
  try { toolPerms.value = await GetToolPermissions() || [] } catch {}
  await fetchMCPServers()
  await fetchCronJobs()
  fetchLarkStatus()
  await fetchProfiles()
  smsWatcherRunning.value = await IsSMSWatcherRunning()
  offProgress = EventsOn('knowledge:progress', (p) => { importProgress.value = p })
  // Refresh per-screen sizes when the user moves the mouse to a different screen.
  offScreen = EventsOn('screen:active:changed', async (info) => {
    try {
      const petSize = await GetPetSize(info.width, info.height)
      if (petSize > 0) cfg.value.PetSize = petSize
    } catch (e) { console.warn('SettingsWindow screen:active:changed: GetPetSize failed', e) }
    try {
      const [cw, ch] = await GetChatSize(info.width, info.height)
      if (cw > 0) cfg.value.ChatWidth = cw
      if (ch > 0) cfg.value.ChatHeight = ch
    } catch (e) { console.warn('SettingsWindow screen:active:changed: GetChatSize failed', e) }
  })
  // Auto-fetch model list if URL is already configured.
  if (cfg.value.LLMBaseURL) fetchLLMModels()
})

onUnmounted(() => {
  offProgress?.()
  offScreen?.()
})

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

/** fetchProfiles loads all model profiles from the backend. */
async function fetchProfiles() {
  try {
    profiles.value = await ListModelProfiles() || []
    const loaded = await GetConfig()
    activeProfileID.value = loaded?.ActiveProfileID || 0
  } catch (e) {
    console.error('fetchProfiles:', e)
  }
}

/** openProfileForm opens the add-profile form with empty fields. */
function openProfileForm() {
  profileForm.value = { id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, tts_model: '', tts_voice: '', tts_speed: 1.0 }
  profileFormError.value = ''
  profileModels.value = []
  ttsVoices.value = []
  showProfileForm.value = true
}

/** editProfile opens the form pre-filled for an existing profile. */
function editProfile(p) {
  profileForm.value = { ...p }
  profileFormError.value = ''
  profileModels.value = []
  showProfileForm.value = true
  fetchTTSVoices()
}

/** fetchProfileModels fetches models for the profile form's base_url. */
async function fetchProfileModels() {
  fetchingProfileModels.value = true
  try {
    if (profileForm.value.provider === 'openrouter') {
      profileModels.value = await ListOpenRouterModels(profileForm.value.base_url, profileForm.value.api_key) || []
    } else {
      if (!profileForm.value.base_url) return
      profileModels.value = await ListLLMModels(profileForm.value.base_url, profileForm.value.api_key) || []
    }
  } catch {
    profileModels.value = []
  } finally {
    fetchingProfileModels.value = false
  }
}

/** saveProfile creates or updates a profile. */
async function saveProfile() {
  profileFormError.value = ''
  if (!profileForm.value.name.trim()) { profileFormError.value = '请输入配置名称'; return }
  if (!profileForm.value.model.trim()) { profileFormError.value = '请输入模型名称'; return }
  if (profileForm.value.provider === 'openai' && !profileForm.value.base_url.trim()) {
    profileFormError.value = '请输入 Base URL'; return
  }
  try {
    await SaveModelProfile({ ...profileForm.value })
    showProfileForm.value = false
    await fetchProfiles()
  } catch (e) {
    profileFormError.value = '保存失败: ' + e
  }
}

/** activateProfile switches to the given profile. */
async function activateProfile(id) {
  try {
    await ActivateModelProfile(id)
    activeProfileID.value = id
    statusMsg.value = '已切换模型配置'
  } catch (e) {
    statusMsg.value = '切换失败: ' + e
  }
}

/** deleteProfile removes a profile by id. */
async function deleteProfile(id) {
  try {
    await DeleteModelProfile(id)
    await fetchProfiles()
  } catch (e) {
    statusMsg.value = '删除失败: ' + e
  }
}

/** previewPetSize emits a real-time size change and persists for the active screen. */
function previewPetSize(e) {
  const size = Number(e.target.value)
  cfg.value.PetSize = size
  EventsEmit('config:pet:size:changed', size)
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0) {
    SavePetSize(size, sw, sh).catch(err => console.warn('SavePetSize failed', err))
  }
}

/** previewChatSize emits a real-time resize event and persists for the active screen. */
function previewChatSize(field, e) {
  const val = Number(e.target.value)
  cfg.value[field] = val
  EventsEmit('config:chat:size:changed', { width: cfg.value.ChatWidth, height: cfg.value.ChatHeight })
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0 && cfg.value.ChatWidth > 0 && cfg.value.ChatHeight > 0) {
    SaveChatSize(cfg.value.ChatWidth, cfg.value.ChatHeight, sw, sh)
      .catch(err => console.warn('SaveChatSize failed', err))
  }
}

/** resetChatSize restores default chat bubble dimensions for the active screen. */
function resetChatSize() {
  cfg.value.ChatWidth  = 0
  cfg.value.ChatHeight = 0
  EventsEmit('config:chat:size:changed', { width: 0, height: 0 })
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0) {
    SaveChatSize(0, 0, sw, sh).catch(err => console.warn('SaveChatSize failed', err))
  }
}

const reloading = ref(false)

/** reload re-fetches all config and data from the backend, discarding unsaved changes. */
async function reload() {
  reloading.value = true
  statusMsg.value = ''
  try {
    const loaded = await GetConfig()
    if (loaded) {
      Object.assign(cfg.value, loaded)
      cfg.value.SkillsDirs = Array.isArray(loaded.SkillsDirs)
        ? loaded.SkillsDirs.join('\n')
        : (loaded.SkillsDirs || '')
    }
    sources.value = await ListKnowledgeSources() || []
    try { toolPerms.value = await GetToolPermissions() || [] } catch {}
    await fetchMCPServers()
    await fetchCronJobs()
    await fetchProfiles()
    statusMsg.value = '已刷新'
  } catch (e) {
    statusMsg.value = '刷新失败: ' + e
  } finally {
    reloading.value = false
  }
}

/** save persists configuration to the backend. */
async function save() {
  saving.value = true
  statusMsg.value = ''
  try {
    // Convert SkillsDirs textarea string back to []string for Go.
    const payload = {
      ...cfg.value,
      SkillsDirs: cfg.value.SkillsDirs
        ? cfg.value.SkillsDirs.split('\n').map(s => s.trim()).filter(Boolean)
        : [],
      ActiveProfileID: activeProfileID.value,
    }
    await SaveConfig(payload)
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

/** editMCPServer pre-fills the form with an existing server's data. */
function editMCPServer(srv) {
  mcpForm.value = {
    id: srv.id,
    name: srv.name,
    transport: srv.transport,
    command: srv.command || '',
    args: Array.isArray(srv.args) ? srv.args.join(' ') : (srv.args || ''),
    url: srv.url || '',
    headers: srv.headers ? Object.entries(srv.headers).map(([k, v]) => `${k}: ${v}`).join('\n') : '',
    enabled: srv.enabled,
  }
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

/** toggleSMSWatcher starts or stops the SMS verification code watcher. */
async function toggleSMSWatcher() {
  smsWatcherLoading.value = true
  smsWatcherError.value = ''
  try {
    if (smsWatcherRunning.value) {
      await StopSMSWatcher()
      smsWatcherRunning.value = false
    } else {
      await StartSMSWatcher()
      smsWatcherRunning.value = true
    }
  } catch (e) {
    smsWatcherError.value = String(e)
  } finally {
    smsWatcherLoading.value = false
  }
}

/** toggleVoiceAutoSend updates voice auto-send setting immediately and notifies ChatPanel. */
async function toggleVoiceAutoSend() {
  try {
    await SetVoiceAutoSend(cfg.value.VoiceAutoSend)
    EventsEmit('config:voice:auto-send:changed', cfg.value.VoiceAutoSend)
  } catch (e) {
    console.warn('toggleVoiceAutoSend failed:', e)
  }
}

/** toggleSoundsEnabled updates sound effects setting immediately and notifies ChatPanel. */
async function toggleSoundsEnabled() {
  try {
    await SetSoundsEnabled(cfg.value.SoundsEnabled)
    EventsEmit('config:sounds:changed', cfg.value.SoundsEnabled)
  } catch (e) {
    console.warn('toggleSoundsEnabled failed:', e)
  }
}

/** fetchTTSVoices loads voice list for the profile currently being edited. */
async function fetchTTSVoices() {
  if (!profileForm.value.tts_model) { ttsVoices.value = []; return }
  try {
    ttsVoices.value = await GetTTSVoices(
      profileForm.value.base_url,
      profileForm.value.api_key,
      profileForm.value.tts_model,
    ) || []
  } catch { ttsVoices.value = [] }
}

/** toggleTTSAutoPlay persists the auto-play TTS setting. */
async function toggleTTSAutoPlay() {
  try {
    await SetTTSAutoPlay(cfg.value.TTSAutoPlay)
  } catch (e) {
    console.warn('toggleTTSAutoPlay failed:', e)
  }
}

// ── 提醒事项 ──────────────────────────────────────────────
const proactiveItems = ref([])
const proactiveError = ref('')

/** loadProactiveItems fetches all pending reminders from the backend. */
async function loadProactiveItems() {
  try {
    proactiveError.value = ''
    proactiveItems.value = await ListProactiveItems() ?? []
  } catch (e) {
    proactiveError.value = '加载失败'
  }
}

/** deleteProactiveItem removes a reminder optimistically, rolls back on error. */
async function deleteProactiveItem(id) {
  proactiveItems.value = proactiveItems.value.filter(i => i.ID !== id)
  try {
    await DeleteProactiveItem(id)
  } catch (e) {
    await loadProactiveItems()
  }
}

/** formatProactiveTime formats a UTC time string to local M/D HH:mm. */
function formatProactiveTime(t) {
  return new Date(t).toLocaleString('zh-CN', {
    month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit'
  })
}

/** truncatePrompt truncates a prompt string to n characters. */
function truncatePrompt(s, n) {
  return s.length > n ? s.slice(0, n) + '…' : s
}

const publicToolNames = computed(() =>
  toolPerms.value.filter(p => p.Level === 'public').map(p => p.ToolName).join('、')
)
const protectedToolPerms = computed(() =>
  toolPerms.value.filter(p => p.Level !== 'public')
)

watch(automationSubTab, v => { if (v === 'proactive') loadProactiveItems() })
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

      <div class="win-content">
        <!-- 模型设置 -->
        <div v-if="activeTab === 'model'" class="tab-pane">
          <div class="profile-header">
            <span class="section-title">模型配置</span>
            <button class="btn-add" @click="openProfileForm">+ 新增</button>
          </div>

          <div v-if="profiles.length === 0" class="empty-hint">暂无配置，点击「新增」添加第一个模型配置</div>

          <div v-for="p in profiles" :key="p.id" :class="['profile-card', { active: p.id === activeProfileID }]">
            <div class="profile-card-main">
              <span class="profile-name">{{ p.name }}</span>
              <span class="profile-meta">{{ p.provider }} · {{ p.model }}</span>
              <span v-if="p.id === activeProfileID" class="profile-badge">使用中</span>
            </div>
            <div class="profile-card-actions">
              <button v-if="p.id !== activeProfileID" class="btn-activate" @click="activateProfile(p.id)">激活</button>
              <button class="btn-edit" @click="editProfile(p)">编辑</button>
              <button class="btn-del" @click="deleteProfile(p.id)">删除</button>
            </div>
          </div>

          <!-- Profile form dialog -->
          <div v-if="showProfileForm" class="modal-overlay" @click.self="showProfileForm = false">
            <div class="modal-box">
              <div class="modal-title">{{ profileForm.id ? '编辑配置' : '新增配置' }}</div>
              <label>名称<input v-model="profileForm.name" placeholder="我的 OpenAI" /></label>
              <label>Provider
                <select v-model="profileForm.provider">
                  <option value="openai">OpenAI 兼容</option>
                  <option value="openrouter">OpenRouter</option>
                </select>
              </label>
              <label>Base URL
                <div class="url-row">
                  <input
                    v-model="profileForm.base_url"
                    :placeholder="profileForm.provider === 'openrouter' ? 'https://openrouter.ai/api/v1（留空使用默认）' : 'http://localhost:11434/v1'"
                  />
                  <button class="fetch-btn" @click="fetchProfileModels" :disabled="fetchingProfileModels || (profileForm.provider !== 'openrouter' && !profileForm.base_url)">
                    {{ fetchingProfileModels ? '获取中...' : '获取模型' }}
                  </button>
                </div>
              </label>              <label>API Key<input v-model="profileForm.api_key" type="password" placeholder="（可选）" /></label>
              <label>Model
                <div class="select-row">
                  <select v-if="profileModels.length" v-model="profileForm.model">
                    <option value="">-- 请选择模型 --</option>
                    <option v-for="m in profileModels" :key="m" :value="m">{{ m }}</option>
                  </select>
                  <input v-else v-model="profileForm.model" placeholder="gpt-4o" />
                </div>
              </label>
              <label>Embedding Model
                <div class="select-row">
                  <select v-if="profileModels.length" v-model="profileForm.embedding_model">
                    <option value="">-- 不使用 Embedding --</option>
                    <option v-for="m in profileModels" :key="m" :value="m">{{ m }}</option>
                  </select>
                  <input v-else v-model="profileForm.embedding_model" placeholder="text-embedding-3-small（可选）" />
                </div>
              </label>
              <label>Embedding 维度<input type="number" v-model.number="profileForm.embedding_dim" min="256" max="4096" /></label>
              <div class="form-group" style="margin-top:12px">
                <label>TTS Model</label>
                <input v-model="profileForm.tts_model" placeholder="留空则使用系统 say" @change="fetchTTSVoices" />
              </div>
              <div class="form-group" style="margin-top:8px">
                <label>TTS Voice</label>
                <select v-if="ttsVoices.length > 0" v-model="profileForm.tts_voice">
                  <option value="">-- 选择声线 --</option>
                  <option v-for="v in ttsVoices" :key="v" :value="v">{{ v }}</option>
                </select>
                <input v-else v-model="profileForm.tts_voice" placeholder="声线名称，如 tara" />
              </div>
              <div class="form-group" style="margin-top:8px">
                <label>TTS Speed（{{ profileForm.tts_speed }}x）</label>
                <input type="range" v-model.number="profileForm.tts_speed" min="0.5" max="2.0" step="0.1" style="width:100%" />
              </div>
              <div v-if="profileFormError" class="form-error">{{ profileFormError }}</div>
              <div class="modal-actions">
                <button class="btn-cancel" @click="showProfileForm = false">取消</button>
                <button class="btn-save" @click="saveProfile">保存</button>
              </div>
            </div>
          </div>
        </div>

        <!-- AI 设置 -->
        <div v-if="activeTab === 'ai'" class="tab-pane">
          <label>System Prompt<textarea v-model="cfg.SystemPrompt" rows="5" /></label>
          <label>短期记忆轮数（1-100）<input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" /></label>
          <label>自我成长 Nudge 间隔（轮）<input type="number" v-model.number="cfg.NudgeInterval" min="1" max="100" /></label>
          <label>Skills 目录<span class="field-hint">每行一个路径</span><textarea v-model="cfg.SkillsDirs" rows="3" placeholder="~/.aiko/skills" /></label>
        </div>

        <!-- 外观与交互 -->
        <div v-if="activeTab === 'appearance'" class="tab-pane">
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
          <label>桌宠大小
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
            <div class="screen-label" v-if="props.activeScreen.width > 0">
              当前屏幕：{{ props.activeScreen.width }}×{{ props.activeScreen.height }}
            </div>
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
          <div class="sms-toggle-row" style="margin-top:16px">
            <span class="sms-status-label" style="flex:1">自动朗读回复</span>
            <label class="voice-auto-send-switch">
              <input type="checkbox" v-model="cfg.TTSAutoPlay" @change="toggleTTSAutoPlay" />
              <span class="voice-auto-send-slider"></span>
            </label>
          </div>
          <p class="sms-desc" style="margin-top:4px">LLM 回复完成后自动朗读内容（需在 ModelProfile 中配置 TTS Model）</p>
        </div>

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

        <!-- 飞书 lark-cli -->
        <div v-if="activeTab === 'lark'" class="tab-pane">
          <div class="url-row" style="margin-bottom:8px">
            <span style="flex:1;font-size:12px;color:#9ca3af">lark-cli 路径由 PATH 自动查找</span>
            <button class="fetch-btn" @click="fetchLarkStatus" :disabled="larkStatusLoading">
              {{ larkStatusLoading ? '检测中...' : '检测状态' }}
            </button>
          </div>

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
                <div class="lark-step-title">安装 CLI</div>
                <code class="lark-code">npm install -g @larksuite/cli</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">2</span>
              <div class="lark-step-body">
                <div class="lark-step-title">安装 CLI SKILL（必需）</div>
                <code class="lark-code">npx skills add larksuite/cli -y -g</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">3</span>
              <div class="lark-step-body">
                <div class="lark-step-title">配置应用凭证（仅需一次，交互式引导完成）</div>
                <code class="lark-code">lark-cli config init</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">4</span>
              <div class="lark-step-body">
                <div class="lark-step-title">登录授权（--recommend 自动选择常用权限）</div>
                <code class="lark-code">lark-cli auth login --recommend</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">5</span>
              <div class="lark-step-body">
                <div class="lark-step-title">完成后点击"检测状态"验证</div>
              </div>
            </div>
          </div>

          <p class="lark-hint">
            配置完成后，AI 可通过 lark-cli 操作飞书，例如：发消息、查日历、读文档等。<br>
            <strong>注意：</strong>需在"模型"标签页的 Skills 目录中添加飞书 Skills 路径（通常为 <code>~/.agents/skills</code>）。
          </p>
        </div>

        <!-- 短信监听 -->
        <div v-if="activeTab === 'sms'" class="tab-pane">
          <div class="section-header">
            <h3>短信验证码监听</h3>
          </div>
          <p class="sms-desc">
            监听 macOS 信息 App 的 SMS 短信，自动识别验证码并复制到剪贴板，同时弹出通知气泡。<br>
            <strong>需要权限：</strong>系统设置 → 隐私与安全性 → <strong>完全磁盘访问权限</strong> → 授权 Aiko。
          </p>

          <div class="sms-toggle-row">
            <span class="sms-status-dot" :class="smsWatcherRunning ? 'dot-on' : 'dot-off'"></span>
            <span class="sms-status-label">{{ smsWatcherRunning ? '监听中' : '已停止' }}</span>
            <button class="fetch-btn" @click="toggleSMSWatcher" :disabled="smsWatcherLoading">
              {{ smsWatcherLoading ? '处理中...' : (smsWatcherRunning ? '停止监听' : '开启监听') }}
            </button>
          </div>

          <div v-if="smsWatcherError" class="lark-status lark-status--err" style="margin-top:8px">
            {{ smsWatcherError }}
          </div>

          <div class="sms-guide">
            <div class="sms-guide-step">
              <span class="lark-step-num">1</span>
              <div class="lark-step-body">
                <div class="lark-step-title">授予完全磁盘访问权限</div>
                <p class="lark-step-desc">系统设置 → 隐私与安全性 → 完全磁盘访问权限 → 点击 + 添加 Aiko</p>
              </div>
            </div>
            <div class="sms-guide-step">
              <span class="lark-step-num">2</span>
              <div class="lark-step-body">
                <div class="lark-step-title">点击「开启监听」</div>
                <p class="lark-step-desc">收到含验证码的短信后，验证码自动写入剪贴板并弹出通知。</p>
              </div>
            </div>
          </div>

        </div>

      </div>
    </div>

    <!-- Footer -->
    <div class="win-footer">
      <span class="status-msg">{{ statusMsg }}</span>
      <button class="btn-reload" @click="reload" :disabled="reloading">{{ reloading ? '刷新中...' : '刷新' }}</button>
      <button @click="save" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
    </div>
  </div>
</template>

<style scoped>
/* Window container */
.settings-win {
  position: fixed;
  z-index: 99990;
  width: 940px;
  height: 820px;
  background: rgba(12, 15, 26, 0.55);
  backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  -webkit-backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 20px;
  box-shadow:
    0 12px 40px rgba(0, 0, 0, 0.5),
    0 1px 0 rgba(255, 255, 255, 0.08) inset,
    0 0 0 0.5px rgba(255, 255, 255, 0.04) inset;
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
  width: 100px;
  background: rgba(0, 0, 0, 0.18);
  border-right: 1px solid rgba(255, 255, 255, 0.05);
  display: flex;
  flex-direction: column;
  padding: 8px 6px;
  gap: 3px;
  flex-shrink: 0;
  overflow-y: auto;
}
.win-sidebar button {
  -webkit-appearance: none;
  appearance: none;
  background: none;
  border: none;
  outline: none;
  color: rgba(156, 163, 175, 0.6);
  padding: 8px 4px;
  cursor: pointer;
  font-size: 11px;
  text-align: center;
  border-radius: 9px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
  width: 100%;
}
.win-sidebar button:focus,
.win-sidebar button:focus-visible {
  outline: none;
  box-shadow: none;
}
.win-sidebar button:hover {
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.75);
}
.win-sidebar button.active {
  background: rgba(99, 102, 241, 0.15);
  color: #c4b5fd;
  font-weight: 600;
}
.nav-icon {
  font-size: 18px;
  line-height: 1;
  display: block;
}
.nav-label {
  font-size: 11px;
  line-height: 1.2;
  white-space: nowrap;
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

/* Model profiles */
.profile-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.section-title { font-size: 13px; font-weight: 600; color: rgba(255,255,255,0.7); }
.btn-add { padding: 4px 12px; background: rgba(59,130,246,0.7); border: none; border-radius: 6px; color: #fff; font-size: 12px; cursor: pointer; }
.btn-add:hover { background: rgba(59,130,246,0.9); }
.empty-hint { color: rgba(255,255,255,0.35); font-size: 12px; padding: 24px 0; text-align: center; }
.profile-card {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 12px; margin-bottom: 8px;
  background: rgba(255,255,255,0.04); border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.06);
  transition: border-color 0.15s;
}
.profile-card.active { border-color: rgba(59,130,246,0.5); background: rgba(59,130,246,0.08); }
.profile-card-main { display: flex; flex-direction: column; gap: 3px; }
.profile-name { font-size: 13px; font-weight: 600; color: rgba(255,255,255,0.85); }
.profile-meta { font-size: 11px; color: rgba(255,255,255,0.4); }
.profile-badge { font-size: 10px; color: #60a5fa; font-weight: 600; }
.profile-card-actions { display: flex; gap: 6px; }
.btn-activate { padding: 3px 10px; background: rgba(34,197,94,0.2); border: 1px solid rgba(34,197,94,0.4); border-radius: 5px; color: #4ade80; font-size: 11px; cursor: pointer; }
.btn-activate:hover { background: rgba(34,197,94,0.35); }
.btn-edit { padding: 3px 10px; background: rgba(255,255,255,0.06); border: 1px solid rgba(255,255,255,0.1); border-radius: 5px; color: rgba(255,255,255,0.7); font-size: 11px; cursor: pointer; }
.btn-edit:hover { background: rgba(255,255,255,0.1); }
.btn-del { padding: 3px 10px; background: rgba(239,68,68,0.12); border: 1px solid rgba(239,68,68,0.25); border-radius: 5px; color: #f87171; font-size: 11px; cursor: pointer; }
.btn-del:hover { background: rgba(239,68,68,0.25); }

/* Modal overlay for profile form */
.modal-overlay {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center;
}
.modal-box {
  background: #1e2433; border: 1px solid rgba(255,255,255,0.1); border-radius: 12px;
  padding: 20px; width: 360px; max-height: 80vh; overflow-y: auto;
  display: flex; flex-direction: column; gap: 10px;
}
.modal-title { font-size: 14px; font-weight: 700; color: rgba(255,255,255,0.85); margin-bottom: 4px; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
.btn-cancel { padding: 5px 14px; background: rgba(255,255,255,0.06); border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; color: rgba(255,255,255,0.6); font-size: 12px; cursor: pointer; }
.btn-cancel:hover { background: rgba(255,255,255,0.12); }
.btn-save { padding: 5px 14px; background: rgba(59,130,246,0.7); border: none; border-radius: 6px; color: #fff; font-size: 12px; cursor: pointer; }
.btn-save:hover { background: rgba(59,130,246,0.9); }
.form-error { color: #f87171; font-size: 12px; }

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
  box-shadow: none;
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
.btn-reload {
  padding: 5px 12px;
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px;
  color: rgba(255,255,255,0.6);
  font-size: 12px;
  cursor: pointer;
}
.btn-reload:hover:not(:disabled) { background: rgba(255,255,255,0.12); }
.btn-reload:disabled { opacity: 0.4; cursor: not-allowed; }

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
  box-shadow: none;
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
  max-height: 140px;
  overflow: auto;
}
.lark-status--ok  { background: rgba(34,197,94,0.08); border: 1px solid rgba(34,197,94,0.2); color: #4ade80; }
.lark-status--err { background: rgba(239,68,68,0.08); border: 1px solid rgba(239,68,68,0.2); color: #f87171; }
.lark-status pre { margin: 0; white-space: pre-wrap; word-break: break-all; }
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
.lark-step-body { display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 0; }
.lark-step-title { font-size: 12px; font-weight: 600; color: #f9fafb; }
.lark-code {
  display: block;
  font-family: 'Fira Code', monospace;
  font-size: 11px;
  background: rgba(0,0,0,0.4);
  border: 1px solid rgba(255,255,255,0.08);
  border-radius: 4px;
  padding: 5px 10px;
  color: #e2e8f0;
  user-select: text;
  white-space: nowrap;
  overflow-x: auto;
}
.lark-step-hint { font-size: 11px; color: #9ca3af; margin: 2px 0 0; }
.lark-hint {
  font-size: 11px;
  color: #6b7280;
  line-height: 1.6;
  padding: 8px 12px;
  background: rgba(255,255,255,0.02);
  border-radius: 6px;
}
.lark-hint code { font-family: 'Fira Code', monospace; color: #a5b4fc; }

.screen-label {
  font-size: 11px;
  color: rgba(255,255,255,0.45);
  margin-bottom: 6px;
}

/* SMS watcher tab */
.sms-desc {
  font-size: 12px;
  color: #9ca3af;
  line-height: 1.6;
  margin-bottom: 14px;
}
.sms-toggle-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
}
.sms-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dot-on  { background: #4ade80; box-shadow: 0 0 6px #4ade80; }
.dot-off { background: #6b7280; }
.sms-status-label { font-size: 12px; color: #d1d5db; flex: 1; }
.sms-guide { display: flex; flex-direction: column; gap: 10px; margin-top: 16px; }
.sms-guide-step { display: flex; align-items: flex-start; gap: 10px; }
.voice-auto-send-switch {
  position: relative;
  display: inline-block;
  width: 40px;
  height: 22px;
  cursor: pointer;
}
.voice-auto-send-switch input { display: none; }
.voice-auto-send-slider {
  position: absolute;
  inset: 0;
  background: #374151;
  border-radius: 11px;
  transition: background 0.2s;
}
.voice-auto-send-slider::before {
  content: '';
  position: absolute;
  width: 16px;
  height: 16px;
  left: 3px;
  top: 3px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.2s;
}
.voice-auto-send-switch input:checked + .voice-auto-send-slider { background: #6366f1; }
.voice-auto-send-switch input:checked + .voice-auto-send-slider::before { transform: translateX(18px); }

/* ── 提醒事项 tab ───────────────────────────────────────── */
.proactive-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  background: rgba(255,255,255,0.04);
  border-radius: 8px;
  margin-bottom: 8px;
}
.proactive-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
  min-width: 0;
}
.proactive-time {
  font-size: 12px;
  color: #a5b4fc;
  font-variant-numeric: tabular-nums;
}
.proactive-prompt {
  font-size: 13px;
  color: rgba(255,255,255,0.8);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.btn-danger {
  color: #f87171;
  border-color: rgba(248,113,113,0.3);
}
.btn-danger:hover {
  background: rgba(248,113,113,0.15);
}

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
</style>

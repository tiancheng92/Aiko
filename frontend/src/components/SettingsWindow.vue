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
  ResetBallPosition,
  StartSMSWatcher, StopSMSWatcher, IsSMSWatcherRunning,
  GetVoiceAutoSend, SetVoiceAutoSend,
  GetSoundsEnabled, SetSoundsEnabled,
  GetKokoroTTSVoices, SetTTSAutoPlay, SetupKokoroTTS,
  GetVersion, CheckUpdate, InstallUpdate,
  ListVRMModels, ImportVRMFile, DeleteVRMModel,
  GetAutoLaunch, SetAutoLaunch,
} from '../../wailsjs/go/main/App'
import { ListProactiveItems, DeleteProactiveItem } from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { useModelPath } from '../composables/useModelPath.js'
import { useEscapeKey } from '../composables/useEscapeKey.js'
import { useConfirm } from '../composables/useConfirm.js'
import {
  ICON_TAB_MODEL, ICON_TAB_AI, ICON_TAB_APPEARANCE, ICON_TAB_TOOLS,
  ICON_TAB_KNOWLEDGE, ICON_TAB_AUTOMATION, ICON_TAB_LARK, ICON_TAB_SMS, ICON_TAB_ABOUT,
  ICON_TAB_GENERAL,
} from '../utils/icons'

const confirm = useConfirm()

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
  AllowedPaths: [],
  ShellTrustedCommands: [],
  ShellTimeout: 30,
  CodeTimeout: 60,
  RenderBackend: 'live2d',
  VRMModel: '',
  ThemeStyle: 'frosted',
})
const availableVRMModels = ref([])
const { availableModels, loadModels } = useModelPath()
const toolPerms = ref([])   // [{ ToolName, Level, Granted }]
const sources = ref([])
const importProgress = ref(null)
const saving = ref(false)   // true while a debounced save is in-flight
const statusMsg = ref('')   // operation-level feedback (profile switch, trigger, etc.)
const mountedReady = ref(false)  // gate to suppress watcher fires during initial load
let saveTimer = null
const activeTab = ref('model')  // 'model' | 'ai' | 'appearance' | 'tools' | 'knowledge' | 'automation' | 'lark' | 'sms'
const toolsSubTab = ref('mcp')         // 'mcp' | 'permissions' | 'settings'
const automationSubTab = ref('cron')   // 'cron' | 'proactive'
const newPathInput = ref('')           // input buffer for adding allowed paths
const newTrustedCmdInput = ref('') // input buffer for adding trusted commands

const llmModels = ref([])       // fetched from /v1/models
const fetchingModels = ref(false)

// Model profiles
const profiles = ref([])
const activeProfileID = ref(0)
const showProfileForm = ref(false)
const profileForm = ref({ id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '' })
const profileFormError = ref('')
const profileFormSaving = ref(false)
const profileModels = ref([])
const fetchingProfileModels = ref(false)
const kokoroTTSVoices = ref([])
const kokoroInstalling = ref(false)

// MCP servers
const mcpServers = ref([])
const showMCPForm = ref(false)
const mcpForm = ref({ id: 0, name: '', transport: 'stdio', command: '', args: '', url: '', headers: '', enabled: true })
const mcpFormError = ref('')
const mcpFormSaving = ref(false)

// Cron jobs
const cronJobs = ref([])
const showCronForm = ref(false)
const cronForm = ref({ id: 0, name: '', description: '', schedule: '', prompt: '' })
const cronFormError = ref('')
const cronFormSaving = ref(false)

// Lark
const larkStatus = ref('')
const larkStatusLoading = ref(false)
const larkStatusError = ref('')

// Launch at login
const autoLaunch = ref(false)

// SMS watcher
const smsWatcherRunning = ref(false)
const smsWatcherLoading = ref(false)
const smsWatcherError = ref('')

// Escape closes each open form dialog (does nothing if closed).
useEscapeKey(() => { showProfileForm.value = false }, showProfileForm)
useEscapeKey(() => { showMCPForm.value = false }, showMCPForm)
useEscapeKey(() => { showCronForm.value = false }, showCronForm)
useEscapeKey(() => confirm.resolve(false), confirm.visible)

// Reset content scroll when switching tabs so each tab starts at the top.
const winContentEl = ref(null)
watch(activeTab, () => { if (winContentEl.value) winContentEl.value.scrollTop = 0 })

// Version / update
const currentVersion = ref('…')
const updateInfo = ref(null)       // null | UpdateInfo
const updateChecking = ref(false)
const updateInstalling = ref(false)
const updateProgress = ref(0)
const updateProgressMsg = ref('')
const updateError = ref('')

/** applyConfig merges a raw GetConfig() result into cfg.value, converting
 * array fields that the textarea UI expects as newline-separated strings. */
function applyConfig(loaded) {
  Object.assign(cfg.value, loaded)
  cfg.value.SkillsDirs = Array.isArray(loaded.SkillsDirs)
    ? loaded.SkillsDirs.join('\n')
    : (loaded.SkillsDirs || '')
}

// Draggable window state
const DEFAULT_W = 960
const DEFAULT_H = 720
const MIN_W = 760
const MIN_H = 560
const pos = ref({ x: Math.round(window.innerWidth / 2 - DEFAULT_W / 2), y: Math.round(window.innerHeight / 2 - DEFAULT_H / 2) })
const winSize = ref({ w: DEFAULT_W, h: DEFAULT_H })
const searchQuery = ref('')
let dragStart = null
let resizeStart = null
let offProgress = null
let offScreen = null
let offUpdateProgress = null

/**
 * tabMeta defines sidebar tabs with SVG icons and search keywords.
 * `_haystack` is pre-lowercased so the search filter doesn't re-normalize
 * label/keywords on every keystroke.
 */
const tabMeta = [
  { id: 'model',      label: '模型',       iconSvg: ICON_TAB_MODEL,      keywords: 'model profile openai provider key embedding tts 模型 配置 接入 语音合成' },
  { id: 'ai',         label: '对话',       iconSvg: ICON_TAB_AI,         keywords: 'prompt system memory skill 提示词 记忆 技能 上下文 自我成长' },
  { id: 'knowledge',  label: '知识库',     iconSvg: ICON_TAB_KNOWLEDGE,  keywords: 'knowledge rag document import 文档 导入 向量' },
  { id: 'tools',      label: '工具',       iconSvg: ICON_TAB_TOOLS,      keywords: 'mcp permission shell code path tool 权限 服务器 执行 白名单' },
  { id: 'automation', label: '自动化',     iconSvg: ICON_TAB_AUTOMATION, keywords: 'cron schedule proactive reminder 定时 任务 提醒' },
  { id: 'appearance', label: '外观',       iconSvg: ICON_TAB_APPEARANCE, keywords: 'live2d vrm pet size chat 模型 大小 语音 音效 朗读 桌宠' },
  { id: 'general',    label: '通用',       iconSvg: ICON_TAB_GENERAL,    keywords: 'theme launch autostart 主题 启动 风格 自启 液态玻璃 毛玻璃' },
  { id: 'lark',       label: '飞书',       iconSvg: ICON_TAB_LARK,       keywords: 'lark feishu cli 飞书' },
  { id: 'sms',        label: '短信',       iconSvg: ICON_TAB_SMS,        keywords: 'sms message verification 短信 验证码 监听' },
  { id: 'about',      label: '关于',       iconSvg: ICON_TAB_ABOUT,      keywords: 'version update about 版本 更新 关于' },
].map(t => ({ ...t, _haystack: (t.label + ' ' + t.keywords).toLowerCase() }))

const searchNeedle = computed(() => searchQuery.value.trim().toLowerCase())
const filteredTabs = computed(() => {
  const q = searchNeedle.value
  return q ? tabMeta.filter(t => t._haystack.includes(q)) : tabMeta
})

/** isSearchMatch returns true when the tab's haystack contains the current query. */
function isSearchMatch(tab) {
  const q = searchNeedle.value
  return !!q && tab._haystack.includes(q)
}

/** onResizeStart begins resizing the window from the bottom-right corner. */
function onResizeStart(e) {
  e.preventDefault()
  e.stopPropagation()
  resizeStart = { mx: e.clientX, my: e.clientY, w: winSize.value.w, h: winSize.value.h }
  window.addEventListener('mousemove', onResizeMove)
  window.addEventListener('mouseup', onResizeEnd)
  window.addEventListener('blur', onResizeEnd)
}

/** onResizeMove updates window size during resize. */
function onResizeMove(e) {
  if (!resizeStart) return
  const w = Math.max(MIN_W, resizeStart.w + (e.clientX - resizeStart.mx))
  const h = Math.max(MIN_H, resizeStart.h + (e.clientY - resizeStart.my))
  winSize.value = { w, h }
}

/** onResizeEnd releases resize listeners. */
function onResizeEnd() {
  resizeStart = null
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeEnd)
  window.removeEventListener('blur', onResizeEnd)
}

onMounted(async () => {
  loadModels()
  const [loaded, version] = await Promise.all([
    GetConfig().catch(() => null),
    GetVersion().catch(() => '…'),
  ])
  if (loaded) applyConfig(loaded)
  currentVersion.value = version

  // Per-screen sizes override global config so the UI shows what's active here.
  const { width: sw, height: sh } = props.activeScreen
  const [ksrc, perms, pet, chat, sms] = await Promise.all([
    ListKnowledgeSources().catch(() => []),
    GetToolPermissions().catch(() => []),
    sw > 0 && sh > 0 ? GetPetSize(sw, sh).catch(() => 0) : Promise.resolve(0),
    sw > 0 && sh > 0 ? GetChatSize(sw, sh).catch(() => [0, 0]) : Promise.resolve([0, 0]),
    IsSMSWatcherRunning().catch(() => false),
    fetchMCPServers(),
    fetchCronJobs(),
    fetchProfiles(),
  ])
  sources.value = ksrc || []
  toolPerms.value = perms || []
  if (pet > 0) cfg.value.PetSize = pet
  if (chat?.[0] > 0) cfg.value.ChatWidth = chat[0]
  if (chat?.[1] > 0) cfg.value.ChatHeight = chat[1]
  smsWatcherRunning.value = sms
  try {
    availableVRMModels.value = await ListVRMModels()
  } catch (e) {
    console.warn('SettingsWindow: failed to load VRM models', e)
  }
  try { autoLaunch.value = await GetAutoLaunch() } catch (e) {
    console.warn('GetAutoLaunch failed:', e)
  }
  fetchLarkStatus()
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

  offUpdateProgress = EventsOn('update:progress', (data) => {
    updateProgress.value = data.pct
    updateProgressMsg.value = data.msg
  })

  // Enable auto-save watcher only after all initial data has been loaded.
  mountedReady.value = true
})

onUnmounted(() => {
  offProgress?.()
  offScreen?.()
  offUpdateProgress?.()
  clearTimeout(saveTimer)
  // Safety net — ensure no drag listeners linger if the component unmounts mid-drag.
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  window.removeEventListener('blur', onMouseUp)
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeEnd)
  window.removeEventListener('blur', onResizeEnd)
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

/** checkUpdate queries GitHub for the latest release. */
async function checkUpdate() {
  updateChecking.value = true
  updateError.value = ''
  updateInfo.value = null
  try {
    updateInfo.value = await CheckUpdate()
  } catch (e) {
    updateError.value = '检查失败: ' + e
  } finally {
    updateChecking.value = false
  }
}

/** installUpdate downloads and installs the latest release, then restarts. */
async function installUpdate() {
  if (!updateInfo.value?.download_url) return
  updateInstalling.value = true
  updateProgress.value = 0
  updateProgressMsg.value = ''
  updateError.value = ''
  try {
    await InstallUpdate(updateInfo.value.download_url)
  } catch (e) {
    updateError.value = '安装失败: ' + e
    updateInstalling.value = false
  }
  // On success the app will quit/restart; no need to reset installing state.
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
  profileForm.value = { id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '' }
  profileFormError.value = ''
  profileModels.value = []
  showProfileForm.value = true
}

/** editProfile opens the form pre-filled for an existing profile. */
function editProfile(p) {
  profileForm.value = { ...p, tts_backend: p.tts_backend || '' }
  profileFormError.value = ''
  profileModels.value = []
  showProfileForm.value = true
  fetchKokoroTTSVoices()
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
  if (profileFormSaving.value) return
  profileFormSaving.value = true
  profileFormError.value = ''
  if (!profileForm.value.name.trim()) { profileFormError.value = '请输入配置名称'; profileFormSaving.value = false; return }
  if (!profileForm.value.model.trim()) { profileFormError.value = '请输入模型名称'; profileFormSaving.value = false; return }
  if (profileForm.value.provider === 'openai' && !profileForm.value.base_url.trim()) {
    profileFormError.value = '请输入 Base URL'; profileFormSaving.value = false; return
  }
  try {
    await SaveModelProfile({ ...profileForm.value })
    showProfileForm.value = false
    await fetchProfiles()
  } catch (e) {
    profileFormError.value = '保存失败: ' + e
  } finally {
    profileFormSaving.value = false
  }
}

/** activateProfile switches to the given profile. */
async function activateProfile(id) {
  try {
    await ActivateModelProfile(id)
    activeProfileID.value = id
    // Refresh cfg so subsequent Save() doesn't overwrite the new profile's
    // LLM fields with stale values loaded before the profile switch.
    const loaded = await GetConfig()
    if (loaded) applyConfig(loaded)
    statusMsg.value = '已切换模型配置'
  } catch (e) {
    statusMsg.value = '切换失败: ' + e
  }
}

/** deleteProfile removes a profile by id after a confirmation prompt. */
async function deleteProfile(id) {
  const p = profiles.value.find(x => x.id === id)
  const ok = await confirm.ask({
    title: '删除模型配置',
    message: `确认删除配置「${p?.name || ''}」？此操作不可撤销。`,
    confirmText: '删除',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await DeleteModelProfile(id)
    await fetchProfiles()
  } catch (e) {
    statusMsg.value = '删除失败: ' + e
  }
}

/** setRenderBackend updates config and emits backend change event. */
function setRenderBackend(backend) {
  cfg.value.RenderBackend = backend
  EventsEmit('config:render:backend:changed', backend)
}

/** setThemeStyle updates the UI theme and applies it immediately to the document root. */
function setThemeStyle(style) {
  cfg.value.ThemeStyle = style
  document.documentElement.dataset.theme = style
}

const VRM_PREVIEW_ANIMS = [
  { file: 'waiting.vrma',        label: '待机' },
  { file: 'wave_big.vrma',       label: '挥手' },
  { file: 'nod.vrma',            label: '点头' },
  { file: 'curious.vrma',        label: '好奇' },
  { file: 'relaxed.vrma',        label: '伸懒腰' },
  { file: 'sleepy.vrma',         label: '困倦' },
  { file: 'hand_talk.vrma',      label: '说话' },
  { file: 'embarrassed.vrma',    label: '尴尬' },
  { file: 'sad.vrma',            label: '悲伤' },
  { file: 'angry.vrma',          label: '傲娇' },
  { file: 'surprised_react.vrma',label: '惊讶' },
  { file: 'appearing.vrma',      label: '登场' },
]

/** previewVRMAnim sends a preview event to VRMPet to play the animation once. */
function previewVRMAnim(file) {
  EventsEmit('vrm:preview:anim', `/vrm/${file}`)
}

/** onVRMModelChange emits hot-reload event when VRM model is changed in settings. */
function onVRMModelChange() {
  EventsEmit('config:vrm:model:changed', cfg.value.VRMModel)
}

/** onLive2DModelChange emits hot-reload event when Live2D model is changed in settings. */
function onLive2DModelChange() {
  EventsEmit('config:model:changed', cfg.value.Live2DModel)
}

const vrmUploading = ref(false)
const vrmUploadError = ref('')

/** uploadVRMModel reads a .vrm file and sends it to the backend for storage in ~/.aiko/vrm/. */
async function uploadVRMModel(e) {
  const file = e.target.files?.[0]
  if (!file) return
  e.target.value = ''
  vrmUploadError.value = ''
  vrmUploading.value = true
  try {
    const buf = await file.arrayBuffer()
    const bytes = new Uint8Array(buf)
    let binary = ''
    const chunk = 8192
    for (let i = 0; i < bytes.length; i += chunk) {
      binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
    }
    const b64 = btoa(binary)
    await ImportVRMFile(file.name, b64)
    availableVRMModels.value = await ListVRMModels()
    cfg.value.VRMModel = file.name
    onVRMModelChange()
  } catch (err) {
    vrmUploadError.value = String(err)
  } finally {
    vrmUploading.value = false
  }
}

/** deleteVRMModel removes a user-imported VRM file from disk. */
async function deleteVRMModel(name) {
  const ok = await confirm.ask({ title: '删除模型', message: `确认删除 "${name}"？此操作不可撤销。`, variant: 'danger' })
  if (!ok) return
  try {
    await DeleteVRMModel(name)
    availableVRMModels.value = await ListVRMModels()
    if (cfg.value.VRMModel === name) {
      cfg.value.VRMModel = availableVRMModels.value[0]?.name ?? ''
      onVRMModelChange()
    }
  } catch (err) {
    vrmUploadError.value = String(err)
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

/** resetBallPosition clears saved ball position for the active screen so the pet snaps to default. */
async function resetBallPosition() {
  const { width: sw, height: sh } = props.activeScreen
  if (sw > 0 && sh > 0) {
    await ResetBallPosition(sw, sh).catch(err => console.warn('ResetBallPosition failed', err))
  }
  EventsEmit('ball:position:reset')
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

/** save persists the current cfg to the backend (called by the debounced watcher). */
async function save() {
  saving.value = true
  try {
    const payload = {
      ...cfg.value,
      SkillsDirs: cfg.value.SkillsDirs
        ? cfg.value.SkillsDirs.split('\n').map(s => s.trim()).filter(Boolean)
        : [],
      ActiveProfileID: activeProfileID.value,
    }
    await SaveConfig(payload)
  } catch (e) {
    console.error('auto-save failed:', e)
  } finally {
    saving.value = false
  }
}

/** debouncedSave queues a save 600ms after the last cfg mutation. */
function debouncedSave() {
  if (!mountedReady.value) return
  clearTimeout(saveTimer)
  saving.value = true
  saveTimer = setTimeout(save, 600)
}

// Watch cfg deeply; fires whenever any field changes after initial load.
watch(cfg, debouncedSave, { deep: true })

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

/** deleteSource removes a knowledge source after confirmation. */
async function deleteSource(src) {
  const ok = await confirm.ask({
    title: '删除知识源',
    message: `确认从知识库中移除「${src}」？对应的向量索引会一并删除。`,
    confirmText: '删除',
    variant: 'danger',
  })
  if (!ok) return
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
  window.addEventListener('blur', onMouseUp)
}

/** onMouseMove updates position during drag. */
function onMouseMove(e) {
  if (!dragStart) return
  pos.value = { x: e.clientX - dragStart.mx, y: e.clientY - dragStart.my }
}

/** onMouseUp ends the drag. Also invoked from the window blur listener so a
 *  deactivation mid-drag releases the listeners instead of leaving them attached. */
function onMouseUp() {
  dragStart = null
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  window.removeEventListener('blur', onMouseUp)
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
  if (mcpFormSaving.value) return
  mcpFormError.value = ''
  if (!mcpForm.value.name.trim()) { mcpFormError.value = '请输入名称'; return }
  if (mcpForm.value.transport === 'stdio' && !mcpForm.value.command.trim()) {
    mcpFormError.value = '请输入可执行命令'; return
  }
  if (mcpForm.value.transport !== 'stdio' && !mcpForm.value.url.trim()) {
    mcpFormError.value = '请输入 URL'; return
  }
  mcpFormSaving.value = true
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
  } finally {
    mcpFormSaving.value = false
  }
}

/** deleteMCPServer removes an MCP server by ID after confirmation. */
async function deleteMCPServer(id) {
  const srv = mcpServers.value.find(s => s.id === id)
  const ok = await confirm.ask({
    title: '删除 MCP 服务器',
    message: `确认删除「${srv?.name || ''}」？Agent 将无法再调用该服务器提供的工具。`,
    confirmText: '删除',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await DeleteMCPServer(id)
    await fetchMCPServers()
  } catch (e) {
    console.error('deleteMCPServer:', e)
  }
}

/** addPath appends a manually entered path (supports globs) to AllowedPaths. */
function addPath() {
  const p = newPathInput.value.trim()
  if (!p) return
  if (!cfg.value.AllowedPaths) cfg.value.AllowedPaths = []
  if (!cfg.value.AllowedPaths.includes(p)) {
    cfg.value.AllowedPaths.push(p)
  }
  newPathInput.value = ''
}

/** removePath removes the path at the given index from AllowedPaths. */
function removePath(index) {
  cfg.value.AllowedPaths.splice(index, 1)
}

/** addTrustedCommand appends a command prefix to ShellTrustedCommands. */
function addTrustedCommand() {
  const cmd = newTrustedCmdInput.value.trim()
  if (!cmd) return
  if (!cfg.value.ShellTrustedCommands) cfg.value.ShellTrustedCommands = []
  if (!cfg.value.ShellTrustedCommands.includes(cmd)) {
    cfg.value.ShellTrustedCommands.push(cmd)
  }
  newTrustedCmdInput.value = ''
}

/** removeTrustedCommand removes the command prefix at the given index. */
function removeTrustedCommand(index) {
  cfg.value.ShellTrustedCommands.splice(index, 1)
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

/** isValidCron checks whether `expr` is a standard 5- or 6-field cron spec.
 * Accepts common syntax (@every, @daily, etc.) for the robfig/cron parser too. */
function isValidCron(expr) {
  const e = expr.trim()
  if (!e) return false
  if (/^@(every\s+\S+|yearly|annually|monthly|weekly|daily|midnight|hourly|reboot)$/.test(e)) return true
  const fields = e.split(/\s+/)
  return fields.length === 5 || fields.length === 6
}

/** saveCronJob creates or updates a job. */
async function saveCronJob() {
  if (cronFormSaving.value) return
  const { id, name, description, schedule, prompt } = cronForm.value
  if (!name.trim() || !schedule.trim() || !prompt.trim()) {
    cronFormError.value = '名称、Cron 表达式和触发提示词为必填项'
    return
  }
  if (!isValidCron(schedule)) {
    cronFormError.value = 'Cron 表达式格式错误：应为 5 或 6 个字段，或 @every/@daily 等助记符'
    return
  }
  cronFormSaving.value = true
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
  } finally {
    cronFormSaving.value = false
  }
}

/** deleteCronJob removes a job after confirmation. */
async function deleteCronJob(id) {
  const job = cronJobs.value.find(j => j.ID === id)
  const ok = await confirm.ask({
    title: '删除定时任务',
    message: `确认删除任务「${job?.Name || ''}」？`,
    confirmText: '删除',
    variant: 'danger',
  })
  if (!ok) return
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

/** fetchKokoroTTSVoices loads the static Kokoro voice list. */
async function fetchKokoroTTSVoices() {
  try {
    kokoroTTSVoices.value = await GetKokoroTTSVoices() || []
  } catch { kokoroTTSVoices.value = [] }
}

const kokoroError = ref('')

/** setupKokoroTTS 触发后台一键安装 Kokoro TTS 环境。 */
async function setupKokoroTTS() {
  kokoroInstalling.value = true
  kokoroError.value = ''
  try {
    await SetupKokoroTTS()
  } catch (e) {
    kokoroError.value = '安装失败: ' + e
  } finally {
    kokoroInstalling.value = false
  }
}

/** toggleTTSAutoPlay persists the auto-play TTS setting. */
async function toggleTTSAutoPlay() {
  try {
    await SetTTSAutoPlay(cfg.value.TTSAutoPlay)
  } catch (e) {
    console.warn('toggleTTSAutoPlay failed:', e)
  }
}

/** toggleAutoLaunch enables or disables launch-at-login immediately. */
async function toggleAutoLaunch(val) {
  try {
    await SetAutoLaunch(val)
    autoLaunch.value = val
  } catch (e) {
    console.warn('SetAutoLaunch failed:', e)
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

/** deleteProactiveItem removes a reminder after confirmation; optimistic
 * delete with rollback on error. */
async function deleteProactiveItem(id) {
  const item = proactiveItems.value.find(i => i.ID === id)
  const ok = await confirm.ask({
    title: '删除提醒',
    message: `确认删除提醒「${item?.Title || item?.Content?.slice(0, 20) || ''}」？`,
    confirmText: '删除',
    variant: 'danger',
  })
  if (!ok) return
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
  <div
    class="settings-win"
    :style="{ left: pos.x + 'px', top: pos.y + 'px', width: winSize.w + 'px', height: winSize.h + 'px' }"
  >
    <!-- Draggable title bar (macOS style) -->
    <div class="win-titlebar" @mousedown="onHeaderMouseDown">
      <div class="traffic-lights" @mousedown.stop>
        <button class="traffic-btn tl-close" aria-label="关闭设置" @click.stop="$emit('close')">
          <svg viewBox="0 0 10 10" width="7" height="7"><path d="M2 2 L8 8 M8 2 L2 8" stroke="#4c0519" stroke-width="1.3" stroke-linecap="round"/></svg>
        </button>
        <span class="traffic-btn tl-min" aria-hidden="true" />
        <span class="traffic-btn tl-max" aria-hidden="true" />
      </div>
      <span class="win-title">设置</span>
      <div class="titlebar-search">
        <svg class="search-icon" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>
        </svg>
        <input
          v-model="searchQuery"
          type="search"
          placeholder="搜索设置..."
          class="search-input"
          spellcheck="false"
          autocorrect="off"
          autocomplete="off"
          @mousedown.stop
        />
      </div>
    </div>

    <!-- Sidebar + content -->
    <div class="win-body">
      <nav class="win-sidebar" aria-label="设置分类">
        <button
          v-for="tab in filteredTabs"
          :key="tab.id"
          :class="['nav-item', { active: activeTab === tab.id, match: isSearchMatch(tab) }]"
          @click="activeTab = tab.id"
        >
          <span class="nav-icon-wrap" v-html="tab.iconSvg" />
          <span class="nav-label">{{ tab.label }}</span>
        </button>
        <div v-if="filteredTabs.length === 0" class="nav-empty">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>
          </svg>
          <span>无匹配结果</span>
        </div>
      </nav>

      <div class="win-content" ref="winContentEl">
        <!-- 模型设置 -->
        <div v-if="activeTab === 'model'" class="tab-pane">
          <div class="profile-header">
            <span class="section-title">模型配置方案</span>
            <button class="btn-add" @click="openProfileForm">+ 新增</button>
          </div>

          <div v-if="profiles.length === 0" class="empty-hint">暂无配置方案，点击「新增」接入第一个 AI 模型</div>

          <div v-for="p in profiles" :key="p.id" :class="['profile-card', { active: p.id === activeProfileID }]">
            <div class="profile-card-main">
              <span class="profile-name" :title="p.name">{{ p.name }}</span>
              <span class="profile-meta" :title="`${p.provider} · ${p.model}`">{{ p.provider }} · {{ p.model }}</span>
              <span v-if="p.id === activeProfileID" class="profile-badge">使用中</span>
            </div>
            <div class="profile-card-actions">
              <button v-if="p.id !== activeProfileID" class="btn-activate" @click="activateProfile(p.id)">激活</button>
              <button class="btn-edit" @click="editProfile(p)">编辑</button>
              <button class="btn-del" @click="deleteProfile(p.id)">删除</button>
            </div>
          </div>

          <!-- Profile form dialog -->
          <div v-if="showProfileForm" class="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="profile-form-title" @click.self="showProfileForm = false">
            <div class="modal-box">
              <div id="profile-form-title" class="modal-title">{{ profileForm.id ? '编辑配置' : '新增配置' }}</div>
              <label>名称<input v-model="profileForm.name" placeholder="我的 OpenAI" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
              <label>接入方式
                <select v-model="profileForm.provider">
                  <option value="openai">OpenAI 兼容接口</option>
                  <option value="openrouter">OpenRouter</option>
                </select>
              </label>
              <label>Base URL
                <div class="url-row">
                  <input
                    v-model="profileForm.base_url"
                    :placeholder="profileForm.provider === 'openrouter' ? 'https://openrouter.ai/api/v1（留空使用默认）' : 'http://localhost:11434/v1'"
                    spellcheck="false" autocorrect="off" autocomplete="off"
                  />
                  <button class="fetch-btn" @click="fetchProfileModels" :disabled="fetchingProfileModels || (profileForm.provider !== 'openrouter' && !profileForm.base_url)">
                    {{ fetchingProfileModels ? '获取中...' : '获取模型' }}
                  </button>
                </div>
              </label>              <label>API Key<input v-model="profileForm.api_key" type="password" placeholder="（可选）" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
              <label>Model
                <div class="select-row">
                  <select v-if="profileModels.length" v-model="profileForm.model">
                    <option value="">-- 请选择模型 --</option>
                    <option v-for="m in profileModels" :key="m" :value="m">{{ m }}</option>
                  </select>
                  <input v-else v-model="profileForm.model" placeholder="gpt-4o" spellcheck="false" autocorrect="off" autocomplete="off" />
                </div>
              </label>
              <label>向量模型（Embedding）
                <div class="select-row">
                  <select v-if="profileModels.length" v-model="profileForm.embedding_model">
                    <option value="">-- 不启用（关闭知识库检索）--</option>
                    <option v-for="m in profileModels" :key="m" :value="m">{{ m }}</option>
                  </select>
                  <input v-else v-model="profileForm.embedding_model" placeholder="text-embedding-3-small（可选）" spellcheck="false" autocorrect="off" autocomplete="off" />
                </div>
              </label>
              <label>向量维度<span class="field-hint">与所选向量模型保持一致，默认 1536</span><input type="number" v-model.number="profileForm.embedding_dim" min="256" max="4096" /></label>
              <div class="form-group" style="margin-top:12px">
                <label class="form-label">语音合成引擎（TTS）</label>
                <select v-model="profileForm.tts_backend" class="form-input">
                  <option value="">系统语音（macOS 内置）</option>
                  <option value="kokoro">Kokoro-82M（本地离线，动漫中文风格）</option>
                </select>
              </div>

              <!-- Kokoro 专属选项 -->
              <template v-if="profileForm.tts_backend === 'kokoro'">
                <div class="form-group" style="margin-top:8px">
                  <label class="form-label">声线</label>
                  <select v-model="profileForm.tts_voice" class="form-input">
                    <option v-for="v in (kokoroTTSVoices.length ? kokoroTTSVoices : ['zf_xiaobei'])" :key="v" :value="v">{{ v }}</option>
                  </select>
                </div>
                <div class="form-group" style="margin-top:8px">
                  <label class="form-label">语速（0.5–2.0）</label>
                  <input
                    v-model.number="profileForm.tts_speed"
                    type="number" min="0.5" max="2.0" step="0.1"
                    class="form-input"
                  />
                </div>
                <div class="form-group" style="margin-top:12px">
                  <button class="btn-setup" :disabled="kokoroInstalling" @click="setupKokoroTTS">
                    {{ kokoroInstalling ? '安装中…' : '安装 / 检查 Kokoro 环境' }}
                  </button>
                  <div v-if="kokoroError" class="form-error" style="margin-top:8px">
                    {{ kokoroError }}
                    <button class="btn-retry" style="margin-left:8px" :disabled="kokoroInstalling" @click="setupKokoroTTS">重试</button>
                  </div>
                </div>
              </template>
              <div v-if="profileFormError" class="form-error">{{ profileFormError }}</div>
              <div class="modal-actions">
                <button class="btn-cancel" @click="showProfileForm = false">取消</button>
                <button class="btn-save" @click="saveProfile" :disabled="profileFormSaving">{{ profileFormSaving ? '保存中…' : '保存' }}</button>
              </div>
            </div>
          </div>
        </div>

        <!-- 对话设置 -->
        <div v-if="activeTab === 'ai'" class="tab-pane">
          <label>系统提示词<textarea v-model="cfg.SystemPrompt" rows="5" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
          <label>上下文记忆轮数（1–100）<span class="field-hint">保留最近 N 轮对话作为上下文，越大记得越多但消耗 token 也越多</span><input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" /></label>
          <label>自我成长触发间隔（轮）<span class="field-hint">每隔 N 轮对话，AI 自动整理用户画像与记忆；设为 0 可关闭</span><input type="number" v-model.number="cfg.NudgeInterval" min="1" max="100" /></label>
          <label>技能目录<span class="field-hint">每行一个路径，AI 可调用目录内的 YAML 自定义技能</span><textarea v-model="cfg.SkillsDirs" rows="3" placeholder="~/.aiko/auto-skills" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
        </div>

        <!-- 通用 -->
        <div v-if="activeTab === 'general'" class="tab-pane">
          <div class="settings-section-title">系统</div>
          <div class="sms-toggle-row" style="margin-top:8px">
            <span class="sms-status-label" style="flex:1">登录时自动启动</span>
            <label class="toggle">
              <input type="checkbox" :checked="autoLaunch" @change="toggleAutoLaunch($event.target.checked)" />
              <span class="toggle-track" />
            </label>
          </div>
          <p class="sms-desc" style="margin-top:4px;margin-bottom:20px">开机登录 macOS 后自动运行 Aiko</p>

          <div class="settings-section-title">界面主题</div>
          <label style="margin-top:8px">风格
            <div class="backend-toggle">
              <button
                :class="['backend-btn', cfg.ThemeStyle !== 'frosted' ? 'active' : '']"
                @click="setThemeStyle('liquid-glass')"
              >液态玻璃</button>
              <button
                :class="['backend-btn', cfg.ThemeStyle === 'frosted' ? 'active' : '']"
                @click="setThemeStyle('frosted')"
              >毛玻璃</button>
            </div>
          </label>
          <p class="sms-desc" style="margin-top:4px">液态玻璃：近透明折射光影；毛玻璃：经典深色质感。切换后点击「保存」持久化。</p>

          <div class="settings-section-title" style="margin-top:24px">快捷键</div>
          <div class="shortcut-list">
            <div class="shortcut-row">
              <div class="shortcut-keys"><kbd>⌥</kbd><kbd>⌥</kbd></div>
              <span class="shortcut-desc">双击 Option — 显示 / 隐藏聊天框</span>
            </div>
            <div class="shortcut-row">
              <div class="shortcut-keys"><kbd>⌥</kbd><span class="shortcut-hold">长按 1s</span></div>
              <span class="shortcut-desc">按住 Option 1 秒 — 开始语音输入</span>
            </div>
            <div class="shortcut-row">
              <div class="shortcut-keys"><span class="shortcut-release">松开 ⌥</span></div>
              <span class="shortcut-desc">松开 Option — 停止录音，等待识别</span>
            </div>
            <div class="shortcut-row">
              <div class="shortcut-keys"><kbd>↵</kbd></div>
              <span class="shortcut-desc">发送消息</span>
            </div>
            <div class="shortcut-row">
              <div class="shortcut-keys"><kbd>⌘</kbd><kbd>↵</kbd></div>
              <span class="shortcut-desc">消息框内换行</span>
            </div>
            <div class="shortcut-row">
              <div class="shortcut-keys"><kbd>⌘</kbd><kbd>V</kbd></div>
              <span class="shortcut-desc">粘贴图片到消息框（支持截图直接粘贴）</span>
            </div>
            <div class="shortcut-row">
              <div class="shortcut-keys"><span class="shortcut-drag">拖拽</span></div>
              <span class="shortcut-desc">拖动悬浮球 — 重新定位桌宠</span>
            </div>
            <div class="shortcut-row">
              <div class="shortcut-keys"><span class="shortcut-rc">右键</span></div>
              <span class="shortcut-desc">右键聊天框 — 导出记录、清空历史、打开设置</span>
            </div>
          </div>
        </div>

        <!-- 外观 -->
        <div v-if="activeTab === 'appearance'" class="tab-pane">
          <div class="settings-section-title">桌宠模型</div>
          <!-- 渲染后端 -->
          <label style="margin-top:8px">渲染模式
            <div class="backend-toggle">
              <button
                :class="['backend-btn', cfg.RenderBackend !== 'vrm' ? 'active' : '']"
                @click="setRenderBackend('live2d')"
              >Live2D（2D）</button>
              <button
                :class="['backend-btn', cfg.RenderBackend === 'vrm' ? 'active' : '']"
                @click="setRenderBackend('vrm')"
              >VRM（3D）</button>
            </div>
          </label>

          <!-- VRM 模型选择（仅在 VRM 后端下显示） -->
          <label v-if="cfg.RenderBackend === 'vrm'">VRM 模型
            <div class="vrm-model-row">
              <select v-model="cfg.VRMModel" @change="onVRMModelChange" class="vrm-select">
                <option v-for="m in availableVRMModels" :key="m.name" :value="m.name">
                  {{ m.name }} ({{ m.source === 'user' ? '用户导入' : '内置' }}, {{ m.size_kb }}KB)
                </option>
              </select>
              <button
                v-if="availableVRMModels.find(m => m.name === cfg.VRMModel)?.source === 'user'"
                class="btn-vrm-delete"
                @click="deleteVRMModel(cfg.VRMModel)"
                title="删除此模型"
              >删除</button>
            </div>
            <div class="vrm-upload-row">
              <input
                ref="vrmFileInput"
                type="file"
                accept=".vrm"
                style="display:none"
                @change="uploadVRMModel"
              />
              <button
                class="btn-vrm-upload"
                :disabled="vrmUploading"
                @click="$refs.vrmFileInput.click()"
              >{{ vrmUploading ? '上传中…' : '+ 导入 .vrm 模型' }}</button>
            </div>
            <div v-if="vrmUploadError" class="vrm-upload-error">{{ vrmUploadError }}</div>
          </label>

          <!-- VRM 动画预览 -->
          <label v-if="cfg.RenderBackend === 'vrm'">动画预览
            <div class="vrm-anim-grid">
              <button
                v-for="a in VRM_PREVIEW_ANIMS"
                :key="a.file"
                class="vrm-anim-btn"
                @click="previewVRMAnim(a.file)"
              >{{ a.label }}</button>
            </div>
          </label>

          <!-- Live2D 模型选择（仅在 Live2D 后端下显示） -->
          <label v-if="cfg.RenderBackend !== 'vrm'">Live2D 模型
            <div class="vrm-model-row">
              <select v-model="cfg.Live2DModel" @change="onLive2DModelChange" class="vrm-select">
                <option v-for="m in availableModels" :key="m" :value="m">{{ m }}</option>
              </select>
            </div>
          </label>
          <div class="settings-section-title" style="margin-top:16px">大小与位置</div>
          <label style="margin-top:8px">桌宠大小
            <div class="screen-label" v-if="props.activeScreen.width > 0">
              当前屏幕：{{ props.activeScreen.width }}×{{ props.activeScreen.height }}
            </div>
            <div class="size-row">
              <input
                type="range" min="100" max="600" step="10"
                :value="cfg.PetSize || 200"
                @input="previewPetSize"
              />
              <span class="size-val">{{ cfg.PetSize || '自动' }}{{ cfg.PetSize ? 'px' : '' }}</span>
            </div>
            <div class="size-hint">设为 0 时自动根据屏幕高度缩放；拖动滑块可实时预览</div>
            <button class="btn-reset-size" @click="cfg.PetSize = 0; EventsEmit('config:pet:size:changed', 0)">重置为自动</button>
            <button class="btn-reset-size" @click="resetBallPosition" style="margin-top:6px">重置桌宠位置</button>
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
          <div class="settings-section-title" style="margin-top:20px">语音与音效</div>
          <div class="sms-toggle-row" style="margin-top:8px">
            <span class="sms-status-label" style="flex:1">语音识别后自动发送</span>
            <label class="toggle">
              <input type="checkbox" v-model="cfg.VoiceAutoSend" @change="toggleVoiceAutoSend" />
              <span class="toggle-track" />
            </label>
          </div>
          <p class="sms-desc" style="margin-top:4px">松开 Option 键后，识别完成时自动发送消息，无需手动确认</p>
          <div class="sms-toggle-row" style="margin-top:16px">
            <span class="sms-status-label" style="flex:1">界面提示音</span>
            <label class="toggle">
              <input type="checkbox" v-model="cfg.SoundsEnabled" @change="toggleSoundsEnabled" />
              <span class="toggle-track" />
            </label>
          </div>
          <p class="sms-desc" style="margin-top:4px">发送、收到消息或出错时播放轻柔提示音</p>
          <div class="sms-toggle-row" style="margin-top:16px">
            <span class="sms-status-label" style="flex:1">AI 回复自动朗读</span>
            <label class="toggle">
              <input type="checkbox" v-model="cfg.TTSAutoPlay" @change="toggleTTSAutoPlay" />
              <span class="toggle-track" />
            </label>
          </div>
          <p class="sms-desc" style="margin-top:4px">收到 AI 回复后自动语音播放（需在模型配置中选择语音合成引擎）</p>
        </div>

        <!-- 工具 -->
        <div v-if="activeTab === 'tools'" class="tab-pane">
          <div class="sub-tab-bar">
            <button :class="{ active: toolsSubTab === 'permissions' }" @click="toolsSubTab = 'permissions'">内置工具</button>
            <button :class="{ active: toolsSubTab === 'mcp' }" @click="toolsSubTab = 'mcp'">MCP 扩展</button>
            <button :class="{ active: toolsSubTab === 'settings' }" @click="toolsSubTab = 'settings'">执行安全</button>
          </div>

          <!-- MCP 子 tab -->
          <template v-if="toolsSubTab === 'mcp'">
            <div class="section-header">
              <h3>MCP 扩展工具</h3>
              <button class="btn-small" @click="openMCPForm">+ 添加</button>
            </div>

            <div v-if="mcpServers.length === 0" class="empty-hint">
              暂无 MCP 服务器。点击"添加"接入外部工具（如浏览器控制、数据库查询等）
            </div>

            <div v-for="srv in mcpServers" :key="srv.id" class="mcp-row">
              <div class="mcp-info">
                <span class="mcp-name" :title="srv.name">{{ srv.name }}</span>
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

            <!-- Add/Edit Modal -->
            <div v-if="showMCPForm" class="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="mcp-form-title" @click.self="showMCPForm = false">
              <div class="modal-box">
                <div id="mcp-form-title" class="modal-title">{{ mcpForm.id ? '编辑 MCP 服务器' : '新增 MCP 服务器' }}</div>
                <label>名称<input v-model="mcpForm.name" placeholder="my-server" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>传输方式
                  <select v-model="mcpForm.transport">
                    <option value="stdio">stdio</option>
                    <option value="sse">SSE</option>
                    <option value="http">HTTP (Streamable)</option>
                  </select>
                </label>
                <template v-if="mcpForm.transport === 'stdio'">
                  <label>命令<input v-model="mcpForm.command" placeholder="/usr/local/bin/mcp-server" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                  <label>参数<span class="field-hint">空格分隔</span><input v-model="mcpForm.args" placeholder="--flag value" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                </template>
                <template v-else>
                  <label>URL<input v-model="mcpForm.url" placeholder="http://localhost:8080/sse" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                  <label>请求头<span class="field-hint">每行一个，格式：Key: Value</span><textarea v-model="mcpForm.headers" rows="3" placeholder="Authorization: Bearer xxx&#10;X-Custom: value" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                </template>
                <div v-if="mcpFormError" class="form-error">{{ mcpFormError }}</div>
                <div class="modal-actions">
                  <button class="btn-cancel" @click="showMCPForm = false">取消</button>
                  <button class="btn-save" :disabled="mcpFormSaving" @click="saveMCPServer">{{ mcpFormSaving ? '保存中…' : '保存' }}</button>
                </div>
              </div>
            </div>
          </template>

          <!-- 工具权限子 tab -->
          <template v-if="toolsSubTab === 'permissions'">
            <div v-if="toolPerms.length === 0" class="empty">暂无工具信息</div>
            <template v-else>
              <div class="public-tools-title">始终开启（无需授权）</div>
              <div class="public-tools">{{ publicToolNames }}</div>
              <div class="protected-tools-title">敏感工具（可单独开关）</div>
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

          <!-- 执行安全子 tab -->
          <template v-if="toolsSubTab === 'settings'">
            <div class="settings-section" style="margin-top:8px">
              <h3 class="section-title">文件访问目录</h3>
              <p class="section-hint">AI 只能读写列表内的路径。留空则禁止所有文件操作；支持通配符（如 /Users/me/projects/*）</p>
              <div class="path-list">
                <div v-for="(p, i) in cfg.AllowedPaths" :key="i" class="path-row">
                  <span class="path-text">{{ p }}</span>
                  <button class="btn-danger-small" @click="removePath(i)">删除</button>
                </div>
                <p v-if="!cfg.AllowedPaths || cfg.AllowedPaths.length === 0" class="empty-hint">暂无允许路径，文件读写已禁用</p>
              </div>
              <div class="path-add-row" style="margin-top:8px">
                <input
                  v-model="newPathInput"
                  class="path-input"
                  placeholder="/Users/me/projects 或 /tmp/*"
                  @keydown.enter="addPath"
                  spellcheck="false" autocorrect="off" autocomplete="off"
                />
                <button class="btn-small" @click="addPath">添加</button>
              </div>
            </div>

            <div class="settings-section" style="margin-top:12px">
              <h3 class="section-title">免确认命令</h3>
              <p class="section-hint">以下列命令名开头的 Shell 命令将跳过确认直接执行（建议填写低风险命令，如 git、ls）</p>
              <div class="path-list">
                <div v-for="(cmd, i) in cfg.ShellTrustedCommands" :key="i" class="path-row">
                  <span class="path-text">{{ cmd }}</span>
                  <button class="btn-danger-small" @click="removeTrustedCommand(i)">删除</button>
                </div>
                <p v-if="!cfg.ShellTrustedCommands || cfg.ShellTrustedCommands.length === 0" class="empty-hint">暂无免确认命令，所有 Shell 命令均需二次确认</p>
              </div>
              <div class="path-add-row" style="margin-top:8px">
                <input
                  v-model="newTrustedCmdInput"
                  class="path-input"
                  placeholder="git"
                  @keydown.enter="addTrustedCommand"
                  spellcheck="false" autocorrect="off" autocomplete="off"
                />
                <button class="btn-small" @click="addTrustedCommand">添加</button>
              </div>
            </div>

            <div class="settings-section" style="margin-top:12px">
              <h3 class="section-title">超时限制</h3>
              <div class="form-row">
                <label for="shell-timeout-input">Shell 命令超时（秒）</label>
                <input id="shell-timeout-input" type="number" v-model.number="cfg.ShellTimeout" min="1" max="3600" class="short-input" aria-describedby="timeout-hint" />
              </div>
              <div class="form-row">
                <label for="code-timeout-input">代码执行超时（秒）</label>
                <input id="code-timeout-input" type="number" v-model.number="cfg.CodeTimeout" min="1" max="3600" class="short-input" aria-describedby="timeout-hint" />
              </div>
              <p id="timeout-hint" class="section-hint" style="margin-top:8px">超时后进程强制终止，范围 1–3600 秒</p>
            </div>
          </template>
        </div>
        <div v-if="activeTab === 'knowledge'" class="tab-pane">
          <div class="section-header">
            <h3>知识库文件</h3>
            <button @click="importFile" :disabled="!!importProgress" class="btn-small">+ 导入文档</button>
          </div>
          <p class="section-hint">支持 .txt、.md、.pdf、.epub；导入后 AI 可通过语义检索引用文档内容</p>
          <div v-if="importProgress" class="progress">
            正在处理 {{ importProgress.Source }}：{{ importProgress.Processed }}/{{ importProgress.Total }} 段
          </div>
          <ul v-if="sources.length">
            <li v-for="src in sources" :key="src">
              <span>{{ src }}</span>
              <button @click="deleteSource(src)">删除</button>
            </li>
          </ul>
          <p v-else class="empty">暂无知识库文件，点击「导入文档」开始添加</p>
        </div>

        <!-- 自动化 -->
        <div v-if="activeTab === 'automation'" class="tab-pane">
          <div class="sub-tab-bar">
            <button :class="{ active: automationSubTab === 'cron' }" @click="automationSubTab = 'cron'">定时任务</button>
            <button :class="{ active: automationSubTab === 'proactive' }" @click="automationSubTab = 'proactive'">待触发提醒</button>
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

            <div v-for="job in cronJobs" :key="job.ID" class="cron-row">
              <div class="cron-info">
                <div class="cron-name-row">
                  <span class="cron-name" :title="job.Name">{{ job.Name }}</span>
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
            </div>

            <!-- Add/Edit Modal -->
            <div v-if="showCronForm" class="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="cron-form-title" @click.self="showCronForm = false">
              <div class="modal-box">
                <div id="cron-form-title" class="modal-title">{{ cronForm.id ? '编辑定时任务' : '新建定时任务' }}</div>
                <label>名称 *<input v-model="cronForm.name" placeholder="每日早报" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>描述<input v-model="cronForm.description" placeholder="可选说明" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>执行时间（Cron）*<span class="field-hint">5 段 cron 格式（分 时 日 月 周），如 0 8 * * * = 每天 8 点；也支持 @daily、@hourly 等</span><input v-model="cronForm.schedule" placeholder="0 8 * * *" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>触发指令 *<textarea v-model="cronForm.prompt" rows="4" placeholder="到达时间时发给 AI 的指令，如：帮我总结今日新闻" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <div v-if="cronFormError" class="form-error">{{ cronFormError }}</div>
                <div class="modal-actions">
                  <button class="btn-cancel" @click="showCronForm = false">取消</button>
                  <button class="btn-save" :disabled="cronFormSaving" @click="saveCronJob">{{ cronFormSaving ? '保存中…' : '保存' }}</button>
                </div>
              </div>
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
            <strong>注意：</strong>需在「对话」标签页的「技能目录」中添加飞书 Skills 路径（通常为 <code>~/.agents/skills</code>）。
          </p>
        </div>

        <!-- 短信监听 -->
        <div v-if="activeTab === 'sms'" class="tab-pane">
          <div class="section-header">
            <h3>短信验证码自动识别</h3>
          </div>
          <p class="sms-desc">
            监听 macOS「信息」App 收到的短信，自动提取验证码并复制到剪贴板，同时推送通知气泡。<br>
            <strong>所需权限：</strong>系统设置 → 隐私与安全性 → <strong>完全磁盘访问权限</strong> → 添加 Aiko。
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
                <p class="lark-step-desc">系统设置 → 隐私与安全性 → 完全磁盘访问权限 → 点击「+」添加 Aiko</p>
              </div>
            </div>
            <div class="sms-guide-step">
              <span class="lark-step-num">2</span>
              <div class="lark-step-body">
                <div class="lark-step-title">点击「开启监听」</div>
                <p class="lark-step-desc">收到含验证码的短信后，验证码自动写入剪贴板并弹出通知气泡，无需手动复制。</p>
              </div>
            </div>
          </div>

        </div>

        <!-- 关于 -->
        <div v-if="activeTab === 'about'" class="tab-pane about-pane">
          <div class="section-header"><h3>关于 Aiko</h3></div>

          <div class="about-version-row">
            <span class="about-label">当前版本</span>
            <span class="about-version">v{{ currentVersion }}</span>
          </div>

          <div class="about-update-area">
            <!-- Not yet checked -->
            <button v-if="!updateInfo && !updateChecking && !updateInstalling"
              class="fetch-btn" @click="checkUpdate">
              检查更新
            </button>

            <!-- Checking -->
            <span v-if="updateChecking" class="about-hint">正在检查…</span>

            <!-- No update -->
            <div v-if="updateInfo && !updateInfo.has_update && !updateInstalling" class="about-hint">
              已是最新版本（v{{ updateInfo.latest_version }}）
            </div>

            <!-- Has update -->
            <div v-if="updateInfo && updateInfo.has_update && !updateInstalling" class="about-update-available">
              <span>发现新版本 <strong>v{{ updateInfo.latest_version }}</strong></span>
              <a
                class="about-changelog-link"
                :href="`https://github.com/tiancheng92/Aiko/releases/tag/v${updateInfo.latest_version}`"
                target="_blank"
                rel="noopener noreferrer"
              >更新内容</a>
              <button class="fetch-btn fetch-btn--primary" @click="installUpdate"
                :disabled="!updateInfo.download_url">
                {{ updateInfo.download_url ? '立即更新' : '无可用下载' }}
              </button>
            </div>

            <!-- Installing -->
            <div v-if="updateInstalling" class="about-installing">
              <div class="about-progress-bar">
                <div class="about-progress-fill" :style="{ width: updateProgress + '%' }"></div>
              </div>
              <span class="about-hint">{{ updateProgressMsg || '准备中…' }}（{{ updateProgress }}%）</span>
            </div>

            <div v-if="updateError" class="lark-status lark-status--err" style="margin-top:8px; display:flex; align-items:center; gap:8px; flex-wrap:wrap">
              <span>{{ updateError }}</span>
              <button class="btn-retry" :disabled="updateChecking || updateInstalling" @click="updateInfo ? installUpdate() : checkUpdate()">重试</button>
            </div>
          </div>

          <div class="about-meta">
            <p>Powered by eino · Built with Wails</p>
          </div>
        </div>

      </div>
    </div>

    <!-- Footer -->
    <div class="win-footer">
      <span class="status-msg">
        <template v-if="statusMsg">{{ statusMsg }}</template>
        <template v-else-if="saving">保存中…</template>
      </span>
      <button class="btn-done" @click="$emit('close')">完成</button>
    </div>

    <!-- Resize handle (bottom-right corner) -->
    <div class="win-resize-handle" @mousedown="onResizeStart" aria-label="调整窗口大小">
      <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round">
        <path d="M14 4 4 14M14 9 9 14M14 14l-.01 0"/>
      </svg>
    </div>
    <!-- Confirm dialog — reuses modal-overlay/modal-box pattern so it looks
         identical to the edit/add form modals and stays within .settings-win
         (no Teleport → no click-through issues on macOS). -->
    <div
      v-if="confirm.visible.value"
      class="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-modal-title"
      @click.self="confirm.resolve(false)"
    >
      <div class="modal-box confirm-modal-box">
        <div id="confirm-modal-title" class="modal-title">{{ confirm.title.value }}</div>
        <p class="confirm-modal-text">{{ confirm.message.value }}</p>
        <div class="modal-actions">
          <button class="btn-cancel" @click="confirm.resolve(false)">{{ confirm.cancelText.value }}</button>
          <button
            :class="confirm.variant.value === 'danger' ? 'btn-danger' : 'btn-save'"
            @click="confirm.resolve(true)"
          >{{ confirm.confirmText.value }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-win {
  /* Surface/text tokens specific to Settings window (Chrome ≠ bubble surfaces) */
  --surface-card: rgba(255, 255, 255, 0.045);
  --surface-card-hover: rgba(255, 255, 255, 0.065);
  --border-strong: rgba(255, 255, 255, 0.14);
  --text-primary: rgba(255, 255, 255, 0.92);
  --text-secondary: rgba(255, 255, 255, 0.62);
  --text-tertiary: rgba(255, 255, 255, 0.42);
  --text-disabled: rgba(255, 255, 255, 0.24);
  --r-window: 14px;
  --r-card: 11px;
  --r-input: 7px;
  --r-button: 6px;

  position: fixed;
  z-index: 3000;
  min-width: 760px;
  min-height: 560px;
  background: var(--lg-surface);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border);
  border-radius: var(--r-window);
  box-shadow: var(--lg-shadow);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'SF Pro Display', 'PingFang SC', 'Helvetica Neue', sans-serif;
  font-size: 13px;
  color: var(--text-primary);
  accent-color: var(--accent);
  -webkit-font-smoothing: antialiased;
  user-select: none;
  -webkit-user-select: none;
}

/* Titlebar — traffic lights + title + global search */
.win-titlebar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 12px;
  height: 48px;
  cursor: move;
  flex-shrink: 0;
  user-select: none;
  border-bottom: 1px solid var(--lg-border-subtle);
  background: linear-gradient(to bottom, rgba(255, 255, 255, 0.03), rgba(255, 255, 255, 0));
}

.traffic-lights {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  padding: 6px 4px;
  margin: -6px -4px;
}
.traffic-btn {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: none;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  position: relative;
  box-shadow: inset 0 0 0 0.5px rgba(0, 0, 0, 0.3);
}
.tl-close::after {
  content: '';
  position: absolute;
  inset: -6px;
}
.traffic-btn svg { opacity: 0; transition: opacity 0.12s; }
.traffic-lights:hover .traffic-btn svg { opacity: 1; }
.tl-close { background: #ff5f57; }
.tl-close:hover { background: #ff4c44; }
.tl-close:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.tl-min { background: #febc2e; cursor: default; opacity: 0.55; }
.tl-max { background: #28c840; cursor: default; opacity: 0.55; }

.win-title {
  font-weight: 600;
  font-size: 13px;
  letter-spacing: -0.01em;
  color: var(--text-primary);
  padding-left: 4px;
}

.titlebar-search {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
  width: 220px;
  height: 28px;
  padding: 0 10px;
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border);
  border-radius: 7px;
  transition: border-color 0.15s, background 0.15s;
}
.titlebar-search:focus-within {
  border-color: var(--accent);
  background: var(--lg-surface-input-h);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
.search-icon { color: var(--text-tertiary); flex-shrink: 0; }
.search-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  color: var(--text-primary);
  font-size: 12px;
  font-family: inherit;
  padding: 0;
  box-shadow: none;
}
.search-input:focus,
.search-input:focus-visible {
  outline: none;
  box-shadow: none;
  border: none;
}
.search-input::placeholder { color: var(--text-tertiary); }
.search-input::-webkit-search-cancel-button { -webkit-appearance: none; appearance: none; }

/* Layout — sidebar + content */
.win-body { flex: 1; display: flex; overflow: hidden; }

/* Sidebar */
.win-sidebar {
  width: 200px;
  background: var(--lg-surface-elevated);
  border-right: 1px solid var(--lg-border-subtle);
  display: flex;
  flex-direction: column;
  padding: 10px 8px;
  gap: 1px;
  flex-shrink: 0;
  overflow-y: auto;
}
.win-sidebar::-webkit-scrollbar { width: 0; }

.nav-item {
  position: relative;
  -webkit-appearance: none;
  appearance: none;
  background: transparent;
  border: none;
  color: var(--text-secondary);
  padding: 7px 12px 7px 14px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 400;
  font-family: inherit;
  text-align: left;
  border-radius: 6px;
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  height: 32px;
  transition: background 0.12s, color 0.12s;
  box-shadow: none;
  letter-spacing: -0.01em;
}
.nav-item:hover { background: rgba(255, 255, 255, 0.05); color: var(--text-primary); }
.nav-item.active {
  background: var(--accent);
  color: #fff;
  font-weight: 500;
}
.nav-item.active .nav-svg { color: #fff; }
.nav-item.match { box-shadow: inset 0 0 0 1px var(--accent-alpha-20); }
.nav-item:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }

.nav-icon-wrap {
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.nav-icon-wrap :deep(svg) { width: 16px; height: 16px; color: currentColor; }
.nav-label { font-size: 13px; line-height: 1.2; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.nav-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 20px 8px;
  color: var(--text-tertiary);
  font-size: 11px;
}

/* Content area — section-card pattern */
.win-content {
  flex: 1;
  overflow-y: auto;
  padding: 24px 28px 32px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.15) transparent;
}
.win-content::-webkit-scrollbar { width: 10px; }
.win-content::-webkit-scrollbar-track { background: transparent; }
.win-content::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.12);
  border: 3px solid transparent;
  background-clip: padding-box;
  border-radius: 10px;
}
.win-content::-webkit-scrollbar-thumb:hover { background: rgba(255, 255, 255, 0.22); background-clip: padding-box; }

/* Tab pane — macOS "card of rows" pattern: each top-level label / .settings-section
   / .sms-toggle-row renders as a row inside a card container. */
.tab-pane { display: flex; flex-direction: column; gap: 18px; max-width: 720px; }
.tab-pane > label,
.tab-pane > .settings-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px 16px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  font-size: 13px;
  color: var(--text-secondary);
  font-weight: 400;
  letter-spacing: -0.01em;
  transition: border-color 0.15s;
}
.tab-pane > label:hover,
.tab-pane > .settings-section:hover { border-color: var(--lg-border); }
.tab-pane > label { font-size: 12px; font-weight: 500; color: var(--text-primary); }
.tab-pane > label > input,
.tab-pane > label > textarea,
.tab-pane > label > select { margin-top: 2px; }
.tab-pane .field-hint { font-size: 11px; color: var(--text-tertiary); font-weight: 400; margin-top: -4px; }

/* Base input, textarea, select */
input, textarea, select {
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border);
  border-radius: var(--r-input);
  padding: 6px 10px;
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  font-family: inherit;
  transition: border-color 0.15s, box-shadow 0.15s, background 0.15s;
  user-select: text;
  -webkit-user-select: text;
}
input:hover:not(:focus):not(:disabled),
textarea:hover:not(:focus):not(:disabled),
select:hover:not(:focus):not(:disabled) { background: var(--lg-surface-input-h); }
input:focus, textarea:focus, select:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
input::placeholder, textarea::placeholder { color: var(--text-tertiary); }
textarea { resize: vertical; line-height: 1.5; }
input[type="number"] { font-variant-numeric: tabular-nums; }
select {
  cursor: pointer;
  appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23ffffff' stroke-opacity='0.5' stroke-width='1.5' fill='none' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  padding-right: 28px;
}
input[type="range"] {
  appearance: none;
  -webkit-appearance: none;
  background: transparent;
  padding: 0;
  border: none;
  height: 18px;
}
input[type="range"]::-webkit-slider-runnable-track {
  height: 4px;
  background: rgba(255, 255, 255, 0.12);
  border-radius: 2px;
}
input[type="range"]::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 16px;
  height: 16px;
  background: #fff;
  border-radius: 50%;
  margin-top: -6px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.3), 0 0 0 0.5px rgba(0, 0, 0, 0.1);
  cursor: pointer;
  transition: transform 0.1s;
}
input[type="range"]:active::-webkit-slider-thumb { transform: scale(1.1); }
input[type="checkbox"] { accent-color: var(--accent); }

/* Buttons — macOS style (primary, secondary, destructive, small) */
button {
  background: var(--lg-surface-input);
  color: var(--text-primary);
  border: 1px solid var(--lg-border);
  border-radius: var(--r-button);
  padding: 5px 12px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  font-family: inherit;
  letter-spacing: -0.01em;
  transition: background 0.12s, border-color 0.12s, transform 0.08s;
  box-shadow: none;
  -webkit-appearance: none;
  appearance: none;
}
button:hover:not(:disabled) { background: var(--lg-surface-input-h); border-color: var(--border-strong); }
button:active:not(:disabled) { transform: scale(0.97); }
button:disabled { opacity: 0.4; cursor: not-allowed; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

.btn-primary,
.btn-save,
.btn-setup,
.btn-add {
  background: var(--accent);
  color: #fff;
  border: 1px solid transparent;
  font-weight: 500;
  padding: 6px 14px;
}
.btn-primary:hover:not(:disabled),
.btn-save:hover:not(:disabled),
.btn-setup:hover:not(:disabled),
.btn-add:hover:not(:disabled) { background: var(--accent-hover); border-color: transparent; }

.btn-done {
  background: var(--lg-surface-input);
  color: var(--text-primary);
  border: 1px solid var(--lg-border);
  padding: 5px 20px;
  font-weight: 500;
}
.btn-done:hover { background: var(--lg-surface-input-h); }

.btn-secondary,
.btn-cancel,
.btn-edit,
.btn-small,
.fetch-btn,
.btn-retry,
.btn-reset-size {
  background: var(--lg-surface-input);
  color: var(--text-primary);
  border: 1px solid var(--lg-border);
  padding: 5px 12px;
  font-size: 12px;
}
.btn-retry { padding: 3px 10px; font-size: 11px; }
.btn-small { padding: 4px 10px; font-size: 11px; }
.btn-reset-size { padding: 4px 10px; font-size: 11px; align-self: flex-start; }

.btn-activate {
  background: rgba(48, 209, 88, 0.14);
  color: var(--success);
  border: 1px solid rgba(48, 209, 88, 0.25);
  font-size: 11px;
  padding: 4px 10px;
}
.btn-activate:hover { background: rgba(48, 209, 88, 0.22); border-color: rgba(48, 209, 88, 0.4); }

.btn-del,
.btn-danger-small,
.btn-danger {
  background: var(--danger-bg);
  color: var(--danger);
  border: 1px solid rgba(255, 69, 58, 0.25);
  font-size: 11px;
  padding: 4px 10px;
}
.btn-del:hover,
.btn-danger-small:hover,
.btn-danger:hover { background: rgba(255, 69, 58, 0.22); border-color: rgba(255, 69, 58, 0.4); }

.fetch-btn--primary { background: var(--accent); color: #fff; border-color: transparent; }
.fetch-btn--primary:hover:not(:disabled) { background: var(--accent-hover); }

/* Toggle button (MCP enabled / Cron enable) */
.btn-toggle {
  font-size: 11px;
  padding: 4px 10px;
  border-radius: var(--r-button);
  border: 1px solid var(--lg-border);
  background: var(--lg-surface-input);
  color: var(--text-secondary);
  font-weight: 500;
}
.btn-toggle.active {
  background: rgba(48, 209, 88, 0.15);
  border-color: rgba(48, 209, 88, 0.35);
  color: var(--success);
}
.btn-toggle--enable {
  background: rgba(48, 209, 88, 0.15);
  border-color: rgba(48, 209, 88, 0.35);
  color: var(--success);
}
.btn-toggle--enable:hover { background: rgba(48, 209, 88, 0.25); }

/* URL row with fetch button */
.url-row { display: flex; gap: 8px; align-items: center; }
.url-row input { flex: 1; }

.select-row { display: flex; }
.select-row select, .select-row input { flex: 1; }

/* Section header (used across tabs) */
.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 6px 2px 10px;
}
.section-header h3 {
  margin: 0;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
.section-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-tertiary);
  margin: 0 0 4px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
.section-hint { font-size: 12px; color: var(--text-secondary); margin: 0 0 10px; line-height: 1.5; }

/* Model profile cards */
.profile-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.empty-hint { color: var(--text-secondary); font-size: 12px; padding: 20px 0;text-align: center; }
.profile-card {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 14px; margin-bottom: 8px;
  background: var(--surface-card);
  border-radius: var(--r-card);
  border: 1px solid var(--lg-border-subtle);
  transition: border-color 0.12s, background 0.12s;
}
.profile-card:hover { background: var(--surface-card-hover); border-color: var(--lg-border); }
.profile-card.active {
  border-color: var(--accent);
  background: var(--accent-alpha-08);
  box-shadow: 0 0 0 1px var(--accent-alpha-20);
}
.profile-card-main { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.profile-name { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.profile-meta { font-size: 11px; color: var(--text-tertiary); font-variant-numeric: tabular-nums; }
.profile-badge {
  font-size: 10px;
  color: var(--accent);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  background: var(--accent-alpha-12);
  padding: 2px 7px;
  border-radius: 4px;
  align-self: flex-start;
  margin-top: 2px;
}
.profile-card-actions { display: flex; gap: 6px; flex-shrink: 0; }

/* Modal (profile form dialog) */
.modal-overlay {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex; align-items: center; justify-content: center;
  animation: fadeIn 0.15s ease-out;
}
@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
.modal-box {
  background: var(--lg-surface-modal);
  backdrop-filter: var(--lg-blur-sm);
  -webkit-backdrop-filter: var(--lg-blur-sm);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-card);
  box-shadow: var(--lg-shadow);
  padding: 24px;
  width: 420px;
  max-height: 80vh;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  animation: modalIn 0.18s cubic-bezier(0.34, 1.56, 0.64, 1);
}
@keyframes modalIn { from { transform: scale(0.96); opacity: 0; } to { transform: scale(1); opacity: 1; } }
.modal-box::-webkit-scrollbar { width: 8px; }
.modal-box::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.14); border-radius: 4px; }
.modal-box label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
  padding: 0;
  background: none;
  border: none;
}
.modal-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 6px;
  letter-spacing: -0.01em;
}
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; padding-top: 12px; border-top: 1px solid var(--lg-border-subtle); }

/* Confirm dialog — narrower than form modals; danger button matches btn-save sizing */
.confirm-modal-box { width: 360px; }
.confirm-modal-text {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.55;
  margin: 0;
}
.modal-actions .btn-danger {
  background: var(--danger);
  color: #fff;
  border: 1px solid transparent;
  font-size: 13px;
  font-weight: 500;
  padding: 6px 14px;
}
.modal-actions .btn-danger:hover { background: #ff6961; border-color: transparent; }
.form-error {
  color: var(--danger);
  font-size: 12px;
  padding: 8px 10px;
  background: var(--danger-bg);
  border: 1px solid rgba(255, 69, 58, 0.25);
  border-radius: var(--r-input);
}
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 500; color: var(--text-secondary); }
.form-input { width: 100%; }

/* Tool permissions */
.hint { color: var(--text-secondary); font-size: 12px; margin: 0 0 8px; line-height: 1.55; }
.public-tools-title,
.protected-tools-title {
  font-size: 11px;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-weight: 600;
  margin-bottom: 8px;
}
.protected-tools-title { margin-top: 16px; }
.public-tools {
  font-size: 12px;
  color: var(--text-tertiary);
  line-height: 1.7;
  margin-bottom: 14px;
  padding: 10px 12px;
  background: var(--surface-card);
  border-radius: var(--r-input);
  border: 1px solid var(--lg-border-subtle);
}
.perm-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-input);
  margin-bottom: 4px;
  transition: background 0.12s;
}
.perm-row:hover { background: var(--surface-card-hover); }
.perm-info { display: flex; align-items: center; gap: 10px; min-width: 0; }
.perm-name { font-size: 13px; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.perm-level {
  font-size: 10px;
  padding: 2px 7px;
  border-radius: 4px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.perm-level.public { background: rgba(48, 209, 88, 0.14); color: var(--success); }
.perm-level.protected { background: rgba(255, 159, 10, 0.14); color: var(--warning); }

/* Toggle switch (iOS/macOS style) */
.toggle { display: flex; align-items: center; cursor: pointer; }
.toggle input { display: none; }
.toggle-track {
  width: 38px; height: 22px;
  background: rgba(255, 255, 255, 0.12);
  border-radius: 11px;
  position: relative;
  transition: background 0.2s;
}
.toggle input:checked ~ .toggle-track { background: var(--accent); }
.toggle-track::after {
  content: '';
  position: absolute;
  top: 2px; left: 2px;
  width: 18px; height: 18px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.25), 0 0 0 0.5px rgba(0, 0, 0, 0.08);
}
.toggle input:checked ~ .toggle-track::after { transform: translateX(16px); }
.toggle input:disabled ~ .toggle-track { opacity: 0.35; cursor: not-allowed; }

/* Knowledge list */
ul { list-style: none; padding: 0; margin: 0; }
.tab-pane > ul {
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  overflow: hidden;
}
.tab-pane > ul li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  border-bottom: 1px solid var(--lg-border-subtle);
  font-size: 13px;
  color: var(--text-primary);
}
.tab-pane > ul li:last-child { border-bottom: none; }
.tab-pane > ul li span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-right: 10px; font-variant-numeric: tabular-nums; }
.tab-pane > ul li button { font-size: 11px; padding: 4px 10px; }

.empty { color: var(--text-tertiary); font-size: 12px; margin-top: 6px; padding: 16px 0; text-align: center; }
.progress {
  color: var(--text-secondary);
  font-size: 12px;
  margin: 8px 0;
  padding: 8px 12px;
  background: var(--accent-alpha-08);
  border: 1px solid var(--accent-alpha-20);
  border-radius: var(--r-input);
}

/* Render backend toggle */
.backend-toggle {
  display: flex;
  gap: 4px;
  margin-top: 4px;
}
.backend-btn {
  padding: 4px 12px;
  border-radius: var(--r-button);
  border: 1px solid var(--lg-border);
  background: var(--lg-surface-input);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s, border-color 0.15s, color 0.15s;
}
.backend-btn:hover {
  background: var(--lg-surface-input-h);
  color: var(--text-primary);
  border-color: var(--border-strong);
}
.backend-btn.active {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}

/* Live2D model grid */
.model-grid { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 4px; }
.model-btn {
  background: var(--lg-surface-input);
  color: var(--text-secondary);
  border: 1px solid var(--lg-border);
  border-radius: var(--r-button);
  padding: 5px 12px;
  cursor: pointer;
  font-size: 12px;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.model-btn:hover { background: var(--lg-surface-input-h); color: var(--text-primary); border-color: var(--border-strong); }
.model-btn.selected {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}

/* VRM model upload */
.vrm-model-row {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-top: 4px;
}
.vrm-select { flex: 1; }
.btn-vrm-delete {
  padding: 4px 10px;
  font-size: 12px;
  border-radius: 6px;
  border: 1px solid rgba(255,80,80,0.4);
  background: rgba(255,80,80,0.12);
  color: #ff6b6b;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.15s;
}
.btn-vrm-delete:hover { background: rgba(255,80,80,0.25); }
.vrm-upload-row { margin-top: 6px; }
.btn-vrm-upload {
  padding: 5px 12px;
  font-size: 12px;
  border-radius: 6px;
  border: 1px dashed rgba(255,255,255,0.25);
  background: rgba(255,255,255,0.05);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.btn-vrm-upload:hover:not(:disabled) {
  background: rgba(255,255,255,0.1);
  color: var(--text-primary);
}
.btn-vrm-upload:disabled { opacity: 0.5; cursor: default; }
.vrm-upload-error {
  margin-top: 4px;
  font-size: 12px;
  color: #ff6b6b;
}
.vrm-anim-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
.vrm-anim-btn {
  padding: 4px 10px;
  font-size: 12px;
  border-radius: 6px;
  border: 1px solid rgba(255,255,255,0.15);
  background: rgba(255,255,255,0.06);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.vrm-anim-btn:hover {
  background: rgba(255,255,255,0.14);
  color: var(--text-primary);
}

/* Footer */
.win-footer {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  padding: 12px 18px;
  border-top: 1px solid var(--lg-border-subtle);
  flex-shrink: 0;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.12), rgba(0, 0, 0, 0));
}
.status-msg {
  color: var(--text-secondary);
  font-size: 12px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* MCP server rows */
.mcp-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  margin-bottom: 6px;
  gap: 10px;
  transition: background 0.12s;
}
.mcp-row:hover { background: var(--surface-card-hover); }
.mcp-info { display: flex; flex-direction: column; gap: 3px; flex: 1; min-width: 0; }
.mcp-name { font-weight: 600; font-size: 13px; color: var(--text-primary); }
.mcp-transport {
  font-size: 10px;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-weight: 600;
}
.mcp-endpoint {
  font-size: 11px;
  color: var(--text-tertiary);
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mcp-actions { display: flex; gap: 6px; flex-shrink: 0; }
.mcp-form {
  margin-top: 12px;
  padding: 16px;
  background: var(--surface-card);
  border-radius: var(--r-card);
  border: 1px solid var(--lg-border);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* Form rows (shared MCP / cron / etc.) */
.form-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.form-row label {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
  padding: 0;
  background: none;
  border: none;
}
.form-buttons {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 4px;
}

/* Size sliders (pet / chat) */
.size-row { display: flex; align-items: center; gap: 12px; margin-top: 2px; }
.size-row input[type=range] { flex: 1; cursor: pointer; }
.size-val {
  font-size: 12px;
  color: var(--accent);
  min-width: 52px;
  text-align: right;
  font-variant-numeric: tabular-nums;
  font-weight: 500;
}
.size-hint { font-size: 11px; color: var(--text-secondary); opacity: 0.88; margin-top: 2px; line-height: 1.5; }

/* Cron jobs */
.cron-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  margin-bottom: 6px;
  transition: border-color 0.12s, background 0.12s;
}
.cron-row:hover { background: var(--surface-card-hover); }
.cron-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.cron-name-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.cron-name { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.cron-schedule {
  font-size: 11px;
  color: var(--accent);
  background: var(--accent-alpha-12);
  border-radius: 4px;
  padding: 2px 7px;
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  font-weight: 500;
}
.cron-desc { font-size: 11px; color: var(--text-secondary); opacity: 0.88; }
.cron-prompt {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.5;
}
.cron-lastrun {
  font-size: 11px;
  color: var(--text-tertiary);
  font-variant-numeric: tabular-nums;
}
.cron-actions { display: flex; flex-direction: column; gap: 4px; flex-shrink: 0; }
.cron-status {
  font-size: 10px;
  border-radius: 4px;
  padding: 2px 7px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.cron-status--on  { background: rgba(48, 209, 88, 0.14); color: var(--success); }
.cron-status--off { background: rgba(255, 255, 255, 0.06); color: var(--text-tertiary); }
.cron-row--editing { border-color: var(--accent); background: var(--accent-alpha-08); }
.cron-edit-form { flex: 1; display: flex; flex-direction: column; gap: 12px; }
.cron-form {
  background: var(--surface-card);
  border: 1px solid var(--lg-border);
  border-radius: var(--r-card);
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 8px;
}
.cron-form h4 { margin: 0 0 2px; font-size: 13px; font-weight: 600; color: var(--text-primary); }

/* Lark tab */
.lark-status {
  padding: 10px 14px;
  border-radius: var(--r-input);
  font-size: 12px;
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  max-height: 160px;
  overflow: auto;
  line-height: 1.6;
}
.lark-status--ok {
  background: rgba(48, 209, 88, 0.08);
  border: 1px solid rgba(48, 209, 88, 0.25);
  color: var(--success);
}
.lark-status--err {
  background: var(--danger-bg);
  border: 1px solid rgba(255, 69, 58, 0.25);
  color: var(--danger);
}
.lark-status pre { margin: 0; white-space: pre-wrap; word-break: break-all; }
.lark-guide { display: flex; flex-direction: column; gap: 8px; }
.lark-guide-step,
.sms-guide-step {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
}
.lark-step-num {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--accent);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.lark-step-body { display: flex; flex-direction: column; gap: 6px; flex: 1; min-width: 0; }
.lark-step-title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.lark-step-desc { font-size: 11px; color: var(--text-tertiary); margin: 0; line-height: 1.55; }
.lark-code {
  display: block;
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  font-size: 11px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-input);
  padding: 6px 10px;
  color: var(--text-primary);
  user-select: text;
  white-space: nowrap;
  overflow-x: auto;
}
.lark-hint {
  font-size: 11px;
  color: var(--text-tertiary);
  line-height: 1.6;
  padding: 10px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
}
.lark-hint code {
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  color: var(--accent);
  background: var(--accent-alpha-08);
  padding: 1px 5px;
  border-radius: 3px;
}

.screen-label { font-size: 11px; color: var(--text-tertiary); margin-bottom: 6px; }

/* SMS watcher tab */
.sms-desc {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.6;
  margin: 0 0 14px;
}
.sms-desc strong { color: var(--text-primary); font-weight: 600; }
.sms-toggle-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
}
.sms-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dot-on { background: var(--success); box-shadow: 0 0 8px rgba(48, 209, 88, 0.6); }
.dot-off { background: var(--text-tertiary); }
.sms-status-label { font-size: 13px; color: var(--text-primary); font-weight: 500; }
.sms-guide { display: flex; flex-direction: column; gap: 8px; margin-top: 16px; }

/* Override appearance tab's sms-toggle-row pattern (voice/sounds/tts) — lighter card */
.tab-pane > .sms-toggle-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 500;
}

/* Proactive reminders */
.proactive-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  margin-bottom: 6px;
  gap: 10px;
}
.proactive-info { display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 0; }
.proactive-time {
  font-size: 11px;
  color: var(--accent);
  font-variant-numeric: tabular-nums;
  font-weight: 500;
}
.proactive-prompt {
  font-size: 13px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Sub-tab bar (MCP / permissions / settings; cron / proactive) */
.sub-tab-bar {
  display: inline-flex;
  gap: 2px;
  margin-bottom: 18px;
  padding: 3px;
  background: rgba(0, 0, 0, 0.25);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 8px;
}
.sub-tab-bar button {
  padding: 5px 14px;
  border-radius: 5px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
  box-shadow: none;
}
.sub-tab-bar button:hover { background: rgba(255, 255, 255, 0.04); color: var(--text-primary); }
.sub-tab-bar button.active {
  background: var(--lg-surface-input-h);
  color: var(--text-primary);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}

/* Tools / path white-list */
.path-list { display: flex; flex-direction: column; gap: 4px; }
.path-row {
  display: flex;
  align-items: center;
  gap: 10px;
  background: rgba(0, 0, 0, 0.18);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-input);
  padding: 8px 12px;
}
.path-text {
  flex: 1;
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  font-size: 12px;
  color: var(--text-primary);
  word-break: break-all;
}
.path-add-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: 8px;
}
.path-input {
  flex: 1;
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  font-size: 12px;
}
.short-input { width: 120px; }

/* About tab */
.about-pane { display: flex; flex-direction: column; gap: 14px; }
.about-version-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
}
.about-label { font-size: 13px; color: var(--text-secondary); flex: 1; }
.about-version {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  font-variant-numeric: tabular-nums;
}
.about-update-area { display: flex; flex-direction: column; gap: 10px; }
.about-update-available {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  color: var(--text-primary);
  padding: 12px 14px;
  background: var(--accent-alpha-08);
  border: 1px solid var(--accent-alpha-20);
  border-radius: var(--r-card);
}
.about-update-available > span { flex: 1; }
.about-changelog-link {
  font-size: 12px;
  color: var(--accent);
  text-decoration: none;
  padding: 4px 10px;
  border-radius: var(--r-button);
  border: 1px solid var(--accent-alpha-20);
  transition: background 0.12s;
  white-space: nowrap;
}
.about-changelog-link:hover { background: var(--accent-alpha-08); }
.about-hint { font-size: 12px; color: var(--text-secondary); }
.about-installing { display: flex; flex-direction: column; gap: 8px; padding: 12px 14px; background: var(--surface-card); border: 1px solid var(--lg-border-subtle); border-radius: var(--r-card); }
.about-progress-bar {
  height: 6px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.08);
  overflow: hidden;
}
.about-progress-fill {
  height: 100%;
  border-radius: 3px;
  background: var(--accent);
  transition: width 0.2s ease;
}
.about-meta {
  font-size: 11px;
  color: var(--text-secondary);
  opacity: 0.88;
  margin-top: 8px;
  text-align: center;
}

/* Resize handle */
.win-resize-handle {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 16px;
  height: 16px;
  cursor: nwse-resize;
  color: var(--text-tertiary);
  display: flex;
  align-items: flex-end;
  justify-content: flex-end;
  padding: 1px;
  user-select: none;
  opacity: 0.5;
  transition: opacity 0.15s;
}
.win-resize-handle:hover { opacity: 1; color: var(--text-secondary); }

/* Reduced motion support */
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}

/* ── Keyboard shortcuts reference ──────────────────────── */
.shortcut-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: 10px;
}
.shortcut-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 7px 10px;
  border-radius: var(--r-card);
  transition: background 0.12s;
}
.shortcut-row:hover {
  background: var(--surface-card);
}
.shortcut-keys {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 96px;
  flex-shrink: 0;
}
kbd {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 26px;
  height: 22px;
  padding: 0 6px;
  font-family: -apple-system, 'SF Pro Text', sans-serif;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-primary);
  background: var(--lg-surface-elevated);
  border: 1px solid var(--lg-border);
  border-bottom-width: 2px;
  border-radius: 5px;
  box-shadow: 0 1px 0 rgba(0, 0, 0, 0.25);
  user-select: none;
  line-height: 1;
}
.shortcut-hold,
.shortcut-release,
.shortcut-drag,
.shortcut-rc {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  font-size: 11px;
  font-weight: 500;
  color: var(--text-secondary);
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 5px;
  white-space: nowrap;
  user-select: none;
}
.shortcut-desc {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.4;
}
</style>

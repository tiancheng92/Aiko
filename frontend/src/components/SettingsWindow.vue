<!-- frontend/src/components/SettingsWindow.vue -->
<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { springAnimate } from '../composables/useSpring.js'
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
  GetSMSAllMessagesEnabled, SetSMSAllMessagesEnabled,
  GetVoiceAutoSend, SetVoiceAutoSend,
  GetSoundsEnabled, SetSoundsEnabled,
  GetKokoroTTSVoices, SetTTSAutoPlay, SetupKokoroTTS,
  GetVersion, CheckUpdate, InstallUpdate, RestartStatsTicker,
  ListVRMModels, ImportVRMFile, DeleteVRMModel,
  GetAutoLaunch, SetAutoLaunch,
  SetAvatar, ResetAvatar,
} from '../../wailsjs/go/main/App'
import { ListProactiveItems, DeleteProactiveItem } from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit, BrowserOpenURL } from '../../wailsjs/runtime/runtime'
import { throttle, debounce } from '../utils/timing.js'
import { useModelPath } from '../composables/useModelPath.js'
import { useEscapeKey } from '../composables/useEscapeKey.js'
import { useConfirm } from '../composables/useConfirm.js'
import {
  ICON_TAB_MODEL, ICON_TAB_AI, ICON_TAB_APPEARANCE, ICON_TAB_TOOLS,
  ICON_TAB_KNOWLEDGE, ICON_TAB_AUTOMATION, ICON_TAB_LARK, ICON_TAB_SMS, ICON_TAB_ABOUT,
  ICON_TAB_GENERAL, ICON_TAB_POMODORO,
  ICON_TAB_CLAUDE_CODE,
} from '../utils/icons'

const confirm = useConfirm()
const { locale, t } = useI18n()

const LANG_OPTIONS = computed(() => [
  { value: 'zh-CN', label: '中文' },
  { value: 'en',    label: 'English' },
  { value: 'ja',    label: '日本語' },
  { value: 'ko',    label: '한국어' },
  { value: '',      label: t('settings.language.followSystem') },
])

function detectSystemLocale() {
  const sys = navigator.language
  const short = sys.slice(0, 2)
  return ['zh-CN', 'en', 'ja', 'ko'].find(l => l.startsWith(short)) || 'en'
}

const selectedLang = computed({
  get: () => cfg.value.Language || '',
  set: (val) => {
    cfg.value.Language = val
    const resolved = val || detectSystemLocale()
    locale.value = resolved
    debouncedSave(true)
  },
})

const emit = defineEmits(['close'])

const props = defineProps({
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
})

const cfg = ref({
  LLMBaseURL: '', LLMAPIKey: '', LLMModel: '', EmbeddingModel: '',
  Live2DModel: 'hiyori',
  SystemPrompt: '', ShortTermLimit: 30, NudgeInterval: 5, MaxContextTokens: 10000, SkillsDirs: '',
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
  Language: '',
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
const activeTab = ref('general')  // 'general' | 'appearance' | 'model' | 'ai' | 'tools' | 'knowledge' | 'automation' | 'lark' | 'sms' | 'about'

const claudeCodeHookSnippet = computed(() => {
  const port = cfg.value.ClaudeCodePort || 9876
  return JSON.stringify({
    hooks: {
      PreToolUse: [{
        matcher: "",
        hooks: [{
          type: "http",
          url: `http://127.0.0.1:${port}/event`
        }]
      }],
      PermissionRequest: [{
        matcher: "",
        hooks: [{
          type: "http",
          url: `http://127.0.0.1:${port}/event`
        }]
      }],
      Stop: [{
        matcher: "",
        hooks: [{
          type: "http",
          url: `http://127.0.0.1:${port}/event`
        }]
      }],
      StopFailure: [{
        matcher: "",
        hooks: [{
          type: "http",
          url: `http://127.0.0.1:${port}/event`
        }]
      }]
    }
  }, null, 2)
})

const claudeCodeCopyLabel = ref(t('claudeCode.copy'))

/** debouncedSaveFlush cancels pending debounce and saves immediately.
 *  Used for settings that need instant server restart (e.g. enabled toggle). */
function debouncedSaveFlush() {
  clearTimeout(saveTimer)
  save()
}

/** copyClaudeCodeHook copies the hook config snippet to the clipboard. */
async function copyClaudeCodeHook() {
  try {
    await navigator.clipboard.writeText(claudeCodeHookSnippet.value)
    claudeCodeCopyLabel.value = t('claudeCode.copied')
    setTimeout(() => { claudeCodeCopyLabel.value = t('claudeCode.copy') }, 2000)
  } catch {
    // fallback — clipboard may not be available
  }
}
const toolsSubTab = ref('permissions')  // 'mcp' | 'permissions' | 'settings'
const automationSubTab = ref('cron')   // 'cron' | 'proactive'
const newPathInput = ref('')           // input buffer for adding allowed paths
const newTrustedCmdInput = ref('') // input buffer for adding trusted commands

const llmModels = ref([])       // fetched from /v1/models
const fetchingModels = ref(false)

// Model profiles
const profiles = ref([])
const activeProfileID = ref(0)
const showProfileForm = ref(false)
const profileForm = ref({ id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, embedding_inherit: true, embedding_provider: 'openai', embedding_base_url: '', embedding_api_key: '', tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '', supports_vision: false })
const profileFormError = ref('')
const profileFormSaving = ref(false)
const profileModels = ref([])
const fetchingProfileModels = ref(false)
const embeddingModels = ref([])
const fetchingEmbeddingModels = ref(false)
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
const cronForm = ref({ id: 0, name: '', description: '', schedule: '', prompt: '', saveToMemory: false, notify: true })
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
const smsAllMessagesEnabled = ref(false)

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
let offCronJobDone = null
let offKnowledgeDone = null
let offKnowledgeError = null
let offUpdateError = null
let offModelChanged = null
let progressHandler = null
let screenHandler = null
let updateProgressHandler = null

// ── Modal spring animation ──────────────────────────────────────────────────
// Shared across all 4 modal types (only one modal open at a time).
let cancelModal = null

/** applyModalBoxStyle writes transform + opacity from spring progress p ∈ [0..1]. */
function applyModalBoxStyle(box, p) {
  box.style.transform = `scale(${0.94 + 0.06 * p}) translateY(${8 * (1 - p)}px)`
  box.style.opacity   = Math.min(1, p * 1.6).toString()
}

/** onModalEnter springs the overlay opacity and inner box scale+translate in. */
function onModalEnter(el, done) {
  cancelModal?.()
  const box = el.querySelector('.modal-box')
  el.style.opacity = '0'
  if (box) applyModalBoxStyle(box, 0)

  let overlayDone = false
  let boxDone = !box

  function checkDone() {
    if (overlayDone && boxDone) { cancelModal = null; done() }
  }

  const cancelOverlay = springAnimate({
    from: 0, to: 1, stiffness: 420, damping: 34,
    restDelta: 0.004, restVelocity: 0.03,
    onUpdate : (p) => { el.style.opacity = p.toString() },
    onDone   : () => { el.style.opacity = ''; overlayDone = true; checkDone() },
  })

  const cancelBox = box ? springAnimate({
    from: 0, to: 1, stiffness: 320, damping: 22,
    restDelta: 0.004, restVelocity: 0.03,
    onUpdate : (p) => applyModalBoxStyle(box, p),
    onDone   : () => { box.style.transform = ''; box.style.opacity = ''; boxDone = true; checkDone() },
  }) : null

  cancelModal = () => { cancelOverlay(); cancelBox?.() }
}

/** onModalLeave springs the overlay + box out, then calls done(). */
function onModalLeave(el, done) {
  cancelModal?.()
  const box = el.querySelector('.modal-box')

  let overlayDone = false
  let boxDone = !box

  function checkDone() {
    if (overlayDone && boxDone) { cancelModal = null; done() }
  }

  const cancelOverlay = springAnimate({
    from: 1, to: 0, stiffness: 420, damping: 42,
    restDelta: 0.004, restVelocity: 0.03,
    onUpdate : (p) => { el.style.opacity = p.toString() },
    onDone   : () => { overlayDone = true; checkDone() },
  })

  const cancelBox = box ? springAnimate({
    from: 1, to: 0, stiffness: 420, damping: 40,
    restDelta: 0.004, restVelocity: 0.03,
    onUpdate : (p) => applyModalBoxStyle(box, p),
    onDone   : () => { boxDone = true; checkDone() },
  }) : null

  cancelModal = () => { cancelOverlay(); cancelBox?.() }
}

/**
 * tabMeta defines sidebar tabs with SVG icons and search keywords.
 * `_haystack` is pre-lowercased so the search filter doesn't re-normalize
 * label/keywords on every keystroke.
 */
const tabMeta = [
  { id: 'general',    label: '通用',   iconSvg: ICON_TAB_GENERAL,    iconBg: 'var(--cat-general)',
    keywords: 'theme launch autostart 主题 启动 风格 自启 开机自启 液态玻璃 毛玻璃 frosted liquid glass 界面主题 深色 暗色' },
  { id: 'appearance', label: '外观',   iconSvg: ICON_TAB_APPEARANCE, iconBg: 'var(--cat-appearance)',
    keywords: 'live2d vrm pet size chat 模型 大小 尺寸 语音 音效 朗读 桌宠 渲染 宠物 聊天框 头像 avatar 语音识别 自动发送 提示音 声音 自动朗读 TTS播放 动画' },
  { id: 'model',      label: '模型',   iconSvg: ICON_TAB_MODEL,      iconBg: 'var(--cat-model)',
    keywords: 'model profile openai openrouter deepseek provider key base url api embedding tts kokoro 模型 配置 接入 语音合成 声线 语速 摘要 向量 配置文件 激活' },
  { id: 'ai',         label: '对话',   iconSvg: ICON_TAB_AI,         iconBg: 'var(--cat-ai)',
    keywords: 'prompt system memory skill nudge 提示词 系统提示 记忆 长期记忆 短期记忆 技能 技能目录 上下文 轮数 自我成长 用户画像 沉淀' },
  { id: 'tools',      label: '工具',   iconSvg: ICON_TAB_TOOLS,      iconBg: 'var(--cat-tools)',
    keywords: 'mcp permission shell code path tool server 权限 服务器 执行 白名单 路径 内置工具 免确认 超时 allowed trusted shell timeout 安全 扩展' },
  { id: 'knowledge',  label: '知识库', iconSvg: ICON_TAB_KNOWLEDGE,  iconBg: 'var(--cat-knowledge)',
    keywords: 'knowledge rag document import vector jina tavily 文档 导入 向量 知识 检索 RAG 搜索 API key' },
  { id: 'automation', label: '自动化', iconSvg: ICON_TAB_AUTOMATION, iconBg: 'var(--cat-automation)',
    keywords: 'cron schedule proactive reminder followup 定时 任务 计划 提醒 待触发 follow-up 自动' },
  { id: 'lark',       label: '飞书',   iconSvg: ICON_TAB_LARK,       iconBg: 'var(--cat-lark)',
    keywords: 'lark feishu cli command 飞书 命令 lark-cli' },
  { id: 'sms',        label: '短信',   iconSvg: ICON_TAB_SMS,        iconBg: 'var(--cat-sms)',
    keywords: 'sms message verification code imessage chat.db 短信 验证码 监听 iMessage 短信监听' },
  { id: 'pomodoro', label: '番茄钟', iconSvg: ICON_TAB_POMODORO, iconBg: 'var(--cat-pomodoro)',
    keywords: 'pomodoro timer focus break 番茄 计时 专注 休息 时长 轮数' },
  { id: 'claudeCode', label: 'Claude Code', iconSvg: ICON_TAB_CLAUDE_CODE, iconBg: 'var(--cat-claude-code)',
    keywords: 'claude code hook sync pet 同步 状态 端口 气泡 通知' },
  { id: 'about',      label: '关于',   iconSvg: ICON_TAB_ABOUT,      iconBg: 'var(--cat-about)',
    keywords: 'version update about github release 版本 更新 关于 下载' },
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
  smsAllMessagesEnabled.value = cfg.value.SMSAllMessagesEnabled || false
  try {
    availableVRMModels.value = await ListVRMModels()
  } catch (e) {
    console.warn('SettingsWindow: failed to load VRM models', e)
  }
  try { autoLaunch.value = await GetAutoLaunch() } catch (e) {
    console.warn('GetAutoLaunch failed:', e)
  }
  fetchLarkStatus()
  progressHandler = throttle((p) => { importProgress.value = p }, 100)
  offProgress = EventsOn('knowledge:progress', progressHandler)
  offKnowledgeDone = EventsOn('knowledge:done', async () => {
    importProgress.value = null
    try { sources.value = await ListKnowledgeSources() || [] } catch (_) {}
  })
  offKnowledgeError = EventsOn('knowledge:error', (msg) => {
    importProgress.value = null
    statusMsg.value = t('settings.knowledge.importFailedDetail', { error: msg })
  })
  // Refresh per-screen sizes when the user moves the mouse to a different screen.
  screenHandler = debounce(async (info) => {
    try {
      const petSize = await GetPetSize(info.width, info.height)
      if (petSize > 0) cfg.value.PetSize = petSize
    } catch (e) { console.warn('SettingsWindow screen:active:changed: GetPetSize failed', e) }
    try {
      const [cw, ch] = await GetChatSize(info.width, info.height)
      if (cw > 0) cfg.value.ChatWidth = cw
      if (ch > 0) cfg.value.ChatHeight = ch
    } catch (e) { console.warn('SettingsWindow screen:active:changed: GetChatSize failed', e) }
  }, 200)
  offScreen = EventsOn('screen:active:changed', screenHandler)
  // Auto-fetch model list if URL is already configured.
  if (cfg.value.LLMBaseURL) fetchLLMModels()

  updateProgressHandler = throttle((data) => {
    updateProgress.value = data.pct
    updateProgressMsg.value = data.msg
  }, 100)
  offUpdateProgress = EventsOn('update:progress', updateProgressHandler)

  offCronJobDone = EventsOn('cron:job:done', () => { fetchCronJobs() })
  offModelChanged = EventsOn('config:model:changed', () => {
    if (statusMsg.value === t('settings.model.restartingAgent')) statusMsg.value = t('settings.model.profileSwitched')
  })
  offUpdateError = EventsOn('update:error', (msg) => {
    updateError.value = t('settings.about.installFailed', { error: msg })
    updateInstalling.value = false
  })

  // Enable auto-save watcher only after all initial data has been loaded.
  mountedReady.value = true
})

onUnmounted(() => {
  offProgress?.(); progressHandler?.cancel?.()
  offScreen?.(); screenHandler?.cancel?.()
  offUpdateProgress?.(); updateProgressHandler?.cancel?.()
  offCronJobDone?.()
  offKnowledgeDone?.()
  offKnowledgeError?.()
  offUpdateError?.()
  offModelChanged?.()
  cancelModal?.()
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
    if (llmModels.value.length === 0) statusMsg.value = t('settings.model.noModelsFetched')
  } catch (e) {
    statusMsg.value = t('settings.model.fetchModelsFailed', { error: e })
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
    updateError.value = t('settings.about.checkFailed', { error: e })
  } finally {
    updateChecking.value = false
  }
}

/** installUpdate starts an async download-and-install. Errors come via update:error event. */
async function installUpdate() {
  if (!updateInfo.value?.download_url) return
  updateInstalling.value = true
  updateProgress.value = 0
  updateProgressMsg.value = ''
  updateError.value = ''
  await InstallUpdate(updateInfo.value.download_url)
  // On success the app will quit/restart. Async errors handled by offUpdateError listener.
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
  profileForm.value = { id: 0, name: '', provider: 'openai', base_url: '', api_key: '', model: '', embedding_model: '', embedding_dim: 1536, embedding_inherit: true, embedding_provider: 'openai', embedding_base_url: '', embedding_api_key: '', tts_model_dir: '', tts_voice: '', tts_speed: 1.0, tts_backend: '', supports_vision: false }
  profileFormError.value = ''
  profileModels.value = []
  embeddingModels.value = []
  showProfileForm.value = true
}

/** editProfile opens the form pre-filled for an existing profile. */
function editProfile(p) {
  profileForm.value = { ...p, embedding_inherit: p.embedding_inherit ?? true, tts_backend: p.tts_backend || '' }
  profileFormError.value = ''
  profileModels.value = []
  embeddingModels.value = []
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

/** fetchEmbeddingModels fetches models for the embedding-specific base_url. */
async function fetchEmbeddingModels() {
  fetchingEmbeddingModels.value = true
  try {
    const provider = profileForm.value.embedding_provider
    const baseURL = profileForm.value.embedding_base_url
    const apiKey = profileForm.value.embedding_api_key
    if (provider === 'openrouter') {
      embeddingModels.value = await ListOpenRouterModels(baseURL, apiKey) || []
    } else {
      if (!baseURL) return
      embeddingModels.value = await ListLLMModels(baseURL, apiKey) || []
    }
  } catch {
    embeddingModels.value = []
  } finally {
    fetchingEmbeddingModels.value = false
  }
}

/** saveProfile creates or updates a profile. */
async function saveProfile() {
  if (profileFormSaving.value) return
  profileFormSaving.value = true
  profileFormError.value = ''
  if (!profileForm.value.name.trim()) { profileFormError.value = t('settings.model.validation.nameRequired'); profileFormSaving.value = false; return }
  if (!profileForm.value.model.trim()) { profileFormError.value = t('settings.model.validation.modelRequired'); profileFormSaving.value = false; return }
  if (profileForm.value.provider === 'openai' && !profileForm.value.base_url.trim()) {
    profileFormError.value = t('settings.model.validation.baseUrlRequired'); profileFormSaving.value = false; return
  }
  try {
    await SaveModelProfile({ ...profileForm.value })
    showProfileForm.value = false
    await fetchProfiles()
  } catch (e) {
    profileFormError.value = t('settings.model.saveFailed', { error: e })
  } finally {
    profileFormSaving.value = false
  }
}

/** activateProfile switches to the given profile. DB update returns immediately;
 *  Agent reinit is async — success is confirmed via config:model:changed event. */
async function activateProfile(id) {
  try {
    await ActivateModelProfile(id)
    activeProfileID.value = id
    // Refresh cfg so subsequent Save() doesn't overwrite the new profile's
    // LLM fields with stale values loaded before the profile switch.
    const loaded = await GetConfig()
    if (loaded) applyConfig(loaded)
    statusMsg.value = t('settings.model.restartingAgent')
  } catch (e) {
    statusMsg.value = t('settings.model.switchFailed', { error: e })
  }
}

/** deleteProfile removes a profile by id after a confirmation prompt. */
async function deleteProfile(id) {
  const p = profiles.value.find(x => x.id === id)
  const ok = await confirm.ask({
    title: t('settings.model.deleteProfileTitle'),
    message: t('settings.model.confirmDeleteProfile', { name: p?.name || '' }),
    confirmText: t('settings.model.delete'),
    variant: 'danger',
  })
  if (!ok) return
  try {
    await DeleteModelProfile(id)
    await fetchProfiles()
  } catch (e) {
    statusMsg.value = t('settings.model.deleteFailed', { error: e })
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

const VRM_PREVIEW_ANIMS = computed(() => [
  { file: 'waiting.vrma',        label: t('settings.appearance.vrmAnimLabels.waiting') },
  { file: 'wave_big.vrma',       label: t('settings.appearance.vrmAnimLabels.wave') },
  { file: 'nod.vrma',            label: t('settings.appearance.vrmAnimLabels.nod') },
  { file: 'curious.vrma',        label: t('settings.appearance.vrmAnimLabels.curious') },
  { file: 'relaxed.vrma',        label: t('settings.appearance.vrmAnimLabels.stretch') },
  { file: 'sleepy.vrma',         label: t('settings.appearance.vrmAnimLabels.sleepy') },
  { file: 'hand_talk.vrma',      label: t('settings.appearance.vrmAnimLabels.talk') },
  { file: 'embarrassed.vrma',    label: t('settings.appearance.vrmAnimLabels.awkward') },
  { file: 'sad.vrma',            label: t('settings.appearance.vrmAnimLabels.sad') },
  { file: 'angry.vrma',          label: t('settings.appearance.vrmAnimLabels.tsundere') },
  { file: 'surprised_react.vrma',label: t('settings.appearance.vrmAnimLabels.surprised') },
  { file: 'appearing.vrma',      label: t('settings.appearance.vrmAnimLabels.appear') },
])

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
  const ok = await confirm.ask({ title: t('settings.model.deleteVRMTitle'), message: t('settings.model.confirmDeleteVRM', { name }), variant: 'danger' })
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
    await RestartStatsTicker()
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
    statusMsg.value = t('settings.tools.permissions.updatePermFailed', { error: e })
  }
}

/** importFile opens a file picker and starts an async knowledge base import.
 *  Completion and errors are reported via knowledge:done / knowledge:error events. */
async function importFile() {
  const path = await OpenFileDialog(t('settings.selectFileTitle'), [{ DisplayName: t('settings.knowledge.importFile'), Pattern: '*.txt;*.md;*.pdf;*.epub' }])
  if (!path) return
  importProgress.value = { Source: path, Total: 0, Processed: 0 }
  try {
    await ImportKnowledge(path)
  } catch (e) {
    importProgress.value = null
    statusMsg.value = t('settings.knowledge.importFailedDetail', { error: e })
  }
}

/** deleteSource removes a knowledge source after confirmation. */
async function deleteSource(src) {
  const ok = await confirm.ask({
    title: t('settings.knowledge.deleteSourceTitle'),
    message: t('settings.knowledge.confirmDeleteSource', { name: src }),
    confirmText: t('settings.knowledge.deleteSource'),
    variant: 'danger',
  })
  if (!ok) return
  try {
    await DeleteKnowledgeSource(src)
    sources.value = sources.value.filter(s => s !== src)
  } catch (e) {
    statusMsg.value = t('settings.knowledge.deleteFailed', { error: e })
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
  if (!mcpForm.value.name.trim()) { mcpFormError.value = t('settings.tools.mcp.validation.nameRequired'); return }
  if (mcpForm.value.transport === 'stdio' && !mcpForm.value.command.trim()) {
    mcpFormError.value = t('settings.tools.mcp.validation.commandRequired'); return
  }
  if (mcpForm.value.transport !== 'stdio' && !mcpForm.value.url.trim()) {
    mcpFormError.value = t('settings.tools.mcp.validation.urlRequired'); return
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
    title: t('settings.tools.mcp.deleteServerTitle'),
    message: t('settings.tools.mcp.confirmDelete', { name: srv?.name || '' }),
    confirmText: t('settings.tools.mcp.delete'),
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
  cronForm.value = { id: 0, name: '', description: '', schedule: '', prompt: '', saveToMemory: false, notify: true }
  cronFormError.value = ''
  showCronForm.value = true
}

/** editCronJob opens the form pre-filled with an existing job. */
function editCronJob(job) {
  cronForm.value = { id: job.ID, name: job.Name, description: job.Description, schedule: job.Schedule, prompt: job.Prompt, saveToMemory: job.SaveToMemory, notify: job.Notify }
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
  const { id, name, description, schedule, prompt, saveToMemory, notify } = cronForm.value
  if (!name.trim() || !schedule.trim() || !prompt.trim()) {
    cronFormError.value = t('settings.automation.cron.validation.requiredFields')
    return
  }
  if (!isValidCron(schedule)) {
    cronFormError.value = t('settings.automation.cron.validation.invalidCron')
    return
  }
  cronFormSaving.value = true
  try {
    if (id) {
      await UpdateCronJob(id, name, description, schedule, prompt, saveToMemory, notify)
    } else {
      await CreateCronJob(name, description, schedule, prompt, saveToMemory, notify)
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
    title: t('settings.automation.cron.deleteJobTitle'),
    message: t('settings.automation.cron.confirmDelete', { name: job?.Name || '' }),
    confirmText: t('settings.automation.cron.delete'),
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
    statusMsg.value = t('settings.automation.cron.triggered')
  } catch (e) {
    statusMsg.value = t('settings.automation.cron.triggerFailed', { error: e })
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

/** toggleSMSAllMessages persists the all-messages setting and restarts the watcher. */
async function toggleSMSAllMessages() {
  try {
    await SetSMSAllMessagesEnabled(smsAllMessagesEnabled.value)
  } catch (e) {
    smsWatcherError.value = String(e)
    smsAllMessagesEnabled.value = !smsAllMessagesEnabled.value
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
    kokoroError.value = t('settings.model.kokoro.installFailed', { error: e })
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

// ── 头像管理 ──────────────────────────────────────────────

const aiAvatarInput = ref(null)
const userAvatarInput = ref(null)

/** uploadAvatar triggers the hidden file input for the given role. */
function uploadAvatar(role) {
  if (role === 'ai') aiAvatarInput.value?.click()
  else userAvatarInput.value?.click()
}

/** onAvatarFileChange reads the selected image file and saves it as the avatar. */
async function onAvatarFileChange(role, event) {
  const file = event.target.files?.[0]
  if (!file) return
  event.target.value = ''  // reset so the same file can be re-selected
  const reader = new FileReader()
  reader.onload = async (e) => {
    const dataURL = e.target.result
    try {
      await SetAvatar(role, dataURL)
      if (role === 'ai') cfg.value.AIAvatar = dataURL
      else cfg.value.UserAvatar = dataURL
    } catch (err) {
      console.error('SetAvatar failed:', err)
    }
  }
  reader.readAsDataURL(file)
}

/** resetAvatar resets the avatar for the given role to its built-in default. */
async function resetAvatar(role) {
  try {
    await ResetAvatar(role)
    if (role === 'ai') cfg.value.AIAvatar = ''
    else cfg.value.UserAvatar = ''
  } catch (e) {
    console.error('ResetAvatar failed:', e)
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
    proactiveError.value = t('settings.automation.proactive.loadFailed')
  }
}

/** deleteProactiveItem removes a reminder after confirmation; optimistic
 * delete with rollback on error. */
async function deleteProactiveItem(id) {
  const item = proactiveItems.value.find(i => i.ID === id)
  const ok = await confirm.ask({
    title: t('settings.automation.proactive.deleteTitle'),
    message: t('settings.automation.proactive.confirmDelete', { name: item?.Title || item?.Content?.slice(0, 20) || '' }),
    confirmText: t('settings.automation.proactive.delete'),
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
  return new Date(t).toLocaleString(locale.value || detectSystemLocale(), {
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
    <!-- Sidebar + content — no separate titlebar; sidebar owns traffic lights + title + search -->
    <div class="win-body">
      <nav class="win-sidebar" :aria-label="$t('settings.title')">
        <!-- Traffic lights + drag handle at the very top of the sidebar -->
        <div class="sidebar-drag" @mousedown="onHeaderMouseDown">
          <div class="traffic-lights" @mousedown.stop>
            <button class="traffic-btn tl-close" :aria-label="$t('settings.title')" @click.stop="$emit('close')">
              <svg viewBox="0 0 10 10" width="7" height="7"><path d="M2 2 L8 8 M8 2 L2 8" stroke="#4c0519" stroke-width="1.3" stroke-linecap="round"/></svg>
            </button>
            <span class="traffic-btn tl-min" aria-hidden="true" />
            <span class="traffic-btn tl-max" aria-hidden="true" />
          </div>
        </div>

        <!-- Sidebar title -->
        <div class="sidebar-heading">{{ $t('settings.title') }}</div>

        <!-- Search field -->
        <div class="sidebar-search" @mousedown.stop>
          <svg class="search-icon" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>
          </svg>
          <input
            v-model="searchQuery"
            type="search"
            :placeholder="$t('settings.search')"
            class="search-input"
            spellcheck="false"
            autocorrect="off"
            autocomplete="off"
          />
        </div>

        <!-- Nav items -->
        <div class="sidebar-nav-list">
          <button
            v-for="tab in filteredTabs"
            :key="tab.id"
            :class="['nav-item', { active: activeTab === tab.id, match: isSearchMatch(tab) }]"
            @click="activeTab = tab.id"
          >
            <span class="nav-icon-wrap" :style="{ background: tab.iconBg }" v-html="tab.iconSvg" />
            <span class="nav-label">{{ $t('settings.tabs.' + tab.id) }}</span>
          </button>
          <div v-if="filteredTabs.length === 0" class="nav-empty">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>
            </svg>
            <span>{{ $t('settings.noResults') }}</span>
          </div>
        </div>
      </nav>

      <div class="win-content-wrap">
        <!-- Invisible drag strip at the top of the content area, matching sidebar-drag height -->
        <div class="content-drag" @mousedown="onHeaderMouseDown" />

        <div class="win-content" ref="winContentEl">
        <!-- 模型设置 -->
        <div v-if="activeTab === 'model'" class="tab-pane">
          <div class="profile-header">
            <span class="section-title">{{ $t('settings.model.title') }}</span>
            <button class="btn-add" @click="openProfileForm">+ {{ $t('settings.model.addProfile') }}</button>
          </div>

          <div v-if="profiles.length === 0" class="empty-hint">{{ $t('settings.model.noProfile') }}</div>

          <div v-for="p in profiles" :key="p.id" :class="['profile-card', { active: p.id === activeProfileID }]">
            <div class="profile-card-main">
              <span class="profile-name" :title="p.name">{{ p.name }}</span>
              <span class="profile-meta" :title="`${p.provider} · ${p.model}`">{{ p.provider }} · {{ p.model }}</span>
              <span v-if="p.id === activeProfileID" class="profile-badge">{{ $t('settings.model.activated') }}</span>
            </div>
            <div class="profile-card-actions">
              <button v-if="p.id !== activeProfileID" class="btn-on-sm" @click="activateProfile(p.id)">{{ $t('settings.model.activate') }}</button>
              <button class="btn-edit" @click="editProfile(p)">{{ $t('settings.model.edit') }}</button>
              <button class="btn-danger-sm" @click="deleteProfile(p.id)">{{ $t('settings.model.delete') }}</button>
            </div>
          </div>

          <!-- Profile form dialog -->
          <Transition :css="false" @enter="onModalEnter" @leave="onModalLeave">
          <div v-if="showProfileForm" class="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="profile-form-title" @click.self="showProfileForm = false">
            <div class="modal-box">
              <div id="profile-form-title" class="modal-title">{{ profileForm.id ? $t('settings.model.edit') : $t('settings.model.addProfile') }}</div>
              <label>{{ $t('settings.model.profileName') }}<input v-model="profileForm.name" :placeholder="$t('settings.model.placeholder.profileName')" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
              <label>{{ $t('settings.model.provider') }}
                <select v-model="profileForm.provider">
                  <option value="openai">{{ $t('settings.model.providerLabels.openai') }}</option>
                  <option value="openrouter">{{ $t('settings.model.providerLabels.openrouter') }}</option>
                </select>
              </label>
              <label>{{ $t('settings.model.baseURL') }}
                <div class="url-row">
                  <input
                    v-model="profileForm.base_url"
                    :placeholder="profileForm.provider === 'openrouter' ? $t('settings.model.placeholder.baseURLOpenRouter') : $t('settings.model.placeholder.baseURL')"
                    spellcheck="false" autocorrect="off" autocomplete="off"
                  />
                  <button class="fetch-btn" @click="fetchProfileModels" :disabled="fetchingProfileModels || (profileForm.provider !== 'openrouter' && !profileForm.base_url)">
                    {{ fetchingProfileModels ? $t('settings.model.fetchingModels') : $t('settings.model.fetchModels') }}
                  </button>
                </div>
              </label>              <label>{{ $t('settings.model.apiKey') }}<input v-model="profileForm.api_key" type="password" :placeholder="$t('settings.model.placeholder.apiKeyOptional')" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
              <label>{{ $t('settings.model.model') }}
                <div class="select-row">
                  <select v-if="profileModels.length" v-model="profileForm.model">
                    <option value="">{{ $t('settings.model.placeholder.selectModel') }}</option>
                    <option v-for="m in profileModels" :key="m" :value="m">{{ m }}</option>
                  </select>
                  <input v-else v-model="profileForm.model" :placeholder="$t('settings.model.placeholder.model')" spellcheck="false" autocorrect="off" autocomplete="off" />
                </div>
              </label>
              <div class="embed-inherit-row">
                <span class="embed-inherit-label">{{ $t('settings.model.embeddingInherit') }}<span class="field-hint" style="margin-left:4px">{{ $t('settings.model.embeddingInheritHint') }}</span></span>
                <label class="toggle">
                  <input type="checkbox" v-model="profileForm.embedding_inherit" />
                  <span class="toggle-track" />
                </label>
              </div>
              <template v-if="!profileForm.embedding_inherit">
                <label style="margin-top:4px">{{ $t('settings.model.embeddingProvider') }}
                  <select v-model="profileForm.embedding_provider">
                    <option value="openai">{{ $t('settings.model.providerLabels.openai') }}</option>
                    <option value="openrouter">{{ $t('settings.model.providerLabels.openrouter') }}</option>
                  </select>
                </label>
                <label>{{ $t('settings.model.embeddingBaseURL') }}
                  <div class="url-row">
                    <input
                      v-model="profileForm.embedding_base_url"
                      :placeholder="profileForm.embedding_provider === 'openrouter' ? $t('settings.model.placeholder.baseURLOpenRouter') : $t('settings.model.placeholder.embeddingBaseURL')"
                      spellcheck="false" autocorrect="off" autocomplete="off"
                    />
                    <button class="fetch-btn" @click="fetchEmbeddingModels" :disabled="fetchingEmbeddingModels || (profileForm.embedding_provider !== 'openrouter' && !profileForm.embedding_base_url)">
                      {{ fetchingEmbeddingModels ? $t('settings.model.fetchingModels') : $t('settings.model.fetchModels') }}
                    </button>
                  </div>
                </label>
                <label>{{ $t('settings.model.embeddingAPIKey') }}
                  <input v-model="profileForm.embedding_api_key" type="password" :placeholder="$t('settings.model.placeholder.apiKeyOptional')" spellcheck="false" autocorrect="off" autocomplete="off" />
                </label>
              </template>
              <label>{{ $t('settings.model.embeddingModel') }}
                <div class="select-row">
                  <template v-if="profileForm.embedding_inherit">
                    <select v-if="profileModels.length" v-model="profileForm.embedding_model">
                      <option value="">{{ $t('settings.model.placeholder.embeddingDisable') }}</option>
                      <option v-for="m in profileModels" :key="m" :value="m">{{ m }}</option>
                    </select>
                    <input v-else v-model="profileForm.embedding_model" :placeholder="$t('settings.model.placeholder.embeddingModel')" spellcheck="false" autocorrect="off" autocomplete="off" />
                  </template>
                  <template v-else>
                    <select v-if="embeddingModels.length" v-model="profileForm.embedding_model">
                      <option value="">{{ $t('settings.model.placeholder.embeddingDisable') }}</option>
                      <option v-for="m in embeddingModels" :key="m" :value="m">{{ m }}</option>
                    </select>
                    <input v-else v-model="profileForm.embedding_model" :placeholder="$t('settings.model.placeholder.embeddingModel')" spellcheck="false" autocorrect="off" autocomplete="off" />
                  </template>
                </div>
              </label>
              <label>{{ $t('settings.model.embeddingDim') }}<span class="field-hint">{{ $t('settings.model.embeddingDimHint') }}</span><input type="number" v-model.number="profileForm.embedding_dim" min="256" max="4096" /></label>
              <div class="embed-inherit-row" style="margin-top:8px">
                <span class="embed-inherit-label">{{ $t('settings.model.supportsVision') }}<span class="field-hint" style="margin-left:4px">{{ $t('settings.model.supportsVisionHint') }}</span></span>
                <label class="toggle">
                  <input type="checkbox" v-model="profileForm.supports_vision" />
                  <span class="toggle-track" />
                </label>
              </div>
              <div class="form-group" style="margin-top:12px">
                <label class="form-label">{{ $t('settings.model.ttsBackend') }}</label>
                <select v-model="profileForm.tts_backend" class="form-input">
                  <option value="">{{ $t('settings.model.ttsBackendOptions.systemVoice') }}</option>
                  <option value="kokoro">{{ $t('settings.model.ttsBackendOptions.kokoro') }}</option>
                </select>
              </div>

              <!-- Kokoro 专属选项 -->
              <template v-if="profileForm.tts_backend === 'kokoro'">
                <div class="form-group" style="margin-top:8px">
                  <label class="form-label">{{ $t('settings.model.ttsVoice') }}</label>
                  <select v-model="profileForm.tts_voice" class="form-input">
                    <option v-for="v in (kokoroTTSVoices.length ? kokoroTTSVoices : ['zf_xiaobei'])" :key="v" :value="v">{{ v }}</option>
                  </select>
                </div>
                <div class="form-group" style="margin-top:8px">
                  <label class="form-label">{{ $t('settings.model.ttsSpeed') }}</label>
                  <input
                    v-model.number="profileForm.tts_speed"
                    type="number" min="0.5" max="2.0" step="0.1"
                    class="form-input"
                  />
                </div>
                <div class="form-group" style="margin-top:12px">
                  <button class="btn-setup" :disabled="kokoroInstalling" @click="setupKokoroTTS">
                    {{ kokoroInstalling ? $t('settings.model.kokoro.installing') : $t('settings.model.kokoro.install') }}
                  </button>
                  <div v-if="kokoroError" class="form-error" style="margin-top:8px">
                    {{ kokoroError }}
                    <button class="btn-retry" style="margin-left:8px" :disabled="kokoroInstalling" @click="setupKokoroTTS">{{ $t('settings.model.kokoro.retry') }}</button>
                  </div>
                </div>
              </template>
              <div v-if="profileFormError" class="form-error">{{ profileFormError }}</div>
              <div class="modal-actions">
                <button class="btn-cancel" @click="showProfileForm = false">{{ $t('settings.model.cancel') }}</button>
                <button class="btn-save" @click="saveProfile" :disabled="profileFormSaving">{{ profileFormSaving ? $t('settings.model.saving') : $t('settings.model.saveProfile') }}</button>
              </div>
            </div>
          </div>
          </Transition>
        </div>

        <!-- 对话设置 -->
        <div v-if="activeTab === 'ai'" class="tab-pane">

          <!-- 系统提示词 -->
          <div class="group-label">{{ $t('settings.ai.systemPrompt') }}</div>
          <div class="settings-group">
            <div class="settings-field">
              <div class="field-label">{{ $t('settings.ai.systemPrompt') }}</div>
              <textarea v-model="cfg.SystemPrompt" rows="5" spellcheck="false" autocorrect="off" autocomplete="off" />
            </div>
          </div>

          <!-- 记忆与成长 -->
          <div class="group-label">{{ $t('settings.ai.memoryAndGrowth') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.ai.shortTermLimit') }}</div>
                <div class="row-desc">{{ $t('settings.ai.shortTermLimitDesc') }}</div>
              </div>
              <input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" style="width:72px;text-align:center" />
            </div>
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.ai.maxContextTokens') }}</div>
                <div class="row-desc">{{ $t('settings.ai.maxContextTokensDesc') }}</div>
              </div>
              <input type="number" v-model.number="cfg.MaxContextTokens" min="0" step="1000" style="width:72px;text-align:center" />
            </div>
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.ai.nudgeInterval') }}</div>
                <div class="row-desc">{{ $t('settings.ai.nudgeIntervalDesc') }}</div>
              </div>
              <input type="number" v-model.number="cfg.NudgeInterval" min="0" max="100" style="width:72px;text-align:center" />
            </div>
          </div>

          <!-- 技能扩展 -->
          <div class="group-label">{{ $t('settings.ai.skillsSection') }}</div>
          <div class="settings-group">
            <div class="settings-field">
              <div class="field-label">{{ $t('settings.ai.skillsDirs') }} <span class="field-hint-inline">{{ $t('settings.ai.skillsDirsDesc') }}</span></div>
              <textarea v-model="cfg.SkillsDirs" rows="3" :placeholder="$t('settings.ai.skillsDirsPlaceholder')" spellcheck="false" autocorrect="off" autocomplete="off" />
            </div>
          </div>
        </div>

        <!-- 通用 -->
        <div v-if="activeTab === 'general'" class="tab-pane">

          <!-- 语言 -->
          <div class="group-label">{{ $t('settings.language.label') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.language.label') }}</div>
                <div class="row-desc">{{ $t('settings.general.languageDefaultDesc') }}</div>
              </div>
              <div class="row-ctrl">
                <select v-model="selectedLang" class="vrm-select">
                  <option v-for="opt in LANG_OPTIONS" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                  </option>
                </select>
              </div>
            </div>
          </div>

          <!-- 系统 -->
          <div class="group-label">{{ $t('settings.general.system') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.general.autoLaunch') }}</div>
                <div class="row-desc">{{ $t('settings.general.autoLaunchDesc') }}</div>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="autoLaunch" @change="toggleAutoLaunch($event.target.checked)" />
                <span class="toggle-track" />
              </label>
            </div>
          </div>

          <!-- 界面主题 -->
          <div class="group-label">{{ $t('settings.general.theme') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.general.style') }}</div>
                <div class="row-desc">{{ $t('settings.general.styleDesc') }}</div>
              </div>
              <div class="row-ctrl">
                <div class="backend-toggle">
                  <button
                    :class="['backend-btn', cfg.ThemeStyle !== 'frosted' ? 'active' : '']"
                    @click="setThemeStyle('liquid-glass')"
                  >{{ $t('settings.general.styleLiquidGlass') }}</button>
                  <button
                    :class="['backend-btn', cfg.ThemeStyle === 'frosted' ? 'active' : '']"
                    @click="setThemeStyle('frosted')"
                  >{{ $t('settings.general.styleFrosted') }}</button>
                </div>
              </div>
            </div>
          </div>

          <!-- 快捷键 -->
          <div class="group-label">{{ $t('settings.general.shortcuts') }}</div>
          <div class="settings-group">
            <div class="shortcut-list">
              <div class="shortcut-row">
                <div class="shortcut-keys"><kbd>⌥</kbd><kbd>⌥</kbd></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutOptionDouble') }}</span>
              </div>
              <div class="shortcut-row">
                <div class="shortcut-keys"><kbd>⌥</kbd><span class="shortcut-hold">{{ $t('settings.general.shortcutHoldLabel') }}</span></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutOptionHold') }}</span>
              </div>
              <div class="shortcut-row">
                <div class="shortcut-keys"><span class="shortcut-release">{{ $t('settings.general.shortcutReleaseLabel') }}</span></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutOptionRelease') }}</span>
              </div>
              <div class="shortcut-row">
                <div class="shortcut-keys"><kbd>↵</kbd></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutEnter') }}</span>
              </div>
              <div class="shortcut-row">
                <div class="shortcut-keys"><kbd>⌘</kbd><kbd>↵</kbd></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutCmdEnter') }}</span>
              </div>
              <div class="shortcut-row">
                <div class="shortcut-keys"><kbd>⌘</kbd><kbd>V</kbd></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutCmdV') }}</span>
              </div>
              <div class="shortcut-row">
                <div class="shortcut-keys"><span class="shortcut-drag">{{ $t('settings.general.shortcutDragLabel') }}</span></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutDrag') }}</span>
              </div>
              <div class="shortcut-row">
                <div class="shortcut-keys"><span class="shortcut-rc">{{ $t('settings.general.shortcutRightClickLabel') }}</span></div>
                <span class="shortcut-desc">{{ $t('settings.general.shortcutRightClick') }}</span>
              </div>
            </div>
          </div>

          <!-- 系统资源 -->
          <div class="group-label">{{ $t('system.settings.title') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <span class="row-title">{{ $t('system.settings.interval') }}</span>
                <span class="row-desc">{{ $t('system.settings.intervalDesc') }}</span>
              </div>
              <div class="row-ctrl">
                <input
                  v-model.number="cfg.SystemStatsInterval"
                  type="number"
                  min="1"
                  max="60"
                  class="input-int"
                />
                <span class="row-unit">s</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 外观 -->
        <div v-if="activeTab === 'appearance'" class="tab-pane">
          <div class="group-label">{{ $t('settings.appearance.petModel') }}</div>
          <!-- 渲染后端 -->
          <label style="margin-top:8px">{{ $t('settings.appearance.renderMode') }}
            <div class="backend-toggle">
              <button
                :class="['backend-btn', cfg.RenderBackend !== 'vrm' ? 'active' : '']"
                @click="setRenderBackend('live2d')"
              >{{ $t('settings.appearance.renderModeLive2D') }}</button>
              <button
                :class="['backend-btn', cfg.RenderBackend === 'vrm' ? 'active' : '']"
                @click="setRenderBackend('vrm')"
              >{{ $t('settings.appearance.renderModeVRM') }}</button>
            </div>
          </label>

          <!-- VRM 模型选择（仅在 VRM 后端下显示） -->
          <label v-if="cfg.RenderBackend === 'vrm'">{{ $t('settings.appearance.vrmModel') }}
            <div class="vrm-model-row">
              <select v-model="cfg.VRMModel" @change="onVRMModelChange" class="vrm-select">
                <option v-for="m in availableVRMModels" :key="m.name" :value="m.name">
                  {{ m.name }} ({{ m.source === 'user' ? $t('settings.appearance.vrmUserImported') : $t('settings.appearance.vrmBuiltin') }}, {{ m.size_kb }}KB)
                </option>
              </select>
              <button
                v-if="availableVRMModels.find(m => m.name === cfg.VRMModel)?.source === 'user'"
                class="btn-vrm-delete"
                @click="deleteVRMModel(cfg.VRMModel)"
                :title="$t('settings.appearance.vrmDeleteModel')"
              >{{ $t('settings.appearance.vrmDelete') }}</button>
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
              >{{ vrmUploading ? $t('settings.appearance.vrmUploading') : '+ ' + $t('settings.appearance.vrmImport') }}</button>
            </div>
            <div v-if="vrmUploadError" class="vrm-upload-error">{{ vrmUploadError }}</div>
          </label>

          <!-- VRM 动画预览 -->
          <label v-if="cfg.RenderBackend === 'vrm'">{{ $t('settings.appearance.vrmAnimPreview') }}
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
          <label v-if="cfg.RenderBackend !== 'vrm'">{{ $t('settings.appearance.live2DModel') }}
            <div class="vrm-model-row">
              <select v-model="cfg.Live2DModel" @change="onLive2DModelChange" class="vrm-select">
                <option v-for="m in availableModels" :key="m" :value="m">{{ m }}</option>
              </select>
            </div>
          </label>
          <div class="group-label">{{ $t('settings.appearance.chatSize') }}</div>
          <label style="margin-top:8px">{{ $t('settings.appearance.petSize') }}
            <div class="screen-label" v-if="props.activeScreen.width > 0">
              {{ $t('settings.appearance.currentScreen', { width: props.activeScreen.width, height: props.activeScreen.height }) }}
            </div>
            <div class="size-row">
              <input
                type="range" min="100" max="600" step="10"
                :value="cfg.PetSize || 200"
                @input="previewPetSize"
              />
              <span class="size-val">{{ cfg.PetSize || $t('settings.appearance.petSizeAuto') }}{{ cfg.PetSize ? 'px' : '' }}</span>
            </div>
            <div class="size-hint">{{ $t('settings.appearance.petSizeDesc') }}</div>
            <button class="btn-neutral-sm" @click="cfg.PetSize = 0; EventsEmit('config:pet:size:changed', 0)">{{ $t('settings.appearance.resetToAuto') }}</button>
            <button class="btn-neutral-sm" @click="resetBallPosition" style="margin-top:6px">{{ $t('settings.appearance.resetBall') }}</button>
          </label>
          <label>{{ $t('settings.appearance.chatWidth') }}
            <div class="size-row">
              <input
                type="range" min="300" max="800" step="10"
                :value="cfg.ChatWidth || 420"
                @input="previewChatSize('ChatWidth', $event)"
              />
              <span class="size-val">{{ cfg.ChatWidth || $t('settings.appearance.chatSizeDefault') }}{{ cfg.ChatWidth ? 'px' : '' }}</span>
            </div>
          </label>
          <label>{{ $t('settings.appearance.chatHeight') }}
            <div class="size-row">
              <input
                type="range" min="320" max="900" step="10"
                :value="cfg.ChatHeight || 540"
                @input="previewChatSize('ChatHeight', $event)"
              />
              <span class="size-val">{{ cfg.ChatHeight || $t('settings.appearance.chatSizeDefault') }}{{ cfg.ChatHeight ? 'px' : '' }}</span>
            </div>
            <button class="btn-neutral-sm" @click="resetChatSize">{{ $t('settings.appearance.resetToDefault') }}</button>
          </label>
          <!-- 语音与音效 -->
          <div class="group-label">{{ $t('settings.appearance.voice') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.appearance.voiceAutoSend') }}</div>
                <div class="row-desc">{{ $t('settings.appearance.voiceAutoSendDesc') }}</div>
              </div>
              <label class="toggle">
                <input type="checkbox" v-model="cfg.VoiceAutoSend" @change="toggleVoiceAutoSend" />
                <span class="toggle-track" />
              </label>
            </div>
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.appearance.soundsEnabled') }}</div>
                <div class="row-desc">{{ $t('settings.appearance.soundsEnabledDesc') }}</div>
              </div>
              <label class="toggle">
                <input type="checkbox" v-model="cfg.SoundsEnabled" @change="toggleSoundsEnabled" />
                <span class="toggle-track" />
              </label>
            </div>
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('settings.appearance.ttsAutoPlay') }}</div>
                <div class="row-desc">{{ $t('settings.appearance.ttsAutoPlayDesc') }}</div>
              </div>
              <label class="toggle">
                <input type="checkbox" v-model="cfg.TTSAutoPlay" @change="toggleTTSAutoPlay" />
                <span class="toggle-track" />
              </label>
            </div>
          </div>

          <!-- 头像 -->
          <input ref="aiAvatarInput" type="file" accept="image/*" style="display:none" @change="onAvatarFileChange('ai', $event)" />
          <input ref="userAvatarInput" type="file" accept="image/*" style="display:none" @change="onAvatarFileChange('user', $event)" />
          <div class="group-label">{{ $t('settings.appearance.avatar') }}</div>
          <div class="avatar-settings-row">
            <div class="avatar-item">
              <div class="avatar-preview-wrap" @click="uploadAvatar('ai')" :title="$t('settings.appearance.change')">
                <img v-if="cfg.AIAvatar" :src="cfg.AIAvatar" class="avatar-preview" :alt="$t('settings.appearance.aiAvatar')" draggable="false" />
                <img v-else src="/logo.png" class="avatar-preview" :alt="$t('settings.appearance.aiAvatar')" draggable="false" />
                <div class="avatar-overlay">
                  <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                </div>
              </div>
              <div class="avatar-label">{{ $t('settings.appearance.aiAvatar') }}</div>
              <button v-if="cfg.AIAvatar" class="avatar-reset-btn" @click="resetAvatar('ai')">{{ $t('settings.appearance.resetAvatar') }}</button>
            </div>
            <div class="avatar-item">
              <div class="avatar-preview-wrap" @click="uploadAvatar('user')" :title="$t('settings.appearance.change')">
                <div v-if="!cfg.UserAvatar" class="avatar-preview avatar-default-user">
                  <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.7 0 4.8-2.1 4.8-4.8S14.7 2.4 12 2.4 7.2 4.5 7.2 7.2 9.3 12 12 12zm0 2.4c-3.2 0-9.6 1.6-9.6 4.8v2.4h19.2v-2.4c0-3.2-6.4-4.8-9.6-4.8z"/></svg>
                </div>
                <img v-else :src="cfg.UserAvatar" class="avatar-preview" :alt="$t('settings.appearance.userAvatar')" draggable="false" />
                <div class="avatar-overlay">
                  <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                </div>
              </div>
              <div class="avatar-label">{{ $t('settings.appearance.userAvatar') }}</div>
              <button v-if="cfg.UserAvatar" class="avatar-reset-btn" @click="resetAvatar('user')">{{ $t('settings.appearance.resetAvatar') }}</button>
            </div>
          </div>
        </div>

        <!-- 工具 -->
        <div v-if="activeTab === 'tools'" class="tab-pane">
          <div class="sub-tab-bar">
            <button :class="{ active: toolsSubTab === 'permissions' }" @click="toolsSubTab = 'permissions'">{{ $t('settings.tools.subTabs.permissions') }}</button>
            <button :class="{ active: toolsSubTab === 'mcp' }" @click="toolsSubTab = 'mcp'">{{ $t('settings.tools.subTabs.mcp') }}</button>
            <button :class="{ active: toolsSubTab === 'settings' }" @click="toolsSubTab = 'settings'">{{ $t('settings.tools.subTabs.settings') }}</button>
          </div>

          <!-- MCP 子 tab -->
          <template v-if="toolsSubTab === 'mcp'">
            <div class="section-header">
              <h3>{{ $t('settings.tools.mcp.sectionTitle') }}</h3>
              <button class="btn-add-sm" @click="openMCPForm">+ {{ $t('settings.tools.mcp.addServer') }}</button>
            </div>

            <div v-if="mcpServers.length === 0" class="empty-hint">
              {{ $t('settings.tools.mcp.noServers') }}
            </div>

            <div v-for="srv in mcpServers" :key="srv.id" class="mcp-row">
              <div class="mcp-info">
                <span class="mcp-name" :title="srv.name">{{ srv.name }}</span>
                <span class="mcp-transport">{{ srv.transport }}</span>
                <span class="mcp-endpoint">{{ srv.transport === 'stdio' ? srv.command : srv.url }}</span>
              </div>
              <div class="mcp-actions">
                <button :class="srv.enabled ? 'btn-on-sm' : 'btn-off-sm'" @click="toggleMCPServer(srv)">
                  {{ srv.enabled ? $t('settings.tools.mcp.enabledStatus') : $t('settings.tools.mcp.disabledStatus') }}
                </button>
                <button class="btn-edit-sm" @click="editMCPServer(srv)">{{ $t('settings.model.edit') }}</button>
                <button class="btn-danger-small" @click="deleteMCPServer(srv.id)">{{ $t('settings.tools.mcp.delete') }}</button>
              </div>
            </div>

            <!-- Add/Edit Modal -->
            <Transition :css="false" @enter="onModalEnter" @leave="onModalLeave">
            <div v-if="showMCPForm" class="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="mcp-form-title" @click.self="showMCPForm = false">
              <div class="modal-box">
                <div id="mcp-form-title" class="modal-title">{{ mcpForm.id ? $t('settings.tools.mcp.editServer') : $t('settings.tools.mcp.addServer') }}</div>
                <label>{{ $t('settings.tools.mcp.name') }}<input v-model="mcpForm.name" placeholder="my-server" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>{{ $t('settings.tools.mcp.transport') }}
                  <select v-model="mcpForm.transport">
                    <option value="stdio">stdio</option>
                    <option value="sse">SSE</option>
                    <option value="http">HTTP (Streamable)</option>
                  </select>
                </label>
                <template v-if="mcpForm.transport === 'stdio'">
                  <label>{{ $t('settings.tools.mcp.command') }}<input v-model="mcpForm.command" placeholder="/usr/local/bin/mcp-server" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                  <label>{{ $t('settings.tools.mcp.args') }}<span class="field-hint">{{ $t('settings.tools.mcp.argsHint') }}</span><input v-model="mcpForm.args" placeholder="--flag value" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                </template>
                <template v-else>
                  <label>{{ $t('settings.tools.mcp.url') }}<input v-model="mcpForm.url" placeholder="http://localhost:8080/sse" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                  <label>{{ $t('settings.tools.mcp.headers') }}<span class="field-hint">{{ $t('settings.tools.mcp.headersHint') }}</span><textarea v-model="mcpForm.headers" rows="3" placeholder="Authorization: Bearer xxx&#10;X-Custom: value" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                </template>
                <div v-if="mcpFormError" class="form-error">{{ mcpFormError }}</div>
                <div class="modal-actions">
                  <button class="btn-cancel" @click="showMCPForm = false">{{ $t('settings.tools.mcp.cancel') }}</button>
                  <button class="btn-save" :disabled="mcpFormSaving" @click="saveMCPServer">{{ mcpFormSaving ? $t('settings.tools.mcp.saving') : $t('settings.tools.mcp.save') }}</button>
                </div>
              </div>
            </div>
            </Transition>
          </template>

          <!-- 工具权限子 tab -->
          <template v-if="toolsSubTab === 'permissions'">
            <div v-if="toolPerms.length === 0" class="empty">{{ $t('settings.tools.permissions.noTools') }}</div>
            <template v-else>
              <div class="public-tools-title">{{ $t('settings.tools.permissions.publicTitle') }}</div>
              <div class="public-tools">{{ publicToolNames }}</div>
              <div class="protected-tools-title">{{ $t('settings.tools.permissions.protectedTitle') }}</div>
              <template v-for="perm in protectedToolPerms" :key="perm.ToolName">
                <div class="perm-row">
                  <div class="perm-info">
                    <span class="perm-name">{{ perm.ToolName }}</span>
                    <span :class="['perm-level', perm.Level]">{{ perm.Level }}</span>
                  </div>
                  <label class="toggle">
                    <input type="checkbox" :checked="perm.Granted" @change="togglePerm(perm)" />
                    <span class="toggle-track" />
                  </label>
                </div>
                <template v-if="perm.ToolName === 'web_fetch'">
                  <div class="tool-api-key-row">
                    <label for="jina-api-key-input">Jina API Key</label>
                    <input
                      id="jina-api-key-input"
                      v-model="cfg.JinaAPIKey"
                      type="password"
                      :placeholder="$t('settings.tools.permissions.jinaPlaceholder')"
                      spellcheck="false"
                      autocorrect="off"
                      autocomplete="off"
                      class="tool-api-key-input"
                    />
                  </div>
                  <p class="tool-api-key-hint">{{ $t('settings.tools.permissions.jinaHint') }}</p>
                </template>
                <template v-if="perm.ToolName === 'web_search'">
                  <div class="tool-api-key-row">
                    <label for="tavily-api-key-input">Tavily API Key</label>
                    <input
                      id="tavily-api-key-input"
                      v-model="cfg.TavilyAPIKey"
                      type="password"
                      :placeholder="$t('settings.tools.permissions.tavilyPlaceholder')"
                      spellcheck="false"
                      autocorrect="off"
                      autocomplete="off"
                      class="tool-api-key-input"
                    />
                  </div>
                  <p class="tool-api-key-hint">{{ $t('settings.tools.permissions.tavilyHint') }}</p>
                </template>
              </template>
            </template>
          </template>

          <!-- 执行安全子 tab -->
          <template v-if="toolsSubTab === 'settings'">
            <div class="settings-section" style="margin-top:8px">
              <h3 class="section-title">{{ $t('settings.tools.settings.allowedPaths') }}</h3>
              <p class="section-hint">{{ $t('settings.tools.settings.allowedPathsDesc') }}</p>
              <div class="path-list">
                <div v-for="(p, i) in cfg.AllowedPaths" :key="i" class="path-row">
                  <span class="path-text">{{ p }}</span>
                  <button class="btn-danger-small" @click="removePath(i)">{{ $t('settings.tools.settings.remove') }}</button>
                </div>
                <p v-if="!cfg.AllowedPaths || cfg.AllowedPaths.length === 0" class="empty-hint">{{ $t('settings.tools.settings.noPaths') }}</p>
              </div>
              <div class="path-add-row" style="margin-top:8px">
                <input
                  v-model="newPathInput"
                  class="path-input"
                  :placeholder="$t('settings.tools.settings.pathPlaceholder')"
                  @keydown.enter="addPath"
                  spellcheck="false" autocorrect="off" autocomplete="off"
                />
                <button class="btn-add-sm" @click="addPath">{{ $t('settings.tools.settings.addPath') }}</button>
              </div>
            </div>

            <div class="settings-section" style="margin-top:12px">
              <h3 class="section-title">{{ $t('settings.tools.settings.trustedCommands') }}</h3>
              <p class="section-hint">{{ $t('settings.tools.settings.trustedCommandsDesc') }}</p>
              <div class="path-list">
                <div v-for="(cmd, i) in cfg.ShellTrustedCommands" :key="i" class="path-row">
                  <span class="path-text">{{ cmd }}</span>
                  <button class="btn-danger-small" @click="removeTrustedCommand(i)">{{ $t('settings.tools.settings.remove') }}</button>
                </div>
                <p v-if="!cfg.ShellTrustedCommands || cfg.ShellTrustedCommands.length === 0" class="empty-hint">{{ $t('settings.tools.settings.noCommands') }}</p>
              </div>
              <div class="path-add-row" style="margin-top:8px">
                <input
                  v-model="newTrustedCmdInput"
                  class="path-input"
                  :placeholder="$t('settings.tools.settings.cmdPlaceholder')"
                  @keydown.enter="addTrustedCommand"
                  spellcheck="false" autocorrect="off" autocomplete="off"
                />
                <button class="btn-add-sm" @click="addTrustedCommand">{{ $t('settings.tools.settings.addPath') }}</button>
              </div>
            </div>

            <div class="settings-section" style="margin-top:12px">
              <h3 class="section-title">{{ $t('settings.tools.settings.timeoutSection') }}</h3>
              <div class="form-row">
                <label for="shell-timeout-input">{{ $t('settings.tools.settings.shellTimeout') }}</label>
                <input id="shell-timeout-input" type="number" v-model.number="cfg.ShellTimeout" min="1" max="3600" class="short-input" aria-describedby="timeout-hint" />
              </div>
              <div class="form-row">
                <label for="code-timeout-input">{{ $t('settings.tools.settings.codeTimeout') }}</label>
                <input id="code-timeout-input" type="number" v-model.number="cfg.CodeTimeout" min="1" max="3600" class="short-input" aria-describedby="timeout-hint" />
              </div>
              <p id="timeout-hint" class="section-hint" style="margin-top:8px">{{ $t('settings.tools.settings.codeTimeoutDesc') }}</p>
            </div>

          </template>
        </div>
        <div v-if="activeTab === 'knowledge'" class="tab-pane">
          <div class="section-header">
            <h3>{{ $t('settings.knowledge.sources') }}</h3>
            <button @click="importFile" :disabled="!!importProgress" class="btn-add-sm">+ {{ $t('settings.knowledge.import') }}</button>
          </div>
          <p class="section-hint">{{ $t('settings.knowledge.importHint') }}</p>
          <div v-if="importProgress" class="progress">
            {{ $t('settings.knowledge.importProgress', { source: importProgress.Source, processed: importProgress.Processed, total: importProgress.Total }) }}
          </div>
          <ul v-if="sources.length">
            <li v-for="src in sources" :key="src">
              <span>{{ src }}</span>
              <button class="btn-danger-small" :aria-label="`${$t('settings.knowledge.deleteSource')} ${src}`" @click="deleteSource(src)">{{ $t('settings.knowledge.deleteSource') }}</button>
            </li>
          </ul>
          <p v-else class="empty">{{ $t('settings.knowledge.noSources') }}</p>
        </div>

        <!-- 自动化 -->
        <div v-if="activeTab === 'automation'" class="tab-pane">
          <div class="sub-tab-bar">
            <button :class="{ active: automationSubTab === 'cron' }" @click="automationSubTab = 'cron'">{{ $t('settings.automation.subTabs.cron') }}</button>
            <button :class="{ active: automationSubTab === 'proactive' }" @click="automationSubTab = 'proactive'">{{ $t('settings.automation.subTabs.proactive') }}</button>
          </div>

          <!-- 定时任务子 tab -->
          <template v-if="automationSubTab === 'cron'">
            <div class="section-header">
              <h3>{{ $t('settings.automation.subTabs.cron') }}</h3>
              <button class="btn-add-sm" @click="openCronForm">+ {{ $t('settings.automation.cron.add') }}</button>
            </div>

            <div v-if="cronJobs.length === 0" class="empty-hint">
              {{ $t('settings.automation.cron.noJobs') }}
            </div>

            <div v-for="job in cronJobs" :key="job.ID" class="cron-row">
              <div class="cron-info">
                <div class="cron-name-row">
                  <span class="cron-name" :title="job.Name">{{ job.Name }}</span>
                  <span class="cron-schedule">{{ job.Schedule }}</span>
                  <span class="cron-status" :class="job.Enabled ? 'cron-status--on' : 'cron-status--off'">
                    {{ job.Enabled ? $t('settings.automation.cron.statusEnabled') : $t('settings.automation.cron.statusDisabled') }}
                  </span>
                </div>
                <div v-if="job.Description" class="cron-desc">{{ job.Description }}</div>
                <div class="cron-prompt">{{ job.Prompt }}</div>
                <div v-if="job.Notify || job.SaveToMemory" class="cron-flags">
                  <span v-if="job.Notify" class="cron-flag cron-flag--on">
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" width="10" height="10"><path d="M8 1a5 5 0 0 0-5 5v1.38l-.8 1.6A1 1 0 0 0 3.1 10.5h9.8a1 1 0 0 0 .9-1.52L13 7.38V6a5 5 0 0 0-5-5zm0 13a2 2 0 0 1-1.73-1h3.46A2 2 0 0 1 8 14z"/></svg>
                    {{ $t('settings.automation.cron.notify') }}
                  </span>
                  <span v-if="job.SaveToMemory" class="cron-flag cron-flag--on">
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" width="10" height="10"><path d="M8 1a3.5 3.5 0 1 0 0 7A3.5 3.5 0 0 0 8 1zM3 10.5C3 9.12 5.24 8 8 8s5 1.12 5 2.5V14H3v-3.5z"/></svg>
                    {{ $t('settings.automation.cron.saveToMemory') }}
                  </span>
                </div>
                <div class="cron-lastrun">
                  <span v-if="job.LastRun">{{ $t('settings.automation.cron.lastRun', { time: new Date(job.LastRun).toLocaleString() }) }}</span>
                  <span v-if="job.NextRunAt && job.Enabled">{{ $t('settings.automation.cron.nextRun', { time: new Date(job.NextRunAt).toLocaleString() }) }}</span>
                </div>
              </div>
              <div class="cron-actions">
                <button class="btn-add-sm" @click="runCronJobNow(job.ID)">{{ $t('settings.automation.cron.runNow') }}</button>
                <button class="btn-edit-sm" @click="editCronJob(job)">{{ $t('settings.automation.cron.edit') }}</button>
                <button v-if="job.Enabled" class="btn-off-sm" @click="toggleCronJob(job)">{{ $t('settings.automation.cron.disable') }}</button>
                <button v-else class="btn-on-sm" @click="toggleCronJob(job)">{{ $t('settings.automation.cron.enable') }}</button>
                <button class="btn-danger-small" @click="deleteCronJob(job.ID)">{{ $t('settings.automation.cron.delete') }}</button>
              </div>
            </div>

            <!-- Add/Edit Modal -->
            <Transition :css="false" @enter="onModalEnter" @leave="onModalLeave">
            <div v-if="showCronForm" class="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="cron-form-title" @click.self="showCronForm = false">
              <div class="modal-box">
                <div id="cron-form-title" class="modal-title">{{ cronForm.id ? $t('settings.automation.cron.edit') : $t('settings.automation.cron.add') }}</div>
                <label>{{ $t('settings.automation.cron.name') }} *<input v-model="cronForm.name" :placeholder="$t('settings.automation.cron.placeholder.name')" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>{{ $t('settings.automation.cron.description') }}<input v-model="cronForm.description" :placeholder="$t('settings.automation.cron.placeholder.description')" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>{{ $t('settings.automation.cron.schedule') }} *<span class="field-hint">{{ $t('settings.automation.cron.scheduleHint') }}</span><input v-model="cronForm.schedule" :placeholder="$t('settings.automation.cron.placeholder.schedule')" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <label>{{ $t('settings.automation.cron.prompt') }} *<textarea v-model="cronForm.prompt" rows="4" :placeholder="$t('settings.automation.cron.placeholder.prompt')" spellcheck="false" autocorrect="off" autocomplete="off" /></label>
                <div class="cron-toggle-row">
                  <label class="cron-toggle-label">
                    <span class="cron-toggle-text">
                      <span class="cron-toggle-title">{{ $t('settings.automation.cron.notify') }}</span>
                      <span class="cron-toggle-hint">{{ $t('settings.automation.cron.notifyHint') }}</span>
                    </span>
                    <button
                      type="button"
                      class="toggle-switch"
                      :class="{ 'toggle-switch--on': cronForm.notify }"
                      :aria-checked="cronForm.notify"
                      role="switch"
                      @click="cronForm.notify = !cronForm.notify"
                    ><span class="toggle-thumb" /></button>
                  </label>
                  <label class="cron-toggle-label">
                    <span class="cron-toggle-text">
                      <span class="cron-toggle-title">{{ $t('settings.automation.cron.saveToMemory') }}</span>
                      <span class="cron-toggle-hint">{{ $t('settings.automation.cron.saveToMemoryHint') }}</span>
                    </span>
                    <button
                      type="button"
                      class="toggle-switch"
                      :class="{ 'toggle-switch--on': cronForm.saveToMemory }"
                      :aria-checked="cronForm.saveToMemory"
                      role="switch"
                      @click="cronForm.saveToMemory = !cronForm.saveToMemory"
                    ><span class="toggle-thumb" /></button>
                  </label>
                </div>
                <div v-if="cronFormError" class="form-error">{{ cronFormError }}</div>
                <div class="modal-actions">
                  <button class="btn-cancel" @click="showCronForm = false">{{ $t('settings.automation.cron.cancel') }}</button>
                  <button class="btn-save" :disabled="cronFormSaving" @click="saveCronJob">{{ cronFormSaving ? $t('settings.automation.cron.saving') : $t('settings.automation.cron.save') }}</button>
                </div>
              </div>
            </div>
            </Transition>
          </template>

          <!-- 提醒事项子 tab -->
          <template v-if="automationSubTab === 'proactive'">
            <div class="section-header">
              <h3>{{ $t('settings.automation.subTabs.proactive') }}</h3>
              <button class="btn-neutral-sm" @click="loadProactiveItems">{{ $t('settings.automation.proactive.refresh') }}</button>
            </div>

            <div v-if="proactiveError" class="form-error">{{ proactiveError }}</div>

            <div v-if="proactiveItems.length === 0 && !proactiveError" class="empty-hint">
              {{ $t('settings.automation.proactive.noItems') }}
            </div>

            <div v-for="item in proactiveItems" :key="item.ID" class="proactive-row">
              <div class="proactive-info">
                <span class="proactive-time">{{ formatProactiveTime(item.TriggerAt) }}</span>
                <span class="proactive-prompt">{{ truncatePrompt(item.Prompt, 60) }}</span>
              </div>
              <button class="btn-danger-sm" @click="deleteProactiveItem(item.ID)">{{ $t('settings.automation.proactive.delete') }}</button>
            </div>
          </template>
        </div>

        <!-- 飞书 lark-cli -->
        <div v-if="activeTab === 'lark'" class="tab-pane">
          <div class="url-row" style="margin-bottom:8px">
            <span style="flex:1;font-size:12px;color:var(--text-tertiary)">{{ $t('settings.lark.pathHint') }}</span>
            <button class="fetch-btn" @click="fetchLarkStatus" :disabled="larkStatusLoading">
              {{ larkStatusLoading ? $t('settings.lark.detecting') : $t('settings.lark.status') }}
            </button>
          </div>

          <div v-if="larkStatus" class="lark-status lark-status--ok">
            <pre>{{ larkStatus }}</pre>
          </div>
          <div v-else-if="larkStatusError" class="lark-status lark-status--err">{{ larkStatusError }}</div>

          <div class="section-header" style="margin-top:8px">
            <h3>{{ $t('settings.lark.guide') }}</h3>
          </div>
          <div class="lark-guide">
            <div class="lark-guide-step">
              <span class="lark-step-num">1</span>
              <div class="lark-step-body">
                <div class="lark-step-title">{{ $t('settings.lark.guideSteps.step1Title') }}</div>
                <code class="lark-code">npm install -g @larksuite/cli</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">2</span>
              <div class="lark-step-body">
                <div class="lark-step-title">{{ $t('settings.lark.guideSteps.step2Title') }}</div>
                <code class="lark-code">npx skills add larksuite/cli -y -g</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">3</span>
              <div class="lark-step-body">
                <div class="lark-step-title">{{ $t('settings.lark.guideSteps.step3Title') }}</div>
                <code class="lark-code">lark-cli config init</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">4</span>
              <div class="lark-step-body">
                <div class="lark-step-title">{{ $t('settings.lark.guideSteps.step4Title') }}</div>
                <code class="lark-code">lark-cli auth login --recommend</code>
              </div>
            </div>
            <div class="lark-guide-step">
              <span class="lark-step-num">5</span>
              <div class="lark-step-body">
                <div class="lark-step-title">{{ $t('settings.lark.guideSteps.step5Title') }}</div>
              </div>
            </div>
          </div>

          <p class="lark-hint">
            {{ $t('settings.lark.hint') }}<br>
            <strong>{{ $t('settings.lark.hintNotePrefix') }}</strong>{{ $t('settings.lark.hintNote', { path: '~/.agents/skills' }) }}
          </p>
        </div>

        <!-- 短信监听 -->
        <div v-if="activeTab === 'sms'" class="tab-pane">
          <div class="section-header">
            <h3>{{ $t('settings.sms.watcher') }}</h3>
          </div>
          <p class="sms-desc">
            {{ $t('settings.sms.desc1') }}<br>
            <strong>{{ $t('settings.sms.desc2Prefix') }}</strong>{{ $t('settings.sms.desc2') }}
          </p>

          <div class="sms-toggle-row">
            <span class="sms-status-group">
              <span class="sms-status-dot" :class="smsWatcherRunning ? 'dot-on' : 'dot-off'"></span>
              <span class="sms-status-label">{{ smsWatcherRunning ? $t('settings.sms.running') : $t('settings.sms.stopped') }}</span>
            </span>
            <button class="fetch-btn" @click="toggleSMSWatcher" :disabled="smsWatcherLoading">
              {{ smsWatcherLoading ? $t('settings.sms.processing') : (smsWatcherRunning ? $t('settings.sms.stop') : $t('settings.sms.start')) }}
            </button>
          </div>

          <div v-if="smsWatcherError" class="lark-status lark-status--err" style="margin-top:8px">
            {{ smsWatcherError }}
          </div>

          <div v-if="smsWatcherRunning" class="sms-toggle-row" style="margin-top:12px">
            <span class="sms-status-label">{{ $t('settings.sms.allMessages') }}</span>
            <button
              class="toggle-switch"
              :class="{ 'toggle-switch--on': smsAllMessagesEnabled }"
              role="switch"
              :aria-checked="smsAllMessagesEnabled"
              @click="smsAllMessagesEnabled = !smsAllMessagesEnabled; toggleSMSAllMessages()"
            ><span class="toggle-thumb"></span></button>
          </div>

          <div class="sms-guide">
            <div class="sms-guide-step">
              <span class="lark-step-num">1</span>
              <div class="lark-step-body">
                <div class="lark-step-title">{{ $t('settings.sms.guideSteps.step1Title') }}</div>
                <p class="lark-step-desc">{{ $t('settings.sms.guideSteps.step1Desc') }}</p>
              </div>
            </div>
            <div class="sms-guide-step">
              <span class="lark-step-num">2</span>
              <div class="lark-step-body">
                <div class="lark-step-title">{{ $t('settings.sms.guideSteps.step2Title') }}</div>
                <p class="lark-step-desc">{{ $t('settings.sms.guideSteps.step2Desc') }}</p>
              </div>
            </div>
          </div>

        </div>

        <!-- 番茄钟 -->
        <div v-if="activeTab === 'pomodoro'" class="tab-pane">
          <div class="group-label">{{ $t('pomodoro.settings.title') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <span class="row-title">{{ $t('pomodoro.settings.focusDuration') }}</span>
                <span class="row-desc">{{ $t('pomodoro.settings.focusDurationDesc') }}</span>
              </div>
              <input
                v-model.number="cfg.PomodoroFocusDuration"
                type="number"
                min="1"
                max="120"
                style="width:72px;text-align:center"
              />
            </div>

            <div class="settings-row">
              <div class="row-body">
                <span class="row-title">{{ $t('pomodoro.settings.shortBreakDuration') }}</span>
                <span class="row-desc">{{ $t('pomodoro.settings.shortBreakDurationDesc') }}</span>
              </div>
              <input
                v-model.number="cfg.PomodoroShortBreakDuration"
                type="number"
                min="1"
                max="30"
                style="width:72px;text-align:center"
              />
            </div>

            <div class="settings-row">
              <div class="row-body">
                <span class="row-title">{{ $t('pomodoro.settings.longBreakDuration') }}</span>
                <span class="row-desc">{{ $t('pomodoro.settings.longBreakDurationDesc') }}</span>
              </div>
              <input
                v-model.number="cfg.PomodoroLongBreakDuration"
                type="number"
                min="1"
                max="60"
                style="width:72px;text-align:center"
              />
            </div>

            <div class="settings-row">
              <div class="row-body">
                <span class="row-title">{{ $t('pomodoro.settings.roundsBeforeLongBreak') }}</span>
                <span class="row-desc">{{ $t('pomodoro.settings.roundsBeforeLongBreakDesc') }}</span>
              </div>
              <input
                v-model.number="cfg.PomodoroRoundsBeforeLongBreak"
                type="number"
                min="1"
                max="10"
                style="width:72px;text-align:center"
              />
            </div>
          </div>
          <p class="hint-text" style="margin-top: 12px; font-size: 11px; color: var(--text-secondary);">{{ $t('pomodoro.settings.hint') }}</p>
        </div>

        <!-- Claude Code -->
        <div v-if="activeTab === 'claudeCode'" class="tab-pane">
          <div class="group-label">{{ $t('claudeCode.title') }}</div>
          <div class="settings-group">
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('claudeCode.enabled') }}</div>
                <div class="row-desc">{{ $t('claudeCode.enabledDesc') }}</div>
              </div>
              <label class="toggle">
                <input type="checkbox" v-model="cfg.ClaudeCodeEnabled" @change="debouncedSaveFlush" />
                <span class="toggle-track" />
              </label>
            </div>
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('claudeCode.port') }}</div>
                <div class="row-desc">{{ $t('claudeCode.portDesc') }}</div>
              </div>
              <div class="row-ctrl">
                <input
                  type="number"
                  class="vrm-input"
                  style="width:80px"
                  v-model.number="cfg.ClaudeCodePort"
                  :disabled="!cfg.ClaudeCodeEnabled"
                  min="1024" max="65535"
                />
              </div>
            </div>
            <div class="settings-row">
              <div class="row-body">
                <div class="row-title">{{ $t('claudeCode.notificationSecs') }}</div>
                <div class="row-desc">{{ $t('claudeCode.notificationSecsDesc') }}</div>
              </div>
              <div class="row-ctrl">
                <input
                  type="number"
                  class="vrm-input"
                  style="width:80px"
                  v-model.number="cfg.ClaudeCodeNotificationSecs"
                  :disabled="!cfg.ClaudeCodeEnabled"
                  min="5" max="120"
                />
              </div>
            </div>
          </div>

          <!-- Hook 配置展示区 -->
          <div class="group-label">{{ $t('claudeCode.hookConfig') }}</div>
          <div class="settings-group">
            <div class="settings-row" style="flex-direction:column;align-items:stretch;gap:8px">
              <div class="row-desc">{{ $t('claudeCode.hookConfigHint') }}</div>
              <pre class="hook-config-snippet">{{ claudeCodeHookSnippet }}</pre>
              <button class="btn-on-sm" @click="copyClaudeCodeHook" style="align-self:flex-end">
                {{ claudeCodeCopyLabel }}
              </button>
            </div>
          </div>
        </div>

        <!-- 关于 -->
        <div v-if="activeTab === 'about'" class="tab-pane about-pane">
          <div class="section-header"><h3>{{ $t('settings.about.version') }}</h3></div>

          <div class="about-version-row">
            <span class="about-label">{{ $t('settings.about.currentVersion') }}</span>
            <span class="about-version">v{{ currentVersion }}</span>
          </div>

          <div class="about-update-area">
            <!-- Not yet checked -->
            <button v-if="!updateInfo && !updateChecking && !updateInstalling"
              class="fetch-btn" @click="checkUpdate">
              {{ $t('settings.about.checkUpdate') }}
            </button>

            <!-- Checking -->
            <span v-if="updateChecking" class="about-hint">{{ $t('settings.about.checking') }}</span>

            <!-- No update -->
            <div v-if="updateInfo && !updateInfo.has_update && !updateInstalling" class="about-hint">
              {{ $t('settings.about.upToDate') }}（v{{ updateInfo.latest_version }}）
            </div>

            <!-- Has update -->
            <div v-if="updateInfo && updateInfo.has_update && !updateInstalling" class="about-update-available">
              <span>{{ $t('settings.about.newVersion') }} <strong>v{{ updateInfo.latest_version }}</strong></span>
              <button
                class="about-changelog-link"
                @click="BrowserOpenURL(`https://github.com/tiancheng92/Aiko/releases/tag/v${updateInfo.latest_version}`)"
              >{{ $t('settings.about.releaseNotes') }}</button>
              <button class="fetch-btn fetch-btn--primary" @click="installUpdate"
                :disabled="!updateInfo.download_url">
                {{ updateInfo.download_url ? $t('settings.about.installNow') : $t('settings.about.noDownload') }}
              </button>
            </div>

            <!-- Installing -->
            <div v-if="updateInstalling" class="about-installing">
              <div class="about-progress-bar">
                <div class="about-progress-fill" :style="{ width: updateProgress + '%' }"></div>
              </div>
              <span class="about-hint">{{ updateProgressMsg || $t('settings.about.preparing') }}（{{ updateProgress }}%）</span>
            </div>

            <div v-if="updateError" class="lark-status lark-status--err" style="margin-top:8px; display:flex; align-items:center; gap:8px; flex-wrap:wrap">
              <span>{{ updateError }}</span>
              <button class="btn-retry" :disabled="updateChecking || updateInstalling" @click="updateInfo ? installUpdate() : checkUpdate()">{{ $t('settings.about.retry') }}</button>
            </div>
          </div>

          <div class="about-meta">
            <p>{{ $t('settings.about.poweredBy') }}</p>
          </div>
        </div>

      </div>
      </div><!-- /win-content-wrap -->
    </div><!-- /win-body -->

    <!-- Footer -->
    <div class="win-footer">
      <span class="status-msg">
        <template v-if="statusMsg">{{ statusMsg }}</template>
        <template v-else-if="saving">{{ $t('settings.saving') }}</template>
      </span>
      <button class="btn-done" @click="$emit('close')">{{ $t('settings.done') }}</button>
    </div>

    <!-- Resize handle (bottom-right corner) -->
    <div class="win-resize-handle" @mousedown="onResizeStart" :aria-label="$t('settings.resizeAriaLabel')">
      <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round">
        <path d="M14 4 4 14M14 9 9 14M14 14l-.01 0"/>
      </svg>
    </div>
    <!-- Confirm dialog — reuses modal-overlay/modal-box pattern so it looks
         identical to the edit/add form modals and stays within .settings-win
         (no Teleport → no click-through issues on macOS). -->
    <Transition :css="false" @enter="onModalEnter" @leave="onModalLeave">
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
    </Transition>
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

/* Traffic lights — live inside sidebar-drag */
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

/* Layout — win-body fills the entire window, padding gives sidebar room to float */
.win-body {
  flex: 1;
  display: flex;
  overflow: hidden;
  padding: 6px 0 6px 6px; /* sidebar floats with margin on three sides */
  gap: 0;
}

/* Sidebar — macOS 26 glass panel: floats as an inset card within the window.
   Concentric radius: win=14px → panel inset 6px → panel r=14–6=8px → use 10px for comfort */
.win-sidebar {
  width: 220px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 0;
  overflow: hidden;
  border-radius: 10px;
  background: rgba(28, 30, 45, 0.72);
  backdrop-filter: blur(40px) saturate(1.6) brightness(0.9);
  -webkit-backdrop-filter: blur(40px) saturate(1.6) brightness(0.9);
  border: 1px solid rgba(255, 255, 255, 0.06);
  box-shadow:
    0 2px 16px rgba(0, 0, 0, 0.22),
    inset 0 1px 0 rgba(255, 255, 255, 0.05);
}

/* Drag handle at the top of the sidebar (holds traffic lights).
   Extra top padding compensates for the panel's 6px float margin inside the window. */
.sidebar-drag {
  height: 46px;
  display: flex;
  align-items: center;
  padding: 0 14px;
  cursor: move;
  flex-shrink: 0;
  user-select: none;
}

/* Large "设置" heading below traffic lights — macOS System Settings style */
.sidebar-heading {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.03em;
  color: var(--text-primary);
  padding: 0 16px 12px;
  flex-shrink: 0;
  line-height: 1;
}

/* Search field — capsule pill, macOS System Settings style
   Concentric-radius rule: outer window r=14px → inner pill is fully rounded (r=100px)
   so both arcs are visible simultaneously and create depth hierarchy. */
.sidebar-search {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 0 10px 12px;
  padding: 0 11px;
  height: 28px;
  background: rgba(255, 255, 255, 0.07);
  border: 1px solid rgba(255, 255, 255, 0.09);
  border-radius: 100px;
  flex-shrink: 0;
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s;
}
.sidebar-search:focus-within {
  border-color: var(--accent);
  background: rgba(255, 255, 255, 0.10);
  box-shadow: 0 0 0 3px var(--accent-alpha-12);
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
  line-height: 28px;
  caret-color: var(--accent);
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

/* Nav list — scrollable region below the search, fills remaining sidebar height */
.sidebar-nav-list {
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: 0 8px 8px;
  flex: 1;
  overflow-y: auto;
}
.sidebar-nav-list::-webkit-scrollbar { width: 0; }

.nav-item {
  position: relative;
  -webkit-appearance: none;
  appearance: none;
  background: transparent;
  border: none;
  color: var(--text-primary);
  padding: 0 8px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 400;
  font-family: inherit;
  text-align: left;
  border-radius: 8px;
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  height: 38px;
  transition: background 0.12s;
  letter-spacing: -0.015em;
}
.nav-item:hover { background: rgba(255, 255, 255, 0.07); }
.nav-item.active {
  background: rgba(50, 130, 255, 0.15);
}
/* Left accent bar on active nav item */
.nav-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 18%;
  height: 64%;
  width: 3px;
  border-radius: 0 2px 2px 0;
  background: rgba(70, 150, 255, 0.9);
}
.nav-item.match { outline: 1px solid var(--accent-alpha-20); }
.nav-item:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }

/* Icon container — colored square like iOS app icons */
.nav-icon-wrap {
  width: 26px;
  height: 26px;
  border-radius: 6px;
  /* background set via inline :style from iconBg */
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: #fff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.35), inset 0 0.5px 0 rgba(255, 255, 255, 0.22);
  transition: opacity 0.12s, transform 0.12s;
}
.nav-item:hover .nav-icon-wrap { opacity: 0.9; }
.nav-item.active .nav-icon-wrap { opacity: 1; }
.nav-item:active .nav-icon-wrap { transform: scale(0.93); }
.nav-icon-wrap :deep(svg) { width: 14px; height: 14px; color: #fff; }
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

/* Content area wrapper — takes remaining horizontal space, provides stacked layout */
.win-content-wrap {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-width: 0;
}

/* Invisible drag strip above the content area.
   sidebar-drag=46px + win-body padding-top=6px = 52px total offset. */
.content-drag {
  height: 52px;
  flex-shrink: 0;
  cursor: move;
  user-select: none;
}

/* Content area — section-card pattern */
.win-content {
  flex: 1;
  overflow-y: auto;
  padding: 8px 32px 40px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.12) transparent;
}
.win-content::-webkit-scrollbar { width: 6px; }
.win-content::-webkit-scrollbar-track { background: transparent; }
.win-content::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.10);
  border-radius: 6px;
}
.win-content::-webkit-scrollbar-thumb:hover { background: rgba(255, 255, 255, 0.18); }

/* Tab pane — macOS "card of rows" pattern */
@keyframes tabPaneEnter {
  from { opacity: 0; transform: translateY(5px); }
  to   { opacity: 1; transform: translateY(0);   }
}
.tab-pane {
  display: flex; flex-direction: column; gap: 20px; max-width: 680px;
  /* expo.out — recommended by design system for app transitions */
  animation: tabPaneEnter 0.26s cubic-bezier(0.16, 1, 0.3, 1);
}
@media (prefers-reduced-motion: reduce) { .tab-pane { animation: none; } }
.tab-pane > label,
.tab-pane > .settings-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px 18px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  font-size: 13px;
  color: var(--text-secondary);
  font-weight: 400;
  letter-spacing: -0.01em;
  transition: border-color 0.18s, background 0.18s;
}
.tab-pane > .settings-section:hover,
.tab-pane > label:hover {
  background: rgba(255, 255, 255, 0.062);
  border-color: rgba(255, 255, 255, 0.11);
}
.tab-pane > label { font-size: 13px; font-weight: 500; color: var(--text-primary); }
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

/* ─── Button System ──────────────────────────────────────────────────────────
   7 semantic tiers, 2 sizes. All tiers share the same base reset; size
   variants (no suffix = modal/form size, -sm = inline list/card size).

   Tier  Color    Meaning
   ────  ───────  ────────────────────────────────────────────────────────────
   primary  blue solid   Save / Confirm (high-frequency happy-path action)
   add      blue outline Create / Import / Run (positive, reversible)
   edit     indigo outline  Edit / Configure (neutral-positive)
   on       green outline   Enabled / Active state toggle
   off      amber outline   Disabled state toggle (click to re-enable)
   neutral  grey outline    Cancel / Reset / Refresh (no side-effect)
   danger   red outline/fill Delete / Destructive (irreversible)
   ────────────────────────────────────────────────────────────────────────── */

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
  transition: background 0.12s, border-color 0.12s, color 0.12s, transform 0.08s;
  box-shadow: none;
  -webkit-appearance: none;
  appearance: none;
}
button:hover:not(:disabled) { background: var(--lg-surface-input-h); border-color: var(--border-strong); }
button:active:not(:disabled) { transform: scale(0.97); }
button:disabled { opacity: 0.4; cursor: not-allowed; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

/* PRIMARY — solid blue; save / confirm */
.btn-primary,
.btn-save,
.btn-add {
  background: var(--accent);
  color: #fff;
  border: 1px solid transparent;
  font-weight: 500;
  padding: 6px 14px;
}
.btn-primary:hover:not(:disabled),
.btn-save:hover:not(:disabled),
.btn-add:hover:not(:disabled) { background: var(--accent-hover); border-color: transparent; }

/* ADD — blue outline; create / import / run (smaller inline variant) */
.btn-add-sm {
  font-size: 11px; padding: 4px 10px;
  background: rgba(0, 122, 255, 0.12);
  color: var(--accent);
  border: 1px solid rgba(0, 122, 255, 0.28);
  font-weight: 500;
}
.btn-add-sm:hover:not(:disabled) { background: rgba(0, 122, 255, 0.22); border-color: rgba(0, 122, 255, 0.45); }

/* EDIT — indigo outline; edit / configure */
.btn-edit {
  background: rgba(94, 92, 230, 0.12);
  color: #a5a6f6;
  border: 1px solid rgba(94, 92, 230, 0.28);
  padding: 5px 12px;
  font-size: 12px;
}
.btn-edit:hover:not(:disabled) { background: rgba(94, 92, 230, 0.20); border-color: rgba(94, 92, 230, 0.45); }
.btn-edit-sm {
  font-size: 11px; padding: 4px 10px;
  background: rgba(94, 92, 230, 0.12);
  color: #a5a6f6;
  border: 1px solid rgba(94, 92, 230, 0.28);
  font-weight: 500;
}
.btn-edit-sm:hover:not(:disabled) { background: rgba(94, 92, 230, 0.20); border-color: rgba(94, 92, 230, 0.45); }

/* ON — green outline; enabled / active / activate */
.btn-on-sm {
  font-size: 11px; padding: 4px 10px;
  background: rgba(48, 209, 88, 0.12);
  color: var(--success);
  border: 1px solid rgba(48, 209, 88, 0.28);
  font-weight: 500;
}
.btn-on-sm:hover:not(:disabled) { background: rgba(48, 209, 88, 0.22); border-color: rgba(48, 209, 88, 0.45); }

/* OFF — amber outline; disabled state toggle (click to re-enable) */
.btn-off-sm {
  font-size: 11px; padding: 4px 10px;
  background: rgba(255, 159, 10, 0.10);
  color: #ff9f0a;
  border: 1px solid rgba(255, 159, 10, 0.26);
  font-weight: 500;
}
.btn-off-sm:hover:not(:disabled) { background: rgba(255, 159, 10, 0.20); border-color: rgba(255, 159, 10, 0.44); }

/* NEUTRAL — grey; cancel / reset / refresh / secondary actions */
.btn-neutral,
.btn-cancel,
.btn-done,
.btn-secondary,
.fetch-btn {
  background: var(--lg-surface-input);
  color: var(--text-primary);
  border: 1px solid var(--lg-border);
  padding: 5px 12px;
  font-size: 12px;
}
.btn-neutral:hover:not(:disabled),
.btn-cancel:hover:not(:disabled),
.btn-done:hover:not(:disabled),
.btn-secondary:hover:not(:disabled),
.fetch-btn:hover:not(:disabled) { background: var(--lg-surface-input-h); border-color: var(--border-strong); }
.btn-neutral-sm {
  font-size: 11px; padding: 4px 10px;
  background: var(--lg-surface-input);
  color: var(--text-secondary);
  border: 1px solid var(--lg-border);
  font-weight: 500;
}
.btn-neutral-sm:hover:not(:disabled) { background: var(--lg-surface-input-h); color: var(--text-primary); border-color: var(--border-strong); }

/* DANGER — red; delete / destructive */
.btn-danger-sm,
.btn-danger-small {
  font-size: 11px; padding: 4px 10px;
  background: var(--danger-bg);
  color: var(--danger);
  border: 1px solid rgba(255, 69, 58, 0.25);
  font-weight: 500;
}
.btn-danger-sm:hover:not(:disabled),
.btn-danger-small:hover:not(:disabled) { background: rgba(255, 69, 58, 0.22); border-color: rgba(255, 69, 58, 0.42); }

/* SETUP — same as primary but for setup/install actions */
.btn-setup {
  background: var(--accent);
  color: #fff;
  border: 1px solid transparent;
  font-weight: 500;
  padding: 6px 14px;
}
.btn-setup:hover:not(:disabled) { background: var(--accent-hover); }

/* fetch-btn primary variant */
.fetch-btn--primary { background: var(--accent); color: #fff; border-color: transparent; }
.fetch-btn--primary:hover:not(:disabled) { background: var(--accent-hover); }

/* Inline retry (error recovery) — neutral-sm with tighter padding */
.btn-retry {
  font-size: 11px; padding: 3px 10px;
  background: var(--lg-surface-input);
  color: var(--text-secondary);
  border: 1px solid var(--lg-border);
}
.btn-retry:hover:not(:disabled) { background: var(--lg-surface-input-h); color: var(--text-primary); }

/* URL row with fetch button */
.url-row { display: flex; gap: 8px; align-items: center; }
.url-row input { flex: 1; }

.select-row { display: flex; }
.select-row select, .select-row input { flex: 1; }

/* Embedding inherit toggle row */
.embed-inherit-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  margin: 6px 0 4px;
  background: var(--lg-surface-input, rgba(255,255,255,0.05));
  border: 1px solid var(--lg-border, rgba(255,255,255,0.08));
  border-radius: 8px;
}
.embed-inherit-label {
  font-size: 13px;
  color: var(--text-secondary);
  flex: 1;
  padding-right: 12px;
}

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

/* Inline section group label (used in general / appearance tabs) */
.settings-section-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  padding: 0 2px 6px;
  border-bottom: 1px solid var(--lg-border-subtle);
  margin-bottom: 2px;
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
  transition: border-color 0.18s, background 0.18s, transform 0.22s cubic-bezier(0.34, 1.2, 0.64, 1), box-shadow 0.22s;
}
.profile-card:hover {
  background: var(--surface-card-hover);
  border-color: var(--lg-border);
  transform: translateY(-1.5px);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
}
@media (prefers-reduced-motion: reduce) {
  .profile-card { transition: border-color 0.12s, background 0.12s; }
  .profile-card:hover { transform: none; box-shadow: none; }
}
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

/* Modal (profile form dialog) — animated via JS spring hooks in script */
.modal-overlay {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex; align-items: flex-start; justify-content: center;
  overflow-y: auto;
  padding: 40px 0;
}
.modal-box {
  background: var(--lg-surface-modal);
  backdrop-filter: var(--lg-blur-sm);
  -webkit-backdrop-filter: var(--lg-blur-sm);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-card);
  box-shadow: var(--lg-shadow);
  padding: 24px;
  width: 420px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
  will-change: transform, opacity;
}
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
.tool-api-key-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 12px 4px;
  background: var(--surface-input);
  border: 1px solid var(--lg-border-subtle);
  border-top: none;
  border-radius: 0 0 var(--r-input) var(--r-input);
  margin-top: -2px;
  margin-bottom: 0;
}
.tool-api-key-row label {
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
  min-width: 92px;
}
.tool-api-key-input {
  flex: 1;
  font-size: 12px;
  padding: 4px 8px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 6px;
  color: var(--text-primary);
  outline: none;
}
.tool-api-key-input:focus {
  border-color: var(--accent);
}
.tool-api-key-hint {
  font-size: 11px;
  color: var(--text-tertiary);
  padding: 3px 12px 8px;
  margin: 0;
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
  transition: background 0.18s, border-color 0.18s, transform 0.22s cubic-bezier(0.34, 1.2, 0.64, 1), box-shadow 0.22s;
}
.perm-row:hover {
  background: var(--surface-card-hover);
  border-color: var(--lg-border);
  transform: translateY(-1px);
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.13);
}
@media (prefers-reduced-motion: reduce) {
  .perm-row { transition: background 0.12s; }
  .perm-row:hover { transform: none; box-shadow: none; }
}
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
.toggle input {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
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
  transition: background 0.15s;
}
.tab-pane > ul li:hover { background: var(--surface-card-hover); }
.tab-pane > ul li:last-child { border-bottom: none; }
.tab-pane > ul li span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-right: 10px; font-variant-numeric: tabular-nums; }

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
  align-items: flex-start;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  margin-bottom: 6px;
  gap: 12px;
  transition: background 0.18s, border-color 0.18s, transform 0.22s cubic-bezier(0.34, 1.2, 0.64, 1), box-shadow 0.22s;
}
.mcp-row:hover {
  background: var(--surface-card-hover);
  border-color: var(--lg-border);
  transform: translateY(-1px);
  box-shadow: 0 3px 12px rgba(0, 0, 0, 0.16);
}
@media (prefers-reduced-motion: reduce) {
  .mcp-row { transition: background 0.12s; }
  .mcp-row:hover { transform: none; box-shadow: none; }
}
.mcp-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
  min-width: 0;
  text-align: left;
}
.mcp-name { font-weight: 600; font-size: 13px; color: var(--text-primary); }
.mcp-transport {
  align-self: flex-start;
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
.mcp-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
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
  transition: border-color 0.18s, background 0.18s, transform 0.22s cubic-bezier(0.34, 1.2, 0.64, 1), box-shadow 0.22s;
}
.cron-row:hover {
  background: var(--surface-card-hover);
  border-color: var(--lg-border);
  transform: translateY(-1px);
  box-shadow: 0 3px 12px rgba(0, 0, 0, 0.16);
}
@media (prefers-reduced-motion: reduce) {
  .cron-row { transition: border-color 0.12s, background 0.12s; }
  .cron-row:hover { transform: none; box-shadow: none; }
}
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
  display: flex;
  flex-wrap: wrap;
  gap: 0 14px;
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
.cron-flags {
  display: flex;
  gap: 6px;
  margin-top: 2px;
}
.cron-flag {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 10px;
  border-radius: 4px;
  padding: 2px 6px;
  font-weight: 500;
}
.cron-flag--on  { background: rgba(0, 122, 255, 0.12); color: var(--accent); }
.cron-flag--off { background: rgba(255, 255, 255, 0.05); color: var(--text-tertiary); }
.cron-toggle-row {
  display: flex;
  flex-direction: column;
  gap: 0;
  border-top: 1px solid var(--lg-border-subtle);
  margin-top: 4px;
}
/* Override .modal-box label (column) — this row needs horizontal layout */
.cron-toggle-label {
  display: flex !important;
  flex-direction: row !important;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;
  cursor: default;
  border-bottom: 1px solid var(--lg-border-subtle);
  font-size: 13px;
  font-weight: 400;
  color: var(--text-primary);
}
.cron-toggle-label:last-child { border-bottom: none; }
.cron-toggle-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.cron-toggle-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
  line-height: 1.3;
}
.cron-toggle-hint {
  font-size: 11px;
  color: var(--text-tertiary);
  line-height: 1.4;
}
.toggle-switch {
  position: relative;
  width: 36px;
  height: 20px;
  border-radius: 10px;
  border: none;
  background: rgba(255, 255, 255, 0.12);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.22s, box-shadow 0.22s;
  padding: 0;
}
.toggle-switch--on {
  background: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
.toggle-thumb {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 3px rgba(0,0,0,0.25);
  transition: transform 0.28s cubic-bezier(0.34, 1.56, 0.64, 1);
  display: block;
}
.toggle-switch--on .toggle-thumb { transform: translateX(16px); }
@media (prefers-reduced-motion: reduce) {
  .toggle-thumb { transition: transform 0.15s; }
}
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
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
}
.sms-status-group { display: flex; align-items: center; gap: 8px; }
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
  justify-content: space-between;
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
  align-items: flex-start;
  justify-content: space-between;
  padding: 12px 14px;
  background: var(--surface-card);
  border: 1px solid var(--lg-border-subtle);
  border-radius: var(--r-card);
  margin-bottom: 6px;
  gap: 10px;
  transition: background 0.18s, border-color 0.18s, transform 0.22s cubic-bezier(0.34, 1.2, 0.64, 1), box-shadow 0.22s;
}
.proactive-row:hover {
  background: var(--surface-card-hover);
  border-color: var(--lg-border);
  transform: translateY(-1px);
  box-shadow: 0 3px 12px rgba(0, 0, 0, 0.16);
}
@media (prefers-reduced-motion: reduce) {
  .proactive-row { transition: none; }
  .proactive-row:hover { transform: none; box-shadow: none; }
}
.proactive-info { display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 0; text-align: left; }
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

/* Sub-tab bar — segment control, active state on button itself */
.sub-tab-bar {
  display: flex;
  align-self: flex-start;
  gap: 2px;
  margin-bottom: 18px;
  padding: 3px;
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 8px;
}

.sub-tab-bar button {
  flex: 1;
  padding: 6px 16px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.18s, color 0.18s, box-shadow 0.18s;
  -webkit-appearance: none;
  appearance: none;
}
.sub-tab-bar button:hover { color: var(--text-primary); }
.sub-tab-bar button.active {
  color: var(--text-primary);
  font-weight: 600;
  background: var(--lg-surface-elevated);
  box-shadow:
    0 1px 3px rgba(0, 0, 0, 0.2),
    0 0 0 0.5px rgba(255, 255, 255, 0.08);
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
  transition: border-color 0.18s, transform 0.22s cubic-bezier(0.34, 1.2, 0.64, 1), box-shadow 0.22s;
}
.about-version-row:hover {
  border-color: var(--lg-border);
  transform: translateY(-1px);
  box-shadow: 0 3px 12px rgba(0, 0, 0, 0.16);
}
@media (prefers-reduced-motion: reduce) {
  .about-version-row { transition: border-color 0.15s; }
  .about-version-row:hover { transform: none; box-shadow: none; }
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
  background: transparent;
  padding: 4px 10px;
  border-radius: var(--r-button);
  border: 1px solid var(--accent-alpha-20);
  cursor: pointer;
  transition: background 0.12s;
  white-space: nowrap;
  font-family: inherit;
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

/* ── macOS 26 grouped card pattern ─────────────────────── */

/* Label above a card group — uppercase, small gray text.
 * -12px bottom margin offsets tab-pane's 20px flex gap → net ~8px visual space */
.group-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.07em;
  padding: 0 4px;
  margin-bottom: -12px;
}

/* Avatar settings */
.avatar-settings-row {
  display: flex;
  gap: 24px;
  padding: 16px 0 4px;
}
.avatar-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}
.avatar-preview-wrap {
  position: relative;
  width: 72px;
  height: 72px;
  border-radius: 50%;
  cursor: pointer;
  overflow: hidden;
  border: 2px solid var(--lg-border-subtle);
  transition: border-color 0.15s;
}
.avatar-preview-wrap:hover { border-color: var(--accent); }
.avatar-preview {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.avatar-default-user {
  background: rgba(255, 255, 255, 0.10);
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(255, 255, 255, 0.5);
}
.avatar-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.avatar-preview-wrap:hover .avatar-overlay { opacity: 1; }
.avatar-label {
  font-size: 12px;
  color: var(--text-secondary);
}
.avatar-reset-btn {
  font-size: 11px;
  color: var(--text-tertiary);
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: 4px;
  transition: color 0.12s, background 0.12s;
}
.avatar-reset-btn:hover {
  color: var(--danger);
  background: rgba(255, 69, 58, 0.10);
}

/* Card that groups multiple rows together */
.settings-group {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  overflow: hidden;
}
.settings-group:hover {
  background: rgba(255, 255, 255, 0.062);
  border-color: rgba(255, 255, 255, 0.11);
}

/* Horizontal row inside a card */
.settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 18px;
  min-height: 50px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}
.settings-row:last-child { border-bottom: none; }

/* Left side — stacked label + description */
.row-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.row-title {
  font-size: 13px;
  font-weight: 400;
  color: var(--text-primary);
  line-height: 1.3;
}
.row-desc {
  font-size: 11.5px;
  color: var(--text-tertiary);
  line-height: 1.45;
}
/* Right side control */
.row-ctrl {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

/* Full-width field row (label on top, input below) inside a settings-group */
.settings-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px 18px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}
.settings-field:last-child { border-bottom: none; }
.field-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}
.field-hint-inline {
  font-size: 11px;
  color: var(--text-tertiary);
  font-weight: 400;
  margin-left: 6px;
}

/* ── Keyboard shortcuts reference ──────────────────────── */
.shortcut-list {
  display: flex;
  flex-direction: column;
  gap: 0;
  margin-top: 0;
}
.shortcut-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 18px;
  border-radius: 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  transition: background 0.12s;
}
.shortcut-row:last-child { border-bottom: none; }
.shortcut-row:hover {
  background: rgba(255, 255, 255, 0.04);
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

.hook-config-snippet {
  background: var(--bg-tertiary, rgba(255, 255, 255, 0.04));
  border: 1px solid var(--lg-border);
  border-radius: 8px;
  padding: 12px;
  font-size: 11px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
  color: var(--text-secondary);
}
</style>

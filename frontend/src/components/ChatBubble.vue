<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { defineAsyncComponent } from 'vue'
const ChatPanel = defineAsyncComponent(() => import('./ChatPanel.vue'))
import ContextMenu from './ContextMenu.vue'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { debounce } from '../utils/timing.js'
import { ExportChatHistory, GetChatSize, PingLLM, SaveChatSize, GetConfig, ListModelProfiles } from '../../wailsjs/go/main/App'
import { ICON_EXPORT, ICON_TRASH, ICON_SETTING } from '../utils/icons'
import { springAnimate } from '../composables/useSpring.js'

const props = defineProps({
  ballPos:  { type: Object, default: () => ({ x: -1, y: -1 }) },
  ballSize: { type: Number, default: 64 },
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
  visible:  { type: Boolean, default: false },
})
const emit = defineEmits(['close', 'open-settings'])

const latencyMs = ref(null)        // null = not yet measured, -1 = error, ≥0 = ms
const activeProfileName = ref('') // name of the currently active model profile
const activeModel = ref('')        // model id of the currently active profile

/** latencyColor returns the dot/text color for the current latency value. */
function latencyColor(ms) {
  if (ms === null || ms < 0) return 'rgba(255,255,255,0.25)'
  if (ms < 300) return '#4ade80'
  if (ms <= 800) return '#facc15'
  return '#f87171'
}

/** latencyLabel returns the display text for the current latency value. */
function latencyLabel(ms) {
  if (ms === null || ms < 0) return '—'
  return ms + 'ms'
}

const DEFAULT_W = Math.min(520, Math.max(340, Math.round(window.innerWidth  * 0.24)))
const DEFAULT_H = Math.min(680, Math.max(400, Math.round(window.innerHeight * 0.58)))

const bubbleW = ref(DEFAULT_W)
const bubbleH = ref(DEFAULT_H)

/** applySize updates bubble dimensions; 0 means revert to default. */
function applySize({ width, height }) {
  bubbleW.value = width  >= 300 ? width  : DEFAULT_W
  bubbleH.value = height >= 320 ? height : DEFAULT_H
  // Persist whenever size changes after mount.
  if (mounted) {
    const sw = props.activeScreen.width
    const sh = props.activeScreen.height
    if (sw > 0 && sh > 0) {
      SaveChatSize(bubbleW.value, bubbleH.value, sw, sh).catch(e =>
        console.warn('SaveChatSize failed', e)
      )
    }
  }
}

let offSizeChange = null
let offScreenChanged = null
let screenChangedHandler = null
let latencyTimer = null
let offModelChangedLatency = null
let offVisibilityLatency = null

// ─── Idle auto-close ──────────────────────────────────────────────────────────

const IDLE_MS = 3 * 60_000
const isLLMActive = ref(false)
let idleTimer = null
let offToken = null
let offDone = null
let offChatError = null

/** resetIdleTimer restarts the 60s countdown; skips if LLM is actively streaming. */
function resetIdleTimer() {
  clearTimeout(idleTimer)
  if (!isLLMActive.value) {
    idleTimer = setTimeout(() => emit('close'), IDLE_MS)
  }
}

/** onUserActivity resets the idle timer on any user interaction inside the bubble. */
function onUserActivity() {
  resetIdleTimer()
}

let mounted = false

onMounted(async () => {
  try {
    const [w, h] = await GetChatSize(props.activeScreen.width, props.activeScreen.height)
    applySize({ width: w, height: h })
  } catch (e) {
    console.error('load chat size failed:', e)
  }
  offSizeChange = EventsOn('config:chat:size:changed', applySize)
  /** refreshProfileInfo fetches the active profile name and model id for the title bar tags. */
  async function refreshProfileInfo() {
    try {
      const [cfg, profiles] = await Promise.all([GetConfig(), ListModelProfiles()])
      const active = profiles?.find(p => p.id === cfg.ActiveProfileID)
      if (active) {
        activeProfileName.value = active.name
        activeModel.value = active.model
      }
    } catch { /* non-critical */ }
  }

  const pingOnce = () => PingLLM().then(ms => { latencyMs.value = ms }).catch(() => { latencyMs.value = -1 })

  function startLatencyTimer() {
    if (latencyTimer) return
    pingOnce()
    latencyTimer = setInterval(pingOnce, 5000)
  }
  function stopLatencyTimer() {
    if (latencyTimer) { clearInterval(latencyTimer); latencyTimer = null }
  }

  if (props.visible) startLatencyTimer()
  offVisibilityLatency = watch(() => props.visible, (v) => { v ? startLatencyTimer() : stopLatencyTimer() })

  refreshProfileInfo()
  offModelChangedLatency = EventsOn('config:model:changed', () => { pingOnce(); refreshProfileInfo() })
  screenChangedHandler = debounce(async (info) => {
    try {
      const [w, h] = await GetChatSize(info.width, info.height)
      applySize({ width: w, height: h })
    } catch (e) {
      console.warn('screen:active:changed: GetChatSize failed', e)
    }
  }, 200)
  offScreenChanged = EventsOn('screen:active:changed', screenChangedHandler)

  offToken = EventsOn('chat:token', () => {
    isLLMActive.value = true
    clearTimeout(idleTimer)
  })
  offDone = EventsOn('chat:done', () => {
    isLLMActive.value = false
    resetIdleTimer()
  })
  offChatError = EventsOn('chat:error', () => {
    isLLMActive.value = false
    resetIdleTimer()
  })

  mounted = true
})

onUnmounted(() => {
  offSizeChange?.()
  offScreenChanged?.()
  screenChangedHandler?.cancel?.()
  offToken?.()
  offDone?.()
  offChatError?.()
  if (latencyTimer) { clearInterval(latencyTimer); latencyTimer = null }
  offVisibilityLatency?.()
  clearTimeout(idleTimer)
  offModelChangedLatency?.()
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeUp)
  window.removeEventListener('blur', onResizeUp)
  cancelFsAnim?.()
})

watch(() => props.visible, (v) => {
  if (v) {
    resetIdleTimer()
  } else {
    clearTimeout(idleTimer)
  }
}, { immediate: true })

const isFullscreen = ref(false)

// ─── Fullscreen spring animation ──────────────────────────────────────────────
// When non-null, bubbleStyle uses these values instead of the reactive pos/size.
const animLeft   = ref(null)
const animTop    = ref(null)
const animWidth  = ref(null)
const animHeight = ref(null)
const animRadius = ref(null)
let cancelFsAnim = null

/**
 * bubbleStyle computes the :style binding for the chat bubble.
 * During a fullscreen toggle animation the spring-driven anim* refs take
 * priority; otherwise the normal reactive pos/bubbleW/bubbleH are used.
 */
const bubbleStyle = computed(() => {
  if (animLeft.value !== null) {
    return {
      left:         animLeft.value   + 'px',
      top:          animTop.value    + 'px',
      width:        animWidth.value  + 'px',
      height:       animHeight.value + 'px',
      borderRadius: animRadius.value + 'px',
    }
  }
  if (isFullscreen.value) {
    return {
      left:         '0px',
      top:          '38px',
      width:        '100vw',
      height:       'calc(100vh - 38px)',
      borderRadius: '0px',
    }
  }
  return {
    left:   pos.value.x    + 'px',
    top:    pos.value.y    + 'px',
    width:  bubbleW.value  + 'px',
    height: bubbleH.value  + 'px',
  }
})

/** toggleFullscreen spring-animates all geometry between normal and fullscreen. */
function toggleFullscreen() {
  cancelFsAnim?.()

  const MENU_BAR  = 38
  const toFs      = !isFullscreen.value
  const fromX     = isFullscreen.value ? 0                          : pos.value.x
  const fromY     = isFullscreen.value ? MENU_BAR                   : pos.value.y
  const fromW     = isFullscreen.value ? window.innerWidth          : bubbleW.value
  const fromH     = isFullscreen.value ? window.innerHeight - MENU_BAR : bubbleH.value
  const fromR     = isFullscreen.value ? 0                          : 14
  const toX       = toFs ? 0                          : pos.value.x
  const toY       = toFs ? MENU_BAR                   : pos.value.y
  const toW       = toFs ? window.innerWidth          : bubbleW.value
  const toH       = toFs ? window.innerHeight - MENU_BAR : bubbleH.value
  const toR       = toFs ? 0                          : 14

  animLeft.value   = fromX
  animTop.value    = fromY
  animWidth.value  = fromW
  animHeight.value = fromH
  animRadius.value = fromR
  isFullscreen.value = toFs       // flip immediately for z-index / class

  cancelFsAnim = springAnimate({
    from: 0, to: 1,
    stiffness: 280, damping: 30,
    restDelta: 0.004, restVelocity: 0.02,
    onUpdate: (p) => {
      animLeft.value   = fromX + (toX - fromX) * p
      animTop.value    = fromY + (toY - fromY) * p
      animWidth.value  = fromW + (toW - fromW) * p
      animHeight.value = fromH + (toH - fromH) * p
      animRadius.value = fromR + (toR - fromR) * p
    },
    onDone: () => {
      animLeft.value   = null
      animTop.value    = null
      animWidth.value  = null
      animHeight.value = null
      animRadius.value = null
      cancelFsAnim     = null
    },
  })
}

const MIN_W = 300
const MIN_H = 320

const isResizing = ref(false)
let resizeDrag = null

/** startResize begins a drag-resize operation. */
function startResize(e, edge) {
  if (isFullscreen.value) return
  isResizing.value = true
  document.body.classList.add('no-select')
  resizeDrag = {
    edge,
    startX: e.clientX,
    startY: e.clientY,
    startW: bubbleW.value,
    startH: bubbleH.value,
  }
  window.addEventListener('mousemove', onResizeMove)
  window.addEventListener('mouseup', onResizeUp)
  window.addEventListener('blur', onResizeUp)
}

/** onResizeMove updates bubble dimensions during drag. */
function onResizeMove(e) {
  if (!resizeDrag) return
  const dx = e.clientX - resizeDrag.startX
  const dy = e.clientY - resizeDrag.startY
  const { edge } = resizeDrag
  if (edge === 'e' || edge === 'se') {
    bubbleW.value = Math.max(MIN_W, resizeDrag.startW + dx)
  }
  if (edge === 'w') {
    bubbleW.value = Math.max(MIN_W, resizeDrag.startW - dx)
  }
  if (edge === 's' || edge === 'se') {
    bubbleH.value = Math.max(MIN_H, resizeDrag.startH + dy)
  }
}

/** onResizeUp finalizes resize and persists via SaveChatSize. */
function onResizeUp() {
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', onResizeUp)
  window.removeEventListener('blur', onResizeUp)
  isResizing.value = false
  document.body.classList.remove('no-select')
  if (!resizeDrag) return
  resizeDrag = null
  const sw = props.activeScreen.width
  const sh = props.activeScreen.height
  if (sw > 0 && sh > 0) {
    SaveChatSize(bubbleW.value, bubbleH.value, sw, sh).catch(e =>
      console.warn('SaveChatSize on resize failed', e)
    )
  }
}


const pos = computed(() => {
  const { x, y } = props.ballPos
  const vw = window.innerWidth
  const vh = window.innerHeight
  const MARGIN = 8
  const MENU_BAR = 38  // macOS menu bar height

  let bx, by
  if (x < 0 || y < 0) {
    bx = vw - bubbleW.value - 24
    by = vh - bubbleH.value - 100
  } else {
    bx = x + props.ballSize - bubbleW.value
    by = y - bubbleH.value - 8
  }

  // Clamp to visible viewport — keep clear of menu bar at top.
  bx = Math.max(MARGIN, Math.min(bx, vw - bubbleW.value - MARGIN))
  by = Math.max(MENU_BAR + MARGIN, Math.min(by, vh - bubbleH.value - MARGIN))

  return { x: bx, y: by }
})

// ─── Context menu ────────────────────────────────────────────────────────────

const chatMenuRef = ref(null)
const chatPanelRef = ref(null)

const chatMenuItems = computed(() => [
  { iconSvg: ICON_EXPORT,  label: '导出聊天记录', action: exportHistory },
  { iconSvg: ICON_TRASH,   label: '清空聊天历史', action: clearHistory, danger: true },
  { divider: true },
  { iconSvg: ICON_SETTING, label: '打开设置',     action: () => emit('open-settings') },
])

/** clearHistory broadcasts a clear event to ChatPanel. */
function clearHistory() {
  EventsEmit('chat:clear')
}

/** exportHistory opens a native save dialog and writes chat history to a file. */
async function exportHistory() {
  try {
    await ExportChatHistory()
  } catch (e) {
    console.error('export chat history failed:', e)
  }
}

/** onBubbleContextMenu shows the chat bubble right-click menu. */
function onBubbleContextMenu(e) {
  e.preventDefault()
  chatMenuRef.value?.show(e.clientX, e.clientY)
}

/** focusInput delegates to the ChatPanel textarea focus. */
function focusInput() {
  chatPanelRef.value?.focusInput()
}

/** scrollToBottom delegates to the ChatPanel scroll-to-bottom. */
function scrollToBottom() {
  chatPanelRef.value?.scrollToBottom()
}

defineExpose({ focusInput, scrollToBottom })
</script>

<template>
  <div
    class="chat-bubble"
    :class="{ fullscreen: isFullscreen, 'no-transition': isResizing || animLeft !== null }"
    :style="bubbleStyle"
    @contextmenu="onBubbleContextMenu"
    @keydown="onUserActivity"
    @mousedown="onUserActivity"
    @mouseenter="clearTimeout(idleTimer)"
    @mouseleave="resetIdleTimer()"
  >
    <div class="title-bar">
      <svg class="title-logo" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
        <!-- large bubble (top-left) -->
        <rect x="1.5" y="2" width="14" height="10" rx="3.5" fill="currentColor"/>
        <path d="M5 12 L4 15.5 L8.5 12.5" fill="currentColor"/>
        <!-- small bubble (bottom-right), cut out from large with white backing -->
        <rect x="9" y="10.5" width="13" height="9" rx="3" fill="var(--lg-surface)"/>
        <rect x="9.5" y="11" width="12" height="8" rx="2.8" fill="currentColor" opacity="0.72"/>
        <path d="M19.5 19 L20.5 22 L16.5 19.5" fill="currentColor" opacity="0.72"/>
      </svg>
      <span class="title">聊天</span>
      <div class="title-spacer"></div>
      <div class="title-tags">
        <div class="title-tag latency-tag" :style="{ color: latencyColor(latencyMs) }" aria-label="LLM 延迟">
          <span class="latency-dot">●</span>
          <span class="latency-value">{{ latencyLabel(latencyMs) }}</span>
        </div>
        <div v-if="activeModel" class="title-tag model-tag" :title="activeModel">{{ activeModel }}</div>
        <div v-if="activeProfileName" class="title-tag profile-tag">{{ activeProfileName }}</div>
      </div>
      <button class="icon-btn" :title="isFullscreen ? '退出全屏' : '全屏'" :aria-label="isFullscreen ? '退出全屏' : '全屏'" @click="toggleFullscreen">
        <svg v-if="!isFullscreen" xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>
        <svg v-else xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="10" y1="14" x2="3" y2="21"/><line x1="21" y1="3" x2="14" y2="10"/></svg>
      </button>
      <button class="close-btn" aria-label="关闭" @click="$emit('close')">
        <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </button>
    </div>
    <div class="content">
      <Suspense>
        <ChatPanel ref="chatPanelRef" />
        <template #fallback></template>
      </Suspense>
    </div>
    <ContextMenu ref="chatMenuRef" :items="chatMenuItems" />
    <!-- Resize handles — invisible drag strips, hidden in fullscreen -->
    <template v-if="!isFullscreen">
      <div class="resize-handle resize-e"  @mousedown.stop="startResize($event, 'e')" />
      <div class="resize-handle resize-s"  @mousedown.stop="startResize($event, 's')" />
      <div class="resize-handle resize-w"  @mousedown.stop="startResize($event, 'w')" />
      <div class="resize-handle resize-se" @mousedown.stop="startResize($event, 'se')" />
    </template>
  </div>
</template>

<style scoped>
.chat-bubble {
  position: fixed;
  background: var(--lg-surface);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 14px;
  box-shadow: var(--lg-shadow);
  display: flex;
  flex-direction: column;
  z-index: 2000;
  overflow: hidden;
  will-change: left, top, width, height;
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', sans-serif;
  -webkit-font-smoothing: antialiased;
  user-select: none;
  -webkit-user-select: none;
  transition:
    left          0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    top           0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    width         0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    height        0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    border-radius 0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94);
}

.chat-bubble.no-transition {
  transition: none !important;
}

/* Title bar */
.title-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  height: 42px;
  flex-shrink: 0;
  user-select: none;
  border-bottom: 1px solid var(--lg-border-subtle);
  background: linear-gradient(to bottom, rgba(255, 255, 255, 0.05), rgba(255, 255, 255, 0));
}
.title-logo {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  margin-left: 4px;
  color: var(--text-primary);
}
.title {
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.01em;
  margin-left: 2px;
}
.title-spacer { flex: 1; }

.title-tags {
  display: flex;
  align-items: center;
  gap: 4px;
}
.title-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 20px;
  padding: 0 7px;
  border-radius: 5px;
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border-subtle);
  font-size: 11px;
  font-weight: 500;
  white-space: nowrap;
  user-select: none;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.latency-tag {
  font-variant-numeric: tabular-nums;
  transition: color 0.4s;
  max-width: none;
}
.model-tag   { color: var(--text-secondary); max-width: 200px; }
.profile-tag { color: var(--text-tertiary); }
.latency-dot {
  font-size: 7px;
  line-height: 1;
  flex-shrink: 0;
}
.latency-value {
  line-height: 1;
  min-width: 30px;
  text-align: right;
}

.icon-btn,
.close-btn {
  background: transparent;
  border: none;
  color: var(--text-tertiary);
  width: 26px;
  height: 26px;
  padding: 0;
  cursor: pointer;
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  transition: background 0.12s, color 0.12s;
  -webkit-appearance: none;
  appearance: none;
}
.icon-btn:hover {
  background: var(--lg-surface-input);
  color: var(--text-primary);
}
.close-btn:hover {
  background: rgba(255, 69, 58, 0.16);
  color: var(--danger);
}
.icon-btn:focus-visible,
.close-btn:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

/* Fullscreen mode — position/size/border-radius are all driven by :style (spring anim) */
.chat-bubble.fullscreen {
  z-index: 2001;
}

.content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.resize-handle {
  position: absolute;
  z-index: 10;
  user-select: none;
  transition: opacity 0.2s ease;
}
.resize-e  { right: 0;  top: 6px; bottom: 6px; width: 6px; cursor: ew-resize; }
.resize-w  { left: 0;   top: 6px; bottom: 6px; width: 6px; cursor: ew-resize; }
.resize-s  { bottom: 0; left: 6px; right: 6px; height: 6px; cursor: ns-resize; }
.resize-se { right: 0;  bottom: 0; width: 14px; height: 14px; cursor: nwse-resize; }
/* Hide resize handles gracefully when in fullscreen */
.chat-bubble.fullscreen .resize-handle { opacity: 0; pointer-events: none; }
</style>

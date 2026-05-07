<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import ChatPanel from './ChatPanel.vue'
import ContextMenu from './ContextMenu.vue'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { ExportChatHistory, GetChatSize, PingLLM, SaveChatSize } from '../../wailsjs/go/main/App'
import { ICON_EXPORT, ICON_TRASH, ICON_SETTING } from '../utils/icons'

const props = defineProps({
  ballPos:  { type: Object, default: () => ({ x: -1, y: -1 }) },
  ballSize: { type: Number, default: 64 },
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
})
const emit = defineEmits(['close', 'open-settings'])

const latencyMs = ref(null)  // null = not yet measured, -1 = error, ≥0 = ms

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
let latencyTimer = null
let offModelChangedLatency = null

let mounted = false

onMounted(async () => {
  try {
    const [w, h] = await GetChatSize(props.activeScreen.width, props.activeScreen.height)
    applySize({ width: w, height: h })
  } catch (e) {
    console.error('load chat size failed:', e)
  }
  offSizeChange = EventsOn('config:chat:size:changed', applySize)
  const pingOnce = () => PingLLM().then(ms => { latencyMs.value = ms }).catch(() => { latencyMs.value = -1 })
  pingOnce()
  latencyTimer = setInterval(pingOnce, 5000)
  offModelChangedLatency = EventsOn('config:model:changed', pingOnce)
  offScreenChanged = EventsOn('screen:active:changed', async (info) => {
    try {
      const [w, h] = await GetChatSize(info.width, info.height)
      applySize({ width: w, height: h })
    } catch (e) {
      console.warn('screen:active:changed: GetChatSize failed', e)
    }
  })
  mounted = true
})

onUnmounted(() => {
  offSizeChange?.()
  offScreenChanged?.()
  clearInterval(latencyTimer)
  offModelChangedLatency?.()
})

const isFullscreen = ref(false)

/** toggleFullscreen switches between normal and fullscreen chat mode. */
function toggleFullscreen() {
  isFullscreen.value = !isFullscreen.value
}


const pos = computed(() => {
  const { x, y } = props.ballPos
  if (x < 0 || y < 0) {
    return { x: window.innerWidth - bubbleW.value - 24, y: window.innerHeight - bubbleH.value - 100 }
  }
  return {
    x: x + props.ballSize - bubbleW.value,
    y: y - bubbleH.value - 8,
  }
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
    :class="{ fullscreen: isFullscreen }"
    :style="isFullscreen ? {} : {
      left:   pos.x + 'px',
      top:    pos.y + 'px',
      width:  bubbleW + 'px',
      height: bubbleH + 'px',
    }"
    @contextmenu="onBubbleContextMenu"
  >
    <div class="title-bar">
      <span class="title">聊天</span>
      <div
        class="latency-badge"
        :style="{ color: latencyColor(latencyMs) }"
        title="LLM 调用延迟"
        aria-label="LLM 调用延迟"
      >
        <span class="latency-dot">●</span>
        <span class="latency-value">{{ latencyLabel(latencyMs) }}</span>
      </div>
      <div class="title-spacer"></div>
      <button class="icon-btn" :title="isFullscreen ? '退出全屏' : '全屏'" :aria-label="isFullscreen ? '退出全屏' : '全屏'" @click="toggleFullscreen">
        <svg v-if="!isFullscreen" xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>
        <svg v-else xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="10" y1="14" x2="3" y2="21"/><line x1="21" y1="3" x2="14" y2="10"/></svg>
      </button>
      <button class="close-btn" aria-label="关闭" @click="$emit('close')">
        <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </button>
    </div>
    <div class="content">
      <ChatPanel ref="chatPanelRef" />
    </div>
    <ContextMenu ref="chatMenuRef" :items="chatMenuItems" />
  </div>
</template>

<style scoped>
.chat-bubble {
  --surface: rgba(28, 28, 32, 0.78);
  --text-primary: rgba(255, 255, 255, 0.94);
  --text-tertiary: rgba(255, 255, 255, 0.44);
  --border-subtle: rgba(255, 255, 255, 0.08);

  position: fixed;
  background: var(--surface);
  backdrop-filter: blur(40px) saturate(180%);
  -webkit-backdrop-filter: blur(40px) saturate(180%);
  border: 1px solid var(--border-subtle);
  border-radius: 14px;
  box-shadow:
    0 24px 64px rgba(0, 0, 0, 0.55),
    0 0 0 0.5px rgba(0, 0, 0, 0.3),
    0 1px 0 rgba(255, 255, 255, 0.07) inset;
  display: flex;
  flex-direction: column;
  z-index: 9998;
  overflow: hidden;
  will-change: left, top, width, height;
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', sans-serif;
  -webkit-font-smoothing: antialiased;
  transition:
    left          0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    top           0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    width         0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    height        0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94),
    border-radius 0.42s cubic-bezier(0.25, 0.46, 0.45, 0.94);
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
  border-bottom: 1px solid var(--border-subtle);
  background: linear-gradient(to bottom, rgba(255, 255, 255, 0.03), rgba(255, 255, 255, 0));
}
.title {
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.01em;
  margin-left: 4px;
}
.title-spacer { flex: 1; }

.latency-badge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border-radius: 5px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border-subtle);
  font-size: 11px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  transition: color 0.4s, background 0.15s;
  white-space: nowrap;
  user-select: none;
}
.latency-dot {
  font-size: 7px;
  line-height: 1;
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
  background: rgba(255, 255, 255, 0.08);
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

/* Fullscreen mode — flush under macOS menu bar (38px) */
.chat-bubble.fullscreen {
  position: fixed;
  left: 0 !important;
  top: 38px !important;
  width: 100vw !important;
  height: calc(100vh - 38px) !important;
  border-radius: 0;
  z-index: 9999;
}

.content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
</style>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import ChatPanel from './ChatPanel.vue'
import ContextMenu from './ContextMenu.vue'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { ExportChatHistory, GetConfig } from '../../wailsjs/go/main/App'

const props = defineProps({
  ballPos:  { type: Object, default: () => ({ x: -1, y: -1 }) },
  ballSize: { type: Number, default: 64 },
})
const emit = defineEmits(['close', 'open-settings'])

const DEFAULT_W = Math.min(520, Math.max(340, Math.round(window.innerWidth  * 0.24)))
const DEFAULT_H = Math.min(680, Math.max(400, Math.round(window.innerHeight * 0.58)))

const bubbleW = ref(DEFAULT_W)
const bubbleH = ref(DEFAULT_H)

/** applySize updates bubble dimensions; 0 means revert to default. */
function applySize({ width, height }) {
  bubbleW.value = width  >= 300 ? width  : DEFAULT_W
  bubbleH.value = height >= 320 ? height : DEFAULT_H
}

let offSizeChange = null

onMounted(async () => {
  try {
    const cfg = await GetConfig()
    applySize({ width: cfg.ChatWidth, height: cfg.ChatHeight })
  } catch (e) {
    console.error('load chat size failed:', e)
  }
  offSizeChange = EventsOn('config:chat:size:changed', applySize)
})

onUnmounted(() => {
  offSizeChange?.()
})

/** pos aligns the bubble's right edge to the ball's right edge, sitting above the ball. */
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
const chatMenuItems = computed(() => [
  { icon: '💾', label: '导出聊天记录', action: exportHistory },
  { icon: '🗑️', label: '清空聊天历史', action: clearHistory },
  { divider: true },
  { icon: '⚙️', label: '打开设置',      action: () => emit('open-settings') },
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
</script>

<template>
  <div
    class="chat-bubble"
    :style="{
      left:   pos.x + 'px',
      top:    pos.y + 'px',
      width:  bubbleW + 'px',
      height: bubbleH + 'px',
    }"
    @contextmenu="onBubbleContextMenu"
  >
    <div class="title-bar">
      <span class="title">聊天</span>
      <button class="close-btn" @click="$emit('close')">✕</button>
    </div>
    <div class="content">
      <ChatPanel />
    </div>
    <ContextMenu ref="chatMenuRef" :items="chatMenuItems" />
  </div>
</template>

<style scoped>
.chat-bubble {
  position: fixed;
  min-width: 300px;
  max-width: 800px;
  min-height: 320px;
  max-height: 900px;
  background: rgba(12, 15, 26, 0.55);
  backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  -webkit-backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 20px;
  box-shadow:
    0 12px 40px rgba(0, 0, 0, 0.5),
    0 1px 0 rgba(255, 255, 255, 0.08) inset,
    0 0 0 0.5px rgba(255,255,255,0.04) inset;
  display: flex;
  flex-direction: column;
  z-index: 9998;
  overflow: hidden;
}
.title-bar {
  display: flex;
  align-items: center;
  padding: 0 14px;
  height: 44px;
  flex-shrink: 0;
  user-select: none;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  background: rgba(255, 255, 255, 0.03);
}
.title {
  flex: 1;
  color: rgba(255, 255, 255, 0.85);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.close-btn {
  background: none;
  border: none;
  color: rgba(255, 255, 255, 0.3);
  padding: 8px;
  cursor: pointer;
  font-size: 13px;
  border-radius: 6px;
  transition: background 0.15s, color 0.15s;
  line-height: 1;
}
.close-btn:hover {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
}
.content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
</style>

<script setup>
import { ref, computed } from 'vue'
import ChatPanel from './ChatPanel.vue'
import ContextMenu from './ContextMenu.vue'
import { EventsEmit } from '../../wailsjs/runtime/runtime'
import { ExportChatHistory } from '../../wailsjs/go/main/App'

const props = defineProps({
  ballPos:  { type: Object, default: () => ({ x: -1, y: -1 }) },
  ballSize: { type: Number, default: 64 },
})
const emit = defineEmits(['close', 'open-settings'])

// Mirror CSS clamp so the JS position matches the rendered size exactly.
const bubbleW = computed(() => Math.min(520, Math.max(340, window.innerWidth  * 0.24)))
const bubbleH = computed(() => Math.min(680, Math.max(400, window.innerHeight * 0.58)))

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

const chatMenuRef = ref(null)
const chatMenuItems = computed(() => [
  { icon: '💾', label: '导出聊天记录', action: exportHistory },
  { icon: '🗑️', label: '清空聊天历史', action: clearHistory },
  { divider: true },
  { icon: '⚙️', label: '打开设置', action: () => emit('open-settings') },
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
  <div class="chat-bubble" :style="{ left: pos.x + 'px', top: pos.y + 'px' }" @contextmenu="onBubbleContextMenu">
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
  width: clamp(340px, 24vw, 520px);
  height: clamp(400px, 58vh, 680px);
  background: rgba(15, 18, 30, 0.82);
  backdrop-filter: blur(20px) saturate(180%);
  -webkit-backdrop-filter: blur(20px) saturate(180%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 20px;
  box-shadow:
    0 8px 32px rgba(0, 0, 0, 0.6),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
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

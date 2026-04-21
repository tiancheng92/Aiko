<script setup>
import { ref, computed } from 'vue'
import ChatPanel from './ChatPanel.vue'
import ContextMenu from './ContextMenu.vue'
import { EventsEmit } from '../../wailsjs/runtime/runtime'

const props = defineProps({
  ballPos:  { type: Object, default: () => ({ x: -1, y: -1 }) },
  ballSize: { type: Number, default: 64 },
})
const emit = defineEmits(['close', 'open-settings'])

// Mirror CSS clamp so the JS position matches the rendered size exactly.
const bubbleW = computed(() => Math.min(480, Math.max(320, window.innerWidth  * 0.22)))
const bubbleH = computed(() => Math.min(620, Math.max(360, window.innerHeight * 0.55)))

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
  { icon: '🗑️', label: '清空聊天历史', action: clearHistory },
  { divider: true },
  { icon: '⚙️', label: '打开设置', action: () => emit('open-settings') },
])

/** clearHistory broadcasts a clear event to ChatPanel. */
function clearHistory() {
  EventsEmit('chat:clear')
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
  width: clamp(320px, 22vw, 480px);
  height: clamp(360px, 55vh, 620px);
  background: #111827;
  border-radius: 16px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.5);
  display: flex;
  flex-direction: column;
  z-index: 9998;
  overflow: hidden;
}
.title-bar {
  display: flex;
  align-items: center;
  background: #1f2937;
  border-bottom: 1px solid #374151;
  padding: 0 8px;
  flex-shrink: 0;
  user-select: none;
}
.title { flex: 1; color: #f9fafb; font-size: 13px; font-weight: 600; padding: 10px 6px; }
.close-btn { background: none; border: none; color: #6b7280; padding: 10px 8px; cursor: pointer; font-size: 13px; }
.close-btn:hover { color: #f9fafb; }
.content { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
</style>

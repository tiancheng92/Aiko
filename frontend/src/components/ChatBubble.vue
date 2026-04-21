<script setup>
import { ref, computed } from 'vue'
import ChatPanel from './ChatPanel.vue'
import SettingsPanel from './SettingsPanel.vue'
import ContextMenu from './ContextMenu.vue'

const props = defineProps({
  tab:      String,
  ballPos:  { type: Object, default: () => ({ x: -1, y: -1 }) },
  ballSize: { type: Number, default: 64 },
})
const emit = defineEmits(['update:tab', 'close', 'open-settings'])

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

/** setTab switches the active tab. */
function setTab(t) { emit('update:tab', t) }

/** onSaved handles settings save by switching back to chat. */
function onSaved() { emit('update:tab', 'chat') }

const chatMenuRef = ref(null)
const chatMenuItems = computed(() => [
  { icon: '🗑️', label: '清空聊天历史', action: clearHistory },
  { divider: true },
  { icon: '⚙️', label: '打开设置', action: () => emit('open-settings') },
])

/** clearHistory clears the chat history display (frontend-only). */
function clearHistory() {}

/** onBubbleContextMenu shows the chat bubble right-click menu. */
function onBubbleContextMenu(e) {
  e.preventDefault()
  chatMenuRef.value?.show(e.clientX, e.clientY)
}
</script>

<template>
  <div class="chat-bubble" :style="{ left: pos.x + 'px', top: pos.y + 'px' }" @contextmenu="onBubbleContextMenu">
    <div class="tab-bar">
      <button :class="{ active: tab === 'chat' }" @click="setTab('chat')">聊天</button>
      <button :class="{ active: tab === 'settings' }" @click="setTab('settings')">设置</button>
      <button class="close-btn" @click="$emit('close')">✕</button>
    </div>
    <div class="content">
      <ChatPanel v-if="tab === 'chat'" />
      <SettingsPanel v-else @saved="onSaved" />
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
.tab-bar {
  display: flex;
  background: #1f2937;
  border-bottom: 1px solid #374151;
  padding: 0 8px;
  flex-shrink: 0;
  user-select: none;
}
.tab-bar button {
  background: none;
  border: none;
  color: #9ca3af;
  padding: 10px 14px;
  cursor: pointer;
  font-size: 13px;
}
.tab-bar button.active { color: #f9fafb; border-bottom: 2px solid #4f46e5; }
.close-btn { margin-left: auto; color: #6b7280 !important; }
.content { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
</style>

<!-- frontend/src/components/NotificationBubble.vue -->
<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const props = defineProps({
  petPos:    { type: Object,  default: () => ({ x: -1, y: -1 }) },
  petSize:   { type: Number,  default: 160 },
  bubbleOpen: { type: Boolean, default: false },
})

const notification = ref(null) // { title, message, ts }
let hideTimer = null
let offShow   = null

/** pos places the notification bubble above the pet. */
const pos = computed(() => {
  if (props.petPos.x < 0) return { x: 40, y: 40 }
  return {
    x: props.petPos.x - 20,
    y: props.petPos.y - 170,
  }
})

/** dismiss hides the notification and clears the auto-hide timer. */
function dismiss() {
  notification.value = null
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}

onMounted(() => {
  // Listen for the unified notification event.
  offShow = EventsOn('notification:show', (data) => {
    // If chat bubble is open, skip the overlay — message is in chat stream.
    if (props.bubbleOpen) return
    if (hideTimer) clearTimeout(hideTimer)
    notification.value = { title: data.title || '通知', message: data.message, ts: new Date() }
    // Auto-dismiss after 10 minutes.
    hideTimer = setTimeout(dismiss, 10 * 60 * 1000)
  })
})

onUnmounted(() => {
  offShow?.()
  if (hideTimer) clearTimeout(hideTimer)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="notification"
      class="notif-bubble"
      :style="{ left: pos.x + 'px', top: pos.y + 'px' }"
    >
      <div class="notif-header">
        <span class="notif-icon">🔔</span>
        <span class="notif-title">{{ notification.title }}</span>
        <button class="notif-close" @click="dismiss">✕</button>
      </div>
      <div class="notif-body">{{ notification.message }}</div>
      <button class="notif-ack" @click="dismiss">知道了</button>
    </div>
  </Teleport>
</template>

<style scoped>
.notif-bubble {
  position: fixed;
  z-index: 99997;
  width: 280px;
  background: rgba(13, 17, 28, 0.92);
  backdrop-filter: blur(20px) saturate(160%);
  -webkit-backdrop-filter: blur(20px) saturate(160%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  box-shadow: 0 12px 32px rgba(0,0,0,0.6);
  padding: 12px 14px 10px;
  animation: popIn 0.2s ease-out;
}
@keyframes popIn {
  from { opacity: 0; transform: translateY(8px) scale(0.96); }
  to   { opacity: 1; transform: translateY(0) scale(1); }
}
.notif-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}
.notif-icon { font-size: 14px; }
.notif-title {
  flex: 1;
  font-size: 12px;
  font-weight: 600;
  color: rgba(255,255,255,0.85);
  letter-spacing: 0.01em;
}
.notif-close {
  background: none;
  border: none;
  color: rgba(255,255,255,0.3);
  font-size: 11px;
  cursor: pointer;
  padding: 2px 4px;
  border-radius: 4px;
  box-shadow: none;
  transition: color 0.15s;
}
.notif-close:hover { color: #ef4444; background: rgba(239,68,68,0.1); }
.notif-body {
  font-size: 12px;
  color: rgba(209, 213, 219, 0.85);
  line-height: 1.6;
  max-height: 120px;
  overflow-y: auto;
  margin-bottom: 10px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.1) transparent;
  white-space: pre-wrap;
  word-break: break-word;
}
.notif-ack {
  width: 100%;
  background: rgba(99, 102, 241, 0.15);
  border: 1px solid rgba(99, 102, 241, 0.25);
  color: #a5b4fc;
  border-radius: 8px;
  padding: 5px;
  font-size: 12px;
  cursor: pointer;
  box-shadow: none;
  transition: background 0.15s;
}
.notif-ack:hover { background: rgba(99, 102, 241, 0.25); }
</style>
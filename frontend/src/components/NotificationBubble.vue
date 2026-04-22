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
    y: props.petPos.y - 120,
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
      :style="{ left: pos.x + 'px', top: pos.y + 'px', '--tail-left': (props.petSize / 2 + 20) + 'px' }"
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
  width: 260px;
  background: rgba(15, 20, 35, 0.82);
  backdrop-filter: blur(24px) saturate(180%);
  -webkit-backdrop-filter: blur(24px) saturate(180%);
  border: 1px solid rgba(255, 255, 255, 0.10);
  border-radius: 18px;
  border-bottom-right-radius: 4px;
  box-shadow:
    0 8px 32px rgba(0, 0, 0, 0.55),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  padding: 10px 12px 8px;
  animation: popIn 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
  color: #e5e7eb;
}
/* Speech bubble tail pointing down toward pet head */
.notif-bubble::after {
  content: '';
  position: absolute;
  bottom: -10px;
  left: calc(var(--tail-left, 100px) - 8px);
  width: 0;
  height: 0;
  border-left: 8px solid transparent;
  border-right: 8px solid transparent;
  border-top: 10px solid rgba(15, 20, 35, 0.82);
}
@keyframes popIn {
  from { opacity: 0; transform: translateY(12px) scale(0.92); }
  to   { opacity: 1; transform: translateY(0) scale(1); }
}
.notif-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}
.notif-icon { font-size: 15px; flex-shrink: 0; }
.notif-title {
  flex: 1;
  font-size: 13px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.90);
  letter-spacing: 0.01em;
}
.notif-close {
  background: none;
  border: none;
  color: rgba(255, 255, 255, 0.30);
  font-size: 12px;
  cursor: pointer;
  padding: 3px 5px;
  border-radius: 6px;
  box-shadow: none;
  line-height: 1;
  transition: color 0.15s, background 0.15s;
}
.notif-close:hover { color: #ef4444; background: rgba(239, 68, 68, 0.12); }
.notif-body {
  font-size: 12.5px;
  color: rgba(209, 213, 219, 0.85);
  line-height: 1.65;
  max-height: 120px;
  overflow-y: auto;
  margin-bottom: 8px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.08) transparent;
  white-space: pre-wrap;
  word-break: break-word;
}
.notif-ack {
  width: 100%;
  background: rgba(99, 102, 241, 0.20);
  border: 1px solid rgba(99, 102, 241, 0.35);
  color: #a5b4fc;
  border-radius: 10px;
  padding: 5px 8px;
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: none;
  transition: background 0.15s, transform 0.1s;
  letter-spacing: 0.02em;
}
.notif-ack:hover { background: rgba(99, 102, 241, 0.32); }
.notif-ack:active { transform: scale(0.97); }
</style>
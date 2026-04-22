<!-- frontend/src/components/NotificationBubble.vue -->
<script setup>
import { ref, onMounted, onUnmounted, computed, nextTick } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { marked } from 'marked'

const props = defineProps({
  petPos:    { type: Object,  default: () => ({ x: -1, y: -1 }) },
  petSize:   { type: Number,  default: 160 },
})

const notification = ref(null)
const bubbleEl = ref(null)
const bubbleH  = ref(0)
let hideTimer = null
let offShow   = null

const GAP = 12

/** pos places the notification bubble above the pet using measured height. */
const pos = computed(() => {
  if (props.petPos.x < 0) return { x: 40, y: 40 }
  return {
    x: props.petPos.x - 20,
    y: props.petPos.y - bubbleH.value - GAP,
  }
})

/** renderMd renders markdown content for notification body. */
function renderMd(text) {
  if (!text) return ''
  return marked(text, { breaks: true, gfm: true })
}

/** dismiss hides the notification and clears the auto-hide timer. */
function dismiss() {
  notification.value = null
  bubbleH.value = 0
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}

onMounted(() => {
  offShow = EventsOn('notification:show', (data) => {
    if (hideTimer) clearTimeout(hideTimer)
    notification.value = { title: data.title || '通知', message: data.message, ts: new Date() }
    nextTick(() => {
      if (bubbleEl.value) bubbleH.value = bubbleEl.value.offsetHeight
    })
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
      ref="bubbleEl"
      class="notif-bubble"
      :style="{ left: pos.x + 'px', top: pos.y + 'px', '--tail-left': (props.petSize / 2 + 20) + 'px' }"
      @click="dismiss"
    >
      <div class="notif-header">
        <span class="notif-icon">🔔</span>
        <span class="notif-title">{{ notification.title }}</span>
        <button class="notif-close" @click.stop="dismiss">✕</button>
      </div>
      <div class="notif-body markdown" v-html="renderMd(notification.message)" @click.stop />
    </div>
  </Teleport>
</template>

<style scoped>
.notif-bubble {
  position: fixed;
  z-index: 99997;
  width: 320px;
  background: rgba(12, 15, 26, 0.45);
  backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  -webkit-backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 18px;
  box-shadow:
    0 12px 40px rgba(0, 0, 0, 0.5),
    0 1px 0 rgba(255, 255, 255, 0.08) inset,
    0 0 0 0.5px rgba(255, 255, 255, 0.04) inset;
  padding: 12px 16px 14px;
  animation: popIn 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
  color: #e5e7eb;
  cursor: pointer;
}
.notif-bubble::after {
  content: '';
  position: absolute;
  bottom: -10px;
  left: calc(var(--tail-left, 100px) - 8px);
  width: 0;
  height: 0;
  border-left: 8px solid transparent;
  border-right: 8px solid transparent;
  border-top: 10px solid rgba(12, 15, 26, 0.45);
}
@keyframes popIn {
  from { opacity: 0; transform: translateY(12px) scale(0.92); }
  to   { opacity: 1; transform: translateY(0) scale(1); }
}
.notif-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
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
  font-size: 13px;
  color: rgba(209, 213, 219, 0.9);
  line-height: 1.7;
  max-height: 300px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.08) transparent;
  word-break: break-word;
  cursor: text;
}

/* Markdown styles */
.notif-body :deep(p) { margin: 0 0 6px; }
.notif-body :deep(p:last-child) { margin-bottom: 0; }
.notif-body :deep(strong) { color: #fff; font-weight: 600; }
.notif-body :deep(em) { color: #c4b5fd; }
.notif-body :deep(code) {
  background: rgba(255,255,255,0.1);
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 12px;
  font-family: monospace;
}
.notif-body :deep(ul), .notif-body :deep(ol) {
  margin: 4px 0 6px;
  padding-left: 18px;
}
.notif-body :deep(li) { margin: 2px 0; }
.notif-body :deep(a) { color: #7dd3fc; text-decoration: underline; }
.notif-body :deep(hr) { border: none; border-top: 1px solid rgba(255,255,255,0.1); margin: 8px 0; }
.notif-body :deep(table) { width: 100%; border-collapse: collapse; font-size: 12px; margin: 6px 0; }
.notif-body :deep(th), .notif-body :deep(td) {
  padding: 5px 10px;
  border-bottom: 1px solid rgba(255,255,255,0.07);
  text-align: left;
}
.notif-body :deep(thead tr) { background: rgba(255,255,255,0.06); font-weight: 600; }
</style>

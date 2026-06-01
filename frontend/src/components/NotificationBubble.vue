<!-- frontend/src/components/NotificationBubble.vue -->
<script setup>
import { ref, onMounted, onUnmounted, computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { renderMarkdown } from '../composables/useMarkdown.js'

const props = defineProps({
  petPos:  { type: Object, default: () => ({ x: -1, y: -1 }) },
  petSize: { type: Number, default: 160 },
})

const { t } = useI18n()

const notification = ref(null)
const bubbleEl = ref(null)
const bubbleH  = ref(0)
let hideTimer = null
let offShow   = null

const GAP = 12
const DEFAULT_DISMISS_MS = 15000  // 15s — long enough to glance, short enough to not clutter

/** scheduleDismiss arms (or re-arms) the auto-dismiss timer. */
function scheduleDismiss(durationMs) {
  if (hideTimer) clearTimeout(hideTimer)
  hideTimer = setTimeout(dismiss, durationMs || DEFAULT_DISMISS_MS)
}

/** pauseDismiss cancels the pending auto-dismiss (called on hover). */
function pauseDismiss() {
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}

const BUBBLE_W  = 320
const MARGIN    = 8
const MENU_BAR  = 38  // macOS menu bar height

/** pos places the notification bubble above the pet, clamped to the viewport. */
const pos = computed(() => {
  const vw = window.innerWidth
  const vh = window.innerHeight
  let bx, by
  if (props.petPos.x < 0) {
    bx = 40
    by = 40
  } else {
    bx = props.petPos.x - 20
    by = props.petPos.y - bubbleH.value - GAP
  }
  bx = Math.max(MARGIN, Math.min(bx, vw - BUBBLE_W - MARGIN))
  by = Math.max(MENU_BAR + MARGIN, Math.min(by, vh - bubbleH.value - MARGIN))
  return { x: bx, y: by }
})


/** dismiss hides the notification and clears the auto-hide timer. */
function dismiss() {
  notification.value = null
  bubbleH.value = 0
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}

onMounted(() => {
  offShow = EventsOn('notification:show', (data) => {
    // Replace current notification content and reset timer (no stacking).
    notification.value = {
      title: data.title || t('notification.fallbackTitle'),
      message: data.message,
      ts: new Date(),
    }
    nextTick(() => {
      if (bubbleEl.value) bubbleH.value = bubbleEl.value.offsetHeight
    })
    const durationMs = data.durationSecs ? data.durationSecs * 1000 : DEFAULT_DISMISS_MS
    scheduleDismiss(durationMs)
  })
})

onUnmounted(() => {
  offShow?.()
  if (hideTimer) clearTimeout(hideTimer)
})
</script>

<template>
  <Teleport to="body">
    <Transition name="notif-pop">
    <div
      v-if="notification"
      ref="bubbleEl"
      class="notif-bubble"
      role="status"
      aria-live="polite"
      :style="{ left: pos.x + 'px', top: pos.y + 'px', '--tail-left': (props.petSize / 2 + 20) + 'px' }"
      @click="dismiss"
      @mouseenter="pauseDismiss"
      @mouseleave="scheduleDismiss"
    >
      <div class="notif-header">
        <span class="notif-icon" aria-hidden="true">
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/>
            <path d="M13.73 21a2 2 0 0 1-3.46 0"/>
          </svg>
        </span>
        <span class="notif-title">{{ notification.title }}</span>
        <button class="notif-close" :aria-label="$t('notification.close')" @click.stop="dismiss">
          <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>
      <div class="notif-body markdown" v-html="renderMarkdown(notification.message)" @click.stop />
    </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.notif-bubble {
  position: fixed;
  z-index: 99997;
  width: 320px;
  background: var(--lg-surface);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 14px;
  box-shadow: var(--lg-shadow-sm);
  padding: 12px 14px 14px;
  color: var(--text-primary);
  cursor: pointer;
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', sans-serif;
  -webkit-font-smoothing: antialiased;
}

/* Speech-bubble tail pointing down to the pet */
.notif-bubble::after {
  content: '';
  position: absolute;
  bottom: -9px;
  left: calc(var(--tail-left, 100px) - 8px);
  width: 16px;
  height: 10px;
  background: var(--lg-surface);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  clip-path: polygon(0 0, 100% 0, 50% 100%);
  border-left: 1px solid var(--lg-border-subtle);
  border-right: 1px solid var(--lg-border-subtle);
}

.notif-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.notif-icon {
  flex-shrink: 0;
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  background: rgba(0, 122, 255, 0.16);
  color: var(--accent);
}
.notif-icon :deep(svg) { width: 13px; height: 13px; }

.notif-title {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  letter-spacing: -0.01em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notif-close {
  background: transparent;
  border: none;
  color: var(--text-tertiary);
  width: 22px;
  height: 22px;
  padding: 0;
  cursor: pointer;
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  transition: background 0.12s, color 0.12s, transform 0.1s cubic-bezier(0.16, 1, 0.3, 1);
  flex-shrink: 0;
}
.notif-close:hover {
  background: rgba(255, 69, 58, 0.16);
  color: var(--danger);
  transform: scale(1.12);
}
.notif-close:active { transform: scale(0.94); }
.notif-close:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.notif-body {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.65;
  max-height: 300px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.14) transparent;
  word-break: break-word;
  cursor: text;
}
.notif-body::-webkit-scrollbar { width: 6px; }
.notif-body::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.14); border-radius: 3px; }

/* ── Animations ───────────────────────────────────────────────────────── */
.notif-pop-enter-active {
  transition:
    opacity 0.24s cubic-bezier(0.34, 1.56, 0.64, 1),
    transform 0.24s cubic-bezier(0.34, 1.56, 0.64, 1);
  transform-origin: bottom left;
}
.notif-pop-leave-active {
  transition:
    opacity 0.16s ease-in,
    transform 0.16s ease-in;
  transform-origin: bottom left;
}
.notif-pop-enter-from,
.notif-pop-leave-to {
  opacity: 0;
  transform: scale(0.90) translateY(8px);
}
@media (prefers-reduced-motion: reduce) {
  .notif-pop-enter-active,
  .notif-pop-leave-active { transition: opacity 0.1s; }
  .notif-pop-enter-from,
  .notif-pop-leave-to { transform: none; }
}

/* ── Markdown ─────────────────────────────────────────────────────────── */
.notif-body :deep(p) { margin: 0 0 6px; }
.notif-body :deep(p:last-child) { margin-bottom: 0; }
.notif-body :deep(strong) { color: var(--text-primary); font-weight: 600; }
.notif-body :deep(em) { color: var(--text-primary); font-style: italic; }
.notif-body :deep(code) {
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  font-size: 12px;
}
.notif-body :deep(:not(pre) > code) {
  background: rgba(255, 255, 255, 0.08);
  padding: 1px 6px;
  border-radius: 4px;
}
.notif-body :deep(.tool-call-chip) {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 7px 2px 6px;
  border-radius: 5px;
  font-size: 10px;
  font-family: 'SF Mono', ui-monospace, monospace;
  vertical-align: middle;
  white-space: nowrap;
}
.notif-body :deep(.tool-call-chip--tool) {
  background: rgba(234, 179, 8, 0.1);
  border: 1px solid rgba(234, 179, 8, 0.25);
  color: rgba(253, 224, 71, 0.85);
}
.notif-body :deep(.tool-call-chip--skill) {
  background: rgba(34, 197, 94, 0.1);
  border: 1px solid rgba(34, 197, 94, 0.25);
  color: rgba(74, 222, 128, 0.85);
}
.notif-body :deep(.code-block) {
  margin: 6px 0;
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid rgba(255,255,255,0.08);
}
.notif-body :deep(.code-lang-icon) {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  width: 14px;
  height: 14px;
  margin-right: 5px;
}
.notif-body :deep(.code-lang-icon svg) { width: 14px; height: 14px; }
.notif-body :deep(.code-header) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 10px;
  background: rgba(255,255,255,0.04);
  border-bottom: 1px solid rgba(255,255,255,0.06);
}
.notif-body :deep(.code-lang) {
  font-size: 10px;
  color: rgba(125,211,252,0.6);
  font-family: 'SF Mono', ui-monospace, monospace;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.notif-body :deep(.code-copy) {
  font-size: 10px;
  padding: 2px 8px;
  background: var(--accent-alpha-12);
  color: var(--accent);
  border: 1px solid var(--accent-alpha-20);
  border-radius: 4px;
  cursor: pointer;
  font-weight: 500;
  transition: background 0.12s, color 0.12s;
}
.notif-body :deep(.code-copy:hover) { background: var(--accent); color: #fff; border-color: transparent; }
.notif-body :deep(pre) {
  background: rgba(10, 10, 20, 0.6);
  padding: 8px 10px;
  overflow-x: auto;
  margin: 0;
}
.notif-body :deep(pre code) { white-space: pre-wrap; word-break: break-word; background: none; padding: 0; }
.notif-body :deep(.code-line) { display: flex; align-items: flex-start; line-height: 1.6; }
.notif-body :deep(.line-nr) {
  flex-shrink: 0;
  min-width: 2.5ch;
  padding-right: 0.6ch;
  margin-right: 1ch;
  color: rgba(148,163,184,0.35);
  font-size: 11px;
  user-select: none;
  border-right: 1px solid rgba(255,255,255,0.08);
}
.notif-body :deep(.line-code) { flex: 1; min-width: 0; white-space: pre-wrap; word-break: break-word; }
.notif-body :deep(ul), .notif-body :deep(ol) {
  margin: 4px 0 6px;
  padding-left: 18px;
}
.notif-body :deep(li) { margin: 2px 0; }
.notif-body :deep(a) {
  color: var(--accent);
  text-decoration: none;
}
.notif-body :deep(a:hover) { text-decoration: underline; }
.notif-body :deep(hr) {
  border: none;
  border-top: 1px solid var(--lg-border-subtle);
  margin: 8px 0;
}
.notif-body :deep(table) {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  margin: 6px 0;
}
.notif-body :deep(th), .notif-body :deep(td) {
  padding: 5px 10px;
  border-bottom: 1px solid var(--lg-border-subtle);
  text-align: left;
}
.notif-body :deep(thead tr) {
  background: rgba(255, 255, 255, 0.05);
  font-weight: 600;
}
</style>

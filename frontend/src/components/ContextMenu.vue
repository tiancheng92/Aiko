<!-- frontend/src/components/ContextMenu.vue -->
<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useEscapeKey } from '../composables/useEscapeKey'

/**
 * ContextMenu renders a positioned popup menu.
 * items: Array<{
 *   label: string,
 *   icon?: string,          // emoji or text glyph (legacy)
 *   iconSvg?: string,       // raw SVG markup rendered via v-html (preferred)
 *   action: () => void,
 *   divider?: boolean,
 *   danger?: boolean,       // renders the item in the destructive color
 * }>
 */
defineProps({
  items: { type: Array, default: () => [] },
})
const emit = defineEmits(['close'])

const menuRef = ref(null)
const pos = ref({ x: 0, y: 0 })
const visible = ref(false)
const hoveredIndex = ref(null)

/**
 * show displays the menu anchored near (x, y), adjusted to stay within viewport.
 */
function show(x, y) {
  pos.value = { x, y }
  visible.value = true
  hoveredIndex.value = null
  // Expose a hook so the Go-side mouse tracker can update hovered index directly,
  // bypassing WKWebView's key-window restriction on tracking areas / CSS :hover.
  window.__aikoCtxHover = (i) => { hoveredIndex.value = i }
  nextTick(() => {
    if (!menuRef.value) return
    const rect = menuRef.value.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight
    if (x + rect.width > vw) pos.value = { ...pos.value, x: vw - rect.width - 8 }
    if (y + rect.height > vh) pos.value = { ...pos.value, y: vh - rect.height - 8 }
  })
}

/** hide closes the menu and emits close. */
function hide() {
  visible.value = false
  delete window.__aikoCtxHover
  emit('close')
}

/** onOutsideClick dismisses the menu when clicking outside of it. */
function onOutsideClick(e) {
  if (menuRef.value && !menuRef.value.contains(e.target)) hide()
}

useEscapeKey(hide, visible)

onMounted(() => window.addEventListener('mousedown', onOutsideClick, true))
onUnmounted(() => window.removeEventListener('mousedown', onOutsideClick, true))

defineExpose({ show, hide })
</script>

<template>
  <Teleport to="body">
    <Transition name="ctx-pop">
    <div
      v-if="visible"
      ref="menuRef"
      class="ctx-menu"
      role="menu"
      :style="{ left: pos.x + 'px', top: pos.y + 'px' }"
      @contextmenu.prevent
    >
      <template v-for="(item, i) in items" :key="i">
        <div v-if="item.divider" class="ctx-divider" />
        <button
          v-else
          role="menuitem"
          :class="['ctx-item', { danger: item.danger, hovered: hoveredIndex === i, disabled: item.disabled }]"
          :disabled="item.disabled"
          :data-idx="i"
          @click="() => { if (!item.disabled) { item.action(); hide() } }"
        >
          <span class="ctx-icon-wrap">
            <span v-if="item.iconSvg" class="ctx-icon-svg" v-html="item.iconSvg" />
            <span v-else-if="item.icon" class="ctx-icon-text">{{ item.icon }}</span>
          </span>
          <span class="ctx-label">{{ item.label }}</span>
        </button>
      </template>
    </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ctx-menu {
  --surface: rgba(38, 38, 44, 0.78);
  --text-primary: rgba(255, 255, 255, 0.92);
  --text-secondary: rgba(255, 255, 255, 0.62);
  --border-subtle: rgba(255, 255, 255, 0.10);

  position: fixed;
  z-index: 99999;
  background: var(--surface);
  backdrop-filter: blur(40px) saturate(180%);
  -webkit-backdrop-filter: blur(40px) saturate(180%);
  border: 1px solid var(--border-subtle);
  border-radius: 10px;
  padding: 4px;
  min-width: 180px;
  box-shadow:
    0 12px 36px rgba(0, 0, 0, 0.55),
    0 0 0 0.5px rgba(0, 0, 0, 0.3),
    0 1px 0 rgba(255, 255, 255, 0.08) inset;
  user-select: none;
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', sans-serif;
}

.ctx-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  background: transparent;
  border: none;
  color: var(--text-primary);
  padding: 6px 10px;
  font-size: 13px;
  font-weight: 400;
  letter-spacing: -0.01em;
  font-family: inherit;
  cursor: pointer;
  text-align: left;
  border-radius: 6px;
  box-shadow: none;
  transition: background 0.08s, color 0.08s, transform 0.1s cubic-bezier(0.16, 1, 0.3, 1);
  -webkit-appearance: none;
  appearance: none;
}
.ctx-item:hover,
.ctx-item:focus-visible,
.ctx-item.hovered {
  background: var(--accent);
  color: #fff;
  outline: none;
  transform: translateX(2px);
}
.ctx-item:active { transform: translateX(1px) scale(0.98); }
.ctx-item:hover .ctx-icon-wrap,
.ctx-item:focus-visible .ctx-icon-wrap,
.ctx-item.hovered .ctx-icon-wrap { color: #fff; }

.ctx-item.disabled { opacity: 0.38; cursor: not-allowed; }
.ctx-item.disabled:hover,
.ctx-item.disabled.hovered { background: transparent; color: var(--text-primary); transform: none; }

.ctx-item.danger { color: var(--danger); }
.ctx-item.danger:hover,
.ctx-item.danger:focus-visible,
.ctx-item.danger.hovered {
  background: var(--danger);
  color: #fff;
}

.ctx-icon-wrap {
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--text-secondary);
}
.ctx-icon-svg :deep(svg) { width: 14px; height: 14px; }
.ctx-icon-text { font-size: 13px; line-height: 1; }

.ctx-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ctx-divider {
  height: 1px;
  background: var(--border-subtle);
  margin: 4px 6px;
}

/* Open / close animation — subtle scale from top-left */
.ctx-pop-enter-active {
  transition:
    opacity 0.16s cubic-bezier(0.34, 1.56, 0.64, 1),
    transform 0.16s cubic-bezier(0.34, 1.56, 0.64, 1);
  transform-origin: top left;
}
.ctx-pop-leave-active {
  transition:
    opacity 0.10s ease-in,
    transform 0.10s ease-in;
  transform-origin: top left;
}
.ctx-pop-enter-from,
.ctx-pop-leave-to {
  opacity: 0;
  transform: scale(0.92) translateY(-2px);
}

@media (prefers-reduced-motion: reduce) {
  .ctx-pop-enter-active,
  .ctx-pop-leave-active { transition: opacity 0.1s; }
  .ctx-pop-enter-from,
  .ctx-pop-leave-to { transform: none; }
}
</style>

<!-- frontend/src/components/ContextMenu.vue -->
<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'

/**
 * ContextMenu renders a positioned popup menu.
 * items: Array<{ label: string, icon?: string, action: () => void, divider?: boolean }>
 */
const props = defineProps({
  items: { type: Array, default: () => [] },
})
const emit = defineEmits(['close'])

const menuRef = ref(null)
const pos = ref({ x: 0, y: 0 })
const visible = ref(false)

/**
 * show displays the menu anchored near (x, y), adjusted to stay within viewport.
 */
function show(x, y) {
  pos.value = { x, y }
  visible.value = true
  nextTick(() => {
    if (!menuRef.value) return
    const rect = menuRef.value.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight
    if (x + rect.width > vw) pos.value = { ...pos.value, x: vw - rect.width - 8 }
    if (y + rect.height > vh) pos.value = { ...pos.value, y: vh - rect.height - 8 }
  })
}

function hide() {
  visible.value = false
  emit('close')
}

function onOutsideClick(e) {
  if (menuRef.value && !menuRef.value.contains(e.target)) hide()
}

onMounted(() => window.addEventListener('mousedown', onOutsideClick, true))
onUnmounted(() => window.removeEventListener('mousedown', onOutsideClick, true))

defineExpose({ show, hide })
</script>

<template>
  <Teleport to="body">
    <div
      v-if="visible"
      ref="menuRef"
      class="ctx-menu"
      :style="{ left: pos.x + 'px', top: pos.y + 'px' }"
      @contextmenu.prevent
    >
      <template v-for="(item, i) in items" :key="i">
        <div v-if="item.divider" class="ctx-divider" />
        <button
          v-else
          class="ctx-item"
          @click="() => { item.action(); hide() }"
        >
          <span v-if="item.icon" class="ctx-icon">{{ item.icon }}</span>
          <span>{{ item.label }}</span>
        </button>
      </template>
    </div>
  </Teleport>
</template>

<style scoped>
.ctx-menu {
  position: fixed;
  z-index: 99999;
  background: rgba(12, 15, 26, 0.65);
  backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  -webkit-backdrop-filter: blur(40px) saturate(200%) brightness(0.9);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 12px;
  padding: 5px 0;
  min-width: 172px;
  box-shadow:
    0 12px 32px rgba(0, 0, 0, 0.55),
    0 1px 0 rgba(255, 255, 255, 0.08) inset,
    0 0 0 0.5px rgba(255, 255, 255, 0.04) inset;
  user-select: none;
}
.ctx-item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  background: none;
  border: none;
  color: rgba(229, 231, 235, 0.9);
  padding: 7px 14px;
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  border-radius: 0;
  box-shadow: none;
  transition: background 0.12s;
  font-weight: 400;
}
.ctx-item:hover { background: rgba(99, 102, 241, 0.15); color: #f9fafb; }
.ctx-icon { font-size: 14px; width: 18px; text-align: center; flex-shrink: 0; }
.ctx-divider { height: 1px; background: rgba(255, 255, 255, 0.06); margin: 4px 8px; }
</style>

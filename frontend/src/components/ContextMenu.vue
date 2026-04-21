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
  background: #1f2937;
  border: 1px solid #374151;
  border-radius: 8px;
  padding: 4px 0;
  min-width: 160px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.5);
  user-select: none;
}
.ctx-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: none;
  border: none;
  color: #e5e7eb;
  padding: 7px 14px;
  font-size: 13px;
  cursor: pointer;
  text-align: left;
}
.ctx-item:hover { background: #374151; }
.ctx-icon { font-size: 14px; width: 18px; text-align: center; }
.ctx-divider { height: 1px; background: #374151; margin: 3px 0; }
</style>

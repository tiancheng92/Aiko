<script setup>
import { ref, onMounted } from 'vue'
import { GetConfig, SaveConfig, GetScreenSize } from '../../wailsjs/go/main/App'

const emit = defineEmits(['click'])
const pos = ref({ x: 0, y: 0 })
const ballSize = ref(64)
let dragStart = null
let isDragging = false

onMounted(async () => {
  const [cfg, screenSize] = await Promise.all([GetConfig(), GetScreenSize()])
  const [sw, sh] = screenSize
  ballSize.value = Math.min(80, Math.max(48, Math.round(sh * 0.055)))
  if (cfg.BallPositionX >= 0 && cfg.BallPositionY >= 0) {
    pos.value = { x: cfg.BallPositionX, y: cfg.BallPositionY }
  } else {
    pos.value = { x: sw - ballSize.value - 24, y: sh - ballSize.value - 24 }
  }
})

/** onMouseDown starts drag tracking on mouse button press. */
function onMouseDown(e) {
  dragStart = { x: e.clientX - pos.value.x, y: e.clientY - pos.value.y, startX: e.clientX, startY: e.clientY }
  isDragging = false
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}

/** onMouseMove updates the ball position during drag. */
function onMouseMove(e) {
  if (!dragStart) return
  const dx = e.clientX - dragStart.startX
  const dy = e.clientY - dragStart.startY
  if (!isDragging && Math.sqrt(dx * dx + dy * dy) < 5) return
  isDragging = true
  pos.value = { x: e.clientX - dragStart.x, y: e.clientY - dragStart.y }
}

/** onMouseUp finalizes drag or fires click event, then persists position. */
async function onMouseUp(e) {
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  try {
    if (!isDragging) {
      emit('click')
    } else {
      const cfg = await GetConfig()
      cfg.BallPositionX = Math.round(pos.value.x)
      cfg.BallPositionY = Math.round(pos.value.y)
      await SaveConfig(cfg)
    }
  } catch (e) {
    console.error('Failed to save ball position:', e)
  } finally {
    dragStart = null
    isDragging = false
  }
}
</script>

<template>
  <div
    class="floating-ball"
    :style="{ left: pos.x + 'px', top: pos.y + 'px', width: ballSize + 'px', height: ballSize + 'px', fontSize: Math.round(ballSize * 0.44) + 'px' }"
    @mousedown="onMouseDown"
  >
    🐾
  </div>
</template>

<style scoped>
.floating-ball {
  position: fixed;
  border-radius: 50%;
  background: rgba(79, 70, 229, 0.9);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  user-select: none;
  z-index: 9999;
  box-shadow: 0 4px 16px rgba(0,0,0,0.3);
}
.floating-ball:hover { background: rgba(99, 90, 255, 0.95); }
</style>

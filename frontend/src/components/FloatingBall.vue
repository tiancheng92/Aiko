<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { GetBallPosition, SaveBallPosition, GetScreenSize } from '../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'

const emit = defineEmits(['click', 'position', 'ball-size'])
const pos = ref(null)
const ballSize = ref(64)
const sw = ref(0)
const sh = ref(0)
let dragStart = null
let isDragging = false

watch(pos, (p) => { if (p) emit('position', { ...p }) })

/** waitForRuntime polls until the Wails Go bridge is available. */
async function waitForRuntime() {
  while (!window.go?.main?.App) {
    await new Promise(r => setTimeout(r, 20))
  }
}

/** loadPosition fetches the saved ball position for the given screen size. */
async function loadPosition(screenW, screenH) {
  sw.value = screenW
  sh.value = screenH
  ballSize.value = Math.min(80, Math.max(48, Math.round(screenH * 0.055)))
  emit('ball-size', ballSize.value)
  const [bx, by] = await GetBallPosition(screenW, screenH)
  pos.value = (bx >= 0 && by >= 0)
    ? { x: bx, y: by }
    : { x: screenW - ballSize.value - 40, y: screenH - ballSize.value - 40 }
}

let offScreenChanged = null

onMounted(async () => {
  try {
    await waitForRuntime()
    const [screenW, screenH] = await GetScreenSize()
    await loadPosition(screenW, screenH)
  } catch (err) {
    console.error('FloatingBall init:', err)
    const bs = ballSize.value
    pos.value = { x: window.innerWidth - bs - 40, y: window.innerHeight - bs - 40 }
  }

  offScreenChanged = EventsOn('screen:active:changed', async (info) => {
    try {
      await loadPosition(info.width, info.height)
    } catch (err) {
      console.warn('FloatingBall screen:active:changed:', err)
    }
  })
})

onUnmounted(() => {
  if (offScreenChanged) offScreenChanged()
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
  if (!dragStart || !pos.value) return
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
      await SaveBallPosition(Math.round(pos.value.x), Math.round(pos.value.y), sw.value, sh.value)
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
    v-if="pos"
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

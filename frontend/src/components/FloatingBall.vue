<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { GetBallPosition, SaveBallPosition } from '../../bindings/aiko/internal/services/configservice'
import { GetScreenSize } from '../../bindings/aiko/internal/services/windowservice'
import { Events } from '@wailsio/runtime'

const emit = defineEmits(['click', 'position', 'ball-size'])
const pos = ref(null)
const ballSize = ref(64)
const sw = ref(0)
const sh = ref(0)
let dragStart = null
let isDragging = false

watch(pos, (p) => { if (p) emit('position', { ...p }) })

/** waitForRuntime polls until the Wails v3 runtime bridge is available. */
async function waitForRuntime() {
  while (!window._wails?.invoke) {
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

  offScreenChanged = Events.On('screen:active:changed', async (event) => {
    const info = event.data
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
  window.addEventListener('blur', onMouseUp)
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
  window.removeEventListener('blur', onMouseUp)
  try {
    // Blur events fire this with no positional data — only treat actual
    // mouseups as clicks so a window deactivation doesn't trigger emit('click').
    if (!isDragging && e && typeof e.clientX === 'number') {
      emit('click')
    } else if (isDragging) {
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
    :style="{ left: pos.x + 'px', top: pos.y + 'px', width: ballSize + 'px', height: ballSize + 'px' }"
    @mousedown="onMouseDown"
    aria-label="Aiko"
    role="button"
  >
    <svg :width="Math.round(ballSize * 0.44)" :height="Math.round(ballSize * 0.44)" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <!-- Aiko paw icon -->
      <circle cx="9" cy="6" r="2.2" fill="currentColor" opacity="0.85"/>
      <circle cx="15" cy="6" r="2.2" fill="currentColor" opacity="0.85"/>
      <circle cx="5.5" cy="10" r="1.6" fill="currentColor" opacity="0.7"/>
      <circle cx="18.5" cy="10" r="1.6" fill="currentColor" opacity="0.7"/>
      <path d="M12 10.5c-3.5 0-6 2.2-5.5 5.2.4 2.2 2.6 3.8 5.5 3.8s5.1-1.6 5.5-3.8c.5-3-2-5.2-5.5-5.2z" fill="currentColor"/>
    </svg>
  </div>
</template>

<style scoped>
.floating-ball {
  position: fixed;
  border-radius: 50%;
  background: rgba(79, 70, 229, 0.35);
  backdrop-filter: var(--lg-blur-sm);
  -webkit-backdrop-filter: var(--lg-blur-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  user-select: none;
  z-index: 9999;
  color: rgba(255, 255, 255, 0.92);
  box-shadow:
    0 4px 16px rgba(0, 0, 0, 0.3),
    0 0 0 1px rgba(255, 255, 255, 0.12) inset;
  transition: background 0.18s, box-shadow 0.18s, transform 0.12s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.floating-ball:hover {
  background: rgba(99, 90, 255, 0.5);
  box-shadow:
    0 6px 22px rgba(79, 70, 229, 0.45),
    0 0 0 1px rgba(255, 255, 255, 0.18) inset;
  transform: scale(1.06);
}
.floating-ball:active {
  transform: scale(0.95);
  transition-duration: 0.08s;
}
</style>

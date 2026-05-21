<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

const canvasEl = ref(null)
let intervalId = null
let observer = null
let resizeTimer = null

const CHARS = 'アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz!@#$%^&*'
const CHARS_LEN   = CHARS.length
const FONT_SIZE   = 14
const COL_SPACING = 24   // column stride; wider than font = fewer, sparser columns
const INTERVAL_MS = 50
const FALL_SPEED  = 0.9
const RESET_THRESHOLD = 0.97
const DROP_CHANCE = 0.6  // probability per frame that a column emits a new character
const HEAD_COLOR  = 'rgba(200, 255, 200, 1)'
const TRAIL_COLOR = 'rgba(0, 255, 65, 1)'
// Alpha multiplied per frame; 0.85^22 ≈ 0.03 → trail ~22 frames long
const TRAIL_DECAY     = 0.85
const ALPHA_THRESHOLD = 0.03

/** initCanvas sizes the canvas to its parent's dimensions and returns a per-column drops array. Returns null if size is not yet available. */
function initCanvas(canvas) {
  const parent = canvas.parentElement
  const w = parent ? parent.clientWidth  : canvas.offsetWidth
  const h = parent ? parent.clientHeight : canvas.offsetHeight
  if (w === 0 || h === 0) return null
  canvas.width  = w
  canvas.height = h
  const cols = Math.floor(w / COL_SPACING)
  return Array.from({ length: cols }, () => Math.random() * -(h / FONT_SIZE))
}

/** startAnimation begins the matrix rain draw loop and returns the interval id, or null if canvas has no size. */
function startAnimation(canvas) {
  const ctx = canvas.getContext('2d')
  const drops = initCanvas(canvas)
  if (drops === null) return null

  ctx.font = FONT_SIZE + 'px monospace'

  // trails[col] = [{y, char, a}] — characters with decreasing alpha
  const trails = drops.map(() => [])

  return setInterval(() => {
    ctx.clearRect(0, 0, canvas.width, canvas.height)

    for (let i = 0; i < drops.length; i++) {
      const trail = trails[i]

      // Decay alpha of existing trail entries; remove those below threshold
      for (let j = trail.length - 1; j >= 0; j--) {
        trail[j].a *= TRAIL_DECAY
        if (trail[j].a < ALPHA_THRESHOLD) trail.splice(j, 1)
      }

      // Add new head character with DROP_CHANCE probability
      const headY = drops[i] * FONT_SIZE
      if (Math.random() < DROP_CHANCE) {
        trail.push({ y: headY, char: CHARS[Math.floor(Math.random() * CHARS_LEN)], a: 1.0 })
      }

      // Draw all trail characters; the last entry is always the head
      for (let j = 0; j < trail.length; j++) {
        const t = trail[j]
        ctx.globalAlpha = t.a
        ctx.fillStyle = j === trail.length - 1 ? HEAD_COLOR : TRAIL_COLOR
        ctx.fillText(t.char, i * COL_SPACING, t.y)
      }

      // Advance drop; reset to top when it leaves the bottom
      if (drops[i] * FONT_SIZE > canvas.height && Math.random() > RESET_THRESHOLD) {
        drops[i] = 0
        trails[i] = []
      }
      drops[i] += FALL_SPEED
    }

    ctx.globalAlpha = 1.0
  }, INTERVAL_MS)
}

onMounted(() => {
  const canvas = canvasEl.value
  const parent = canvas.parentElement

  // Wait one rAF so flex layout has committed before reading dimensions.
  requestAnimationFrame(() => {
    intervalId = startAnimation(canvas)

    observer = new ResizeObserver(() => {
      clearTimeout(resizeTimer)
      resizeTimer = setTimeout(() => {
        clearInterval(intervalId)
        intervalId = startAnimation(canvas)
      }, 16)
    })
    observer.observe(parent ?? canvas)
  })
})

onUnmounted(() => {
  clearInterval(intervalId)
  clearTimeout(resizeTimer)
  observer?.disconnect()
})
</script>

<template>
  <canvas ref="canvasEl" class="code-rain" />
</template>

<style scoped>
.code-rain {
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  display: block;
}
</style>

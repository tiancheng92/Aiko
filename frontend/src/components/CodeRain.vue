<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

const canvasEl = ref(null)
let intervalId = null
let observer = null
let resizeTimer = null

const CHARS = 'アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz!@#$%^&*'
const CHARS_LEN = CHARS.length
const FONT_SIZE = 14
const INTERVAL_MS = 50
const FALL_SPEED = 0.5
const RESET_THRESHOLD = 0.975
const CHAR_COLOR = 'rgba(0, 255, 70, 0.85)'
const FADE_COLOR = 'rgba(0, 0, 0, 0.05)'

/** initCanvas sizes the canvas to match its parent container and resets the drops array. Returns null if container has no size yet. */
function initCanvas(canvas) {
  const parent = canvas.parentElement
  const w = parent ? parent.clientWidth  : canvas.offsetWidth
  const h = parent ? parent.clientHeight : canvas.offsetHeight
  if (w === 0 || h === 0) return null
  canvas.width  = w
  canvas.height = h
  const cols = Math.floor(w / FONT_SIZE)
  return Array.from({ length: cols }, () => Math.random() * -(h / FONT_SIZE))
}

/** startAnimation begins the matrix rain draw loop and returns the interval id, or null if canvas has no size. */
function startAnimation(canvas) {
  const ctx = canvas.getContext('2d')
  const drops = initCanvas(canvas)
  if (drops === null) return null

  ctx.font = FONT_SIZE + 'px monospace'

  return setInterval(() => {
    ctx.fillStyle = FADE_COLOR
    ctx.fillRect(0, 0, canvas.width, canvas.height)
    ctx.fillStyle = CHAR_COLOR

    for (let i = 0; i < drops.length; i++) {
      const ch = CHARS[Math.floor(Math.random() * CHARS_LEN)]
      ctx.fillText(ch, i * FONT_SIZE, drops[i] * FONT_SIZE)
      if (drops[i] * FONT_SIZE > canvas.height && Math.random() > RESET_THRESHOLD) {
        drops[i] = 0
      }
      drops[i] += FALL_SPEED
    }
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

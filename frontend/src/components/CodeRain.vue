<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

const canvasEl = ref(null)
let intervalId = null
let observer = null

const CHARS = 'アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz!@#$%^&*'
const FONT_SIZE = 14
const INTERVAL_MS = 50
const FALL_SPEED = 0.5
const RESET_THRESHOLD = 0.975
const CHAR_COLOR = 'rgba(0, 255, 70, 0.85)'
const FADE_COLOR = 'rgba(0, 0, 0, 0.05)'

/** initCanvas sizes the canvas to match its CSS display size and resets the drops array. */
function initCanvas(canvas) {
  const w = canvas.offsetWidth
  const h = canvas.offsetHeight
  canvas.width  = w
  canvas.height = h
  const cols = Math.floor(w / FONT_SIZE)
  return Array.from({ length: cols }, () => Math.random() * -(h / FONT_SIZE))
}

/** startAnimation begins the matrix rain draw loop and returns the interval id. */
function startAnimation(canvas) {
  const ctx = canvas.getContext('2d')
  const drops = initCanvas(canvas)

  return setInterval(() => {
    ctx.fillStyle = FADE_COLOR
    ctx.fillRect(0, 0, canvas.width, canvas.height)
    ctx.fillStyle = CHAR_COLOR
    ctx.font = FONT_SIZE + 'px monospace'

    for (let i = 0; i < drops.length; i++) {
      const ch = CHARS[Math.floor(Math.random() * CHARS.length)]
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
  intervalId = startAnimation(canvas)

  observer = new ResizeObserver(() => {
    clearInterval(intervalId)
    intervalId = startAnimation(canvas)
  })
  observer.observe(canvas.parentElement)
})

onUnmounted(() => {
  clearInterval(intervalId)
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

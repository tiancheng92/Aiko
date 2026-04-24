<script setup>
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import Live2DPet from './components/Live2DPet.vue'
import ChatBubble from './components/ChatBubble.vue'
import SettingsWindow from './components/SettingsWindow.vue'
import NotificationBubble from './components/NotificationBubble.vue'
import { MissingRequiredConfig, IsFirstLaunch, MarkWelcomeShown, GetScreenSize } from '../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime'

const bubbleOpen = ref(false)
const settingsOpen = ref(false)
const ballPos  = ref({ x: -1, y: -1 })
const ballSize = ref(160)
const chatBubbleRef = ref(null)
const activeScreen = ref({ width: 0, height: 0 })
let offToggle, offToken, offDone, offError, offSettings
const voiceActive = ref(false)
const siriMounted = ref(false)   // controls v-if (keeps DOM alive during fade-out)
const siriVisible = ref(false)   // controls CSS transition class
let siriHideTimer = null
let offVoiceStart, offVoiceEnd, offVoiceError
let pendingTokens = ''

// ── Apple Intelligence border ────────────────────────────────
// 4 canvas refs, one per layer (each gets its own CSS blur)
const siriCanvases = [ref(null), ref(null), ref(null), ref(null)]
let siriAnim = null

/**
 * Layer config — mirrors IOS.swift / WatchOS.swift:
 * each layer has independent interval + duration so they drift out of phase.
 * cssBlur is applied as CSS filter on the canvas element (reliable in all webviews).
 */
const LAYER_CONFIGS = [
  { interval: 500, duration: 1000, lineWidth: 30, cssBlur: 30, alpha: 0.65 }, // outer bloom
  { interval: 400, duration: 800,  lineWidth: 16, cssBlur: 14, alpha: 0.80 }, // mid glow
  { interval: 300, duration: 600,  lineWidth:  9, cssBlur:  6, alpha: 0.90 }, // tight glow
  { interval: 250, duration: 500,  lineWidth:  3, cssBlur:  0, alpha: 1.00 }, // sharp border
]

/** Apple Intelligence palette (from jacobamobin's implementation) */
const SIRI_COLORS = ['#BC82F3', '#F5B9EA', '#8D9FFF', '#FF6778', '#FFBA71', '#C686FF']

/** Parse hex to [r,g,b]. */
function hexToRgb(hex) {
  const n = parseInt(hex.replace('#', ''), 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

/** Lerp two hex colors. */
function lerpColor(c1, c2, t) {
  const [r1,g1,b1] = hexToRgb(c1), [r2,g2,b2] = hexToRgb(c2)
  return `rgb(${Math.round(r1+(r2-r1)*t)},${Math.round(g1+(g2-g1)*t)},${Math.round(b1+(b2-b1)*t)})`
}

/** easeInOut cubic. */
function ease(t) { return t < 0.5 ? 2*t*t : -1+(4-2*t)*t }

/**
 * Generate random gradient stops sorted by position —
 * mirrors GlowEffect.generateGradientStops() in IOS.swift.
 */
function randomStops() {
  return SIRI_COLORS
    .map(color => ({ color, pos: Math.random() }))
    .sort((a, b) => a.pos - b.pos)
}

/** Interpolate between two stop arrays. */
function interpStops(from, to, t) {
  return from.map((s, i) => ({
    color: lerpColor(s.color, to[i].color, t),
    pos: s.pos + (to[i].pos - s.pos) * t,
  }))
}

/** Draw a conic-gradient stroke on one canvas with global alpha for fade in/out. */
function drawStroke(canvas, stops, lineWidth, globalAlpha = 1) {
  const ctx = canvas.getContext('2d')
  const w = canvas.width, h = canvas.height
  ctx.clearRect(0, 0, w, h)
  if (globalAlpha <= 0) return

  const inset = lineWidth / 2
  const r = 12
  const x = inset, y = inset
  const bw = w - inset * 2, bh = h - inset * 2

  const grad = ctx.createConicGradient(0, w / 2, h / 2)
  stops.forEach(s => grad.addColorStop(Math.min(Math.max(s.pos, 0), 1), s.color))

  ctx.globalAlpha = globalAlpha
  ctx.lineWidth = lineWidth
  ctx.strokeStyle = grad
  ctx.beginPath()
  ctx.moveTo(x + r, y)
  ctx.lineTo(x + bw - r, y)
  ctx.arcTo(x + bw, y, x + bw, y + r, r)
  ctx.lineTo(x + bw, y + bh - r)
  ctx.arcTo(x + bw, y + bh, x + bw - r, y + bh, r)
  ctx.lineTo(x + r, y + bh)
  ctx.arcTo(x, y + bh, x, y + bh - r, r)
  ctx.lineTo(x, y + r)
  ctx.arcTo(x, y, x + r, y, r)
  ctx.closePath()
  ctx.stroke()
}

/** Start the animation — each layer morphs at its own pace. */
function startSiriAnim() {
  const canvases = siriCanvases.map(r => r.value)
  if (canvases.some(c => !c)) return

  const layers = LAYER_CONFIGS.map(cfg => ({
    cfg,
    current: randomStops(),
    target: randomStops(),
    phaseStart: null,
  }))

  function resize() {
    canvases.forEach(c => {
      c.width  = window.innerWidth
      c.height = window.innerHeight
    })
  }
  resize()
  window.addEventListener('resize', resize)

  function frame(ts) {
    // Smoothly step alpha toward target each frame
    siriAlpha += (siriAlphaTarget - siriAlpha) * 0.08
    if (Math.abs(siriAlpha - siriAlphaTarget) < 0.002) siriAlpha = siriAlphaTarget

    layers.forEach((lyr, i) => {
      const { cfg } = lyr
      if (lyr.phaseStart === null) lyr.phaseStart = ts
      const elapsed = ts - lyr.phaseStart
      let t = Math.min(elapsed / cfg.duration, 1)

      if (t >= 1 && elapsed >= cfg.duration + cfg.interval) {
        lyr.current    = lyr.target
        lyr.target     = randomStops()
        lyr.phaseStart = ts
        t = 0
      }

      const stops = interpStops(lyr.current, lyr.target, ease(Math.min(t, 1)))
      drawStroke(canvases[i], stops, cfg.lineWidth, siriAlpha)
    })
    siriAnim = requestAnimationFrame(frame)
  }

  siriAnim = requestAnimationFrame(frame)
  return () => {
    cancelAnimationFrame(siriAnim)
    siriAnim = null
    window.removeEventListener('resize', resize)
  }
}

let stopSiriAnim = null
// globalAlpha for canvas content: 0→1 on appear, 1→0 on disappear
let siriAlpha = 0
let siriAlphaTarget = 0
const SIRI_FADE_SPEED = 1 / 30  // ~30 frames to full opacity at 60fps

watch(voiceActive, async (active) => {
  if (active) {
    if (siriHideTimer) { clearTimeout(siriHideTimer); siriHideTimer = null }
    siriAlpha = 0
    siriAlphaTarget = 1
    siriMounted.value = true
    await nextTick()
    stopSiriAnim = startSiriAnim()
  } else {
    siriAlphaTarget = 0
    siriHideTimer = setTimeout(() => {
      stopSiriAnim?.()
      stopSiriAnim = null
      siriMounted.value = false
      siriHideTimer = null
    }, 600)
  }
})

/** waitForRuntime polls until the Wails Go bridge is available. */
async function waitForRuntime() {
  while (!window.go?.main?.App) {
    await new Promise(r => setTimeout(r, 20))
  }
}

onMounted(async () => {
  await waitForRuntime()
  try {
    const [w, h] = await GetScreenSize()
    if (w > 0 && h > 0) activeScreen.value = { width: w, height: h }
  } catch (e) {
    console.warn('App.vue: GetScreenSize failed', e)
  }
  const missing = await MissingRequiredConfig()
  const firstLaunch = await IsFirstLaunch()
  if (firstLaunch) {
    await MarkWelcomeShown()
    EventsEmit('notification:show', {
      title: '你好！我是你的桌面宠物 ✨',
      message: '请先在设置中配置 LLM 接口，然后就可以开始聊天了~',
    })
  }
  offToggle = EventsOn('bubble:toggle', () => {
    bubbleOpen.value = !bubbleOpen.value
    if (bubbleOpen.value) {
      pendingTokens = ''
      nextTick(() => {
        chatBubbleRef.value?.focusInput()
        chatBubbleRef.value?.scrollToBottom()
      })
    }
  })
  offSettings  = EventsOn('settings:open', () => { settingsOpen.value = true })
  offVoiceStart = EventsOn('voice:start', () => {
    if (!bubbleOpen.value) {
      bubbleOpen.value = true
      nextTick(() => {
        chatBubbleRef.value?.focusInput()
        chatBubbleRef.value?.scrollToBottom()
      })
    }
    voiceActive.value = true
  })
  offVoiceEnd   = EventsOn('voice:end',   () => { voiceActive.value = false })
  offVoiceError = EventsOn('voice:error', () => { voiceActive.value = false })
  EventsOn('screen:changed', (info) => {
    activeScreen.value = { width: info.width, height: info.height }
    EventsEmit('screen:active:changed', info)
  })
  offToken = EventsOn('chat:token', (token) => {
    if (!bubbleOpen.value) pendingTokens += token
  })
  offDone = EventsOn('chat:done', () => {
    if (!bubbleOpen.value && pendingTokens.trim()) {
      EventsEmit('notification:show', { title: '✨ (=^･ω･^=)', message: pendingTokens.trim() })
    }
    pendingTokens = ''
  })
  offError = EventsOn('chat:error', (err) => {
    if (!bubbleOpen.value) {
      pendingTokens = ''
      EventsEmit('notification:show', { title: '😿 出错了', message: err })
    }
  })
})

onUnmounted(() => {
  offToggle?.(); offToken?.(); offDone?.(); offError?.()
  offSettings?.(); offVoiceStart?.(); offVoiceEnd?.(); offVoiceError?.()
  stopSiriAnim?.()
  if (siriHideTimer) clearTimeout(siriHideTimer)
})

/** toggleBubble flips the chat bubble open/close state. */
function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
  if (bubbleOpen.value) {
    pendingTokens = ''
    nextTick(() => {
      chatBubbleRef.value?.focusInput()
      chatBubbleRef.value?.scrollToBottom()
    })
  }
}

/** openSettings opens the settings window. */
function openSettings() {
  settingsOpen.value = true
}
</script>

<template>
  <Live2DPet
    :active-screen="activeScreen"
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
    @open-settings="openSettings"
  />
  <ChatBubble
    ref="chatBubbleRef"
    v-show="bubbleOpen"
    :ball-pos="ballPos"
    :ball-size="ballSize"
    :active-screen="activeScreen"
    @close="bubbleOpen = false"
    @open-settings="openSettings"
  />
  <SettingsWindow
    v-if="settingsOpen"
    :active-screen="activeScreen"
    @close="settingsOpen = false"
  />
  <NotificationBubble
    :pet-pos="ballPos"
    :pet-size="ballSize"
  />

  <!--
    Apple Intelligence glow border — 4 canvas elements, each with its own CSS blur.
    CSS filter on the element itself is the most reliable blur in all WebViews.
    Each layer's gradient morphs independently (different interval/duration) → organic drift.
  -->
  <div v-if="siriMounted" class="siri-wrapper">
    <canvas :ref="siriCanvases[0]" class="siri-canvas" style="filter: blur(30px); opacity: 0.65;" />
    <canvas :ref="siriCanvases[1]" class="siri-canvas" style="filter: blur(14px); opacity: 0.80;" />
    <canvas :ref="siriCanvases[2]" class="siri-canvas" style="filter: blur(6px);  opacity: 0.90;" />
    <canvas :ref="siriCanvases[3]" class="siri-canvas" style="filter: blur(2px);  opacity: 1.0;" />
  </div>

  <div v-if="siriMounted" class="voice-wave">
    <div
      v-for="i in 3"
      :key="i"
      class="wave-ring"
      :style="{ animationDelay: `${(i - 1) * 0.55}s` }"
    />
    <div class="wave-core" />
  </div>
</template>

<style scoped>
/* ── Siri wrapper ───────────────────────────────────────────── */
.siri-wrapper {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 9998;
}

.siri-canvas {
  position: absolute;
  inset: 0;
  pointer-events: none;
}

/* ── Centered circular wave rings ──────────────────────────── */
.voice-wave {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  z-index: 9999;
}

.wave-core {
  position: absolute;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: radial-gradient(circle, #ffffff 0%, #BC82F3 60%, transparent 100%);
  box-shadow: 0 0 12px 4px rgba(188, 130, 243, 0.8),
              0 0 24px 8px rgba(141, 159, 255, 0.4);
  animation: core-pulse 1.6s ease-in-out infinite;
}

@keyframes core-pulse {
  0%, 100% { transform: scale(1);   opacity: 1; }
  50%       { transform: scale(1.3); opacity: 0.8; }
}

.wave-ring {
  position: absolute;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 1.5px solid rgba(188, 130, 243, 0.6);
  animation: wave-expand 1.65s cubic-bezier(0.2, 0.6, 0.4, 1) infinite;
}

.wave-ring:nth-child(2) { border-color: rgba(141, 159, 255, 0.5); }
.wave-ring:nth-child(3) { border-color: rgba(245, 185, 234, 0.4); }

@keyframes wave-expand {
  0%   { transform: scale(1);   opacity: 0.7; }
  100% { transform: scale(4.5); opacity: 0; }
}
</style>

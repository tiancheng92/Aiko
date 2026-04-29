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
let offToggle, offToken, offDone, offError, offSettings, offScreenChanged
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

// ── Water ripple canvas ──────────────────────────────────────
const rippleCanvas = ref(null)
let rippleAnim = null

/**
 * Ripple state — each ripple is spawned at the screen center,
 * expands outward, and fades as it grows.
 */
function startRippleAnim() {
  const canvas = rippleCanvas.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')

  function resize() {
    canvas.width  = window.innerWidth
    canvas.height = window.innerHeight
  }
  resize()
  window.addEventListener('resize', resize)

  const cx = () => canvas.width  / 2
  const cy = () => canvas.height / 2

  /**
   * Water refraction simulation on transparent canvas.
   * Each ring has a physical wave profile:
   *   - Soft fill (lens interior — slightly lighter, like refracted light)
   *   - Dark trough stroke just inside the crest (wave concave side)
   *   - Bright crest highlight stroke (wave convex peak)
   *   - Outer glow falloff
   * The dark trough is what creates depth/refraction illusion even on transparency.
   * 6 rings, non-uniform delays from reference CSS (.wave0–.wave5), 1s each.
   */
  const DURATION = 1000
  const RINGS = [
    { delay:    0, scale: 1.06 },
    { delay:  200, scale: 1.02 },
    { delay:  400, scale: 1.04 },
    { delay:  500, scale: 1.01 },
    { delay:  800, scale: 1.02 },
    { delay: 1000, scale: 1.00 },
  ]
  const PERIOD  = DURATION + 1000   // 2000ms cycle
  const START_R = 15
  const END_R   = 150

  let startTs = null

  function frame(ts) {
    if (!startTs) startTs = ts
    const elapsed = ts - startTs

    ctx.clearRect(0, 0, canvas.width, canvas.height)
    ctx.save()
    ctx.globalAlpha = siriAlpha

    const cycle = elapsed % PERIOD

    // Draw back-to-front (wave5 first, wave0 last = on top)
    for (let i = RINGS.length - 1; i >= 0; i--) {
      const ring = RINGS[i]
      const age  = cycle - ring.delay
      if (age < 0 || age > DURATION) continue

      const t = age / DURATION
      const r = START_R + t * (END_R - START_R)
      const fade = 1 - t   // opacity 1→0 (opac keyframe)

      // ── 1. Lens interior fill ─────────────────────────────────
      ctx.save()
      ctx.beginPath()
      ctx.arc(cx(), cy(), r, 0, Math.PI * 2)
      ctx.clip()
      const gradR = Math.min(canvas.width, canvas.height) * 0.5 * ring.scale
      const lensGrad = ctx.createRadialGradient(cx(), cy(), 0, cx(), cy(), gradR)
      lensGrad.addColorStop(0.0, `rgba(195,228,255,${0.10 * fade})`)
      lensGrad.addColorStop(0.6, `rgba(195,228,255,${0.04 * fade})`)
      lensGrad.addColorStop(1.0, 'rgba(195,228,255,0.00)')
      ctx.fillStyle = lensGrad
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      ctx.restore()

      // ── 2. Dark trough — inner shadow ────────────────────────
      ctx.beginPath()
      ctx.arc(cx(), cy(), Math.max(1, r - 2), 0, Math.PI * 2)
      ctx.strokeStyle = `rgba(10,30,60,${0.22 * fade})`
      ctx.lineWidth = 4
      ctx.stroke()

      // ── 3. Bright crest highlight ────────────────────────────
      ctx.beginPath()
      ctx.arc(cx(), cy(), r, 0, Math.PI * 2)
      ctx.strokeStyle = `rgba(220,240,255,${0.85 * fade})`
      ctx.lineWidth = 1.2
      ctx.stroke()

      // ── 4. Outer glow falloff ────────────────────────────────
      const outerGrad = ctx.createRadialGradient(cx(), cy(), r, cx(), cy(), r + 10)
      outerGrad.addColorStop(0, `rgba(195,228,255,${0.18 * fade})`)
      outerGrad.addColorStop(1, 'rgba(195,228,255,0.00)')
      ctx.beginPath()
      ctx.arc(cx(), cy(), r + 10, 0, Math.PI * 2)
      ctx.fillStyle = outerGrad
      ctx.fill()
    }

    ctx.restore()
    rippleAnim = requestAnimationFrame(frame)
  }

  rippleAnim = requestAnimationFrame(frame)
  return () => {
    cancelAnimationFrame(rippleAnim)
    rippleAnim = null
    window.removeEventListener('resize', resize)
  }
}

let stopRippleAnim = null

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
    stopSiriAnim  = startSiriAnim()
    stopRippleAnim = startRippleAnim()
  } else {
    siriAlphaTarget = 0
    siriHideTimer = setTimeout(() => {
      stopSiriAnim?.()
      stopRippleAnim?.()
      stopSiriAnim  = null
      stopRippleAnim = null
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
  offScreenChanged = EventsOn('screen:changed', (info) => {
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
  offScreenChanged?.()
  stopSiriAnim?.()
  stopRippleAnim?.()
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
  <Transition name="chat-pop">
    <ChatBubble
      ref="chatBubbleRef"
      v-show="bubbleOpen"
      :ball-pos="ballPos"
      :ball-size="ballSize"
      :active-screen="activeScreen"
      @close="bubbleOpen = false"
      @open-settings="openSettings"
    />
  </Transition>
  <Transition name="settings-pop">
    <SettingsWindow
      v-if="settingsOpen"
      :active-screen="activeScreen"
      @close="settingsOpen = false"
    />
  </Transition>
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

  <!-- Water ripple canvas — realistic radial ripples drawn per-frame -->
  <canvas v-if="siriMounted" ref="rippleCanvas" class="ripple-canvas" />
</template>

<style scoped>
/* ── Chat bubble appear / disappear ────────────────────────── */
.chat-pop-enter-active,
.chat-pop-leave-active {
  transform-origin: bottom center;
}
.chat-pop-enter-active {
  transition: opacity 0.28s cubic-bezier(0.34, 1.56, 0.64, 1),
              transform 0.28s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.chat-pop-leave-active {
  transition: opacity 0.18s ease-in,
              transform 0.18s ease-in;
}
.chat-pop-enter-from,
.chat-pop-leave-to {
  opacity: 0;
  transform: scale(0.88) translateY(14px);
}

/* ── Settings window appear / disappear ────────────────────── */
.settings-pop-enter-active,
.settings-pop-leave-active {
  transform-origin: center center;
}
.settings-pop-enter-active {
  transition: opacity 0.25s cubic-bezier(0.34, 1.56, 0.64, 1),
              transform 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.settings-pop-leave-active {
  transition: opacity 0.16s ease-in,
              transform 0.16s ease-in;
}
.settings-pop-enter-from,
.settings-pop-leave-to {
  opacity: 0;
  transform: scale(0.92);
}

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

/* ── Water ripple canvas ─────────────────────────────────────── */
.ripple-canvas {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 9999;
}
</style>

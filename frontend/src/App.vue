<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick, watch, defineAsyncComponent } from 'vue'
const Live2DPet = defineAsyncComponent(() => import('./components/Live2DPet.vue'))
const VRMPet = defineAsyncComponent(() => import('./components/VRMPet.vue'))
import ChatBubble from './components/ChatBubble.vue'
const SettingsWindow = defineAsyncComponent(() => import('./components/SettingsWindow.vue'))
import NotificationBubble from './components/NotificationBubble.vue'
import PomodoroPanel from './components/PomodoroPanel.vue'
import SystemResourcePanel from './components/SystemResourcePanel.vue'
import ClaudeStatusPanel from './components/ClaudeStatusPanel.vue'
import { MissingRequiredConfig, IsFirstLaunch, MarkWelcomeShown, GetScreenSize, GetConfig, SetChatVisible } from '../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime'
import { springAnimate } from './composables/useSpring'
import { useI18n } from 'vue-i18n'

const bubbleOpen = ref(false)
watch(bubbleOpen, (v) => { SetChatVisible(v) })
const { t } = useI18n()
const renderBackend = ref('live2d')
const settingsOpen = ref(false)
const pomodoroPanelOpen = ref(false)
const pomodoroRunning = ref(false)
const pomodoroPanelRef = ref(null)
const pomodoroPanelWasOpen = ref(false)
const claudePanelOpen = ref(false)
const claudePanelRef = ref(null)
const claudePanelWasOpen = ref(false)
const systemPanelOpen = ref(false)
const systemPanelRef = ref(null)
const systemPanelWasOpen = ref(false)
const ballPos  = ref({ x: -1, y: -1 })
const ballSize = ref(160)
const chatBubbleRef = ref(null)
const activeScreen = ref({ width: 0, height: 0 })
let offToggle, offToken, offDone, offError, offSettings, offScreenChanged, offRenderBackend, offPomodoroState = null
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
  try {
    const cfg = await GetConfig()
    if (cfg?.RenderBackend) renderBackend.value = cfg.RenderBackend
  } catch (e) {
    console.warn('App.vue: GetConfig failed', e)
  }
  const missing = await MissingRequiredConfig()
  const firstLaunch = await IsFirstLaunch()
  if (firstLaunch) {
    await MarkWelcomeShown()
    EventsEmit('notification:show', {
      title: t('app.welcomeTitle'),
      message: t('app.welcomeMessage'),
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
  offRenderBackend = EventsOn('config:render:backend:changed', (backend) => {
    renderBackend.value = backend
  })
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
      EventsEmit('notification:show', { title: t('app.pendingNotification'), message: pendingTokens.trim() })
    }
    pendingTokens = ''
  })
  offError = EventsOn('chat:error', (err) => {
    if (!bubbleOpen.value) {
      pendingTokens = ''
      EventsEmit('notification:show', { title: t('app.errorTitle'), message: err })
    }
  })
  offPomodoroState = EventsOn('pomodoro:state:changed', onPomodoroStateChanged)
})

onUnmounted(() => {
  offToggle?.(); offToken?.(); offDone?.(); offError?.()
  offSettings?.(); offVoiceStart?.(); offVoiceEnd?.(); offVoiceError?.()
  offScreenChanged?.(); offRenderBackend?.(); offPomodoroState?.()
  stopSiriAnim?.()
  stopRippleAnim?.()
  if (siriHideTimer) clearTimeout(siriHideTimer)
  cancelBubble?.()
  cancelSettings?.()
})

/** toggleBubble flips the chat bubble open/close state. */
function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
  if (bubbleOpen.value) {
    // Hide floating panels when opening chat.
    if (pomodoroPanelOpen.value) {
      pomodoroPanelWasOpen.value = true
      pomodoroPanelOpen.value = false
    }
    if (claudePanelOpen.value) {
      claudePanelWasOpen.value = true
      claudePanelOpen.value = false
    }
    if (systemPanelOpen.value) {
      systemPanelWasOpen.value = true
      systemPanelOpen.value = false
    }
    pendingTokens = ''
    nextTick(() => {
      chatBubbleRef.value?.focusInput()
      chatBubbleRef.value?.scrollToBottom()
    })
  } else {
    // Restore panels when closing chat.
    if (pomodoroPanelWasOpen.value) {
      pomodoroPanelWasOpen.value = false
      pomodoroPanelOpen.value = true
      nextTick(() => { pomodoroPanelRef.value?.show() })
    }
    if (claudePanelWasOpen.value) {
      claudePanelWasOpen.value = false
      claudePanelOpen.value = true
      nextTick(() => { claudePanelRef.value?.show() })
    }
    if (systemPanelWasOpen.value) {
      systemPanelWasOpen.value = false
      systemPanelOpen.value = true
      nextTick(() => { systemPanelRef.value?.show() })
    }
  }
}

/** openPomodoro toggles the pomodoro panel visibility. */
function openPomodoro() {
  if (pomodoroPanelOpen.value) {
    pomodoroPanelOpen.value = false
    return
  }
  pomodoroPanelOpen.value = true
  pomodoroPanelWasOpen.value = false
  nextTick(() => {
    pomodoroPanelRef.value?.show()
  })
}

function closePomodoro() {
  pomodoroPanelOpen.value = false
}

/** closeClaudePanel hides the Claude Code status panel. */
function closeClaudePanel() {
  claudePanelOpen.value = false
}

/** toggleClaudePanel toggles the Claude Code status panel visibility. */
function toggleClaudePanel() {
  if (claudePanelOpen.value) {
    claudePanelOpen.value = false
    return
  }
  claudePanelOpen.value = true
  claudePanelWasOpen.value = false
  nextTick(() => {
    claudePanelRef.value?.show()
  })
}

/** toggleSystemPanel toggles the system resource panel visibility. */
function toggleSystemPanel() {
  if (systemPanelOpen.value) {
    systemPanelOpen.value = false
    return
  }
  systemPanelOpen.value = true
  systemPanelWasOpen.value = false
  nextTick(() => {
    systemPanelRef.value?.show()
  })
}

function closeSystemPanel() {
  systemPanelOpen.value = false
}

const panelStackWidth = 280
const panelStackStyle = computed(() => {
  const x = ballPos.value.x - panelStackWidth + 50
  const petBottom = ballPos.value.y + ballSize.value
  const bottom = window.innerHeight - petBottom
  const clampedX = Math.min(Math.max(x, 8), window.innerWidth - panelStackWidth - 8)
  // Clamp bottom so the stack doesn't overflow past the menu bar when pet is high up.
  const maxBottom = window.innerHeight - 38
  const clampedBottom = Math.min(bottom, maxBottom)
  return {
    left: `${clampedX}px`,
    bottom: `${clampedBottom}px`,
    width: `${panelStackWidth}px`,
    maxHeight: `${Math.max(0, petBottom - 38)}px`,
  }
})

function onPomodoroStateChanged(payload) {
  pomodoroRunning.value = payload.state === 'running'
}

/** openSettings opens the settings window. */
function openSettings() {
  settingsOpen.value = true
}

// ── Spring transition helpers ─────────────────────────────────────────────────
// Shared options for normalized progress [0..1] space.
const SPRING_OPTS = { restDelta: 0.005, restVelocity: 0.04 }

// In-flight cancel fns so rapid open→close→open doesn't stack animations.
let cancelBubble = null
let cancelSettings = null

/** applyBubbleStyle writes transform + opacity from a spring progress value p ∈ [0..1].
 *  transform-origin is bottom-center so the bubble grows from the pet position. */
function applyBubbleStyle(el, p) {
  el.style.opacity        = Math.min(1, Math.max(0, p * 2)).toString()
  el.style.transform      = `scale(${0.82 + 0.18 * p}) translateY(${20 * (1 - p)}px)`
  el.style.transformOrigin = 'bottom center'
}

/** applySettingsStyle writes transform + opacity from spring progress p ∈ [0..1]. */
function applySettingsStyle(el, p) {
  el.style.opacity   = Math.min(1, Math.max(0, p * 1.8)).toString()
  el.style.transform = `scale(${0.90 + 0.10 * p}) translateY(${10 * (1 - p)}px)`
}

// ── ChatBubble JS transition hooks ───────────────────────────────────────────
/** onBubbleEnter: spring from 0 → 1, underdamped (ζ ≈ 0.67) for gentle overshoot. */
function onBubbleEnter(el, done) {
  cancelBubble?.()
  applyBubbleStyle(el, 0)
  // ζ = 22/(2·√320) ≈ 0.614 — clear underdamping; scale briefly exceeds 1 by ~8%.
  cancelBubble = springAnimate({
    from: 0, to: 1,
    stiffness: 320, damping: 22,
    ...SPRING_OPTS,
    onUpdate: (p) => applyBubbleStyle(el, p),
    onDone: () => {
      el.style.opacity = ''
      el.style.transform = ''
      el.style.transformOrigin = ''
      cancelBubble = null
      done()
    },
  })
}

/** onBubbleLeave: spring from 1 → 0, near-critical (ζ ≈ 1.0) for decisive close. */
function onBubbleLeave(el, done) {
  cancelBubble?.()
  applyBubbleStyle(el, 1)
  // ζ = 36/(2·√320) ≈ 1.006 — near-critical: clean stop, no bounce.
  cancelBubble = springAnimate({
    from: 1, to: 0,
    stiffness: 320, damping: 36,
    ...SPRING_OPTS,
    onUpdate: (p) => applyBubbleStyle(el, p),
    onDone: () => {
      cancelBubble = null
      done() // Vue applies display:none
    },
  })
}

// ── SettingsWindow JS transition hooks ───────────────────────────────────────
/** onSettingsEnter: spring from 0 → 1, slightly underdamped (ζ ≈ 0.72) — refined pop. */
function onSettingsEnter(el, done) {
  cancelSettings?.()
  applySettingsStyle(el, 0)
  // ζ = 26/(2·√260) ≈ 0.806 — subtle overshoot on scale for a glass-card feel.
  cancelSettings = springAnimate({
    from: 0, to: 1,
    stiffness: 260, damping: 26,
    ...SPRING_OPTS,
    onUpdate: (p) => applySettingsStyle(el, p),
    onDone: () => {
      el.style.opacity = ''
      el.style.transform = ''
      cancelSettings = null
      done()
    },
  })
}

/** onSettingsLeave: spring from 1 → 0, overdamped (ζ ≈ 1.1) — fast, authoritative close. */
function onSettingsLeave(el, done) {
  cancelSettings?.()
  applySettingsStyle(el, 1)
  // ζ = 40/(2·√260) ≈ 1.24 — slightly overdamped: snappy close without any bounce.
  cancelSettings = springAnimate({
    from: 1, to: 0,
    stiffness: 260, damping: 40,
    ...SPRING_OPTS,
    onUpdate: (p) => applySettingsStyle(el, p),
    onDone: () => {
      cancelSettings = null
      done()
    },
  })
}
</script>

<template>
  <Live2DPet
    v-if="renderBackend === 'live2d'"
    :active-screen="activeScreen"
    :pomodoro-panel-open="pomodoroPanelOpen"
    :claude-panel-open="claudePanelOpen"
    :system-panel-open="systemPanelOpen"
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
    @open-settings="openSettings"
    @open-pomodoro="openPomodoro"
    @toggle-system-panel="toggleSystemPanel"
    @toggle-claude-panel="toggleClaudePanel"
  />
  <VRMPet
    v-else-if="renderBackend === 'vrm'"
    :active-screen="activeScreen"
    :pomodoro-panel-open="pomodoroPanelOpen"
    :claude-panel-open="claudePanelOpen"
    :system-panel-open="systemPanelOpen"
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
    @open-settings="openSettings"
    @open-pomodoro="openPomodoro"
    @toggle-system-panel="toggleSystemPanel"
    @toggle-claude-panel="toggleClaudePanel"
  />
  <Transition :css="false" @enter="onBubbleEnter" @leave="onBubbleLeave">
    <ChatBubble
      ref="chatBubbleRef"
      v-show="bubbleOpen"
      :visible="bubbleOpen"
      :ball-pos="ballPos"
      :ball-size="ballSize"
      :active-screen="activeScreen"
      @close="bubbleOpen = false"
      @open-settings="openSettings"
    />
  </Transition>
  <Transition :css="false" @enter="onSettingsEnter" @leave="onSettingsLeave">
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
  <!-- Panel stack: pomodoro → claude status → system resource, bottom-aligned flex column -->
  <Teleport to="body">
    <div v-if="pomodoroPanelOpen || claudePanelOpen || systemPanelOpen" class="panel-stack" :style="panelStackStyle">
      <PomodoroPanel
        v-if="pomodoroPanelOpen"
        ref="pomodoroPanelRef"
        @close="closePomodoro"
      />
      <ClaudeStatusPanel
        v-if="claudePanelOpen"
        ref="claudePanelRef"
        @close="closeClaudePanel"
      />
      <SystemResourcePanel
        v-if="systemPanelOpen"
        ref="systemPanelRef"
        @close="closeSystemPanel"
      />
    </div>
  </Teleport>

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

/* ── Panel stack: pomodoro → claude → system, bottom-aligned flex column */
.panel-stack {
  position: fixed;
  z-index: 99996;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  justify-content: flex-end;
  gap: 8px;
}

/* ── Water ripple canvas ─────────────────────────────────────── */
.ripple-canvas {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 9999;
}
</style>

<template>
    <Transition
      :css="false"
      @enter="onEnter"
      @leave="onLeave"
    >
      <div
        v-if="visible"
        class="pomodoro-panel"
      >
        <div class="pomo-titlebar">
          <span class="pomo-titlebar-label">番茄钟</span>
        </div>
        <div class="pomo-body">
        <!-- Circular progress ring -->
        <div class="timer-ring" :style="ringStyle">
          <div class="timer-inner">
            <div class="timer-time">{{ formattedTime }}</div>
            <div class="timer-label" :style="{ color: phaseColor }">{{ phaseLabel }}</div>
          </div>
        </div>

        <!-- Right side: info + buttons -->
        <div class="timer-info">
          <div class="round-info">
            <span class="round-text">{{ $t('pomodoro.round', { current: round, total: totalRounds }) }}</span>
            <div class="round-dots">
              <span
                v-for="i in totalRounds"
                :key="i"
                class="dot"
                :class="{ done: i < round, current: i === round && state === 'running' }"
                :style="i === round && state === 'running' ? { background: phaseColor, boxShadow: `0 0 6px ${phaseColor}` } : {}"
              />
            </div>
          </div>

          <div class="timer-actions">
            <button
              v-if="state === 'idle'"
              type="button"
              class="pomo-btn primary start-btn"
              @click="onStart"
            >{{ $t('pomodoro.start') }}</button>
            <button
              v-if="state === 'running'"
              type="button"
              class="pomo-btn primary pause-btn"
              @click="onPause"
            >{{ $t('pomodoro.pause') }}</button>
            <button
              v-if="state === 'paused'"
              type="button"
              class="pomo-btn primary resume-btn"
              @click="onResume"
            >{{ $t('pomodoro.resume') }}</button>
            <button
              v-if="state !== 'idle'"
              type="button"
              class="pomo-btn secondary"
              @click="onStop"
            >{{ $t('pomodoro.stop') }}</button>
          </div>
        </div>
        </div>
      </div>
    </Transition>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  StartPomodoro,
  PausePomodoro,
  ResumePomodoro,
  StopPomodoro,
  GetPomodoroStatus,
} from '../../wailsjs/go/main/App'
import { springAnimate } from '../composables/useSpring'

const { t } = useI18n()
const emit = defineEmits(['close'])

const visible = ref(false)
const state = ref('idle')
const phase = ref('focus')
const remaining = ref(0)
const round = ref(1)
const totalRounds = ref(4)
const totalDuration = ref(25 * 60)

let offTick = null
let offPhaseChanged = null
let offStateChanged = null
let cancelAnim = null

// ── computed ──

const formattedTime = computed(() => {
  const m = Math.floor(remaining.value / 60)
  const s = remaining.value % 60
  return `${m}:${String(s).padStart(2, '0')}`
})

const phaseLabel = computed(() => {
  switch (phase.value) {
    case 'focus': return t('pomodoro.focus')
    case 'short_break': return t('pomodoro.shortBreak')
    case 'long_break': return t('pomodoro.longBreak')
    case 'done': return t('pomodoro.done')
    default: return ''
  }
})

const phaseColor = computed(() => {
  switch (phase.value) {
    case 'focus': return '#EF4444'
    case 'short_break': return '#10B981'
    case 'long_break': return '#60A5FA'
    default: return '#EF4444'
  }
})

const progress = computed(() => {
  if (totalDuration.value <= 0) return 0
  return 1 - (remaining.value / totalDuration.value)
})

const ringStyle = computed(() => ({
  background: `conic-gradient(${phaseColor.value} ${progress.value * 360}deg, rgba(255,255,255,0.06) ${progress.value * 360}deg)`,
  boxShadow: `0 0 16px ${phaseColor.value}33, inset 0 0 16px ${phaseColor.value}1a`,
}))

// ── methods ──

async function onStart() {
  try { await StartPomodoro() } catch (e) { console.error('pomodoro start failed:', e) }
}

async function onPause() {
  try { await PausePomodoro() } catch (e) { console.error('pomodoro pause failed:', e) }
}

async function onResume() {
  try { await ResumePomodoro() } catch (e) { console.error('pomodoro resume failed:', e) }
}

async function onStop() {
  try { await StopPomodoro() } catch (e) { console.error('pomodoro stop failed:', e) }
  visible.value = false
  emit('close')
}

function show() {
  visible.value = true
  GetPomodoroStatus().then((st) => {
    state.value = st.state
    phase.value = st.phase
    remaining.value = st.remaining
    round.value = st.currentRound
    totalRounds.value = st.totalRounds
    totalDuration.value = phaseDuration(st.phase, st.config)
  })
}

function phaseDuration(p, cfg) {
  switch (p) {
    case 'focus': return (cfg.FocusDuration || cfg.focusDuration || 25) * 60
    case 'short_break': return (cfg.ShortBreakDuration || cfg.shortBreakDuration || 5) * 60
    case 'long_break': return (cfg.LongBreakDuration || cfg.longBreakDuration || 15) * 60
    default: return 25 * 60
  }
}

// ── animation ──

const prefersReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches

function onEnter(el, done) {
  if (prefersReduced) {
    el.style.opacity = '1'
    done()
    return
  }
  cancelAnim = springAnimate({
    from: 0,
    to: 1,
    stiffness: 320,
    damping: 22,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { done() },
  })
}

function onLeave(el, done) {
  if (prefersReduced) {
    el.style.opacity = '0'
    done()
    return
  }
  cancelAnim = springAnimate({
    from: 1,
    to: 0,
    stiffness: 400,
    damping: 36,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { done() },
  })
}

// ── events ──

onMounted(() => {
  offTick = EventsOn('pomodoro:tick', (p) => {
    remaining.value = p.remaining
    phase.value = p.phase
    round.value = p.round
  })
  offStateChanged = EventsOn('pomodoro:state:changed', (p) => {
    state.value = p.state
  })
  offPhaseChanged = EventsOn('pomodoro:phase:changed', (p) => {
    phase.value = p.phase
    GetPomodoroStatus().then((st) => {
      totalDuration.value = phaseDuration(p.phase, st.config)
    })
  })
})

onUnmounted(() => {
  offTick?.()
  offStateChanged?.()
  offPhaseChanged?.()
  cancelAnim?.()
})

defineExpose({ show })
</script>

<style scoped>
.pomodoro-panel {
  width: 100%;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 14px;
  background: var(--lg-surface-elevated);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border);
  border-radius: 10px;
  box-shadow:
    0 8px 24px rgba(0, 0, 0, 0.45),
    0 0 0 0.5px rgba(0, 0, 0, 0.25),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", sans-serif;
  -webkit-font-smoothing: antialiased;
  user-select: none;
  color: var(--text-primary);
}

.pomo-body {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
}

.pomo-titlebar {
  flex-basis: 100%;
  display: flex;
  align-items: center;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--lg-border-subtle);
}

.pomo-titlebar-label {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

/* ── Ring ── */

.timer-ring {
  width: 90px;
  height: 90px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: background 0.3s ease, box-shadow 0.3s ease;
  align-self: center;
}

.timer-inner {
  width: 76px;
  height: 76px;
  border-radius: 50%;
  background: #0F172A;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.timer-time {
  font-size: 22px;
  font-weight: 700;
  color: #fff;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
  line-height: 1;
}

.timer-label {
  font-size: 11px;
  font-weight: 500;
  margin-top: 2px;
  letter-spacing: 0.02em;
}

/* ── Info ── */

.timer-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.round-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.round-text {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
}

.round-dots {
  display: flex;
  gap: 5px;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  transition: background 0.3s ease, box-shadow 0.3s ease;
}

.dot.done {
  background: rgba(255, 255, 255, 0.25);
}

/* ── Buttons ── */

.timer-actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.pomo-btn {
  width: 84px;
  padding: 7px 14px;
  border: none;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  letter-spacing: 0.01em;
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.pomo-btn:hover {
  opacity: 0.88;
}

.pomo-btn:active {
  transform: scale(0.96);
}

.pomo-btn.primary {
  color: #fff;
}

.pomo-btn.secondary {
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.7);
}

.pomo-btn.secondary:hover {
  background: rgba(255, 255, 255, 0.12);
  color: rgba(255, 255, 255, 0.9);
}

.pomo-btn.start-btn {
  background: #EF4444;
}

.pomo-btn.pause-btn {
  background: rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.9);
}

.pomo-btn.resume-btn {
  background: #10B981;
}

/* ── Reduced motion ── */

@media (prefers-reduced-motion: reduce) {
  .timer-ring,
  .dot {
    transition: none;
  }

  .pomo-btn {
    transition: none;
  }
}
</style>

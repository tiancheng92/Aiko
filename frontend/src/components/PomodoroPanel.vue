<template>
  <Teleport to="body">
    <Transition
      :css="false"
      @enter="onEnter"
      @leave="onLeave"
    >
      <div
        v-if="visible"
        class="pomodoro-panel"
        :style="panelStyle"
        @mousedown.stop
      >
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
              />
            </div>
          </div>

          <div class="timer-actions">
            <button
              v-if="state === 'idle'"
              class="pomo-btn start"
              @click="onStart"
            >{{ $t('pomodoro.start') }}</button>
            <button
              v-if="state === 'running'"
              class="pomo-btn pause"
              @click="onPause"
            >{{ $t('pomodoro.pause') }}</button>
            <button
              v-if="state === 'paused'"
              class="pomo-btn resume"
              @click="onResume"
            >{{ $t('pomodoro.resume') }}</button>
            <button
              v-if="state !== 'idle'"
              class="pomo-btn stop"
              @click="onStop"
            >{{ $t('pomodoro.stop') }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
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

const props = defineProps({
  petPos: { type: Object, required: true },
  petSize: { type: Number, default: 160 },
})

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
    case 'focus': return '#ff6b6b'
    case 'short_break': return '#51cf66'
    case 'long_break': return '#339af0'
    default: return '#ff6b6b'
  }
})

const progress = computed(() => {
  if (totalDuration.value <= 0) return 0
  return 1 - (remaining.value / totalDuration.value)
})

const ringStyle = computed(() => ({
  background: `conic-gradient(${phaseColor.value} ${progress.value * 360}deg, #333 ${progress.value * 360}deg)`,
}))

const panelWidth = 230 // approximate panel width in px
const panelHeight = 140 // approximate panel height in px

const panelStyle = computed(() => {
  const x = props.petPos.x + props.petSize / 2
  const y = props.petPos.y - panelHeight - 8
  const halfW = panelWidth / 2
  const clampedX = Math.min(Math.max(x, halfW + 8), window.innerWidth - halfW - 8)
  const clampedY = Math.min(Math.max(y, 38), window.innerHeight - panelHeight - 8)
  return {
    left: `${clampedX}px`,
    top: `${clampedY}px`,
    transform: 'translate(-50%, 0)',
  }
})

// ── methods ──

async function onStart() {
  await StartPomodoro()
}

async function onPause() {
  await PausePomodoro()
}

async function onResume() {
  await ResumePomodoro()
}

async function onStop() {
  await StopPomodoro()
  visible.value = false
  emit('close')
}

/**
 * Show the panel and fetch current status from the backend.
 */
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

function onEnter(el, done) {
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
  position: fixed;
  z-index: 2001;
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 18px 22px;
  background: rgba(26, 26, 46, 0.88);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-radius: 18px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
  user-select: none;
}

.timer-ring {
  width: 100px;
  height: 100px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.timer-inner {
  width: 86px;
  height: 86px;
  border-radius: 50%;
  background: #1a1a2e;
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
  line-height: 1;
}

.timer-label {
  font-size: 10px;
  margin-top: 2px;
}

.timer-info {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.round-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.round-text {
  font-size: 12px;
  color: #aaa;
}

.round-dots {
  display: flex;
  gap: 4px;
}

.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #444;
}

.dot.done {
  background: var(--phase-color, #ff6b6b);
}

.dot.current {
  background: var(--phase-color, #ff6b6b);
  box-shadow: 0 0 6px var(--phase-color, #ff6b6b);
}

.timer-actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.pomo-btn {
  width: 72px;
  padding: 6px 12px;
  border: none;
  border-radius: 8px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  color: #fff;
  transition: opacity 0.15s;
}

.pomo-btn:hover {
  opacity: 0.85;
}

.pomo-btn.start {
  background: #ff6b6b;
}

.pomo-btn.pause {
  background: rgba(255, 255, 255, 0.12);
}

.pomo-btn.resume {
  background: #51cf66;
}

.pomo-btn.stop {
  background: rgba(255, 255, 255, 0.08);
}
</style>

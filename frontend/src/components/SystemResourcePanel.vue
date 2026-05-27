<template>
  <Teleport to="body">
    <Transition
      :css="false"
      @enter="onEnter"
      @leave="onLeave"
    >
      <div
        v-if="visible"
        class="system-panel"
        :style="panelStyle"
      >
        <!-- CPU -->
        <div class="stat-row">
          <div class="stat-label-label">{{ $t('system.cpu') }}</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{ width: cpu + '%', background: barColor(cpu) }"
              />
            </div>
          </div>
          <div class="stat-value" :style="{ color: barColor(cpu) }">{{ cpu.toFixed(0) }}%</div>
        </div>

        <!-- Memory -->
        <div class="stat-row">
          <div class="stat-label-label">{{ $t('system.memory') }}</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{ width: memory.percent + '%', background: barColor(memory.percent) }"
              />
            </div>
            <div class="stat-detail">{{ $t('system.usedTotal', { used: formatBytes(memory.used), total: formatBytes(memory.total) }) }}</div>
          </div>
          <div class="stat-value" :style="{ color: barColor(memory.percent) }">{{ memory.percent.toFixed(0) }}%</div>
        </div>

        <!-- Disk -->
        <div class="stat-row">
          <div class="stat-label-label">{{ $t('system.disk') }}</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{ width: disk.percent + '%', background: barColor(disk.percent) }"
              />
            </div>
            <div class="stat-detail">{{ $t('system.usedTotal', { used: formatBytes(disk.used), total: formatBytes(disk.total) }) }}</div>
          </div>
          <div class="stat-value" :style="{ color: barColor(disk.percent) }">{{ disk.percent.toFixed(0) }}%</div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { GetSystemStats } from '../../wailsjs/go/main/App'
import { springAnimate } from '../composables/useSpring'

const props = defineProps({
  petPos: { type: Object, required: true },
  petSize: { type: Number, default: 160 },
})

const { t } = useI18n()

const visible = ref(false)
const cpu = ref(0)
const memory = ref({ used: 0, total: 0, percent: 0 })
const disk = ref({ used: 0, total: 0, percent: 0 })

let offStatsUpdate = null
let cancelAnim = null

const panelWidth = 170

const panelStyle = computed(() => {
  const x = props.petPos.x - panelWidth - 6
  const y = props.petPos.y + props.petSize / 2
  const clampedX = Math.min(Math.max(x, 8), window.innerWidth - panelWidth - 8)
  const clampedY = Math.min(Math.max(y - 54, 38), window.innerHeight - 108 - 8)
  return {
    left: `${clampedX}px`,
    top: `${clampedY}px`,
  }
})

/**
 * formatBytes converts a byte count to a human-readable string (e.g. "8.5G", "256M").
 */
function formatBytes(bytes) {
  if (!bytes || bytes === 0) return '0'
  if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + 'G'
  if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + 'M'
  if (bytes >= 1024) return (bytes / 1024).toFixed(0) + 'K'
  return bytes + 'B'
}

function barColor(pct) {
  if (pct >= 90) return '#EF4444'
  if (pct >= 70) return '#F59E0B'
  return '#10B981'
}

function show() {
  visible.value = true
  GetSystemStats().then((st) => {
    cpu.value = st.cpu
    memory.value = st.memory
    disk.value = st.disk
  }).catch(() => {})
}

const prefersReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches

function onEnter(el, done) {
  if (prefersReduced) { el.style.opacity = '1'; done(); return }
  cancelAnim = springAnimate({
    from: 0, to: 1, stiffness: 320, damping: 22,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { done() },
  })
}

function onLeave(el, done) {
  if (prefersReduced) { el.style.opacity = '0'; done(); return }
  cancelAnim = springAnimate({
    from: 1, to: 0, stiffness: 400, damping: 36,
    onUpdate: (v) => { el.style.opacity = v; el.style.scale = 0.8 + 0.2 * v },
    onComplete: () => { done() },
  })
}

onMounted(() => {
  show()
  offStatsUpdate = EventsOn('stats:update', (st) => {
    cpu.value = st.cpu
    memory.value = st.memory
    disk.value = st.disk
  })
})

onUnmounted(() => {
  offStatsUpdate?.()
  cancelAnim?.()
})

defineExpose({ show })
</script>

<style scoped>
.system-panel {
  position: fixed;
  z-index: 2001;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 14px;
  background: rgba(15, 23, 42, 0.92);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border-radius: 14px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
  user-select: none;
}

.stat-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.stat-label-label {
  width: 28px;
  font-size: 10px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.5);
  flex-shrink: 0;
  text-align: right;
}

.stat-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.stat-bar-track {
  width: 100%;
  height: 4px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 2px;
  overflow: hidden;
}

.stat-bar-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.4s ease, background 0.3s ease;
}

.stat-detail {
  font-size: 9px;
  color: rgba(255, 255, 255, 0.35);
  line-height: 1;
}

.stat-value {
  width: 34px;
  font-size: 11px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  text-align: right;
  flex-shrink: 0;
}
</style>

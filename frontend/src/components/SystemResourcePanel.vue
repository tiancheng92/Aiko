<template>
  <Teleport to="body">
    <Transition :css="false" @enter="onEnter" @leave="onLeave">
      <div v-if="visible" ref="panelRef" class="system-panel" :style="panelStyle">
        <!-- CPU -->
        <div class="stat-row">
          <div class="stat-label-label">{{ $t("system.cpu") }}</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{ width: cpu + '%', background: barColor(cpu) }"
              />
            </div>
            <div class="stat-detail">{{ cpuModel }}</div>
          </div>
          <div class="stat-value" :style="{ color: barColor(cpu) }">
            {{ cpu.toFixed(0) }}%
          </div>
        </div>

        <!-- Memory -->
        <div class="stat-row">
          <div class="stat-label-label">{{ $t("system.memory") }}</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{
                  width: memory.percent + '%',
                  background: barColor(memory.percent),
                }"
              />
            </div>
            <div class="stat-detail">
              {{
                $t("system.usedTotal", {
                  used: formatBytes(memory.used),
                  total: formatBytes(memory.total),
                })
              }}
            </div>
          </div>
          <div class="stat-value" :style="{ color: barColor(memory.percent) }">
            {{ memory.percent.toFixed(0) }}%
          </div>
        </div>

        <!-- Disk -->
        <div class="stat-row">
          <div class="stat-label-label">{{ $t("system.disk") }}</div>
          <div class="stat-info">
            <div class="stat-bar-track">
              <div
                class="stat-bar-fill"
                :style="{
                  width: disk.percent + '%',
                  background: barColor(disk.percent),
                }"
              />
            </div>
            <div class="stat-detail">
              {{
                $t("system.usedTotal", {
                  used: formatBytes(disk.used),
                  total: formatBytes(disk.total),
                })
              }}
            </div>
          </div>
          <div class="stat-value" :style="{ color: barColor(disk.percent) }">
            {{ disk.percent.toFixed(0) }}%
          </div>
        </div>

        <!-- Network -->
        <div class="stat-row">
          <div class="stat-label-label">{{ $t("system.network") }}</div>
          <div class="stat-info">
            <div class="stat-detail network-rate">
              {{ formatRate(network.downRate) }} ↓ / {{ formatRate(network.upRate) }} ↑
            </div>
          </div>
        </div>

        <!-- Expand toggle -->
        <button
          class="expand-toggle"
          :title="expanded ? $t('system.collapse') : $t('system.expand')"
          @click="toggleExpand"
        >
          <svg width="10" height="6" viewBox="0 0 10 6" :class="{ rotated: expanded }">
            <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round"/>
          </svg>
        </button>

        <!-- Process lists (expanded) -->
        <div v-if="expanded" ref="processListsRef" class="process-lists">
          <div class="process-section">
            <div class="process-label">{{ $t("system.topCpu") }}</div>
            <div
              v-for="(p, i) in topCpu"
              :key="'cpu-' + i"
              class="process-row"
            >
              <span class="process-name">{{ p.name }}</span>
              <span class="process-val">{{ p.cpu.toFixed(1) }}%</span>
            </div>
            <div v-if="topCpu.length === 0" class="process-empty">--</div>
          </div>
          <div class="process-section">
            <div class="process-label">{{ $t("system.topMemory") }}</div>
            <div
              v-for="(p, i) in topMemory"
              :key="'mem-' + i"
              class="process-row"
            >
              <span class="process-name">{{ p.name }}</span>
              <span class="process-val">{{ p.memory.toFixed(1) }}%</span>
            </div>
            <div v-if="topMemory.length === 0" class="process-empty">--</div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { GetSystemStats, GetTopProcesses } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { springAnimate } from "../composables/useSpring";

const props = defineProps({
  petPos: { type: Object, required: true },
  petSize: { type: Number, default: 160 },
});

const { t } = useI18n();

const visible = ref(false);
const cpu = ref(0);
const cpuModel = ref("");
const memory = ref({ used: 0, total: 0, percent: 0 });
const disk = ref({ used: 0, total: 0, percent: 0 });
const network = ref({ downRate: 0, upRate: 0 });
const expanded = ref(false);
const topCpu = ref([]);
const topMemory = ref([]);
const panelRef = ref(null);
const processListsRef = ref(null);
const expandedHeight = ref(0);
const baseHeight = ref(116);

let offStatsUpdate = null;
let cancelAnim = null;

const panelWidth = 240;

const panelStyle = computed(() => {
  const h = baseHeight.value + expandedHeight.value;
  const x = props.petPos.x - panelWidth + 50;
  const y = props.petPos.y + props.petSize - h;
  const clampedX = Math.min(Math.max(x, 8), window.innerWidth - panelWidth - 8);
  const clampedY = Math.min(Math.max(y, 38), window.innerHeight - h - 8);
  return {
    left: `${clampedX}px`,
    top: `${clampedY}px`,
    width: `${panelWidth}px`,
  };
});

/**
 * formatBytes converts a byte count to a human-readable string (e.g. "8.5G", "256M").
 */
function formatBytes(bytes) {
  if (!bytes || bytes === 0) return "0";
  if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + "G";
  if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + "M";
  if (bytes >= 1024) return (bytes / 1024).toFixed(0) + "K";
  return bytes + "B";
}

function formatRate(bytesPerSec) {
  if (!bytesPerSec || bytesPerSec < 1024) return "0 B/s";
  if (bytesPerSec >= 1048576) return (bytesPerSec / 1048576).toFixed(1) + " MB/s";
  if (bytesPerSec >= 1024) return (bytesPerSec / 1024).toFixed(1) + " KB/s";
  return bytesPerSec.toFixed(0) + " B/s";
}

function barColor(pct) {
  if (pct >= 90) return "#EF4444";
  if (pct >= 70) return "#F59E0B";
  return "#10B981";
}

function show() {
  visible.value = true;
  nextTick(updateBaseHeight);
  GetSystemStats()
    .then((st) => {
      cpu.value = st.cpu;
      cpuModel.value = st.cpuModel || "";
      memory.value = st.memory;
      disk.value = st.disk;
      network.value = st.network || { downRate: 0, upRate: 0 };
    })
    .catch(() => {});
}

function updateBaseHeight() {
  if (panelRef.value) {
    baseHeight.value = panelRef.value.offsetHeight;
  }
}

function fetchTopProcesses() {
  GetTopProcesses()
    .then((tp) => {
      topCpu.value = tp.topCpu || [];
      topMemory.value = tp.topMemory || [];
      nextTick(updateExpandedHeight);
    })
    .catch(() => {});
}

function updateExpandedHeight() {
  if (processListsRef.value) {
    expandedHeight.value = processListsRef.value.offsetHeight;
  }
}

function toggleExpand() {
  expanded.value = !expanded.value;
  if (expanded.value) {
    fetchTopProcesses();
  } else {
    topCpu.value = [];
    topMemory.value = [];
    expandedHeight.value = 0;
  }
}

const prefersReduced = window.matchMedia(
  "(prefers-reduced-motion: reduce)",
).matches;

function onEnter(el, done) {
  if (prefersReduced) {
    el.style.opacity = "1";
    done();
    return;
  }
  cancelAnim = springAnimate({
    from: 0,
    to: 1,
    stiffness: 320,
    damping: 22,
    onUpdate: (v) => {
      el.style.opacity = v;
      el.style.scale = 0.8 + 0.2 * v;
    },
    onComplete: () => {
      done();
    },
  });
}

function onLeave(el, done) {
  if (prefersReduced) {
    el.style.opacity = "0";
    done();
    return;
  }
  cancelAnim = springAnimate({
    from: 1,
    to: 0,
    stiffness: 400,
    damping: 36,
    onUpdate: (v) => {
      el.style.opacity = v;
      el.style.scale = 0.8 + 0.2 * v;
    },
    onComplete: () => {
      done();
    },
  });
}

onMounted(() => {
  show();
  offStatsUpdate = EventsOn("stats:update", (st) => {
    cpu.value = st.cpu;
    cpuModel.value = st.cpuModel || "";
    memory.value = st.memory;
    disk.value = st.disk;
    network.value = st.network || { downRate: 0, upRate: 0 };
    if (!expanded.value) {
      nextTick(updateBaseHeight);
    }
    if (expanded.value) {
      fetchTopProcesses();
    }
  });
});

onUnmounted(() => {
  offStatsUpdate?.();
  cancelAnim?.();
});

defineExpose({ show });
</script>

<style scoped>
.system-panel {
  position: fixed;
  z-index: 2001;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 16px;
  box-sizing: border-box;
  overflow: hidden;
  background: rgba(15, 23, 42, 0.88);
  backdrop-filter: blur(20px) saturate(140%);
  -webkit-backdrop-filter: blur(20px) saturate(140%);
  border-radius: 16px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  box-shadow:
    0 8px 32px rgba(0, 0, 0, 0.45),
    inset 0 1px 0 rgba(255, 255, 255, 0.04);
  user-select: none;
}

.stat-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.stat-label-label {
  width: 30px;
  font-size: 11px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.55);
  flex-shrink: 0;
  text-align: right;
  letter-spacing: 0.02em;
}

.stat-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.stat-bar-track {
  width: 100%;
  height: 5px;
  background: rgba(255, 255, 255, 0.08);
  border-radius: 3px;
  overflow: hidden;
}

.stat-bar-fill {
  height: 100%;
  border-radius: 3px;
  transition:
    width 0.4s ease,
    background 0.3s ease;
}

.stat-detail {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.4);
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.network-rate {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.55);
}

.stat-value {
  width: 36px;
  font-size: 13px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  text-align: right;
  flex-shrink: 0;
  letter-spacing: -0.01em;
}

.expand-toggle {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: 3px 0 1px;
  margin: 0;
  border: none;
  background: none;
  color: rgba(255, 255, 255, 0.3);
  cursor: pointer;
  transition: color 0.2s;
  line-height: 1;
}

.expand-toggle:hover {
  color: rgba(255, 255, 255, 0.7);
}

.expand-toggle svg {
  transition: transform 0.25s ease;
}

.expand-toggle svg.rotated {
  transform: rotate(180deg);
}

.process-lists {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  padding-top: 8px;
}

.process-section {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.process-label {
  font-size: 10px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.35);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.process-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.process-name {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.7);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 140px;
  flex: 1;
  min-width: 0;
}

.process-val {
  font-size: 11px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.5);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.process-empty {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.25);
}
</style>

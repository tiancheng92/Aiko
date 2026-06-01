<template>
    <Transition :css="false" @enter="onEnter" @leave="onLeave">
      <div v-if="visible" ref="panelRef" class="system-panel">
        <!-- CPU -->
        <div class="stat-row">
          <div class="stat-label">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <rect x="4" y="4" width="16" height="16" rx="2"/>
              <path d="M9 1v3m6-3v3M9 20v3m6-3v3M1 9h3m16 0h3M1 15h3m16 0h3"/>
            </svg>
            <span>{{ $t("system.cpu") }}</span>
          </div>
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
            {{ cpu.toFixed(2) }}%
          </div>
        </div>

        <!-- Memory -->
        <div class="stat-row">
          <div class="stat-label">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <rect x="3" y="3" width="18" height="18" rx="2"/>
              <path d="M7 8h10M7 12h10M7 16h6"/>
            </svg>
            <span>{{ $t("system.memory") }}</span>
          </div>
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
            {{ memory.percent.toFixed(2) }}%
          </div>
        </div>

        <!-- Disk -->
        <div class="stat-row">
          <div class="stat-label">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <ellipse cx="12" cy="12" rx="9" ry="9"/>
              <ellipse cx="12" cy="12" rx="3" ry="3"/>
              <path d="M12 3v18"/>
            </svg>
            <span>{{ $t("system.disk") }}</span>
          </div>
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
            {{ disk.percent.toFixed(2) }}%
          </div>
        </div>

        <!-- Network -->
        <div class="stat-row">
          <div class="stat-label">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="6" cy="12" r="3"/>
              <circle cx="18" cy="6" r="3"/>
              <circle cx="18" cy="18" r="3"/>
              <path d="M8.7 10.7l6.6-3.4M8.7 13.3l6.6 3.4"/>
            </svg>
            <span>{{ $t("system.network") }}</span>
          </div>
          <div class="stat-info">
            <div class="network-rates">
              <svg width="10" height="10" viewBox="0 0 10 10" class="network-arrow down">
                <path d="M5 1v7M2 6l3 3 3-3" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
              <span class="network-val down">{{ formatRate(network.downRate) }}</span>
              <span class="network-sep">/</span>
              <svg width="10" height="10" viewBox="0 0 10 10" class="network-arrow up">
                <path d="M5 9V2M2 4l3-3 3 3" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
              <span class="network-val up">{{ formatRate(network.upRate) }}</span>
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
</template>

<script setup>
import { nextTick, onMounted, onUnmounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { GetSystemStats, GetTopProcesses } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { springAnimate } from "../composables/useSpring";

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
let offStatsUpdate = null;
let cancelAnim = null;

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
  width: 100%;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 12px 16px;
  box-sizing: border-box;
  overflow: hidden;
  background: var(--lg-surface-elevated);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border);
  border-radius: 10px;
  box-shadow:
    0 8px 24px rgba(0, 0, 0, 0.45),
    0 0 0 0.5px rgba(0, 0, 0, 0.25),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  user-select: none;
}

.stat-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.stat-label {
  width: 52px;
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.45);
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
  position: relative;
  overflow: hidden;
  transition:
    width 0.4s ease,
    background 0.3s ease;
}

.stat-bar-fill::after {
  content: "";
  position: absolute;
  inset: 0;
  background: linear-gradient(
    90deg,
    transparent 0%,
    rgba(255, 255, 255, 0.25) 50%,
    transparent 100%
  );
  animation: barShimmer 2s ease-in-out infinite;
}

@keyframes barShimmer {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}

.stat-detail {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.5);
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.network-rates {
  display: flex;
  align-items: center;
  gap: 4px;
}

.network-arrow {
  flex-shrink: 0;
}

.network-arrow.down {
  color: #34D399;
}

.network-arrow.up {
  color: #FBBF24;
}

.network-val {
  font-size: 13px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
}

.network-val.down {
  color: #34D399;
}

.network-val.up {
  color: #FBBF24;
}

.network-sep {
  color: rgba(255, 255, 255, 0.25);
  font-size: 11px;
}

.stat-value {
  width: 52px;
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
  padding: 4px 0 2px;
  margin: 0;
  border: none;
  background: rgba(255, 255, 255, 0.04);
  border-radius: 6px;
  color: rgba(255, 255, 255, 0.4);
  cursor: pointer;
  transition: background 0.2s, color 0.2s;
  line-height: 1;
}

.expand-toggle:hover {
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.75);
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
  color: rgba(255, 255, 255, 0.45);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 1px;
}

.process-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 2px 6px;
  margin: 0 -6px;
  border-radius: 4px;
  transition: background 0.15s;
}

.process-row:hover {
  background: rgba(255, 255, 255, 0.05);
}

.process-name {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.78);
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
  color: rgba(255, 255, 255, 0.6);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.process-empty {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.25);
}
</style>

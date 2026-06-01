<!-- frontend/src/components/ClaudeStatusPanel.vue -->
<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { springAnimate } from "../composables/useSpring";

const props = defineProps({
  petPos: { type: Object, required: true },
  petSize: { type: Number, default: 160 },
});

const { t } = useI18n();

const visible = ref(false);
const state = ref("idle"); // "idle" | "thinking" | "error"
const hookEventName = ref("");
const toolName = ref("");
const panelRef = ref(null);
const panelWidth = 280;
const panelH = ref(0);

let offStatus = null;
let cancelAnim = null;

const STATE_CONFIG = {
  idle: { icon: "●", class: "state-idle", label: "idle" },
  thinking: { icon: "◉", class: "state-thinking", label: "thinking" },
  error: { icon: "✕", class: "state-error", label: "error" },
};

/**
 * eventLabel returns a human-readable description for the current hook event.
 */
function eventLabel(name) {
  switch (name) {
    case "PreToolUse":       return t("claudeStatus.eventPreToolUse");
    case "PermissionRequest": return t("claudeStatus.eventPermissionRequest");
    case "Stop":             return t("claudeStatus.eventStop");
    case "StopFailure":      return t("claudeStatus.eventStopFailure");
    default:                 return name || "--";
  }
}

/**
 * toolLabel returns a display-friendly tool name.
 */
function toolLabel(name) {
  if (!name) return "";
  // Friendly names for common tools.
  const map = {
    Bash: "bash",
    Write: "write",
    Edit: "edit",
    Read: "read",
    Grep: "grep",
    Glob: "glob",
    WebFetch: "webFetch",
    WebSearch: "webSearch",
  };
  const key = map[name];
  return key ? t("claudeStatus.tool." + key) : name;
}

const cfg = computed(() => STATE_CONFIG[state.value] || STATE_CONFIG.idle);

const panelStyle = computed(() => {
  const h = panelH.value || 56;
  const sysPanelH = 116; // SystemResourcePanel base height
  const gap = 8;
  const x = props.petPos.x - panelWidth + 50;
  const y = props.petPos.y + props.petSize - sysPanelH - gap - h;
  const clampedX = Math.min(Math.max(x, 8), window.innerWidth - panelWidth - 8);
  const clampedY = Math.min(Math.max(y, 38), window.innerHeight - h - 8);
  return {
    left: `${clampedX}px`,
    top: `${clampedY}px`,
    width: `${panelWidth}px`,
  };
});

/** show opens the panel and measures its height. */
function show() {
  visible.value = true;
  nextTick(() => {
    if (panelRef.value) panelH.value = panelRef.value.offsetHeight;
  });
}

/** hide closes the panel. */
function hide() {
  visible.value = false;
}

function updateHeight() {
  if (panelRef.value) panelH.value = panelRef.value.offsetHeight;
}

const prefersReduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

function onEnter(el, done) {
  if (prefersReduced) { el.style.opacity = "1"; done(); return; }
  cancelAnim = springAnimate({
    from: 0, to: 1, stiffness: 320, damping: 22,
    onUpdate: (v) => { el.style.opacity = v; el.style.transform = `translateY(${8 * (1 - v)}px)`; },
    onDone: () => { cancelAnim = null; done(); },
  });
}

function onLeave(el, done) {
  if (prefersReduced) { done(); return; }
  cancelAnim = springAnimate({
    from: 1, to: 0, stiffness: 320, damping: 36,
    onUpdate: (v) => { el.style.opacity = v; el.style.transform = `translateY(${8 * (1 - v)}px)`; },
    onDone: () => { cancelAnim = null; done(); },
  });
}

onMounted(() => {
  offStatus = EventsOn("claudecco:status", (data) => {
    state.value = data.state || "idle";
    hookEventName.value = data.hookEventName || "";
    toolName.value = data.toolName || "";
    nextTick(updateHeight);
  });
});

onUnmounted(() => {
  offStatus?.();
  if (cancelAnim) cancelAnim();
});

defineExpose({ show, hide });
</script>

<template>
  <Teleport to="body">
    <Transition :css="false" @enter="onEnter" @leave="onLeave">
      <div v-if="visible" ref="panelRef" class="claude-panel" :style="panelStyle">
        <div class="cp-header">
          <span class="cp-dot" :class="cfg.class">{{ cfg.icon }}</span>
          <span class="cp-title">Claude Code</span>
          <span class="cp-state-label">{{ t("claudeStatus." + cfg.label) }}</span>
        </div>
        <div class="cp-detail">
          <span class="cp-event">{{ eventLabel(hookEventName) }}</span>
          <span v-if="toolName" class="cp-tool">{{ toolLabel(toolName) }}</span>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.claude-panel {
  position: fixed;
  z-index: 99997;
  background: var(--lg-surface-elevated);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border);
  border-radius: 10px;
  padding: 10px 12px;
  box-shadow:
    0 8px 24px rgba(0, 0, 0, 0.45),
    0 0 0 0.5px rgba(0, 0, 0, 0.25),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", sans-serif;
  user-select: none;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cp-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.cp-dot {
  font-size: 10px;
  flex-shrink: 0;
}

.state-idle { color: #10B981; }
.state-thinking { color: #F59E0B; animation: pulse-dot 1.2s ease-in-out infinite; }
.state-error { color: #EF4444; }

@keyframes pulse-dot {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.cp-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  letter-spacing: -0.01em;
}

.cp-state-label {
  margin-left: auto;
  font-size: 11px;
  color: var(--text-secondary);
}

.cp-detail {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  color: var(--text-tertiary);
  padding-left: 18px;
}

.cp-event {
  color: var(--text-secondary);
}

.cp-tool {
  color: var(--accent);
  font-family: "SF Mono", "Fira Code", monospace;
  font-size: 10px;
  background: var(--bg-tertiary);
  padding: 1px 6px;
  border-radius: 4px;
}
</style>

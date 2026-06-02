<!-- frontend/src/components/ClaudeStatusPanel.vue -->
<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { springAnimate } from "../composables/useSpring";

const { t } = useI18n();

const visible = ref(false);
const sessions = ref([]); // [{ id, name, state, toolName, hookEventName }]
const panelRef = ref(null);

let offStatus = null;
let cancelAnim = null;

const STATE_CONFIG = {
  idle: {
    svg: '<circle cx="6" cy="6" r="4" fill="currentColor"/>',
    class: "state-idle",
    label: "idle",
  },
  thinking: {
    svg: '<circle cx="6" cy="6" r="4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-dasharray="6 3"><animateTransform attributeName="transform" type="rotate" values="0 6 6;360 6 6" dur="1.5s" repeatCount="indefinite"/></circle>',
    class: "state-thinking",
    label: "thinking",
  },
  error: {
    svg: '<line x1="3" y1="3" x2="9" y2="9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="9" y1="3" x2="3" y2="9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>',
    class: "state-error",
    label: "error",
  },
};

function cfg(s) {
  return STATE_CONFIG[s] || STATE_CONFIG.idle;
}

function toolIcon(name) {
  const icons = {
    Bash: '<path d="M2 4l3 3-3 3" stroke="currentColor" fill="none" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/><rect x="7" y="5" width="4" height="1.5" rx="0.5" fill="currentColor"/>',
    Write: '<path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04a1 1 0 0 0 0-1.41l-2.34-2.34a1 1 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z" fill="currentColor"/>',
    Edit: '<path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" stroke="currentColor" fill="none" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" fill="currentColor"/>',
    Read: '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" stroke="currentColor" fill="none" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/><circle cx="12" cy="12" r="3" stroke="currentColor" fill="none" stroke-width="1.5"/>',
  };
  return icons[name] || null;
}

function eventLabel(name) {
  switch (name) {
    case "PreToolUse":       return t("claudeStatus.eventPreToolUse");
    case "PermissionRequest": return t("claudeStatus.eventPermissionRequest");
    case "Stop":             return t("claudeStatus.eventStop");
    case "StopFailure":      return t("claudeStatus.eventStopFailure");
    default:                 return name || "--";
  }
}

function toolLabel(name) {
  if (!name) return "";
  const map = { Bash: "bash", Write: "write", Edit: "edit", Read: "read", Grep: "grep", Glob: "glob", WebFetch: "webFetch", WebSearch: "webSearch" };
  return map[name] ? t("claudeStatus.tool." + map[name]) : name;
}

function show() { visible.value = true; }
function hide() { visible.value = false; }

const prefersReduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

function onEnter(el, done) {
  if (prefersReduced) { el.style.opacity = "1"; done(); return; }
  cancelAnim = springAnimate({
    from: 0, to: 1,
    stiffness: 280, damping: 24,
    onUpdate: (v) => { el.style.opacity = v; el.style.transform = `translateY(${6 * (1 - v)}px) scale(${0.96 + 0.04 * v})`; },
    onDone: () => { cancelAnim = null; done(); },
  });
}

function onLeave(el, done) {
  if (prefersReduced) { done(); return; }
  cancelAnim = springAnimate({
    from: 1, to: 0,
    stiffness: 350, damping: 34,
    onUpdate: (v) => { el.style.opacity = v; el.style.transform = `translateY(${4 * (1 - v)}px) scale(${0.97 + 0.03 * v})`; },
    onDone: () => { cancelAnim = null; done(); },
  });
}

onMounted(() => {
  offStatus = EventsOn("claudecco:status", (data) => {
    sessions.value = data.sessions || [];
  });
});

onUnmounted(() => {
  offStatus?.();
  if (cancelAnim) cancelAnim();
});

defineExpose({ show, hide });
</script>

<template>
    <Transition :css="false" @enter="onEnter" @leave="onLeave">
      <div v-if="visible && sessions.length > 0" ref="panelRef" class="claude-panel">
        <div
          v-for="s in sessions"
          :key="s.id"
          class="cp-row"
        >
          <span class="cp-dot" :class="cfg(s.state).class">
            <svg v-if="s.state === 'idle'" width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="currentColor"/></svg>
            <svg v-else-if="s.state === 'thinking'" width="12" height="12" viewBox="0 0 12 12" class="spin-svg"><circle cx="6" cy="6" r="4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-dasharray="6 3"/></svg>
            <svg v-else width="12" height="12" viewBox="0 0 12 12"><line x1="3.5" y1="3.5" x2="8.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="8.5" y1="3.5" x2="3.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
          </span>
          <span class="cp-session">{{ s.name }}</span>
          <span v-if="s.toolName" class="cp-tool">
            <svg v-if="toolIcon(s.toolName)" width="11" height="11" viewBox="0 0 24 24" class="cp-tool-icon" v-html="toolIcon(s.toolName)"></svg>
            {{ toolLabel(s.toolName) }}
          </span>
          <span class="cp-status">{{ t("claudeStatus." + cfg(s.state).label) }}</span>
        </div>
      </div>
    </Transition>
</template>

<style scoped>
.claude-panel {
  width: 100%;
  box-sizing: border-box;
  background: var(--lg-surface-elevated);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border);
  border-radius: 10px;
  padding: 6px 10px;
  box-shadow:
    0 8px 24px rgba(0, 0, 0, 0.45),
    0 0 0 0.5px rgba(0, 0, 0, 0.25),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
  font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", sans-serif;
  -webkit-font-smoothing: antialiased;
  user-select: none;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cp-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 4px;
}

.cp-row + .cp-row {
  border-top: 1px solid var(--lg-border-subtle);
}

.cp-dot {
  width: 12px;
  height: 12px;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.state-idle     { color: #10B981; }
.state-thinking { color: #F59E0B; }
.state-error    { color: #EF4444; }

.spin-svg {
  animation: cp-spin 1.5s linear infinite;
  transform-origin: 50% 50%;
}
@keyframes cp-spin { to { transform: rotate(360deg); } }

.cp-session {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  letter-spacing: -0.01em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.cp-status {
  margin-left: auto;
  font-size: 10px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.cp-tool {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--accent);
  font-family: "SF Mono", "Fira Code", monospace;
  font-size: 10px;
  background: var(--bg-tertiary);
  padding: 1px 5px;
  border-radius: 4px;
  flex-shrink: 0;
}

.cp-tool-icon { flex-shrink: 0; }
</style>

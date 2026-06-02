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

/** hasThinking is true when any session is in thinking state. */
const hasThinking = computed(() => sessions.value.some(s => s.state === "thinking"));

/** Compute session groups sorted by CWD. Each group has a cwd label and sessions. */
const groups = computed(() => {
  const map = new Map();
  for (const s of sessions.value) {
    const cwd = s.cwd || "";
    if (!map.has(cwd)) map.set(cwd, []);
    map.get(cwd).push(s);
  }
  // Groups already in order because sessions are sorted CWD → state → name by backend.
  return Array.from(map, ([cwd, items]) => ({ cwd, items }));
});

/** Shorten a CWD path for display (basename). */
function cwdLabel(cwd) {
  if (!cwd) return "";
  const parts = cwd.replace(/\/$/, "").split("/");
  return parts[parts.length - 1] || cwd;
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
      <div v-if="visible && sessions.length > 0" ref="panelRef" class="claude-panel" :class="{ 'claude-panel--thinking': hasThinking }">
        <template v-for="group in groups" :key="group.cwd">
          <div class="cp-group-label">{{ cwdLabel(group.cwd) }}</div>
          <div
            v-for="s in group.items"
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
        </template>
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
  padding: 8px 12px;
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
  will-change: transform, opacity;
  transition: border-color 0.3s var(--ease-enter), box-shadow 0.3s var(--ease-enter);
}

/* Panel-level thinking glow: subtle amber border when any session is thinking */
.claude-panel--thinking {
  border-color: rgba(245, 158, 11, 0.3);
  box-shadow:
    0 8px 24px rgba(0, 0, 0, 0.45),
    0 0 0 0.5px rgba(0, 0, 0, 0.25),
    0 0 16px rgba(245, 158, 11, 0.08),
    0 1px 0 rgba(255, 255, 255, 0.06) inset;
}

.cp-group-label {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  padding: 8px 4px 4px;
  opacity: 0.7;
}

.cp-group-label:first-child {
  padding-top: 2px;
}

.cp-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 4px;
  border-radius: 6px;
  transition: background 0.12s var(--ease-enter);
}

.cp-row:hover {
  background: var(--lg-surface-hover);
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

/* ── Thinking glow pulse ── */
@keyframes thinking-glow {
  0%, 100% { box-shadow: 0 0 6px rgba(245, 158, 11, 0.2); }
  50%      { box-shadow: 0 0 14px rgba(245, 158, 11, 0.45); }
}
.cp-dot.state-thinking {
  border-radius: 50%;
  animation: thinking-glow 1.5s ease-in-out infinite;
}

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
  flex: 1;
}

.cp-status {
  margin-left: auto;
  font-size: 10px;
  font-weight: 500;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.cp-tool {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--accent);
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
  font-size: 10px;
  font-weight: 500;
  background: var(--accent-alpha-12);
  border: 1px solid var(--accent-alpha-20);
  padding: 2px 6px;
  border-radius: 5px;
  flex-shrink: 0;
  line-height: 1.3;
}

.cp-tool-icon {
  flex-shrink: 0;
  opacity: 0.75;
}

/* ── Reduced motion ── */
@media (prefers-reduced-motion: reduce) {
  .spin-svg { animation: none; }
  .cp-dot.state-thinking { animation: none; }
  .cp-row { transition: none; }
}
</style>

<!-- frontend/src/components/ClaudeStatusPanel.vue -->
<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { springAnimate } from "../composables/useSpring";

const { t } = useI18n();
const emit = defineEmits(['close']);

const visible = ref(false);
const sessions = ref([]); // [{ id, name, state, toolName, hookEventName }]
const panelRef = ref(null);

// 计时器相关
const tick = ref(0);
let tickTimer = null;
const sessionStartTimes = new Map(); // id → timestamp ms，首次 thinking 时记录
const sessionEndTimes = new Map();   // id → timestamp ms，离开 thinking 时冻结计时
const sessionToolCounts = reactive({}); // id → number，PreToolUse 事件计数
const dismissed = ref(new Set());   // 手动关闭的 session id

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

/** hasThinking is true when any non-dismissed session is in thinking state. */
const hasThinking = computed(() =>
  sessions.value.some((s) => !dismissed.value.has(s.id) && s.state === "thinking")
);

/** Compute session groups sorted by CWD.
 *  Each group has a cwd label and a list of root sessions, each with a children array. */
const groups = computed(() => {
  const visible = sessions.value.filter((s) => !dismissed.value.has(s.id));
  // Build lookup and children map
  const byId = new Map(visible.map((s) => [s.id, s]));
  const childrenOf = new Map(); // parentId → session[]
  const roots = [];
  for (const s of visible) {
    if (s.parentId && byId.has(s.parentId)) {
      if (!childrenOf.has(s.parentId)) childrenOf.set(s.parentId, []);
      childrenOf.get(s.parentId).push(s);
    } else {
      roots.push(s);
    }
  }
  // Group roots by CWD, attach children
  const cwdMap = new Map();
  for (const s of roots) {
    const cwd = s.cwd || "";
    if (!cwdMap.has(cwd)) cwdMap.set(cwd, []);
    cwdMap.get(cwd).push({ session: s, children: childrenOf.get(s.id) || [] });
  }
  return Array.from(cwdMap, ([cwd, items]) => ({ cwd, items }));
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
  tickTimer = setInterval(() => { tick.value++; }, 1000);
  offStatus = EventsOn("claudecco:status", (data) => {
    const incoming = data.sessions || [];
    const now = Date.now();
    for (const s of incoming) {
      if (s.state === "thinking") {
        // Reset start time when session resumes after a completed/errored run
        if (sessionEndTimes.has(s.id)) {
          sessionStartTimes.set(s.id, now);
          sessionEndTimes.delete(s.id);
        } else if (!sessionStartTimes.has(s.id)) {
          sessionStartTimes.set(s.id, now);
        }
        if (s.hookEventName === "PreToolUse") {
          sessionToolCounts[s.id] = (sessionToolCounts[s.id] || 0) + 1;
        }
      } else if (sessionStartTimes.has(s.id) && !sessionEndTimes.has(s.id)) {
        // Session left thinking → freeze the elapsed time
        sessionEndTimes.set(s.id, now);
      }
    }
    sessions.value = incoming;
    // Prune entries for sessions no longer in the active list
    const activeIds = new Set(incoming.map((s) => s.id));
    for (const id of Object.keys(sessionToolCounts)) {
      if (!activeIds.has(id)) {
        delete sessionToolCounts[id];
        sessionStartTimes.delete(id);
        sessionEndTimes.delete(id);
      }
    }
  });
});

onUnmounted(() => {
  offStatus?.();
  if (cancelAnim) cancelAnim();
  clearInterval(tickTimer);
  tickTimer = null;
});

/** elapsedLabel returns a human-readable elapsed time string for a session.
 *  Freezes at the end time once the session leaves thinking state. */
function elapsedLabel(id) {
  tick.value; // 触发响应式追踪
  const start = sessionStartTimes.get(id);
  if (!start) return "";
  const end = sessionEndTimes.get(id) ?? Date.now();
  const sec = Math.floor((end - start) / 1000);
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60), s = sec % 60;
  if (m < 60) return `${m}m ${s}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

/** toolInputLabel extracts a human-readable summary from a JSON ToolInput string. */
function toolInputLabel(raw) {
  if (!raw) return "";
  try {
    const obj = JSON.parse(raw);
    const key = ["command", "cmd", "file_path", "path", "query", "url", "skill", "prompt"]
      .find((k) => obj[k] && typeof obj[k] === "string");
    if (key) {
      let val = obj[key];
      if (val.length > 60) val = val.slice(0, 60) + "…";
      return val;
    }
  } catch {}
  return raw.length > 60 ? raw.slice(0, 60) + "…" : raw;
}

/** dismiss removes an idle session from the panel for this session lifetime. */
function dismiss(id) {
  dismissed.value = new Set([...dismissed.value, id]);
  sessionStartTimes.delete(id);
  sessionEndTimes.delete(id);
  delete sessionToolCounts[id];
}

defineExpose({ show, hide });
</script>

<template>
    <Transition :css="false" @enter="onEnter" @leave="onLeave">
      <div v-if="visible" ref="panelRef" class="claude-panel" :class="{ 'claude-panel--thinking': hasThinking }">
        <div class="cp-titlebar">
          <span class="cp-titlebar-label">Claude Code</span>
          <button class="panel-close-btn" @click="emit('close')" :title="t('common.close')">×</button>
        </div>
        <div v-if="sessions.length === 0" class="cp-empty">无任务</div>
        <template v-else v-for="group in groups" :key="group.cwd">
          <div class="cp-group-label">{{ cwdLabel(group.cwd) }}</div>
          <template v-for="item in group.items" :key="item.session.id">
            <!-- 主 session 行 -->
            <div class="cp-row">
              <div class="cp-row-main">
                <span class="cp-dot" :class="cfg(item.session.state).class">
                  <svg v-if="item.session.state === 'idle'" width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="currentColor"/></svg>
                  <svg v-else-if="item.session.state === 'thinking'" width="12" height="12" viewBox="0 0 12 12" class="spin-svg"><circle cx="6" cy="6" r="4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-dasharray="6 3"/></svg>
                  <svg v-else width="12" height="12" viewBox="0 0 12 12"><line x1="3.5" y1="3.5" x2="8.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="8.5" y1="3.5" x2="3.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
                </span>
                <span class="cp-session">{{ item.session.name }}</span>
                <button v-if="item.session.state === 'idle'" class="cp-dismiss" @click.stop="dismiss(item.session.id)" :title="t('claudeStatus.dismiss')">×</button>
              </div>
              <div class="cp-row-meta">
                <span v-if="elapsedLabel(item.session.id)" class="cp-elapsed">{{ t('claudeStatus.elapsedTime') }} {{ elapsedLabel(item.session.id) }}</span>
                <span v-if="sessionToolCounts[item.session.id]" class="cp-count">{{ t('claudeStatus.toolCallCount') }} {{ sessionToolCounts[item.session.id] }}</span>
                <span v-if="item.session.toolName" class="cp-tool" :title="t('claudeStatus.currentTool')">
                  <svg v-if="toolIcon(item.session.toolName)" width="11" height="11" viewBox="0 0 24 24" class="cp-tool-icon" v-html="toolIcon(item.session.toolName)"></svg>
                  {{ toolLabel(item.session.toolName) }}
                </span>
                <span class="cp-status">{{ t("claudeStatus." + cfg(item.session.state).label) }}</span>
              </div>
              <div v-if="toolInputLabel(item.session.toolInput)" class="cp-row-sub">
                {{ toolInputLabel(item.session.toolInput) }}
              </div>
            </div>
            <!-- subagent 行（缩进） -->
            <div
              v-for="sub in item.children"
              :key="sub.id"
              class="cp-row cp-row--sub"
            >
              <div class="cp-row-main">
                <span class="cp-sub-indent">└</span>
                <span class="cp-dot" :class="cfg(sub.state).class">
                  <svg v-if="sub.state === 'idle'" width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="currentColor"/></svg>
                  <svg v-else-if="sub.state === 'thinking'" width="12" height="12" viewBox="0 0 12 12" class="spin-svg"><circle cx="6" cy="6" r="4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-dasharray="6 3"/></svg>
                  <svg v-else width="12" height="12" viewBox="0 0 12 12"><line x1="3.5" y1="3.5" x2="8.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="8.5" y1="3.5" x2="3.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
                </span>
                <span class="cp-session cp-session--sub">{{ sub.name }}</span>
                <button v-if="sub.state === 'idle'" class="cp-dismiss" @click.stop="dismiss(sub.id)" :title="t('claudeStatus.dismiss')">×</button>
              </div>
              <div class="cp-row-meta cp-row-meta--sub">
                <span v-if="elapsedLabel(sub.id)" class="cp-elapsed" :title="t('claudeStatus.elapsedTime')">{{ elapsedLabel(sub.id) }}</span>
                <span v-if="sessionToolCounts[sub.id]" class="cp-count" :title="t('claudeStatus.toolCallCount')">×{{ sessionToolCounts[sub.id] }}</span>
                <span v-if="sub.toolName" class="cp-tool" :title="t('claudeStatus.currentTool')">
                  <svg v-if="toolIcon(sub.toolName)" width="11" height="11" viewBox="0 0 24 24" class="cp-tool-icon" v-html="toolIcon(sub.toolName)"></svg>
                  {{ toolLabel(sub.toolName) }}
                </span>
                <span class="cp-status">{{ t("claudeStatus." + cfg(sub.state).label) }}</span>
              </div>
              <div v-if="toolInputLabel(sub.toolInput)" class="cp-row-sub cp-row-sub--indented">
                {{ toolInputLabel(sub.toolInput) }}
              </div>
            </div>
          </template>
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
  padding: 10px 14px;
  gap: 4px;
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

.cp-titlebar {
  display: flex;
  align-items: center;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--lg-border-subtle);
}

.cp-titlebar-label {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.panel-close-btn {
  margin-left: auto;
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
  color: var(--text-tertiary);
  opacity: 0.3;
  border-radius: 3px;
  padding: 0;
  transition: opacity 0.15s, background 0.15s;
}

.panel-close-btn:hover {
  opacity: 1;
  background: var(--lg-surface-hover);
  color: var(--text-primary);
}

.cp-empty {
  font-size: 12px;
  color: var(--text-tertiary);
  padding: 4px;
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
  flex-direction: column;
  gap: 2px;
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

.cp-row-main {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

/* subagent 行整体左移 */
.cp-row--sub {
  padding-left: 8px;
  opacity: 0.85;
}

.cp-sub-indent {
  font-size: 10px;
  color: var(--text-tertiary);
  flex-shrink: 0;
  opacity: 0.5;
  line-height: 1;
}

.cp-session--sub {
  font-weight: 500;
  color: var(--text-secondary);
}

.cp-row-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-left: 18px; /* 与 cp-session 对齐（dot 12px + gap 6px） */
  min-width: 0;
}

.cp-row-sub {
  font-size: 10px;
  color: var(--text-tertiary);
  padding-left: 18px; /* 与 cp-session 对齐（dot 12px + gap 6px） */
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
  opacity: 0.75;
}

/* subagent 行的 meta/sub 对齐：额外加 └(10px) + gap(6px) = 16px */
.cp-row-meta--sub {
  padding-left: 34px;
}

.cp-row-sub--indented {
  padding-left: 34px;
}

.cp-count {
  font-size: 10px;
  font-weight: 500;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
}

.cp-elapsed {
  font-size: 10px;
  font-weight: 500;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
}

.cp-dismiss {
  margin-left: auto;
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
  color: var(--text-tertiary);
  opacity: 0.3;
  border-radius: 3px;
  padding: 0;
  transition: opacity 0.15s, background 0.15s;
}

.cp-row:hover .cp-dismiss {
  opacity: 0.7;
}

.cp-dismiss:hover {
  opacity: 1 !important;
  background: var(--lg-surface-hover);
  color: var(--text-primary);
}

/* ── Reduced motion ── */
@media (prefers-reduced-motion: reduce) {
  .spin-svg { animation: none; }
  .cp-dot.state-thinking { animation: none; }
  .cp-row { transition: none; }
}
</style>

<!-- frontend/src/components/ClaudeStatusPanel.vue -->
<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { springAnimate } from "../composables/useSpring";

const { t } = useI18n();
const emit = defineEmits(['close']);

const visible = ref(true);
const sessions = ref([]); // [{ id, name, state, toolName, hookEventName }]
const panelRef = ref(null);

// Tooltip state for truncated elements
const tooltip = ref({ show: false, text: '', x: 0, y: 0 });
let tooltipTimer = null;

/** Show tooltip only if the element's text is actually truncated. */
function onTruncatedEnter(e, text) {
  const el = e.currentTarget;
  if (el.scrollWidth <= el.clientWidth) return;
  clearTimeout(tooltipTimer);
  const rect = el.getBoundingClientRect();
  tooltip.value = { show: true, text, x: rect.left + rect.width / 2, y: rect.top };
}

function onTruncatedLeave() {
  tooltipTimer = setTimeout(() => { tooltip.value.show = false; }, 80);
}

// 计时器相关
const tick = ref(0);
let tickTimer = null;
const sessionFirstSeen = new Map();  // id → timestamp ms，session 首次出现时记录
const sessionStartTimes = new Map(); // id → timestamp ms，首次 thinking 时记录
const sessionEndTimes = new Map();   // id → timestamp ms，离开 thinking 时冻结计时
const sessionToolCounts = reactive({});   // id → number，PreToolUse 事件计数（总）
const sessionToolOkCounts = reactive({});  // id → number，PostToolUse 成功
const sessionToolFailCounts = reactive({}); // id → number，PostToolUseFailure 失败
const sessionUpdateTimes = new Map();       // id → timestamp ms，上次收到该 session 的事件时间
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
  compact: {
    svg: '<rect x="2" y="2" width="8" height="8" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M6 4v4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><path d="M4 6h4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>',
    class: "state-compact",
    label: "compact",
  },
};

function cfg(s) {
  return STATE_CONFIG[s] || STATE_CONFIG.idle;
}

function toolIcon(name) {
  const icons = {
    Bash: '<path d="M4 17l6-6-6-6" stroke="currentColor" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M12 19h8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
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

/** SVG icons for notification types. */
function notifIcon(typ) {
  const icons = {
    permission_prompt:  '<path d="M12 2a2 2 0 0 0-2 2v1.5A6.5 6.5 0 0 0 5.5 12v2a1 1 0 0 1-1 1H4a1 1 0 0 0-1 1v1a1 1 0 0 0 1 1h16a1 1 0 0 0 1-1v-1a1 1 0 0 0-1-1h-.5a1 1 0 0 1-1-1v-2A6.5 6.5 0 0 0 14 5.5V4a2 2 0 1 0-4 0z" stroke="currentColor" fill="none" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/><path d="M10 17a2 2 0 1 0 4 0" stroke="currentColor" fill="none" stroke-width="1.8" stroke-linecap="round"/>',
    idle_prompt:        '<circle cx="12" cy="12" r="9" stroke="currentColor" fill="none" stroke-width="1.8"/><path d="M12 7v5l3 3" stroke="currentColor" fill="none" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>',
    auth_success:       '<path d="M9 12l2 2 4-4" stroke="currentColor" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><circle cx="12" cy="12" r="9" stroke="currentColor" fill="none" stroke-width="1.8"/>',
    elicitation_dialog: '<rect x="3" y="3" width="18" height="14" rx="2" stroke="currentColor" fill="none" stroke-width="1.8"/><path d="M7 8h10M7 12h6" stroke="currentColor" fill="none" stroke-width="1.8" stroke-linecap="round"/>',
  };
  return icons[typ] || '<circle cx="12" cy="12" r="9" stroke="currentColor" fill="none" stroke-width="1.8"/><path d="M12 7v5M12 15.5v.01" stroke="currentColor" fill="none" stroke-width="1.8" stroke-linecap="round"/>';
}

/** Notification type → CSS class mapping. */
const NOTIF_CLASS = {
  permission_prompt:  'cp-notif--warn',
  idle_prompt:        'cp-notif--info',
  auth_success:       'cp-notif--ok',
  elicitation_dialog: 'cp-notif--info',
};

function notifClass(typ) {
  return NOTIF_CLASS[typ] || '';
}

function show() { visible.value = true; }
/** Returns true if a thinking session has had no events for >60 seconds. */
function isStale(id) {
  const s = sessions.value.find((x) => x.id === id);
  if (!s || s.state !== "thinking") return false;
  return Date.now() - (sessionUpdateTimes.get(id) || 0) > 60_000;
}

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
  tickTimer = setInterval(() => {
    tick.value++;
    // Auto-idle: sessions stuck in thinking for >5min with no update → idle.
    const nowTs = Date.now();
    const stale = sessions.value.filter(
      (s) => (s.state === "thinking" || s.state === "compact") && nowTs - (sessionUpdateTimes.get(s.id) || 0) > 300_000
    );
    for (const s of stale) {
      s.state = "idle";
      if (sessionStartTimes.has(s.id) && !sessionEndTimes.has(s.id)) {
        sessionEndTimes.set(s.id, nowTs);
      }
    }
    if (stale.length) sessions.value = [...sessions.value];
  }, 1000);
  offStatus = EventsOn("claudecco:status", (data) => {
    const incoming = data.sessions || [];
    const now = Date.now();
    for (const s of incoming) {
      // Restore dismissed sessions when they receive new events.
      if (s.hookEventName && dismissed.value.has(s.id)) {
        const next = new Set(dismissed.value);
        next.delete(s.id);
        dismissed.value = next;
      }
      // Only reset the staleness timer if this session actually got a new event.
      if (s.hookEventName) sessionUpdateTimes.set(s.id, now);
      else if (!sessionUpdateTimes.has(s.id)) sessionUpdateTimes.set(s.id, now);
      // Record first-seen time for every session so elapsed always shows.
      if (!sessionFirstSeen.has(s.id)) sessionFirstSeen.set(s.id, now);
      const isActive = s.state === "thinking" || s.state === "compact";
      if (isActive) {
        // Reset start time when session resumes after a completed/errored run
        if (sessionEndTimes.has(s.id)) {
          sessionStartTimes.set(s.id, now);
          sessionEndTimes.delete(s.id);
        } else if (s.state === "compact") {
          // thinking→compact: reset timer, compaction is a new phase
          sessionStartTimes.set(s.id, now);
        } else if (!sessionStartTimes.has(s.id)) {
          sessionStartTimes.set(s.id, now);
        }
        if (s.hookEventName === "PreToolUse") {
          sessionToolCounts[s.id] = (sessionToolCounts[s.id] || 0) + 1;
        } else if (s.hookEventName === "PostToolUse") {
          sessionToolOkCounts[s.id] = (sessionToolOkCounts[s.id] || 0) + 1;
        } else if (s.hookEventName === "PostToolUseFailure") {
          sessionToolFailCounts[s.id] = (sessionToolFailCounts[s.id] || 0) + 1;
        }
      } else if (sessionStartTimes.has(s.id) && !sessionEndTimes.has(s.id)) {
        // Session left active states (thinking/compact) → freeze the elapsed time
        sessionEndTimes.set(s.id, now);
      }
    }
    sessions.value = incoming;
    // Prune entries for sessions no longer in the active list
    const activeIds = new Set(incoming.map((s) => s.id));
    for (const id of Object.keys(sessionToolCounts)) {
      if (!activeIds.has(id)) {
        delete sessionToolCounts[id];
        sessionFirstSeen.delete(id);
        sessionStartTimes.delete(id);
        sessionEndTimes.delete(id);
        sessionUpdateTimes.delete(id);
      }
    }
    for (const id of Object.keys(sessionToolOkCounts)) {
      if (!activeIds.has(id)) delete sessionToolOkCounts[id];
    }
    for (const id of Object.keys(sessionToolFailCounts)) {
      if (!activeIds.has(id)) delete sessionToolFailCounts[id];
    }
  });
});

onUnmounted(() => {
  offStatus?.();
  if (cancelAnim) cancelAnim();
  clearInterval(tickTimer);
  clearTimeout(tooltipTimer);
  tickTimer = null;
});

/** elapsedLabel returns a human-readable elapsed time string for a session.
 *  Only shows time when the session has actually been in "thinking" state —
 *  sessions that stay idle/error/compact without ever thinking show nothing. */
function elapsedLabel(id) {
  tick.value; // 触发响应式追踪
  const thinkStart = sessionStartTimes.get(id);
  if (!thinkStart) return "";
  const end = sessionEndTimes.get(id) ?? Date.now();
  const sec = Math.floor((end - thinkStart) / 1000);
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
  // JSON parse failed (e.g. truncated) — don't show raw JSON
  return "";
}

/** toolInputFull returns the full extracted value from a JSON ToolInput, without truncation. */
function toolInputFull(raw) {
  if (!raw) return "";
  try {
    const obj = JSON.parse(raw);
    const key = ["command", "cmd", "file_path", "path", "query", "url", "skill", "prompt"]
      .find((k) => obj[k] && typeof obj[k] === "string");
    if (key) return obj[key];
  } catch {}
  return "";
}

/** dismiss removes an idle session and all its descendants from the panel. */
function dismiss(id) {
  const toDismiss = new Set([id]);
  // Collect all descendant IDs (handle arbitrary nesting depth).
  let added = true;
  while (added) {
    added = false;
    for (const s of sessions.value) {
      if (s.parentId && toDismiss.has(s.parentId) && !toDismiss.has(s.id)) {
        toDismiss.add(s.id);
        added = true;
      }
    }
  }
  dismissed.value = new Set([...dismissed.value, ...toDismiss]);
  for (const sid of toDismiss) {
    sessionFirstSeen.delete(sid);
    sessionStartTimes.delete(sid);
    sessionEndTimes.delete(sid);
    delete sessionToolCounts[sid];
    delete sessionToolOkCounts[sid];
    delete sessionToolFailCounts[sid];
  }
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
        <div v-if="groups.length === 0" class="cp-empty">无任务</div>
        <template v-else v-for="group in groups" :key="group.cwd">
          <div class="cp-group-label">{{ cwdLabel(group.cwd) }}</div>
          <template v-for="item in group.items" :key="item.session.id">
            <!-- 主 session 行 -->
            <div class="cp-row" :class="{ 'cp-row--thinking': item.session.state === 'thinking', 'cp-row--compact': item.session.state === 'compact', 'cp-row--error': item.session.state === 'error' }">
              <div class="cp-row-main">
                <span class="cp-dot" :class="cfg(item.session.state).class">
                  <svg v-if="item.session.state === 'idle'" width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="currentColor"/></svg>
                  <svg v-else-if="item.session.state === 'thinking'" width="12" height="12" viewBox="0 0 24 24" class="spin-svg" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"><path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" opacity="0.8"/><path d="M22 12a10 10 0 0 1-10 10" opacity="0.3"/></svg>
                  <svg v-else-if="item.session.state === 'compact'" width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><rect x="2" y="2" width="8" height="8" rx="1.5"/><path d="M4 6h4"/></svg>
                  <svg v-else width="12" height="12" viewBox="0 0 12 12"><line x1="3.5" y1="3.5" x2="8.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="8.5" y1="3.5" x2="3.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
                </span>
                <span class="cp-session" @mouseenter="onTruncatedEnter($event, item.session.name)" @mouseleave="onTruncatedLeave">{{ item.session.name }}</span>
                <button class="cp-dismiss" @click.stop="dismiss(item.session.id)" :title="t('claudeStatus.dismiss')">×</button>
              </div>
              <div class="cp-row-meta">
                <span v-if="elapsedLabel(item.session.id)" class="cp-elapsed">{{ t('claudeStatus.elapsedTime') }} {{ elapsedLabel(item.session.id) }}</span>
                <span v-if="elapsedLabel(item.session.id)" class="cp-sep">·</span>
                <span class="cp-tool-label">{{ t('claudeStatus.toolCountLabel') }}</span>
                <span v-if="sessionToolOkCounts[item.session.id]" class="cp-count cp-count--ok">✓{{ sessionToolOkCounts[item.session.id] }}</span>
                <span v-if="sessionToolFailCounts[item.session.id]" class="cp-count cp-count--fail">✗{{ sessionToolFailCounts[item.session.id] }}</span>
                <span v-if="!sessionToolOkCounts[item.session.id] && !sessionToolFailCounts[item.session.id]" class="cp-count">—</span>
                <span class="cp-status" :class="{ 'cp-status--stale': isStale(item.session.id) }">{{ isStale(item.session.id) ? t('claudeStatus.stale') : t("claudeStatus." + cfg(item.session.state).label) }}</span>
              </div>
              <div v-if="item.session.toolName || toolInputLabel(item.session.toolInput)" class="cp-row-sub">
                <span v-if="item.session.toolName" class="cp-tool" :title="toolLabel(item.session.toolName)" @mouseenter="onTruncatedEnter($event, toolLabel(item.session.toolName))" @mouseleave="onTruncatedLeave">
                  <svg v-if="toolIcon(item.session.toolName)" width="11" height="11" viewBox="0 0 24 24" class="cp-tool-icon" v-html="toolIcon(item.session.toolName)"></svg>
                  {{ toolLabel(item.session.toolName) }}
                </span>
                <span v-if="toolInputLabel(item.session.toolInput)" class="cp-row-sub-text" @mouseenter="onTruncatedEnter($event, toolInputFull(item.session.toolInput))" @mouseleave="onTruncatedLeave">{{ toolInputLabel(item.session.toolInput) }}</span>
                <span v-if="item.session.toolOk === true" class="cp-tool-ok" title="OK">✓</span>
                <span v-if="item.session.toolOk === false" class="cp-tool-err" title="Failed">✗</span>
              </div>
              <div v-if="item.session.notification" class="cp-row-notif" :class="notifClass(item.session.notificationType)"><svg width="12" height="12" viewBox="0 0 24 24" class="cp-notif-icon" v-html="notifIcon(item.session.notificationType)"></svg>{{ item.session.notification }}</div>
            </div>
            <!-- subagent 行（缩进） -->
            <div
              v-for="sub in item.children"
              :key="sub.id"
              class="cp-row cp-row--sub"
              :class="{ 'cp-row--thinking': sub.state === 'thinking', 'cp-row--error': sub.state === 'error' }"
            >
              <div class="cp-row-main">
                <span class="cp-dot" :class="cfg(sub.state).class">
                  <svg v-if="sub.state === 'idle'" width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" fill="currentColor"/></svg>
                  <svg v-else-if="sub.state === 'thinking'" width="12" height="12" viewBox="0 0 24 24" class="spin-svg" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"><path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" opacity="0.8"/><path d="M22 12a10 10 0 0 1-10 10" opacity="0.3"/></svg>
                  <svg v-else-if="sub.state === 'compact'" width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><rect x="2" y="2" width="8" height="8" rx="1.5"/><path d="M4 6h4"/></svg>
                  <svg v-else width="12" height="12" viewBox="0 0 12 12"><line x1="3.5" y1="3.5" x2="8.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><line x1="8.5" y1="3.5" x2="3.5" y2="8.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
                </span>
                <span class="cp-session cp-session--sub" @mouseenter="onTruncatedEnter($event, sub.name)" @mouseleave="onTruncatedLeave">{{ sub.name }}</span>
                <button class="cp-dismiss" @click.stop="dismiss(sub.id)" :title="t('claudeStatus.dismiss')">×</button>
              </div>
              <div class="cp-row-meta cp-row-meta--sub">
                <span v-if="elapsedLabel(sub.id)" class="cp-elapsed">{{ t('claudeStatus.elapsedTime') }} {{ elapsedLabel(sub.id) }}</span>
                <span v-if="elapsedLabel(sub.id)" class="cp-sep">·</span>
                <span class="cp-tool-label">{{ t('claudeStatus.toolCountLabel') }}</span>
                <span v-if="sessionToolOkCounts[sub.id]" class="cp-count cp-count--ok">✓{{ sessionToolOkCounts[sub.id] }}</span>
                <span v-if="sessionToolFailCounts[sub.id]" class="cp-count cp-count--fail">✗{{ sessionToolFailCounts[sub.id] }}</span>
                <span v-if="!sessionToolOkCounts[sub.id] && !sessionToolFailCounts[sub.id]" class="cp-count">—</span>
                <span class="cp-status" :class="{ 'cp-status--stale': isStale(sub.id) }">{{ isStale(sub.id) ? t('claudeStatus.stale') : t("claudeStatus." + cfg(sub.state).label) }}</span>
              </div>
              <div v-if="sub.toolName || toolInputLabel(sub.toolInput)" class="cp-row-sub cp-row-sub--indented">
                <span v-if="sub.toolName" class="cp-tool" :title="toolLabel(sub.toolName)" @mouseenter="onTruncatedEnter($event, toolLabel(sub.toolName))" @mouseleave="onTruncatedLeave">
                  <svg v-if="toolIcon(sub.toolName)" width="11" height="11" viewBox="0 0 24 24" class="cp-tool-icon" v-html="toolIcon(sub.toolName)"></svg>
                  {{ toolLabel(sub.toolName) }}
                </span>
                <span v-if="toolInputLabel(sub.toolInput)" class="cp-row-sub-text" @mouseenter="onTruncatedEnter($event, toolInputFull(sub.toolInput))" @mouseleave="onTruncatedLeave">{{ toolInputLabel(sub.toolInput) }}</span>
                <span v-if="sub.toolOk === true" class="cp-tool-ok" title="OK">✓</span>
                <span v-if="sub.toolOk === false" class="cp-tool-err" title="Failed">✗</span>
              </div>
              <div v-if="sub.notification" class="cp-row-notif cp-row-notif--indented" :class="notifClass(sub.notificationType)"><svg width="12" height="12" viewBox="0 0 24 24" class="cp-notif-icon" v-html="notifIcon(sub.notificationType)"></svg>{{ sub.notification }}</div>
            </div>
          </template>
        </template>
      </div>
    </Transition>

    <!-- Hover tooltip for truncated text — teleported to avoid clipping -->
    <Teleport to="body">
      <div
        v-if="tooltip.show"
        class="cp-tooltip-overlay"
        :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }"
      >{{ tooltip.text }}</div>
    </Teleport>
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
  font-size: 9px;
  font-weight: 600;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.07em;
  padding: 6px 6px 3px;
  opacity: 0.55;
}

.cp-group-label:first-of-type {
  padding-top: 4px;
}

.cp-row {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 5px 6px;
  border-radius: 7px;
  transition: background 0.15s var(--ease-enter);
  overflow: hidden;
  min-width: 0;
}

.cp-row:hover {
  background: var(--lg-surface-hover);
}

/* 父行之间用 margin 间隔，不用 border-top（subagent 行不需要分隔线） */
.cp-row + .cp-row:not(.cp-row--sub) {
  margin-top: 2px;
}

/* thinking 状态行：极淡琥珀底色强化状态感知（补充圆点的颜色语义） */
.cp-row--thinking {
  background: rgba(245, 158, 11, 0.04);
}

.cp-row--thinking:hover {
  background: rgba(245, 158, 11, 0.07);
}

/* compact 状态行：极淡紫色底色 */
.cp-row--compact {
  background: rgba(139, 92, 246, 0.04);
}

.cp-row--compact:hover {
  background: rgba(139, 92, 246, 0.07);
}

/* error 状态行：极淡红色底色 */
.cp-row--error {
  background: rgba(239, 68, 68, 0.04);
}

.cp-row--error:hover {
  background: rgba(239, 68, 68, 0.07);
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
.state-compact  { color: #8B5CF6; }
.state-error    { color: #EF4444; }

.spin-svg {
  animation: cp-spin 0.8s linear infinite;
  transform-origin: 50% 50%;
}
@keyframes cp-spin { to { transform: rotate(-360deg); } }

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
  color: var(--text-tertiary);
  flex-shrink: 0;
}

/* 状态文字跟随状态语义色 */
.cp-row--thinking .cp-status { color: rgba(245, 158, 11, 0.8); }
.cp-row--compact  .cp-status { color: rgba(139, 92, 246, 0.8); }
.cp-row--error    .cp-status { color: rgba(239, 68, 68, 0.8); }
.cp-status--stale { color: #EF4444 !important; font-weight: 600; }

.cp-tool {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--accent);
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
  font-size: 10px;
  font-weight: 500;
  background: var(--accent-alpha-12);
  border: 1px solid var(--accent-alpha-20);
  padding: 1px 6px 2px;
  border-radius: 4px;
  flex-shrink: 0;
  line-height: 1.4;
  cursor: default;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cp-tool-icon {
  flex-shrink: 0;
  opacity: 0.75;
}

.cp-tool-ok {
  font-size: 10px;
  color: #10B981;
  flex-shrink: 0;
  line-height: 1;
}

.cp-tool-err {
  font-size: 10px;
  color: #EF4444;
  flex-shrink: 0;
  line-height: 1;
}

.cp-row-main {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

/* subagent 行：缩进 + 降低视觉重量（不用 opacity 以免影响文字对比度） */
.cp-row--sub {
  padding-left: 18px;
}

.cp-session--sub {
  font-weight: 500;
  color: var(--text-secondary);
}

.cp-row-meta,
.cp-row-sub,
.cp-row-notif {
  position: relative;
}

.cp-row-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 6px;
  padding-left: 18px; /* 与 cp-session 对齐（dot 12px + gap 6px） */
  min-width: 0;
  overflow: hidden;
}

.cp-row-sub {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 10px;
  color: var(--text-tertiary);
  padding-left: 18px; /* 与 cp-session 对齐（dot 12px + gap 6px） */
  min-width: 0;
  overflow: hidden;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
  opacity: 0.75;
}

.cp-row-sub-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
  flex: 1;
}

.cp-sep {
  font-size: 9px;
  color: var(--text-tertiary);
  opacity: 0.35;
  flex-shrink: 0;
}

.cp-tool-label {
  font-size: 10px;
  font-weight: 400;
  color: var(--text-tertiary);
  flex-shrink: 0;
}

/* subagent 行的 meta/sub 对齐：与 cp-session 对齐（dot 12px + gap 6px = 18px） */
.cp-row-meta--sub {
  padding-left: 18px;
}

.cp-row-sub--indented {
  padding-left: 18px;
}

.cp-row-notif {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 10px;
  font-weight: 500;
  color: var(--accent);
  padding: 2px 6px;
  padding-left: 18px;
  margin-top: 1px;
  border-radius: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cp-notif-icon {
  flex-shrink: 0;
}

.cp-row-notif--indented {
  padding-left: 18px;
}

.cp-notif--warn {
  color: #D97706;
}

.cp-notif--info {
  color: #3B82F6;
}

.cp-notif--ok {
  color: #059669;
}

.cp-count {
  font-size: 10px;
  font-weight: 400;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0;
}

.cp-count--ok {
  color: #10B981;
}

.cp-count--fail {
  color: #EF4444;
}

.cp-elapsed {
  font-size: 10px;
  font-weight: 400;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
  letter-spacing: 0;
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
  .cp-row { transition: none; }
  .claude-panel { transition: none; }
}
</style>

<!-- Non-scoped styles for teleported tooltip -->
<style>
.cp-tooltip-overlay {
  position: fixed;
  transform: translate(-50%, calc(-100% - 6px));
  background: var(--lg-surface-elevated);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border);
  border-radius: 6px;
  padding: 5px 10px;
  font-size: 11px;
  line-height: 1.5;
  color: var(--text-primary);
  font-family: "SF Mono", "Fira Code", ui-monospace, monospace;
  white-space: normal;
  word-break: break-word;
  max-width: 520px;
  pointer-events: none;
  z-index: 99999;
  box-shadow:
    0 6px 16px rgba(0, 0, 0, 0.35),
    0 0 0 0.5px rgba(0, 0, 0, 0.2);
  animation: cp-tooltip-in 0.12s ease-out both;
}

@keyframes cp-tooltip-in {
  from { opacity: 0; transform: translate(-50%, calc(-100% - 2px)); }
  to   { opacity: 1; transform: translate(-50%, calc(-100% - 6px)); }
}

@media (prefers-reduced-motion: reduce) {
  .cp-tooltip-overlay { animation: none; }
}
</style>

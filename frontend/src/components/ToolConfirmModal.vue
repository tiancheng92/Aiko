<!-- ToolConfirmModal.vue — confirmation dialog for shell/code tool execution requests -->
<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { ConfirmToolExecution } from '../../wailsjs/go/main/App'
import { useEscapeKey } from '../composables/useEscapeKey'

const visible = ref(false)
const request = ref(null) // ToolConfirmRequest
const editedContent = ref('')

/** Human-readable label for the tool type / language. */
const languageLabel = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell'
  const map = { python: 'Python', node: 'Node.js', ruby: 'Ruby', bash: 'Bash' }
  return map[request.value.language] || request.value.language
})

/** Risk warning text shown below the editor. */
const riskText = computed(() => {
  if (!request.value) return ''
  if (request.value.tool_type === 'shell') return 'Shell 命令可修改系统文件、执行任意操作，请确认安全后再批准。'
  return `${languageLabel.value} 代码将使用系统解释器直接执行，请检查内容后再批准。`
})

/** Called when the backend emits a tool:confirm event. */
function onConfirmEvent(req) {
  request.value = req
  editedContent.value = req.tool_type === 'shell' ? req.command : req.code
  visible.value = true
}

/** approve — send edited content back to the backend. */
async function approve() {
  visible.value = false
  await ConfirmToolExecution(request.value.id, true, editedContent.value)
}

/** reject — cancel the pending execution. */
async function reject() {
  visible.value = false
  await ConfirmToolExecution(request.value.id, false, '')
}

// EventsOff(name) removes all listeners for that name, so always invoke the
// handle returned by EventsOn instead.
let offConfirm = null
onMounted(() => { offConfirm = EventsOn('tool:confirm', onConfirmEvent) })
onUnmounted(() => offConfirm?.())

useEscapeKey(reject, visible)
</script>

<template>
  <Transition name="tool-confirm-pop">
  <div v-if="visible" class="tool-confirm-modal" role="dialog" aria-modal="true" aria-labelledby="tc-title">
    <div class="modal-backdrop" @click.self="reject" />
    <div class="modal-box">
      <div class="modal-header">
        <div class="modal-header-icon" aria-hidden="true">
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
            <line x1="12" y1="9" x2="12" y2="13"/>
            <line x1="12" y1="17" x2="12.01" y2="17"/>
          </svg>
        </div>
        <div class="modal-header-text">
          <h2 id="tc-title" class="modal-title">
            Agent 请求执行{{ request?.tool_type === 'shell' ? ' Shell 命令' : '代码' }}
          </h2>
          <span class="badge">{{ languageLabel }}</span>
        </div>
      </div>

      <div class="modal-field">
        <label>工作目录</label>
        <span class="dir-path">{{ request?.working_dir }}</span>
      </div>

      <div class="modal-field">
        <label>{{ request?.tool_type === 'shell' ? '命令' : '代码' }}<span class="editable-hint">（可编辑）</span></label>
        <textarea
          v-model="editedContent"
          class="content-editor"
          :rows="request?.tool_type === 'code' ? 8 : 3"
          spellcheck="false"
          autocomplete="off"
          autocorrect="off"
        />
      </div>

      <div class="risk-callout" role="note">
        <svg class="risk-icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="10"/>
          <line x1="12" y1="8" x2="12" y2="12"/>
          <line x1="12" y1="16" x2="12.01" y2="16"/>
        </svg>
        <span>{{ riskText }}</span>
      </div>

      <div class="modal-actions">
        <button class="btn-reject" @click="reject">拒绝</button>
        <button class="btn-approve" @click="approve">批准执行</button>
      </div>
    </div>
  </div>
  </Transition>
</template>

<style scoped>
.tool-confirm-modal {
  position: absolute;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: auto;
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', sans-serif;
  -webkit-font-smoothing: antialiased;
}

.modal-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}

.modal-box {
  position: relative;
  background: var(--lg-surface-modal);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid var(--lg-border);
  border-radius: 14px;
  padding: 24px;
  width: 480px;
  max-width: 90vw;
  box-shadow: var(--lg-shadow);
  color: var(--text-primary);
  display: flex;
  flex-direction: column;
  gap: 14px;
}

/* ── Header ───────────────────────────────────────────────────────────── */
.modal-header {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.modal-header-icon {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  border-radius: 9px;
  background: var(--warning-bg);
  color: var(--warning);
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.modal-header-icon :deep(svg) { width: 20px; height: 20px; }
.modal-header-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-top: 2px;
}
.modal-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  letter-spacing: -0.01em;
  line-height: 1.3;
}
.badge {
  align-self: flex-start;
  background: var(--lg-surface-input);
  color: var(--text-secondary);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 5px;
  padding: 2px 8px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
}

/* ── Fields ───────────────────────────────────────────────────────────── */
.modal-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.modal-field label {
  font-size: 11px;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-weight: 600;
}
.editable-hint {
  font-size: 11px;
  color: var(--text-tertiary);
  text-transform: none;
  letter-spacing: 0;
  font-weight: 400;
  margin-left: 4px;
}
.dir-path {
  font-size: 12px;
  color: var(--text-secondary);
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  padding: 8px 12px;
  background: rgba(0, 0, 0, 0.22);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 7px;
  word-break: break-all;
  line-height: 1.5;
}

.content-editor {
  width: 100%;
  background: rgba(0, 0, 0, 0.22);
  border: 1px solid var(--lg-border);
  border-radius: 8px;
  color: var(--text-primary);
  font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', Menlo, monospace;
  font-size: 13px;
  line-height: 1.55;
  padding: 10px 12px;
  resize: vertical;
  box-sizing: border-box;
  outline: none;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.content-editor:hover:not(:focus) { background: rgba(0, 0, 0, 0.28); }
.content-editor:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}

/* ── Risk callout ─────────────────────────────────────────────────────── */
.risk-callout {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  background: var(--warning-bg);
  border: 1px solid rgba(255, 159, 10, 0.3);
  border-radius: 8px;
  font-size: 12px;
  color: var(--warning);
  line-height: 1.5;
}
.risk-icon {
  width: 14px;
  height: 14px;
  flex-shrink: 0;
  margin-top: 2px;
}

/* ── Actions ──────────────────────────────────────────────────────────── */
.modal-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 4px;
  padding-top: 14px;
  border-top: 1px solid var(--lg-border-subtle);
}

.btn-reject,
.btn-approve {
  padding: 7px 18px;
  border-radius: 7px;
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  letter-spacing: -0.01em;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s, transform 0.08s;
  -webkit-appearance: none;
  appearance: none;
}
.btn-reject:active,
.btn-approve:active { transform: scale(0.97); }
.btn-reject:focus-visible,
.btn-approve:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

.btn-reject {
  background: var(--lg-surface-input);
  color: var(--text-primary);
  border: 1px solid var(--lg-border);
}
.btn-reject:hover { background: var(--lg-surface-input-h); border-color: var(--lg-border); }

.btn-approve {
  background: var(--accent);
  color: #fff;
  border: 1px solid transparent;
  font-weight: 600;
}
.btn-approve:hover { background: var(--accent-hover); }

/* ── Animations ───────────────────────────────────────────────────────── */
.tool-confirm-pop-enter-active { transition: opacity 0.22s ease; }
.tool-confirm-pop-leave-active { transition: opacity 0.14s ease-in; }
.tool-confirm-pop-enter-from,
.tool-confirm-pop-leave-to { opacity: 0; }

.tool-confirm-pop-enter-active .modal-box {
  transition: transform 0.24s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.tool-confirm-pop-leave-active .modal-box {
  transition: transform 0.14s ease-in;
}
.tool-confirm-pop-enter-from .modal-box { transform: scale(0.92); }
.tool-confirm-pop-leave-to .modal-box { transform: scale(0.96); }

@media (prefers-reduced-motion: reduce) {
  .tool-confirm-pop-enter-active,
  .tool-confirm-pop-leave-active { transition: opacity 0.1s; }
  .tool-confirm-pop-enter-active .modal-box,
  .tool-confirm-pop-leave-active .modal-box { transition: none; }
  .tool-confirm-pop-enter-from .modal-box,
  .tool-confirm-pop-leave-to .modal-box { transform: none; }
}
</style>

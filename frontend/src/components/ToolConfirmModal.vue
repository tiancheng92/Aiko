<!-- ToolConfirmModal.vue — confirmation dialog for shell/code tool execution requests -->
<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { ConfirmToolExecution } from '../../wailsjs/go/main/App'

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

/** User approved — send edited content back to the backend. */
async function approve() {
  visible.value = false
  await ConfirmToolExecution(request.value.id, true, editedContent.value)
}

/** User rejected — cancel the pending execution. */
async function reject() {
  visible.value = false
  await ConfirmToolExecution(request.value.id, false, '')
}

// Store the handler returned by EventsOn and invoke it on unmount — passing
// the event name alone to EventsOff removes all listeners for that event,
// which would break other components subscribing to the same name later.
let offConfirm = null
onMounted(() => { offConfirm = EventsOn('tool:confirm', onConfirmEvent) })
onUnmounted(() => { offConfirm?.() })
</script>

<template>
  <Transition name="tool-confirm-pop">
  <div v-if="visible" class="tool-confirm-modal">
    <div class="modal-backdrop" @click.self="reject" />
    <div class="modal-box">
      <div class="modal-header">
        <span class="badge">{{ languageLabel }}</span>
        <span class="title">⚠️ Agent 请求执行{{ request?.tool_type === 'shell' ? ' Shell 命令' : '代码' }}</span>
      </div>

      <div class="modal-field">
        <label>工作目录</label>
        <span class="dir-path">{{ request?.working_dir }}</span>
      </div>

      <div class="modal-field">
        <label>{{ request?.tool_type === 'shell' ? '命令' : '代码' }}（可编辑）</label>
        <textarea
          v-model="editedContent"
          class="content-editor"
          :rows="request?.tool_type === 'code' ? 8 : 3"
          spellcheck="false"
        />
      </div>

      <p class="risk-text">{{ riskText }}</p>

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
}
.modal-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
}
.modal-box {
  position: relative;
  background: rgb(5, 6, 12);
  backdrop-filter: blur(24px) saturate(140%);
  -webkit-backdrop-filter: blur(24px) saturate(140%);
  border: 1px solid rgba(255, 255, 255, 0.07);
  border-radius: 16px;
  padding: 24px;
  width: 480px;
  max-width: 90vw;
  box-shadow:
    0 16px 48px rgba(0, 0, 0, 0.65),
    0 1px 0 rgba(255, 255, 255, 0.05) inset;
}
.tool-confirm-pop-enter-active {
  transition: opacity 0.22s ease;
}
.tool-confirm-pop-leave-active {
  transition: opacity 0.14s ease-in;
}
.tool-confirm-pop-enter-from,
.tool-confirm-pop-leave-to {
  opacity: 0;
}
.tool-confirm-pop-enter-active .modal-box {
  transition: transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.tool-confirm-pop-leave-active .modal-box {
  transition: transform 0.14s ease-in;
}
.tool-confirm-pop-enter-from .modal-box,
.tool-confirm-pop-leave-to .modal-box {
  transform: scale(0.90);
}
.modal-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}
.badge {
  background: rgba(255,180,0,0.2);
  color: #ffb400;
  border: 1px solid rgba(255,180,0,0.3);
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 12px;
  font-weight: 600;
}
.title {
  font-size: 14px;
  font-weight: 600;
  color: #e0e0e0;
}
.modal-field {
  margin-bottom: 12px;
}
.modal-field label {
  display: block;
  font-size: 11px;
  color: #888;
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.dir-path {
  font-size: 12px;
  color: #aaa;
  font-family: monospace;
}
.content-editor {
  width: 100%;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 8px;
  color: #e0e0e0;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 13px;
  padding: 10px 12px;
  resize: vertical;
  box-sizing: border-box;
  outline: none;
}
.content-editor:focus {
  border-color: rgba(59, 130, 246, 0.4);
}
.risk-text {
  font-size: 12px;
  color: #f59e0b;
  margin: 12px 0 16px;
  line-height: 1.5;
}
.modal-actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}
.btn-reject {
  padding: 8px 20px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  background: rgba(255, 255, 255, 0.05);
  color: rgba(229, 231, 235, 0.8);
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s;
}
.btn-reject:hover { background: rgba(255, 255, 255, 0.1); }
.btn-approve {
  padding: 8px 20px;
  border-radius: 6px;
  border: none;
  background: #3b6ff5;
  color: #fff;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
}
.btn-approve:hover { background: #4a7cf7; }
</style>

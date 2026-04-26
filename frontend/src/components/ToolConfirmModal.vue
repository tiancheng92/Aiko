<!-- ToolConfirmModal.vue — confirmation dialog for shell/code tool execution requests -->
<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
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

onMounted(() => EventsOn('tool:confirm', onConfirmEvent))
onUnmounted(() => EventsOff('tool:confirm'))
</script>

<template>
  <Teleport to="body">
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
  </Teleport>
</template>

<style scoped>
.tool-confirm-modal {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
}
.modal-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
}
.modal-box {
  position: relative;
  background: #1e1e2e;
  border: 1px solid rgba(255,255,255,0.12);
  border-radius: 12px;
  padding: 24px;
  width: 480px;
  max-width: 90vw;
  box-shadow: 0 20px 60px rgba(0,0,0,0.5);
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
  background: #12121e;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px;
  color: #e0e0e0;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 13px;
  padding: 10px 12px;
  resize: vertical;
  box-sizing: border-box;
  outline: none;
}
.content-editor:focus {
  border-color: rgba(120,160,255,0.4);
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
  border-radius: 6px;
  border: 1px solid rgba(255,255,255,0.15);
  background: transparent;
  color: #ccc;
  cursor: pointer;
  font-size: 13px;
}
.btn-reject:hover { background: rgba(255,255,255,0.06); }
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

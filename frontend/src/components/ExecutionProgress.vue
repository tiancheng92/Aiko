<!-- ExecutionProgress.vue — in-chat indicator shown while a tool command is running -->
<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { KillToolExecution } from '../../wailsjs/go/main/App'

/** Currently running executions: [{ id, elapsed, intervalId }] */
const executions = ref([])

/** Starts tracking a new execution when backend emits tool:executing. */
function onExecuting({ id }) {
  const startTime = Date.now()
  const intervalId = setInterval(() => {
    const item = executions.value.find(e => e.id === id)
    if (item) item.elapsed = Math.floor((Date.now() - startTime) / 1000)
  }, 1000)
  executions.value.push({ id, elapsed: 0, intervalId })
}

/** Removes the execution entry when backend emits tool:executed. */
function onExecuted({ id }) {
  const idx = executions.value.findIndex(e => e.id === id)
  if (idx !== -1) {
    clearInterval(executions.value[idx].intervalId)
    executions.value.splice(idx, 1)
  }
}

/** Sends a kill signal to the running process. */
async function kill(id) {
  await KillToolExecution(id)
}

// Store the handler refs returned by EventsOn; passing just the event name to
// EventsOff would tear down any other subscribers registered for the same name.
let offExecuting = null
let offExecuted = null
onMounted(() => {
  offExecuting = EventsOn('tool:executing', onExecuting)
  offExecuted = EventsOn('tool:executed', onExecuted)
})
onUnmounted(() => {
  offExecuting?.()
  offExecuted?.()
  executions.value.forEach(e => clearInterval(e.intervalId))
})
</script>

<template>
  <div v-for="exec in executions" :key="exec.id" class="execution-progress">
    <span class="exec-icon">⚙️</span>
    <span class="exec-label">正在执行工具…</span>
    <span class="exec-timer">{{ exec.elapsed }}s</span>
    <button class="exec-kill" @click="kill(exec.id)">终止</button>
  </div>
</template>

<style scoped>
.execution-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 8px;
  padding: 8px 14px;
  margin: 4px 0;
  font-size: 13px;
  color: #ccc;
}
.exec-icon { font-size: 14px; }
.exec-label { flex: 1; }
.exec-timer { color: #888; font-family: monospace; }
.exec-kill {
  padding: 3px 10px;
  border-radius: 4px;
  border: 1px solid rgba(255,80,80,0.4);
  background: rgba(255,80,80,0.1);
  color: #ff6b6b;
  cursor: pointer;
  font-size: 12px;
}
.exec-kill:hover { background: rgba(255,80,80,0.2); }
</style>

<!-- ExecutionProgress.vue — in-chat indicator shown while a tool command is running -->
<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'
import { KillToolExecution } from '../../bindings/aiko/internal/services/toolservice'

/** Currently running executions: [{ id, elapsed, intervalId }] */
const executions = ref([])

/** Starts tracking a new execution when backend emits tool:executing. */
function onExecuting(event) {
  const { id } = event.data
  const startTime = Date.now()
  const intervalId = setInterval(() => {
    const item = executions.value.find(e => e.id === id)
    if (item) item.elapsed = Math.floor((Date.now() - startTime) / 1000)
  }, 1000)
  executions.value.push({ id, elapsed: 0, intervalId })
}

/** Removes the execution entry when backend emits tool:executed. */
function onExecuted(event) {
  const { id } = event.data
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
  offExecuting = Events.On('tool:executing', onExecuting)
  offExecuted = Events.On('tool:executed', onExecuted)
})
onUnmounted(() => {
  offExecuting?.()
  offExecuted?.()
  executions.value.forEach(e => clearInterval(e.intervalId))
})
</script>

<template>
  <TransitionGroup name="exec-item" tag="div" class="exec-list">
    <div v-for="exec in executions" :key="exec.id" class="execution-progress">
      <span class="exec-spinner" aria-hidden="true" />
      <span class="exec-label">正在执行工具…</span>
      <span class="exec-timer">{{ exec.elapsed }}s</span>
      <button class="exec-kill" @click="kill(exec.id)">终止</button>
    </div>
  </TransitionGroup>
</template>

<style scoped>
.exec-list { display: contents; }

.execution-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.10);
  border-radius: 8px;
  padding: 8px 14px;
  margin: 4px 0;
  font-size: 13px;
  color: #ccc;
}

/* Animated spinner replacing the static emoji */
.exec-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.15);
  border-top-color: rgba(255, 255, 255, 0.6);
  border-radius: 50%;
  flex-shrink: 0;
  animation: exec-spin 0.8s linear infinite;
}
@keyframes exec-spin { to { transform: rotate(360deg); } }

.exec-label { flex: 1; }
.exec-timer { color: #888; font-family: monospace; }

.exec-kill {
  padding: 3px 10px;
  border-radius: 4px;
  border: 1px solid rgba(255, 80, 80, 0.4);
  background: rgba(255, 80, 80, 0.1);
  color: #ff6b6b;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.12s, transform 0.08s;
}
.exec-kill:hover  { background: rgba(255, 80, 80, 0.22); }
.exec-kill:active { transform: scale(0.96); }

/* TransitionGroup: slide-down + fade on enter, fade on leave */
.exec-item-enter-active {
  transition: opacity 0.22s ease, transform 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}
.exec-item-leave-active {
  transition: opacity 0.15s ease-in, transform 0.15s ease-in;
}
.exec-item-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}
.exec-item-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
@media (prefers-reduced-motion: reduce) {
  .exec-item-enter-active,
  .exec-item-leave-active { transition: opacity 0.12s; }
  .exec-item-enter-from,
  .exec-item-leave-to     { transform: none; }
  .exec-spinner           { animation: none; }
}
</style>

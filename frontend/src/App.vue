<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import FloatingBall from './components/FloatingBall.vue'
import ChatBubble from './components/ChatBubble.vue'
import { MissingRequiredConfig } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

const bubbleOpen = ref(false)
const activeTab = ref('chat')
const ballPos  = ref({ x: -1, y: -1 })
const ballSize = ref(64)
let offToggle

/** waitForRuntime polls until the Wails Go bridge is available. */
async function waitForRuntime() {
  while (!window.go?.main?.App) {
    await new Promise(r => setTimeout(r, 20))
  }
}

onMounted(async () => {
  await waitForRuntime()
  const missing = await MissingRequiredConfig()
  if (missing && missing.length > 0) {
    activeTab.value = 'settings'
    bubbleOpen.value = true
  }
  offToggle = EventsOn('bubble:toggle', () => { bubbleOpen.value = !bubbleOpen.value })
})

onUnmounted(() => { offToggle?.() })

/** toggleBubble flips the bubble open/close state. */
function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
}
</script>

<template>
  <FloatingBall
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
  />
  <ChatBubble
    v-if="bubbleOpen"
    v-model:tab="activeTab"
    :ball-pos="ballPos"
    :ball-size="ballSize"
    @close="bubbleOpen = false"
  />
</template>

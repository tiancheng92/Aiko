<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import FloatingBall from './components/FloatingBall.vue'
import ChatBubble from './components/ChatBubble.vue'
import { MissingRequiredConfig } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

const bubbleOpen = ref(false)
const activeTab = ref('chat')
let offToggle

onMounted(async () => {
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
  <FloatingBall @click="toggleBubble" />
  <ChatBubble
    v-if="bubbleOpen"
    v-model:tab="activeTab"
    @close="bubbleOpen = false"
  />
</template>

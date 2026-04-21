<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import Live2DPet from './components/Live2DPet.vue'
import ChatBubble from './components/ChatBubble.vue'
import SettingsWindow from './components/SettingsWindow.vue'
import { MissingRequiredConfig } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

const bubbleOpen = ref(false)
const settingsOpen = ref(false)
const ballPos  = ref({ x: -1, y: -1 })
const ballSize = ref(160)
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
    settingsOpen.value = true
  }
  offToggle = EventsOn('bubble:toggle', () => { bubbleOpen.value = !bubbleOpen.value })
})

onUnmounted(() => { offToggle?.() })

/** toggleBubble flips the chat bubble open/close state. */
function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
}

/** openSettings opens the settings window. */
function openSettings() {
  settingsOpen.value = true
}
</script>

<template>
  <Live2DPet
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
    @open-settings="openSettings"
  />
  <ChatBubble
    v-if="bubbleOpen"
    :ball-pos="ballPos"
    :ball-size="ballSize"
    @close="bubbleOpen = false"
    @open-settings="openSettings"
  />
  <SettingsWindow
    v-if="settingsOpen"
    @close="settingsOpen = false"
  />
</template>

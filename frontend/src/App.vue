<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import Live2DPet from './components/Live2DPet.vue'
import ChatBubble from './components/ChatBubble.vue'
import SettingsWindow from './components/SettingsWindow.vue'
import NotificationBubble from './components/NotificationBubble.vue'
import { MissingRequiredConfig, IsFirstLaunch, MarkWelcomeShown } from '../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime'

const bubbleOpen = ref(false)
const settingsOpen = ref(false)
const ballPos  = ref({ x: -1, y: -1 })
const ballSize = ref(160)
let offToggle, offToken, offDone, offError, offSettings

// Accumulates tokens when chat bubble is closed.
let pendingTokens = ''

/** waitForRuntime polls until the Wails Go bridge is available. */
async function waitForRuntime() {
  while (!window.go?.main?.App) {
    await new Promise(r => setTimeout(r, 20))
  }
}

onMounted(async () => {
  await waitForRuntime()
  const missing = await MissingRequiredConfig()
  const firstLaunch = await IsFirstLaunch()
  if (firstLaunch) {
    await MarkWelcomeShown()
    EventsEmit('notification:show', {
      title: '你好！我是你的桌面宠物 ✨',
      message: '请先在设置中配置 LLM 接口，然后就可以开始聊天了~',
    })
  }
  offToggle = EventsOn('bubble:toggle', () => { bubbleOpen.value = !bubbleOpen.value })
  offSettings = EventsOn('settings:open', () => { settingsOpen.value = true })

  // Always listen for chat stream events so we can show a notification
  // when the chat bubble is closed (e.g. scheduler-triggered replies).
  offToken = EventsOn('chat:token', (token) => {
    if (!bubbleOpen.value) {
      pendingTokens += token
    }
  })

  offDone = EventsOn('chat:done', () => {
    if (!bubbleOpen.value && pendingTokens.trim()) {
      EventsEmit('notification:show', {
        title: '✨ (=^･ω･^=)',
        message: pendingTokens.trim(),
      })
    }
    pendingTokens = ''
  })

  offError = EventsOn('chat:error', (err) => {
    if (!bubbleOpen.value) {
      pendingTokens = ''
      EventsEmit('notification:show', {
        title: '😿 出错了',
        message: err,
      })
    }
  })
})

onUnmounted(() => {
  offToggle?.()
  offToken?.()
  offDone?.()
  offError?.()
  offSettings?.()
})

/** toggleBubble flips the chat bubble open/close state. */
function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
  // Discard any pending tokens when user opens the bubble —
  // the ChatPanel will show the streamed content directly.
  if (bubbleOpen.value) pendingTokens = ''
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
    v-show="bubbleOpen"
    :ball-pos="ballPos"
    :ball-size="ballSize"
    @close="bubbleOpen = false"
    @open-settings="openSettings"
  />
  <SettingsWindow
    v-if="settingsOpen"
    @close="settingsOpen = false"
  />
  <NotificationBubble
    :pet-pos="ballPos"
    :pet-size="ballSize"
  />
</template>

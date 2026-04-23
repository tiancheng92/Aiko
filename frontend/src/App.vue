<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import Live2DPet from './components/Live2DPet.vue'
import ChatBubble from './components/ChatBubble.vue'
import SettingsWindow from './components/SettingsWindow.vue'
import NotificationBubble from './components/NotificationBubble.vue'
import { MissingRequiredConfig, IsFirstLaunch, MarkWelcomeShown, GetScreenSize } from '../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../wailsjs/runtime/runtime'

const bubbleOpen = ref(false)
const settingsOpen = ref(false)
const ballPos  = ref({ x: -1, y: -1 })
const ballSize = ref(160)
const chatBubbleRef = ref(null)
// activeScreen holds the current screen resolution; updated on screen:changed events.
const activeScreen = ref({ width: 0, height: 0 })
let offToggle, offToken, offDone, offError, offSettings
const voiceActive = ref(false)
let offVoiceStart, offVoiceEnd, offVoiceError

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
  try {
    const [w, h] = await GetScreenSize()
    if (w > 0 && h > 0) activeScreen.value = { width: w, height: h }
  } catch (e) {
    console.warn('App.vue: GetScreenSize failed', e)
  }
  const missing = await MissingRequiredConfig()
  const firstLaunch = await IsFirstLaunch()
  if (firstLaunch) {
    await MarkWelcomeShown()
    EventsEmit('notification:show', {
      title: '你好！我是你的桌面宠物 ✨',
      message: '请先在设置中配置 LLM 接口，然后就可以开始聊天了~',
    })
  }
  offToggle = EventsOn('bubble:toggle', () => {
    bubbleOpen.value = !bubbleOpen.value
    if (bubbleOpen.value) {
      pendingTokens = ''
      nextTick(() => {
        chatBubbleRef.value?.focusInput()
        chatBubbleRef.value?.scrollToBottom()
      })
    }
  })
  offSettings = EventsOn('settings:open', () => { settingsOpen.value = true })
  offVoiceStart = EventsOn('voice:start', () => {
    if (!bubbleOpen.value) {
      bubbleOpen.value = true
      nextTick(() => {
        chatBubbleRef.value?.focusInput()
        chatBubbleRef.value?.scrollToBottom()
      })
    }
    voiceActive.value = true
  })

  offVoiceEnd = EventsOn('voice:end', () => {
    voiceActive.value = false
  })

  offVoiceError = EventsOn('voice:error', () => {
    voiceActive.value = false
  })
  EventsOn('screen:changed', (info) => {
    activeScreen.value = { width: info.width, height: info.height }
    EventsEmit('screen:active:changed', info)
  })

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
  offVoiceStart?.()
  offVoiceEnd?.()
  offVoiceError?.()
})

/** toggleBubble flips the chat bubble open/close state. */
function toggleBubble() {
  bubbleOpen.value = !bubbleOpen.value
  // Discard any pending tokens when user opens the bubble —
  // the ChatPanel will show the streamed content directly.
  if (bubbleOpen.value) {
    pendingTokens = ''
    nextTick(() => {
      chatBubbleRef.value?.focusInput()
      chatBubbleRef.value?.scrollToBottom()
    })
  }
}

/** openSettings opens the settings window. */
function openSettings() {
  settingsOpen.value = true
}
</script>

<template>
  <Live2DPet
    :active-screen="activeScreen"
    @click="toggleBubble"
    @position="p => ballPos = p"
    @ball-size="s => ballSize = s"
    @open-settings="openSettings"
  />
  <ChatBubble
    ref="chatBubbleRef"
    v-show="bubbleOpen"
    :ball-pos="ballPos"
    :ball-size="ballSize"
    :active-screen="activeScreen"
    @close="bubbleOpen = false"
    @open-settings="openSettings"
  />
  <SettingsWindow
    v-if="settingsOpen"
    :active-screen="activeScreen"
    @close="settingsOpen = false"
  />
  <NotificationBubble
    :pet-pos="ballPos"
    :pet-size="ballSize"
  />

  <!-- Voice recording visual effects: Siri border + wave rings -->
  <div class="siri-border" :class="{ active: voiceActive }" />
  <div v-if="voiceActive" class="voice-wave">
    <div
      v-for="i in 4"
      :key="i"
      class="wave-ring"
      :style="{ animationDelay: `${(i - 1) * 0.3}s` }"
    />
  </div>
</template>

<style scoped>
/* ── Siri-style screen border glow ─────────────────────────── */
.siri-border {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 9998;
  border-radius: 12px;
  opacity: 0;
  transition: opacity 0.3s ease;
}

.siri-border.active {
  opacity: 1;
  animation: siri-border-flow 3s linear infinite;
}

@keyframes siri-border-flow {
  0%   { box-shadow: 0 0 20px 4px #3b82f6, inset 0 0 0 3px #3b82f6; }
  25%  { box-shadow: 0 0 20px 4px #8b5cf6, inset 0 0 0 3px #8b5cf6; }
  50%  { box-shadow: 0 0 20px 4px #ec4899, inset 0 0 0 3px #ec4899; }
  75%  { box-shadow: 0 0 20px 4px #f97316, inset 0 0 0 3px #f97316; }
  100% { box-shadow: 0 0 20px 4px #3b82f6, inset 0 0 0 3px #3b82f6; }
}

/* ── Centered circular wave rings ──────────────────────────── */
.voice-wave {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  z-index: 9997;
}

.wave-ring {
  position: absolute;
  width: 80px;
  height: 80px;
  border-radius: 50%;
  border: 2px solid #8b5cf6;
  animation: wave-pulse 1.6s ease-out infinite;
}

@keyframes wave-pulse {
  0%   { transform: scale(1);   opacity: 0.6; }
  100% { transform: scale(3);   opacity: 0; }
}
</style>

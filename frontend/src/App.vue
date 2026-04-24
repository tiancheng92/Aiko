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
  <div v-if="voiceActive" class="siri-outer">
    <div class="siri-spinning" />
  </div>
  <div v-if="voiceActive" class="voice-wave">
    <div
      v-for="i in 3"
      :key="i"
      class="wave-ring"
      :style="{ animationDelay: `${(i - 1) * 0.55}s` }"
    />
    <div class="wave-core" />
  </div>
</template>

<style scoped>
/*
 * Siri border effect — based on Apple's spinning wheel technique:
 * A large Apple-color-wheel SVG rotates behind the screen, blurred heavily.
 * A white mask covers the center, leaving only the glowing edge visible.
 *
 * SVG: Apple spinning wheel with 6 colored segments (green/orange/purple/red/blue/yellow)
 */

/* ── Container: clips to screen, no pointer events ─────────── */
.siri-outer {
  position: fixed;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
  z-index: 9998;
  border-radius: 12px;
}

/* ── The rotating Apple color wheel ────────────────────────── */
.siri-spinning {
  position: absolute;
  /* Centered, oversized so the wheel fills beyond screen edges */
  width: 140%;
  aspect-ratio: 1 / 1;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%) rotate(0deg) scale(1.5);
  background-image: url("data:image/svg+xml,%3Csvg id='spinning' xmlns='http://www.w3.org/2000/svg' viewBox='0 0 500 500'%3E%3Cdefs%3E%3Cstyle%3E.st0%7Bfill:%233bb44c%7D.st1%7Bfill:%23f79c1d%7D.st2%7Bfill:%23a95fa6%7D.st3%7Bfill:%23ee3c3c%7D.st4%7Bfill:%234791ce%7D.st5%7Bfill:%23f9d20a%7D%3C/style%3E%3C/defs%3E%3Cpath class='st1' d='M355.05,23.15c13.68,6.33,26.7,13.87,38.9,22.49,5.76,16.1,8.9,33.44,8.9,51.51,0,27.83-7.44,53.93-20.44,76.4-26.42,45.7-75.82,76.45-132.41,76.45,28.29-49,26.37-107.15,0-152.88-12.96-22.5-31.84-42-55.95-55.92-15.67-9.05-32.27-15-49.1-18.05C176.88,8.35,212.47.1,250,.1s73.12,8.25,105.05,23.05Z'/%3E%3Cpath class='st4' d='M250,250c-28.29,49-26.37,107.15,0,152.88,12.96,22.5,31.84,42,55.95,55.92,15.67,9.05,32.27,15,49.1,18.05-31.93,14.8-67.52,23.05-105.05,23.05s-73.12-8.25-105.05-23.05c-13.68-6.33-26.7-13.87-38.9-22.49-5.76-16.1-8.9-33.44-8.9-51.51,0-27.83,7.44-53.92,20.43-76.4h.01c26.42-45.7,75.82-76.45,132.41-76.45Z'/%3E%3Cpath class='st5' d='M250,97.12c26.37,45.73,28.29,103.88,0,152.88-28.29-49.01-79.62-76.41-132.41-76.45-25.96-.02-52.28,6.58-76.39,20.5-15.62,9.02-29.05,20.38-40.1,33.39,3.31-37.08,14.7-71.81,32.4-102.43,18.17-31.44,42.99-58.53,72.55-79.37,12.2-8.62,25.22-16.16,38.9-22.49,16.83,3.05,33.43,9,49.1,18.05,24.11,13.92,42.99,33.42,55.95,55.92Z'/%3E%3Cpath class='st3' d='M498.9,227.45c.66,7.43,1,14.94,1,22.55s-.34,15.13-1,22.56c-11.05,13.01-24.48,24.37-40.1,33.39-24.11,13.92-50.42,20.52-76.38,20.5h-.01c-52.79-.04-104.12-27.44-132.41-76.45,56.59,0,105.99-30.75,132.41-76.45,13-22.47,20.44-48.57,20.44-76.4,0-18.07-3.14-35.41-8.9-51.51,29.56,20.84,54.38,47.93,72.55,79.37,17.7,30.62,29.09,65.35,32.4,102.43h0Z'/%3E%3Cpath class='st2' d='M250,250c28.29,49.01,79.62,76.41,132.41,76.45h.01c25.96.02,52.27-6.58,76.38-20.5,15.62-9.02,29.05-20.38,40.1-33.39-3.31,37.08-14.7,71.81-32.4,102.43-18.17,31.44-42.99,58.53-72.55,79.37-12.2,8.62-25.22,16.16-38.9,22.49-16.83-3.05-33.43-9-49.1-18.05-24.11-13.92-42.99-33.42-55.95-55.92-26.37-45.73-28.29-103.88,0-152.88Z'/%3E%3Cpath class='st0' d='M106.05,454.36c-29.56-20.84-54.38-47.93-72.55-79.37-17.7-30.62-29.09-65.35-32.4-102.43-.66-7.43-1-14.95-1-22.56s.34-15.13,1-22.56c11.05-13.01,24.48-24.37,40.1-33.39,24.11-13.92,50.43-20.52,76.39-20.5,52.79.04,104.12,27.44,132.41,76.45-56.59,0-105.99,30.75-132.41,76.45h-.01c-12.99,22.48-20.43,48.57-20.43,76.4,0,18.07,3.14,35.41,8.9,51.51Z'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: center center;
  background-size: 150%;
  filter: blur(50px);
  animation: siri-rotate 10s linear infinite;
}

/* ── White mask: covers center, leaves glowing edge ─────────── */
.siri-mask {
  position: absolute;
  top: 12px;
  left: 12px;
  right: 12px;
  bottom: 12px;
  border-radius: 8px;
  /* Match the app background color */
  background: transparent;
  animation: siri-breathe 5s ease-in-out infinite;
}

@keyframes siri-rotate {
  from { transform: translate(-50%, -50%) rotate(0deg) scale(1.5); }
  to   { transform: translate(-50%, -50%) rotate(360deg) scale(1.5); }
}

@keyframes siri-breathe {
  0%, 100% { top: 13px; left: 13px; right: 13px; bottom: 13px; }
  50%       { top: 9px;  left: 9px;  right: 9px;  bottom: 9px; }
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

.wave-core {
  position: absolute;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: radial-gradient(circle, #ffffff 0%, #4791ce 60%, transparent 100%);
  box-shadow: 0 0 12px 4px rgba(71, 145, 206, 0.8),
              0 0 24px 8px rgba(169, 95, 166, 0.4);
  animation: core-pulse 1.6s ease-in-out infinite;
}

@keyframes core-pulse {
  0%, 100% { transform: scale(1);   opacity: 1; }
  50%       { transform: scale(1.3); opacity: 0.8; }
}

.wave-ring {
  position: absolute;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 1.5px solid rgba(71, 145, 206, 0.6);
  animation: wave-expand 1.65s cubic-bezier(0.2, 0.6, 0.4, 1) infinite;
}

.wave-ring:nth-child(2) { border-color: rgba(169, 95, 166, 0.5); }
.wave-ring:nth-child(3) { border-color: rgba(59, 180, 76, 0.4); }

@keyframes wave-expand {
  0%   { transform: scale(1);   opacity: 0.7; }
  100% { transform: scale(4.5); opacity: 0; }
}
</style>

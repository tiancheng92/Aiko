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
  <div class="siri-border" :class="{ active: voiceActive }">
    <div class="siri-border-inner" />
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
/* ── Apple Intelligence Siri border ────────────────────────── */
/*
 * Technique: rotating conic-gradient mask + blur bloom.
 * The outer div clips to screen edges; the inner pseudo creates
 * the rotating gradient. A second blurred copy gives the glow bloom.
 */

@property --siri-angle {
  syntax: '<angle>';
  initial-value: 0deg;
  inherits: false;
}

.siri-border {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 9998;
  border-radius: 12px;
  opacity: 0;
  transition: opacity 0.5s ease;
}

.siri-border.active {
  opacity: 1;
}

/* Sharp border layer */
.siri-border.active::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 12px;
  padding: 3px;
  background: conic-gradient(
    from var(--siri-angle),
    transparent 0deg,
    transparent 60deg,
    #bf5af2 80deg,
    #ffffff 100deg,
    #0a84ff 130deg,
    #32d74b 155deg,
    transparent 180deg,
    transparent 230deg,
    #ff375f 255deg,
    #ff9f0a 275deg,
    transparent 310deg,
    transparent 360deg
  );
  -webkit-mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask-composite: exclude;
  animation: siri-rotate 3s linear infinite;
}

/* Blurred glow bloom layer */
.siri-border.active::after {
  content: '';
  position: absolute;
  inset: -2px;
  border-radius: 14px;
  padding: 6px;
  background: conic-gradient(
    from var(--siri-angle),
    transparent 0deg,
    transparent 60deg,
    #bf5af2 80deg,
    #ffffff 100deg,
    #0a84ff 130deg,
    #32d74b 155deg,
    transparent 180deg,
    transparent 230deg,
    #ff375f 255deg,
    #ff9f0a 275deg,
    transparent 310deg,
    transparent 360deg
  );
  -webkit-mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask-composite: exclude;
  filter: blur(8px);
  opacity: 0.7;
  animation: siri-rotate 3s linear infinite;
}

.siri-border-inner {
  display: none;
}

@keyframes siri-rotate {
  to { --siri-angle: 360deg; }
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

/* Core glowing dot */
.wave-core {
  position: absolute;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: radial-gradient(circle, #ffffff 0%, #0a84ff 60%, transparent 100%);
  box-shadow: 0 0 12px 4px rgba(10, 132, 255, 0.8),
              0 0 24px 8px rgba(191, 90, 242, 0.4);
  animation: core-pulse 1.6s ease-in-out infinite;
}

@keyframes core-pulse {
  0%, 100% { transform: scale(1);   opacity: 1; }
  50%       { transform: scale(1.3); opacity: 0.8; }
}

/* Expanding rings */
.wave-ring {
  position: absolute;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 1.5px solid rgba(10, 132, 255, 0.6);
  animation: wave-expand 1.65s cubic-bezier(0.2, 0.6, 0.4, 1) infinite;
}

.wave-ring:nth-child(2) { border-color: rgba(191, 90, 242, 0.5); }
.wave-ring:nth-child(3) { border-color: rgba(255, 55, 95, 0.4); }

@keyframes wave-expand {
  0%   { transform: scale(1);   opacity: 0.7; }
  100% { transform: scale(4.5); opacity: 0; }
}
</style>

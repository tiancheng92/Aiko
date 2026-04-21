<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, GetMessages, ClearChatHistory } from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'

const messages = ref([])
const input = ref('')
const loading = ref(false)
const messagesEl = ref(null)

let offToken, offDone, offError, offClear

onMounted(async () => {
  const history = await GetMessages(50)
  messages.value = (history || []).map(m => ({ role: m.Role, content: m.Content }))
  scrollToBottom()

  offClear = EventsOn('chat:clear', async () => {
    try {
      await ClearChatHistory()
      messages.value = []
    } catch (e) {
      console.error('clear chat history failed:', e)
    }
  })

  offToken = EventsOn('chat:token', (token) => {
    const idx = messages.value.length - 1
    const last = messages.value[idx]
    if (last && last.role === 'assistant' && last.streaming) {
      messages.value[idx] = { ...last, content: last.content + token }
    } else {
      messages.value.push({ role: 'assistant', content: token, streaming: true })
      EventsEmit('pet:state:change', 'speaking')
    }
    scrollToBottom()
  })

  offDone = EventsOn('chat:done', () => {
    const idx = messages.value.length - 1
    if (idx >= 0) messages.value[idx] = { ...messages.value[idx], streaming: false }
    loading.value = false
    EventsEmit('pet:state:change', 'idle')
  })

  offError = EventsOn('chat:error', (err) => {
    messages.value.push({ role: 'system', content: '错误: ' + err })
    loading.value = false
    EventsEmit('pet:state:change', 'error')
  })
})

onUnmounted(() => { offToken?.(); offDone?.(); offError?.(); offClear?.() })

/** send submits the current input as a user message. */
async function send() {
  const text = input.value.trim()
  if (!text || loading.value) return
  input.value = ''
  loading.value = true
  messages.value.push({ role: 'user', content: text })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  try {
    await SendMessage(text)
  } catch (e) {
    messages.value.push({ role: 'system', content: '发送失败: ' + e })
    loading.value = false
    EventsEmit('pet:state:change', 'error')
  }
}

/** scrollToBottom scrolls the message list to the bottom on the next tick. */
function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
}
</script>

<template>
  <div class="chat-panel">
    <div class="messages" ref="messagesEl">
      <div v-for="(m, i) in messages" :key="i" :class="['msg', m.role]">
        <span class="bubble">{{ m.content }}<span v-if="m.streaming" class="cursor">▋</span></span>
      </div>
    </div>
    <div class="input-row">
      <textarea
        v-model="input"
        placeholder="输入消息... (Enter 发送)"
        rows="1"
        @keydown.enter.exact.prevent="send"
        :disabled="loading"
      />
      <button @click="send" :disabled="loading">发送</button>
    </div>
  </div>
</template>

<style scoped>
.chat-panel { display: flex; flex-direction: column; height: 100%; }
.messages { flex: 1; overflow-y: auto; padding: 12px; display: flex; flex-direction: column; gap: 8px; }
.msg { display: flex; }
.msg.user { justify-content: flex-end; }
.msg.assistant, .msg.system { justify-content: flex-start; }
.bubble { max-width: 80%; padding: 8px 12px; border-radius: 12px; font-size: 13px; line-height: 1.5; white-space: pre-wrap; word-break: break-word; }
.user .bubble { background: #4f46e5; color: #fff; border-radius: 12px 12px 2px 12px; }
.assistant .bubble { background: #374151; color: #e5e7eb; border-radius: 12px 12px 12px 2px; }
.system .bubble { background: #dc2626; color: #fff; border-radius: 8px; font-size: 12px; }
.cursor { animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }
.input-row { display: flex; gap: 8px; padding: 10px; border-top: 1px solid #374151; flex-shrink: 0; }
.input-row textarea { flex: 1; background: #1f2937; border: 1px solid #374151; border-radius: 8px; padding: 8px 12px; color: #f9fafb; font-size: 13px; outline: none; resize: none; font-family: inherit; }
.input-row button { background: #4f46e5; color: #fff; border: none; border-radius: 8px; padding: 8px 16px; cursor: pointer; font-size: 13px; }
.input-row button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>

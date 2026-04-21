<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, GetMessages, ClearChatHistory } from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { marked, Renderer } from 'marked'
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import bash from 'highlight.js/lib/languages/bash'
import go from 'highlight.js/lib/languages/go'
import json from 'highlight.js/lib/languages/json'
import css from 'highlight.js/lib/languages/css'
import xml from 'highlight.js/lib/languages/xml'
import 'highlight.js/styles/github-dark.css'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('go', go)
hljs.registerLanguage('json', json)
hljs.registerLanguage('css', css)
hljs.registerLanguage('xml', xml)

const renderer = new Renderer()
renderer.code = ({ text, lang }) => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(text, { language }).value
    : hljs.highlightAuto(text).value
  const cls = language ? `hljs language-${language}` : 'hljs'
  return `<pre><code class="${cls}">${highlighted}</code></pre>`
}

marked.use({ renderer, breaks: true, gfm: true })

const messages = ref([])
const input = ref('')
const loading = ref(false)
const messagesEl = ref(null)
const copiedIdx = ref(null)

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
    // Remove thinking placeholder on first real token.
    const thinkIdx = messages.value.findLastIndex(m => m.thinking)
    if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)

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
    const thinkIdx = messages.value.findLastIndex(m => m.thinking)
    if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)
    messages.value.push({ role: 'system', content: '错误: ' + err })
    loading.value = false
    EventsEmit('pet:state:change', 'error')
  })
})

onUnmounted(() => { offToken?.(); offDone?.(); offError?.(); offClear?.() })

/** renderMarkdown converts markdown text to HTML. */
function renderMarkdown(text) {
  if (!text) return ''
  return marked(text)
}

/** copyMessage copies the message content to clipboard. */
async function copyMessage(idx) {
  const m = messages.value[idx]
  if (!m) return
  try {
    await navigator.clipboard.writeText(m.content)
    copiedIdx.value = idx
    setTimeout(() => { copiedIdx.value = null }, 2000)
  } catch {}
}

/** send submits the current input as a user message. */
async function send() {
  const text = input.value.trim()
  if (!text || loading.value) return
  input.value = ''
  loading.value = true
  messages.value.push({ role: 'user', content: text })
  messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  try {
    await SendMessage(text)
  } catch (e) {
    const idx = messages.value.findLastIndex(m => m.thinking)
    if (idx >= 0) messages.value.splice(idx, 1)
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
        <div class="bubble-wrap">
          <span v-if="m.role !== 'assistant'" class="bubble">
            {{ m.content }}<span v-if="m.streaming" class="cursor">▋</span>
          </span>
          <template v-else>
            <div v-if="m.thinking" class="bubble thinking-bubble">
              <span class="dot" /><span class="dot" /><span class="dot" />
            </div>
            <div v-else class="bubble markdown" v-html="renderMarkdown(m.content) + (m.streaming ? '<span class=\'cursor\'>▋</span>' : '')" />
          </template>
          <button
            v-if="m.role === 'assistant' && !m.streaming && !m.thinking"
            class="copy-btn"
            @click="copyMessage(i)"
            :title="copiedIdx === i ? '已复制' : '复制'"
          >{{ copiedIdx === i ? '✓' : '⎘' }}</button>
        </div>
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
.bubble-wrap { position: relative; max-width: 80%; display: flex; flex-direction: column; }
.msg.user .bubble-wrap { align-items: flex-end; }
.bubble { padding: 8px 12px; border-radius: 12px; font-size: 13px; line-height: 1.5; word-break: break-word; }
.user .bubble { background: #4f46e5; color: #fff; border-radius: 12px 12px 2px 12px; white-space: pre-wrap; }
.assistant .bubble { background: #374151; color: #e5e7eb; border-radius: 12px 12px 12px 2px; }
.system .bubble { background: #dc2626; color: #fff; border-radius: 8px; font-size: 12px; white-space: pre-wrap; }
.cursor { animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }

/* Thinking animation */
.thinking-bubble { display: flex; align-items: center; gap: 4px; padding: 12px 16px; }
.dot { width: 8px; height: 8px; background: #9ca3af; border-radius: 50%; display: inline-block; animation: bounce 1.2s infinite ease-in-out; }
.dot:nth-child(1) { animation-delay: 0s; }
.dot:nth-child(2) { animation-delay: 0.2s; }
.dot:nth-child(3) { animation-delay: 0.4s; }
@keyframes bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.5; }
  40% { transform: translateY(-6px); opacity: 1; }
}

/* Copy button */
.copy-btn { position: absolute; top: 4px; right: -28px; background: #374151; border: none; color: #9ca3af; border-radius: 4px; width: 22px; height: 22px; cursor: pointer; font-size: 12px; display: flex; align-items: center; justify-content: center; opacity: 0; transition: opacity 0.15s; padding: 0; }
.bubble-wrap:hover .copy-btn { opacity: 1; }
.copy-btn:hover { background: #4b5563; color: #f9fafb; }

/* Markdown prose styles */
.bubble.markdown :deep(p) { margin: 0 0 6px; }
.bubble.markdown :deep(p:last-child) { margin-bottom: 0; }
.bubble.markdown :deep(pre) { background: #1a1a2e; border-radius: 6px; padding: 10px 12px; overflow-x: auto; margin: 6px 0; }
.bubble.markdown :deep(code) { font-family: 'Fira Code', monospace; font-size: 12px; }
.bubble.markdown :deep(pre code) { background: none; padding: 0; }
.bubble.markdown :deep(:not(pre) > code) { background: #1f2937; padding: 1px 5px; border-radius: 3px; font-size: 12px; }
.bubble.markdown :deep(ul), .bubble.markdown :deep(ol) { padding-left: 18px; margin: 4px 0; }
.bubble.markdown :deep(li) { margin: 2px 0; }
.bubble.markdown :deep(blockquote) { border-left: 3px solid #4f46e5; margin: 6px 0; padding-left: 10px; color: #9ca3af; }
.bubble.markdown :deep(h1), .bubble.markdown :deep(h2), .bubble.markdown :deep(h3) { margin: 8px 0 4px; font-size: 14px; }
.bubble.markdown :deep(a) { color: #818cf8; }
.bubble.markdown :deep(table) { border-collapse: collapse; margin: 6px 0; font-size: 12px; }
.bubble.markdown :deep(th), .bubble.markdown :deep(td) { border: 1px solid #374151; padding: 4px 8px; }

.input-row { display: flex; gap: 8px; padding: 10px; border-top: 1px solid #374151; flex-shrink: 0; }
.input-row textarea { flex: 1; background: #1f2937; border: 1px solid #374151; border-radius: 8px; padding: 8px 12px; color: #f9fafb; font-size: 13px; outline: none; resize: none; font-family: inherit; }
.input-row button { background: #4f46e5; color: #fff; border: none; border-radius: 8px; padding: 8px 16px; cursor: pointer; font-size: 13px; }
.input-row button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>

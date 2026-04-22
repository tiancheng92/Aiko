<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, GetMessages, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown } from '../../wailsjs/go/main/App'
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

  // Show welcome message on first launch when chat history is empty.
  if ((history || []).length === 0) {
    try {
      const first = await IsFirstLaunch()
      if (first) {
        messages.value.push({
          role: 'assistant',
          content: '你好！👋 我是你的 AI 桌面宠物。\n\n我支持：\n- 💬 **自然语言对话**\n- 🔧 **工具调用**（查询时间、系统信息、网络状态等）\n- 📚 **知识库问答**（在设置中导入文档）\n\n**快速操作提示：**\n- 右键点击我 → 切换表情 / 更换模型 / 打开设置\n- 右键点击聊天框 → 导出聊天记录\n\n请先在 ⚙️ **设置** 中配置 LLM 模型后开始聊天。',
        })
        scrollToBottom()
        await MarkWelcomeShown()
      }
    } catch (e) {
      console.warn('welcome check failed:', e)
    }
  }

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

/* Messages list */
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.1) transparent;
}
.messages::-webkit-scrollbar { width: 4px; }
.messages::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.12); border-radius: 2px; }

/* Row */
.msg { display: flex; }
.msg.user { justify-content: flex-end; }
.msg.assistant, .msg.system { justify-content: flex-start; }

/* Wrap */
.bubble-wrap { position: relative; max-width: 82%; display: flex; flex-direction: column; }
.msg.user .bubble-wrap { align-items: flex-end; }

/* Bubble base */
.bubble {
  padding: 9px 14px;
  border-radius: 16px;
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
}

/* User bubble */
.user .bubble {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #fff;
  border-radius: 16px 16px 4px 16px;
  white-space: pre-wrap;
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.4);
}

/* Assistant bubble */
.assistant .bubble {
  background: rgba(55, 65, 81, 0.7);
  color: #e5e7eb;
  border-radius: 16px 16px 16px 4px;
  border: 1px solid rgba(255,255,255,0.06);
}

/* System / error bubble */
.system .bubble {
  background: rgba(220, 38, 38, 0.15);
  color: #fca5a5;
  border: 1px solid rgba(220, 38, 38, 0.3);
  border-radius: 10px;
  font-size: 12px;
  white-space: pre-wrap;
}

/* Cursor blink */
.cursor { animation: blink 1s step-end infinite; }
@keyframes blink { 50% { opacity: 0; } }

/* Thinking dots */
.thinking-bubble { display: flex; align-items: center; gap: 5px; padding: 12px 16px; }
.dot {
  width: 7px; height: 7px;
  background: rgba(156, 163, 175, 0.7);
  border-radius: 50%;
  display: inline-block;
  animation: bounce 1.2s infinite ease-in-out;
}
.dot:nth-child(1) { animation-delay: 0s; }
.dot:nth-child(2) { animation-delay: 0.2s; }
.dot:nth-child(3) { animation-delay: 0.4s; }
@keyframes bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
  40% { transform: translateY(-5px); opacity: 1; }
}

/* Copy button */
.copy-btn {
  position: absolute;
  top: 4px; right: -30px;
  background: rgba(55, 65, 81, 0.8);
  border: 1px solid rgba(255,255,255,0.08);
  color: rgba(156, 163, 175, 0.8);
  border-radius: 6px;
  width: 24px; height: 24px;
  cursor: pointer;
  font-size: 12px;
  display: flex; align-items: center; justify-content: center;
  opacity: 0;
  transition: opacity 0.15s, background 0.15s;
  padding: 0;
}
.bubble-wrap:hover .copy-btn { opacity: 1; }
.copy-btn:hover { background: rgba(75, 85, 99, 0.9); color: #f9fafb; }

/* Markdown prose */
.bubble.markdown :deep(p) { margin: 0 0 6px; }
.bubble.markdown :deep(p:last-child) { margin-bottom: 0; }
.bubble.markdown :deep(pre) {
  background: rgba(10, 10, 20, 0.6);
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 8px;
  padding: 10px 12px;
  overflow-x: auto;
  margin: 6px 0;
}
.bubble.markdown :deep(code) { font-family: 'Fira Code', 'JetBrains Mono', monospace; font-size: 12px; }
.bubble.markdown :deep(pre code) { background: none; padding: 0; }
.bubble.markdown :deep(:not(pre) > code) {
  background: rgba(79, 70, 229, 0.2);
  color: #a5b4fc;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 12px;
}
.bubble.markdown :deep(ul), .bubble.markdown :deep(ol) { padding-left: 18px; margin: 4px 0; }
.bubble.markdown :deep(li) { margin: 2px 0; }
.bubble.markdown :deep(blockquote) {
  border-left: 3px solid #6366f1;
  margin: 6px 0;
  padding-left: 10px;
  color: #9ca3af;
  background: rgba(99, 102, 241, 0.05);
  border-radius: 0 6px 6px 0;
}
.bubble.markdown :deep(h1), .bubble.markdown :deep(h2), .bubble.markdown :deep(h3) {
  margin: 8px 0 4px; font-size: 14px; color: #f9fafb;
}
.bubble.markdown :deep(a) { color: #818cf8; text-decoration: none; }
.bubble.markdown :deep(a:hover) { text-decoration: underline; }
.bubble.markdown :deep(table) { border-collapse: collapse; margin: 6px 0; font-size: 12px; width: 100%; }
.bubble.markdown :deep(th) { background: rgba(255,255,255,0.05); }
.bubble.markdown :deep(th), .bubble.markdown :deep(td) {
  border: 1px solid rgba(255,255,255,0.08);
  padding: 4px 8px;
}

/* Input row */
.input-row {
  display: flex;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid rgba(255,255,255,0.06);
  background: rgba(255,255,255,0.02);
  flex-shrink: 0;
}
.input-row textarea {
  flex: 1;
  background: rgba(31, 41, 55, 0.6);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  padding: 8px 12px;
  color: #f9fafb;
  font-size: 13px;
  outline: none;
  resize: none;
  font-family: inherit;
  transition: border-color 0.15s;
  line-height: 1.5;
}
.input-row textarea:focus { border-color: rgba(99, 102, 241, 0.6); }
.input-row textarea::placeholder { color: rgba(156, 163, 175, 0.5); }
.input-row button {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #fff;
  border: none;
  border-radius: 10px;
  padding: 8px 16px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: opacity 0.15s, transform 0.1s;
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.35);
}
.input-row button:hover:not(:disabled) { opacity: 0.9; transform: translateY(-1px); }
.input-row button:active:not(:disabled) { transform: translateY(0); }
.input-row button:disabled { opacity: 0.4; cursor: not-allowed; box-shadow: none; }
</style>

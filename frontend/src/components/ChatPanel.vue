<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, SendMessageWithImages, SendMessageWithFiles, GetMessages, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend, StopGeneration, SpeakText, StopTTS, GetConfig } from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit, BrowserOpenURL } from '../../wailsjs/runtime/runtime'
import { marked, Renderer } from 'marked'
import markedKatex from 'marked-katex-extension'
import 'katex/dist/katex.min.css'
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
import { useSounds } from '../composables/useSounds'
import { useTypingScheduler } from '../composables/useTypingScheduler'
import { GetSoundsEnabled } from '../../wailsjs/go/main/App'
import ToolConfirmModal from './ToolConfirmModal.vue'
import ExecutionProgress from './ExecutionProgress.vue'

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
  // Add copy button to code blocks
  const escaped = text.replace(/`/g, '&#96;')
  return `<div class="code-block"><div class="code-header"><span class="code-lang">${language || 'text'}</span><button class="code-copy" onclick="navigator.clipboard.writeText(decodeURIComponent(atob(this.dataset.code)));this.textContent='✓';setTimeout(()=>this.textContent='复制',2000)" data-code="${btoa(encodeURIComponent(text))}">复制</button></div><pre><code class="${cls}">${highlighted}</code></pre></div>`
}
renderer.table = (token) => {
  const alignStyle = (align) => align ? ` style="text-align:${align}"` : ''
  const headerHtml = token.header.map(cell =>
    `<th${alignStyle(cell.align)}>${marked.parseInline(cell.text)}</th>`
  ).join('')
  const rowsHtml = token.rows.map(row =>
    `<tr>${row.map(cell => `<td${alignStyle(cell.align)}>${marked.parseInline(cell.text)}</td>`).join('')}</tr>`
  ).join('')
  return `<div class="table-wrapper"><table><thead><tr>${headerHtml}</tr></thead><tbody>${rowsHtml}</tbody></table></div>`
}
renderer.link = ({ href, title, text }) => {
  // Resolve DDG redirect URLs to the actual destination
  const realHref = extractRealUrl(href) || href
  const display = text && text !== href ? text : shortenUrl(realHref)
  const safeHref = realHref.replace(/"/g, '&quot;')
  const titleAttr = title ? ` title="${title}"` : ''
  return `<a href="${safeHref}"${titleAttr} target="_blank" rel="noopener">${display}</a>`
}

marked.use(markedKatex({ throwOnError: false, output: 'html' }))
marked.use({ renderer, breaks: true, gfm: true })

/** extractRealUrl unwraps DuckDuckGo redirect URLs (//duckduckgo.com/l/?uddg=...). */
function extractRealUrl(href) {
  if (!href) return null
  // Handle protocol-relative DDG redirects
  const full = href.startsWith('//') ? 'https:' + href : href
  try {
    const u = new URL(full)
    if (u.hostname.includes('duckduckgo.com') && u.searchParams.has('uddg')) {
      return decodeURIComponent(u.searchParams.get('uddg'))
    }
  } catch {}
  return null
}

/** shortenUrl returns a readable short form of a URL (hostname + truncated path). */
function shortenUrl(url) {
  try {
    const u = new URL(url.startsWith('//') ? 'https:' + url : url)
    const path = u.pathname.length > 30 ? u.pathname.slice(0, 28) + '…' : u.pathname
    return u.hostname + (path !== '/' ? path : '')
  } catch {
    return url.length > 50 ? url.slice(0, 48) + '…' : url
  }
}

const messages = ref([])
const input = ref('')
/** pendingImages holds data URLs of images pasted by the user, awaiting send. */
const pendingImages = ref([])
/** pendingFiles holds text files selected by the user, awaiting send. */
const pendingFiles = ref([])
/** fileInputEl is the hidden <input type="file"> element for triggering the OS picker. */
const fileInputEl = ref(null)

/** lightboxSrc holds the data URL of the image currently shown in the lightbox, or null. */
const lightboxSrc = ref(null)

/** previewImage opens the lightbox for the given image src. */
function previewImage(src) {
  lightboxSrc.value = src
}
const loading = ref(false)
const messagesEl = ref(null)
const copiedIdx = ref(null)
const textareaEl = ref(null)
const isRecording = ref(false)
const voiceHint = ref('')
const voiceAutoSend = ref(false)
const isStreaming = ref(false)
const activeTTSMsgId = ref(null)  // id of the message currently being spoken
const cfg = ref(null)
const { playSend, playReceive, playError, playStop } = useSounds()
let soundsEnabled = false

/** applyToken appends a token to the last streaming assistant message. */
function applyToken(token) {
  // Remove thinking placeholder on first real token.
  const thinkIdx = messages.value.findLastIndex(m => m.thinking)
  if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)

  const idx = messages.value.length - 1
  const last = messages.value[idx]
  if (last && last.role === 'assistant' && last.streaming) {
    messages.value[idx] = { ...last, content: last.content + token }
  } else {
    messages.value.push({ role: 'assistant', content: token, streaming: true, isProactive: proactiveStarted })
    EventsEmit('pet:state:change', 'speaking')
  }
  scrollToBottom()
}

const typingScheduler = useTypingScheduler(applyToken)

let firstTokenThisTurn = true

/** formatTime formats a datetime string or Date to YYYY-MM-DD HH:mm:ss. */
function formatTime(ts) {
  if (!ts) return ''
  const d = ts instanceof Date ? ts : new Date(ts.replace(' ', 'T'))
  if (isNaN(d)) return ''
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

let proactiveStarted = false
let offToken, offDone, offError, offClear, offProactiveStart, offProactiveMessage
let offTTSDone, offTTSError, offTTSAudio
/** @type {HTMLAudioElement|null} 当前正在播放的 TTS Audio 实例，用于暂停 */
let currentTTSAudio = null

onMounted(async () => {
  const history = await GetMessages(50)
  messages.value = (history || []).map(m => ({ role: m.Role, content: m.Content, time: m.CreatedAt, images: m.Images || [], files: m.Files || [] }))
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

  offProactiveStart = EventsOn('chat:proactive:start', () => {
    proactiveStarted = true
    messages.value.push({ role: 'assistant', content: '', streaming: true, isProactive: true })
    EventsEmit('pet:state:change', 'speaking')
    scrollToBottom()
  })

  offProactiveMessage = EventsOn('chat:proactive:message', (text) => {
    messages.value.push({ role: 'assistant', content: text, isProactive: true })
    EventsEmit('pet:state:change', 'speaking')
    scrollToBottom()
    setTimeout(() => EventsEmit('pet:state:change', 'idle'), 2000)
  })

  offToken = EventsOn('chat:token', (token) => {
    if (firstTokenThisTurn) {
      firstTokenThisTurn = false
      if (soundsEnabled) playReceive()
    }
    typingScheduler.enqueue(token)
  })

  offDone = EventsOn('chat:done', () => {
    typingScheduler.flush()
    const idx = messages.value.length - 1
    const lastMsg = messages.value[idx]
    if (idx >= 0) messages.value[idx] = { ...messages.value[idx], streaming: false, time: new Date() }
    loading.value = false
    isStreaming.value = false
    proactiveStarted = false
    EventsEmit('pet:state:change', 'idle')
    // Auto-play TTS if enabled and this is not a voice-triggered response
    if (cfg.value?.TTSAutoPlay && lastMsg?.content && !isRecording.value) {
      activeTTSMsgId.value = idx
      SpeakText(lastMsg.content).catch(() => { activeTTSMsgId.value = null })
    }
  })

  offError = EventsOn('chat:error', (err) => {
    typingScheduler.clear()
    const thinkIdx = messages.value.findLastIndex(m => m.thinking)
    if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)
    messages.value.push({ role: 'system', content: '错误: ' + err })
    loading.value = false
    isStreaming.value = false
    proactiveStarted = false
    if (soundsEnabled) playError()
    EventsEmit('pet:state:change', 'error')
  })

  try { voiceAutoSend.value = await GetVoiceAutoSend() } catch {}
  try { soundsEnabled = await GetSoundsEnabled() } catch {}
  try { cfg.value = await GetConfig() } catch {}

  EventsOn('config:sounds:changed', (val) => {
    soundsEnabled = val
  })

  // tts:done 表示 Go 端处理完毕。
  // 对于有 audio bytes 的后端（kokoro），activeTTSMsgId 由 audio.onended 清除。
  // 对于 SystemSpeaker（say），没有 tts:audio 事件，直接在 tts:done 里清除状态。
  offTTSDone  = EventsOn('tts:done',  () => {
    if (!currentTTSAudio) activeTTSMsgId.value = null
  })
  offTTSError = EventsOn('tts:error', () => {
    activeTTSMsgId.value = null
    if (currentTTSAudio) { currentTTSAudio.pause(); currentTTSAudio = null }
  })
  offTTSAudio = EventsOn('tts:audio', ({ data, format }) => {
    // 停止上一段（若有）再播新的
    if (currentTTSAudio) {
      currentTTSAudio.pause()
      currentTTSAudio = null
    }
    const bytes = Uint8Array.from(atob(data), c => c.charCodeAt(0))
    const blob  = new Blob([bytes], { type: `audio/${format}` })
    const url   = URL.createObjectURL(blob)
    const audio = new Audio(url)
    currentTTSAudio = audio
    audio.play()
    audio.onended = () => {
      URL.revokeObjectURL(url)
      if (currentTTSAudio === audio) {
        currentTTSAudio = null
        activeTTSMsgId.value = null
      }
    }
  })

  EventsOn('voice:start', () => {
    isRecording.value = true
    voiceHint.value = ''
    input.value = ''
    nextTick(() => textareaEl.value?.focus())
  })

  EventsOn('voice:transcript', (text) => {
    input.value = text
    voiceHint.value = text
  })

  EventsOn('voice:end', () => {
    isRecording.value = false
    voiceHint.value = ''
  })

  EventsOn('voice:final', (text) => {
    input.value = text
    voiceHint.value = ''
    if (voiceAutoSend.value && text.trim()) {
      send()
    }
  })

  EventsOn('voice:error', (errMsg) => {
    isRecording.value = false
    voiceHint.value = ''
    input.value = ''
    EventsEmit('notification:show', {
      title: '🎙️ 语音识别失败',
      message: errMsg === 'mic_denied'
        ? '请在系统偏好设置中允许 Aiko 使用麦克风。'
        : errMsg === 'speech_denied'
          ? '请在系统偏好设置中允许 Aiko 使用语音识别。'
          : `语音识别出错：${errMsg}`,
    })
  })

  EventsOn('config:voice:auto-send:changed', (val) => {
    voiceAutoSend.value = val
  })
})

onUnmounted(() => { offToken?.(); offDone?.(); offError?.(); offClear?.(); offProactiveStart?.(); offProactiveMessage?.(); offTTSDone?.(); offTTSError?.(); offTTSAudio?.() })

/** renderMarkdown converts markdown text to sanitized HTML. */
function renderMarkdown(text) {
  if (!text) return ''
  // Strip LLM thinking blocks before rendering.
  const stripped = text.replace(/<thinking>[\s\S]*?<\/thinking>/gi, '').trim()
  if (!stripped) return ''
  // Replace bare DDG redirect URLs with the real destination so marked's
  // autolink / link renderer can display them cleanly.
  const processed = stripped.replace(
    /(?<![(\[])(?:https?:)?\/\/(?:html\.)?duckduckgo\.com\/l\/\?[^\s)>\]]+/g,
    (match) => {
      const real = extractRealUrl(match.startsWith('//') ? 'https:' + match : match)
      return real || match
    }
  )
  return marked(processed)
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

/** speakMessage triggers TTS for a specific message; toggles stop if already speaking. */
async function speakMessage(idx) {
  if (activeTTSMsgId.value === idx) {
    if (currentTTSAudio) {
      currentTTSAudio.pause()
      currentTTSAudio = null
    }
    await StopTTS()
    activeTTSMsgId.value = null
    return
  }
  activeTTSMsgId.value = idx
  const m = messages.value[idx]
  if (!m) return
  SpeakText(m.content).catch(() => { activeTTSMsgId.value = null })
}

/** onPaste handles clipboard paste events on the textarea.
 *  If the clipboard contains an image, it is captured as a data URL and
 *  added to pendingImages for preview; the default paste action is suppressed. */
function onPaste(e) {
  const items = [...(e.clipboardData?.items ?? [])]
  const imageItem = items.find(i => i.type.startsWith('image/'))
  if (!imageItem) return
  e.preventDefault()
  const blob = imageItem.getAsFile()
  if (!blob) return
  const reader = new FileReader()
  reader.onload = (ev) => {
    pendingImages.value.push(ev.target.result)
  }
  reader.readAsDataURL(blob)
}

/** removeImage removes a pending image by index. */
function removeImage(idx) {
  pendingImages.value.splice(idx, 1)
}

const READABLE_MIME_PREFIXES = ['text/']
const READABLE_MIME_EXACT = new Set([
  'application/json',
  'application/xml',
  'application/javascript',
  'application/typescript',
  'application/x-sh',
  'application/x-python',
])
const MAX_FILE_BYTES = 200 * 1024

/** isReadableMime returns true if the MIME type is a supported text type. */
function isReadableMime(mime) {
  if (READABLE_MIME_PREFIXES.some(p => mime.startsWith(p))) return true
  return READABLE_MIME_EXACT.has(mime)
}

/** addFile validates and reads a File object, pushing to pendingFiles on success. */
function addFile(file) {
  if (file.size > MAX_FILE_BYTES) {
    messages.value.push({ role: 'system', content: `文件过大（最大 200KB）：${file.name}` })
    return
  }
  const mime = file.type || 'text/plain'
  if (!isReadableMime(mime)) {
    messages.value.push({ role: 'system', content: `不支持此文件类型，仅支持文本文件：${file.name}` })
    return
  }
  const reader = new FileReader()
  reader.onload = (ev) => {
    pendingFiles.value.push({ name: file.name, mimeType: mime, content: ev.target.result })
  }
  reader.onerror = () => {
    messages.value.push({ role: 'system', content: `文件读取失败：${file.name}` })
  }
  reader.readAsText(file)
}

/** onFileInputChange handles files selected via the OS file picker. */
function onFileInputChange(e) {
  for (const file of e.target.files) {
    addFile(file)
  }
  e.target.value = ''
}

/** removeFile removes a pending file by index. */
function removeFile(idx) {
  pendingFiles.value.splice(idx, 1)
}

/** send submits the current input as a user message. */
async function send() {
  const text = input.value.trim()
  if ((!text && pendingImages.value.length === 0 && pendingFiles.value.length === 0) || loading.value) return
  input.value = ''
  loading.value = true
  isStreaming.value = true
  firstTokenThisTurn = true
  if (soundsEnabled) playSend()

  const imgs = [...pendingImages.value]
  pendingImages.value = []
  const fileAttachments = pendingFiles.value.map(f => ({ name: f.name, mimeType: f.mimeType, content: f.content }))
  const fileNames = pendingFiles.value.map(f => f.name)
  pendingFiles.value = []

  messages.value.push({ role: 'user', content: text, images: imgs, files: fileNames, time: new Date() })
  messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  try {
    if (imgs.length > 0 || fileAttachments.length > 0) {
      await SendMessageWithFiles(text, imgs, fileAttachments)
    } else {
      await SendMessage(text)
    }
  } catch (e) {
    const idx = messages.value.findLastIndex(m => m.thinking)
    if (idx >= 0) messages.value.splice(idx, 1)
    messages.value.push({ role: 'system', content: '发送失败: ' + e })
    loading.value = false
    isStreaming.value = false
    EventsEmit('pet:state:change', 'error')
  }
}

/** stopGeneration cancels the current in-flight AI response and marks the interrupted
 *  messages as ghost bubbles (visual only — not persisted, not sent to LLM context). */
async function stopGeneration() {
  try {
    await StopGeneration()
    typingScheduler.clear()
  } catch (e) {
    console.warn('StopGeneration failed:', e)
  }
  if (soundsEnabled) playStop()
  isStreaming.value = false
  loading.value = false

  // Mark the last user message and last assistant message (thinking or streaming) as ghost.
  const lastUser = messages.value.findLastIndex(m => m.role === 'user' && !m.ghost)
  if (lastUser >= 0) messages.value[lastUser] = { ...messages.value[lastUser], ghost: true }

  const lastAssistant = messages.value.findLastIndex(m => m.role === 'assistant' && !m.ghost)
  if (lastAssistant >= 0) {
    messages.value[lastAssistant] = {
      ...messages.value[lastAssistant],
      ghost: true,
      streaming: false,
      thinking: false,
    }
  }
  EventsEmit('pet:state:change', 'idle')
}

/** onMessagesClick intercepts link clicks and opens them in the system browser. */
function onMessagesClick(e) {
  const a = e.target.closest('a[href]')
  if (!a) return
  e.preventDefault()
  const href = a.getAttribute('href')
  if (href) BrowserOpenURL(href)
}
function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
}

/** focusInput focuses the textarea input. */
function focusInput() {
  nextTick(() => { textareaEl.value?.focus() })
}

defineExpose({ focusInput, scrollToBottom })
</script>

<template>
  <div class="chat-panel">
    <div class="messages" ref="messagesEl" @click="onMessagesClick">
      <div v-for="(m, i) in messages" :key="i" :class="['msg', m.role]">
        <div class="bubble-wrap" :class="{ ghost: m.ghost }">
          <div class="bubble-row">
            <!-- Bubble content -->
            <div v-if="m.role !== 'assistant'" class="bubble markdown" :class="{ 'has-images': (m.images && m.images.length > 0) || (m.files && m.files.length > 0) }">
              <div v-if="m.images && m.images.length > 0" class="msg-images">
                <img v-for="(img, imgIdx) in m.images" :key="imgIdx" :src="img" class="msg-img" @click.stop="previewImage(img)" />
              </div>
              <div v-if="m.files && m.files.length > 0" class="msg-files">
                <div v-for="(fname, fi) in m.files" :key="fi" class="msg-file-chip">
                  <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
                  <span>{{ fname }}</span>
                </div>
              </div>
              <div v-if="m.content" v-html="renderMarkdown(m.content) + (m.streaming ? '<span class=\'cursor\'>▋</span>' : '')"></div>
            </div>
            <template v-else>
              <div v-if="m.thinking || (m.streaming && !renderMarkdown(m.content))" :class="['bubble', 'thinking-bubble', { proactive: m.isProactive }]">
                <span class="dot" /><span class="dot" /><span class="dot" />
              </div>
              <div v-else :class="['bubble', 'markdown', { proactive: m.isProactive }]" v-html="renderMarkdown(m.content) + (m.streaming ? '<span class=\'cursor\'>▋</span>' : '')" />
            </template>

            <!-- Action buttons: absolutely positioned, no layout impact -->
            <div
              v-if="!m.streaming && !m.thinking"
              :class="['msg-actions', m.role]"
            >
              <button
                class="msg-action-btn"
                @click="copyMessage(i)"
                :title="copiedIdx === i ? '已复制' : '复制'"
              >
                <svg v-if="copiedIdx !== i" xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              </button>
              <button
                v-if="m.role === 'assistant'"
                class="msg-action-btn"
                :title="activeTTSMsgId === i ? '停止朗读' : '朗读'"
                @click="speakMessage(i)"
              >
                <svg v-if="activeTTSMsgId !== i" xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/></svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>
              </button>
            </div>
          </div>
          <div v-if="m.time && !m.streaming && !m.thinking" class="msg-time">{{ formatTime(m.time) }}</div>
        </div>
      </div>
    </div>
    <!-- Image lightbox -->
    <Teleport to="body">
      <div v-if="lightboxSrc" class="lightbox" @click="lightboxSrc = null">
        <img :src="lightboxSrc" class="lightbox-img" @click.stop />
      </div>
    </Teleport>

      <!-- Tool execution confirmation modal -->
      <ToolConfirmModal />

      <!-- In-chat progress indicators for running tools -->
      <ExecutionProgress />

      <!-- Voice recording status bar -->
      <div v-if="isRecording" class="voice-hint-bar">
        <span class="voice-hint-icon">🎙️</span>
        <span class="voice-hint-text">
          {{ voiceHint ? `"${voiceHint}"` : '正在聆听...' }}
        </span>
        <span class="voice-hint-dots">
          <span />
          <span />
          <span />
        </span>
      </div>
    <!-- Pending image previews shown above the input row -->
    <div v-if="pendingImages.length > 0" class="pending-images">
      <div v-for="(img, idx) in pendingImages" :key="idx" class="pending-img-wrap">
        <img :src="img" class="pending-img" />
        <button class="pending-img-remove" @click="removeImage(idx)">×</button>
      </div>
    </div>
    <!-- Pending file chips shown above the input row -->
    <div v-if="pendingFiles.length > 0" class="pending-files">
      <div v-for="(f, idx) in pendingFiles" :key="idx" class="pending-file-chip">
        <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
        <span class="pending-file-name">{{ f.name }}</span>
        <button class="pending-file-remove" @click="removeFile(idx)">×</button>
      </div>
    </div>
    <div class="input-row">
      <input
        ref="fileInputEl"
        type="file"
        multiple
        style="display:none"
        @change="onFileInputChange"
      />
      <button
        class="attach-btn"
        title="附加文件"
        :disabled="loading"
        @click="fileInputEl.click()"
      >
        <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>
      </button>
      <textarea
        ref="textareaEl"
        v-model="input"
        placeholder="输入消息... (Enter 发送)"
        rows="1"
        @keydown.enter.exact.prevent="send"
        @paste="onPaste"
        :disabled="loading"
      />
      <button v-if="isStreaming" class="stop-btn" @click="stopGeneration">⏹ 停止</button>
      <button v-else class="send-btn" @click="send" :disabled="loading">发送</button>
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
.bubble-wrap { max-width: 88%; display: flex; flex-direction: column; }
.msg.user .bubble-wrap { align-items: flex-end; }

/* Bubble row: relative container，按钮绝对定位不占空间 */
.bubble-row { position: relative; display: inline-flex; }
.msg.user .bubble-row { justify-content: flex-end; }
.msg.assistant .bubble-row { justify-content: flex-start; }

/* Bubble base */
.bubble {
  padding: 11px 16px;
  border-radius: 16px;
  font-size: 13.5px;
  line-height: 1.65;
  word-break: break-word;
}

/* User bubble */
.user .bubble {
  background: linear-gradient(135deg, #0369a1, #0c4a6e);
  color: #fff;
  border-radius: 16px 16px 4px 16px;
  box-shadow: 0 2px 12px rgba(3, 105, 161, 0.4);
}
.user .bubble.has-images {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Assistant bubble */
.assistant .bubble {
  background: rgba(255,255,255,0.06);
  color: #e5e7eb;
  border-radius: 16px 16px 16px 4px;
  border: 1px solid rgba(255,255,255,0.08);
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

/* Ghost bubbles: interrupted messages, visual only */
.ghost .bubble {
  opacity: 0.35;
  font-style: italic;
}
.ghost .bubble::after {
  content: ' ⊘';
  font-size: 11px;
  opacity: 0.6;
  font-style: normal;
}

/* Proactive assistant messages: subtle left-border accent */
.assistant .bubble.proactive {
  border-left: 3px solid rgba(120, 180, 255, 0.6);
  padding-left: 13px;
}

/* Stop button */
.stop-btn {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 8px;
  padding: 6px 14px;
  font-size: 13px;
  cursor: pointer;
  transition: background 0.15s;
}
.stop-btn:hover {
  background: rgba(239, 68, 68, 0.25);
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

/* Timestamp */
.msg-time {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.28);
  margin-top: 3px;
  padding: 0 4px;
  user-select: none;
}
.msg.user .msg-time { text-align: right; }
.msg.assistant .msg-time { text-align: left; }

/* Action buttons: absolutely positioned outside bubble */
.msg-actions {
  position: absolute;
  top: 6px;
  display: flex;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.15s;
  pointer-events: none;
}
.msg-actions.assistant { left: 100%; padding-left: 6px; }
.msg-actions.user { right: 100%; padding-right: 6px; }
.bubble-wrap:hover .msg-actions,
.msg-actions:hover { opacity: 1; pointer-events: auto; }

.msg-action-btn {
  flex-shrink: 0;
  background: rgba(55, 65, 81, 0.85);
  border: 1px solid rgba(255,255,255,0.08);
  color: rgba(156, 163, 175, 0.9);
  border-radius: 6px;
  width: 28px;
  height: 28px;
  cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  outline: none;
  padding: 0;
  transition: background 0.15s, color 0.15s;
}
.msg-action-btn:hover { background: rgba(75, 85, 99, 0.95); color: #f9fafb; }

/* Markdown prose */
.bubble.markdown :deep(p) { margin: 0 0 8px; }
.bubble.markdown :deep(p:last-child) { margin-bottom: 0; }

/* Code blocks */
.bubble.markdown :deep(.code-block) {
  margin: 8px 0;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid rgba(255,255,255,0.08);
}
.bubble.markdown :deep(.code-header) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 10px;
  background: rgba(255,255,255,0.05);
  border-bottom: 1px solid rgba(255,255,255,0.06);
}
.bubble.markdown :deep(.code-lang) {
  font-size: 11px;
  color: rgba(165,180,252,0.7);
  font-family: 'Fira Code', monospace;
}
.bubble.markdown :deep(.code-copy) {
  font-size: 11px;
  padding: 2px 8px;
  background: rgba(99,102,241,0.2);
  color: #a5b4fc;
  border: 1px solid rgba(99,102,241,0.3);
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.15s;
}
.bubble.markdown :deep(.code-copy:hover) { background: rgba(99,102,241,0.35); }
.bubble.markdown :deep(pre) {
  background: rgba(10, 10, 20, 0.6);
  padding: 10px 12px;
  overflow-x: auto;
  margin: 0;
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

/* Lists */
.bubble.markdown :deep(ul), .bubble.markdown :deep(ol) { padding-left: 20px; margin: 4px 0 8px; }
.bubble.markdown :deep(li) { margin: 3px 0; line-height: 1.6; }
.bubble.markdown :deep(li > ul), .bubble.markdown :deep(li > ol) { margin: 2px 0; }

/* Blockquote */
.bubble.markdown :deep(blockquote) {
  border-left: 3px solid #0369a1;
  margin: 8px 0;
  padding: 6px 10px;
  color: #9ca3af;
  background: rgba(99, 102, 241, 0.06);
  border-radius: 0 6px 6px 0;
}

/* Headings */
.bubble.markdown :deep(h1) { margin: 10px 0 6px; font-size: 16px; color: #f9fafb; font-weight: 700; }
.bubble.markdown :deep(h2) { margin: 10px 0 5px; font-size: 15px; color: #f9fafb; font-weight: 700; }
.bubble.markdown :deep(h3) { margin: 8px 0 4px; font-size: 14px; color: #e5e7eb; font-weight: 600; }
.bubble.markdown :deep(hr) { border: none; border-top: 1px solid rgba(255,255,255,0.1); margin: 10px 0; }

/* Links — break long URLs so they don't overflow the bubble */
.bubble.markdown :deep(a) {
  color: #818cf8;
  text-decoration: none;
  word-break: break-all;
}
.bubble.markdown :deep(a:hover) { text-decoration: underline; color: #a5b4fc; }

/* Tables */
.bubble.markdown :deep(.table-wrapper) {
  overflow-x: auto;
  margin: 10px 0;
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.1);
}
.bubble.markdown :deep(table) {
  border-collapse: collapse;
  font-size: 13px;
  width: 100%;
  min-width: 360px;
}
.bubble.markdown :deep(thead tr) {
  background: rgba(255,255,255,0.07);
}
.bubble.markdown :deep(th) {
  font-weight: 600;
  white-space: nowrap;
  color: rgba(255,255,255,0.9);
}
.bubble.markdown :deep(th), .bubble.markdown :deep(td) {
  border: none;
  border-bottom: 1px solid rgba(255,255,255,0.07);
  padding: 8px 14px;
  text-align: left;
  vertical-align: middle;
}
.bubble.markdown :deep(tbody tr:last-child td) {
  border-bottom: none;
}
.bubble.markdown :deep(tbody tr:nth-child(even)) {
  background: rgba(255,255,255,0.03);
}
.bubble.markdown :deep(tbody tr:hover) {
  background: rgba(79,172,254,0.08);
}

/* KaTeX math — adapt to dark theme */
.bubble.markdown :deep(.katex) { font-size: 1em; color: #e2e8f0; }
.bubble.markdown :deep(.katex-display) {
  margin: 8px 0;
  overflow-x: auto;
  overflow-y: hidden;
}
.bubble.markdown :deep(.katex-html) { color: #e2e8f0; }

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
  background: rgba(255,255,255,0.06);
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
.input-row textarea:focus { border-color: rgba(3, 105, 161, 0.6); }
.input-row textarea::placeholder { color: rgba(156, 163, 175, 0.5); }
.send-btn {
  background: linear-gradient(135deg, #0369a1, #0c4a6e);
  color: #fff;
  border: none;
  border-radius: 10px;
  padding: 8px 16px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: opacity 0.15s, transform 0.1s;
  box-shadow: 0 2px 8px rgba(3, 105, 161, 0.4);
}
.send-btn:hover:not(:disabled) { opacity: 0.9; transform: translateY(-1px); }
.send-btn:active:not(:disabled) { transform: translateY(0); }
.send-btn:disabled { opacity: 0.4; cursor: not-allowed; box-shadow: none; }

/* ── Voice hint status bar ─────────────────────────────────── */
.voice-hint-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px 12px;
  padding: 10px 14px;
  border-radius: 10px;
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.15), rgba(139, 92, 246, 0.15));
  border: 1px solid rgba(139, 92, 246, 0.3);
  font-size: 13px;
  color: rgba(200, 210, 255, 0.9);
}

.voice-hint-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.voice-hint-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.voice-hint-dots {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.voice-hint-dots span {
  display: inline-block;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #8b5cf6;
  animation: dot-bounce 1.2s ease-in-out infinite;
}

.voice-hint-dots span:nth-child(2) { animation-delay: 0.2s; }
.voice-hint-dots span:nth-child(3) { animation-delay: 0.4s; }

@keyframes dot-bounce {
  0%, 80%, 100% { transform: translateY(0);    opacity: 0.4; }
  40%           { transform: translateY(-4px); opacity: 1; }
}

/* Pending image previews above input */
.pending-images {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 6px 12px 0;
}
.pending-img-wrap {
  position: relative;
  display: inline-block;
}
.pending-img {
  width: 72px;
  height: 72px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.15);
}
.pending-img-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: rgba(0,0,0,0.7);
  color: #fff;
  border: none;
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  box-shadow: none;
}
.pending-img-remove:hover { background: rgba(220,50,50,0.8); }

/* Images inside sent user messages */
.msg-images {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
.msg-img {
  max-width: 200px;
  max-height: 200px;
  border-radius: 8px;
  object-fit: cover;
  border: 1px solid rgba(255,255,255,0.1);
  cursor: zoom-in;
}

/* Lightbox */
.lightbox {
  position: fixed;
  inset: 0;
  z-index: 9999;
  background: rgba(0,0,0,0.85);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
}
.lightbox-img {
  max-width: 90vw;
  max-height: 90vh;
  border-radius: 10px;
  box-shadow: 0 8px 40px rgba(0,0,0,0.6);
  object-fit: contain;
  cursor: default;
}

/* Attach file button */
.attach-btn {
  flex-shrink: 0;
  align-self: center;
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(156,163,175,0.8);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  padding: 0;
}
.attach-btn:hover:not(:disabled) { background: rgba(255,255,255,0.1); color: #f9fafb; }
.attach-btn:disabled { opacity: 0.35; cursor: not-allowed; }

/* Pending file chips above input */
.pending-files {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 6px 12px 0;
}
.pending-file-chip {
  display: flex;
  align-items: center;
  gap: 5px;
  background: rgba(99,102,241,0.15);
  border: 1px solid rgba(99,102,241,0.3);
  border-radius: 8px;
  padding: 4px 8px;
  font-size: 12px;
  color: #a5b4fc;
  max-width: 220px;
}
.pending-file-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.pending-file-remove {
  background: none;
  border: none;
  color: rgba(165,180,252,0.7);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 0;
  flex-shrink: 0;
  box-shadow: none;
}
.pending-file-remove:hover { color: #f9fafb; }

/* File chips inside sent user messages */
.msg-files {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-bottom: 4px;
}
.msg-file-chip {
  display: flex;
  align-items: center;
  gap: 4px;
  background: rgba(255,255,255,0.12);
  border: 1px solid rgba(255,255,255,0.18);
  border-radius: 6px;
  padding: 3px 8px;
  font-size: 11.5px;
  color: rgba(255,255,255,0.85);
  max-width: 200px;
  overflow: hidden;
  white-space: nowrap;
}
.msg-file-chip span {
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>

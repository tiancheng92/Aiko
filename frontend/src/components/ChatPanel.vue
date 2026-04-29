<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, SendMessageWithImages, SendMessageWithFiles, GetMessages, GetMessagesBeforeID, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend, StopGeneration, SpeakText, StopTTS, GetConfig } from '../../wailsjs/go/main/App'
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
import LinkPreview from './LinkPreview.vue'

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
  // Generate line numbers
  const lines = highlighted.split('\n')
  const digits = String(lines.length).length
  const numbered = lines.map((line, i) =>
    `<span class="code-line"><span class="line-nr">${String(i + 1).padStart(digits)}</span><span class="line-code">${line || ' '}</span></span>`
  ).join('')
  return `<div class="code-block"><div class="code-header"><span class="code-lang">${language || 'text'}</span><button class="code-copy" onclick="navigator.clipboard.writeText(decodeURIComponent(atob(this.dataset.code)));this.textContent='✓';setTimeout(()=>this.textContent='复制',2000)" data-code="${btoa(encodeURIComponent(text))}">复制</button></div><pre><code class="${cls}">${numbered}</code></pre></div>`
}
const TABLE_PAGE_SIZE = 10

window.__tableState = window.__tableState || {}

/** buildRowHtml renders one <tr> from raw text cells; origIdx maps back to rawRows for detail lookup. */
function buildRowHtml(cells, origIdx, aligns) {
  const tds = cells.map((cell, j) => {
    const align = aligns[j] ? ` style="text-align:${aligns[j]}"` : ''
    return `<td${align}>${marked.parseInline(cell)}</td>`
  }).join('')
  return `<tr class="tbl-row" onclick="window.__tr(this)" data-row-idx="${origIdx}">${tds}</tr>`
}

/** renderTablePage rebuilds tbody and pagination controls from the current filter+sort+page state. */
function renderTablePage(wrapper, state) {
  const { sortedIndices, rawRows, aligns, currentPage, filterQuery } = state
  const totalPages = Math.ceil(sortedIndices.length / TABLE_PAGE_SIZE)
  const p = currentPage
  wrapper.querySelector('tbody').innerHTML = sortedIndices
    .slice((p - 1) * TABLE_PAGE_SIZE, p * TABLE_PAGE_SIZE)
    .map(i => buildRowHtml(rawRows[i], i, aligns))
    .join('')
  const infoEl = wrapper.querySelector('.tbl-page-info')
  if (infoEl) {
    const id = wrapper.id
    const matchLabel = filterQuery
      ? `${sortedIndices.length} / ${rawRows.length} 行  ·  ${p} / ${totalPages || 1}`
      : `${p} / ${totalPages}`
    infoEl.textContent = matchLabel
    const [prevBtn, nextBtn] = wrapper.querySelectorAll('.tbl-page-btn')
    prevBtn.disabled = p <= 1
    prevBtn.setAttribute('onclick', `window.__tp('${id}',${p - 1})`)
    nextBtn.disabled = p >= totalPages
    nextBtn.setAttribute('onclick', `window.__tp('${id}',${p + 1})`)
  }
}

/** updateSortHeaders updates <th> sort indicators to reflect the current sort state. */
function updateSortHeaders(wrapper, state) {
  wrapper.querySelectorAll('thead th').forEach((th, i) => {
    const ind = th.querySelector('.sort-indicator')
    if (!ind) return
    const active = i === state.sortCol && state.sortDir !== 'none'
    ind.textContent = active ? (state.sortDir === 'asc' ? ' ↑' : ' ↓') : ''
    th.classList.toggle('sorted', active)
  })
}

renderer.table = (token) => {
  const aligns = token.header.map(c => c.align || '')
  const rawRows = token.rows.map(row => row.map(c => c.text))
  const headers = token.header.map(c => c.text)
  const encodedHeaders = btoa(encodeURIComponent(JSON.stringify(headers)))
  const encodedRaw = btoa(encodeURIComponent(JSON.stringify(rawRows)))
  const id = 'tbl-' + Math.random().toString(36).slice(2, 9)

  window.__tableState[id] = {
    rawRows, aligns, headers,
    sortCol: -1, sortDir: 'none',
    sortedIndices: rawRows.map((_, i) => i),
    currentPage: 1,
    filterQuery: '',
  }

  const headerHtml = token.header.map((cell, i) => {
    const align = cell.align ? ` style="text-align:${cell.align}"` : ''
    return `<th${align} class="sortable-th" onclick="window.__ts('${id}',${i})">${marked.parseInline(cell.text)}<span class="sort-indicator"></span></th>`
  }).join('')

  const firstRowsHtml = rawRows.slice(0, TABLE_PAGE_SIZE)
    .map((cells, i) => buildRowHtml(cells, i, aligns)).join('')

  const totalPages = Math.ceil(rawRows.length / TABLE_PAGE_SIZE)
  const paginationHtml = rawRows.length > TABLE_PAGE_SIZE
    ? `<div class="table-pagination"><button class="tbl-page-btn" onclick="window.__tp('${id}',0)" disabled>‹</button><span class="tbl-page-info">1 / ${totalPages}</span><button class="tbl-page-btn" onclick="window.__tp('${id}',2)"${totalPages <= 1 ? ' disabled' : ''}>›</button></div>`
    : ''

  const filterBar = `<div class="tbl-filter-bar"><input class="tbl-filter-input" type="text" placeholder="筛选..." oninput="window.__tf('${id}',this.value)"></div>`

  return `<div class="table-wrapper" id="${id}" data-headers="${encodedHeaders}" data-raw="${encodedRaw}">${filterBar}<div class="tbl-scroll"><table><thead><tr>${headerHtml}</tr></thead><tbody>${firstRowsHtml}</tbody></table></div>${paginationHtml}</div>`
}

/** __tp navigates a paginated markdown table to the requested page. */
window.__tp = (id, page) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  const totalPages = Math.ceil(state.sortedIndices.length / TABLE_PAGE_SIZE)
  state.currentPage = Math.max(1, Math.min(page, totalPages))
  renderTablePage(wrapper, state)
}

/** __ts toggles sort on a column: none → asc → desc → none. */
window.__ts = (id, colIdx) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  if (state.sortCol === colIdx) {
    state.sortDir = { none: 'asc', asc: 'desc', desc: 'none' }[state.sortDir]
    if (state.sortDir === 'none') state.sortCol = -1
  } else {
    state.sortCol = colIdx
    state.sortDir = 'asc'
  }
  const indices = state.rawRows.map((_, i) => i)
  if (state.sortDir !== 'none') {
    const dir = state.sortDir === 'asc' ? 1 : -1
    indices.sort((a, b) => {
      const va = state.rawRows[a][colIdx] ?? ''
      const vb = state.rawRows[b][colIdx] ?? ''
      const na = parseFloat(va.replace(/,/g, ''))
      const nb = parseFloat(vb.replace(/,/g, ''))
      return (!isNaN(na) && !isNaN(nb) ? na - nb : va.localeCompare(vb, undefined, { numeric: true })) * dir
    })
  }
  state.sortedIndices = indices
  state.currentPage = 1
  renderTablePage(wrapper, state)
  updateSortHeaders(wrapper, state)
}

/** __tf filters table rows by a case-insensitive substring across all columns, then re-applies the current sort. */
window.__tf = (id, query) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  state.filterQuery = query.trim().toLowerCase()
  const q = state.filterQuery
  let indices = state.rawRows.map((_, i) => i)
  if (q) {
    indices = indices.filter(i => state.rawRows[i].some(cell => cell.toLowerCase().includes(q)))
  }
  if (state.sortDir !== 'none' && state.sortCol >= 0) {
    const col = state.sortCol
    const dir = state.sortDir === 'asc' ? 1 : -1
    indices.sort((a, b) => {
      const va = state.rawRows[a][col] ?? ''
      const vb = state.rawRows[b][col] ?? ''
      const na = parseFloat(va.replace(/,/g, ''))
      const nb = parseFloat(vb.replace(/,/g, ''))
      return (!isNaN(na) && !isNaN(nb) ? na - nb : va.localeCompare(vb, undefined, { numeric: true })) * dir
    })
  }
  state.sortedIndices = indices
  state.currentPage = 1
  renderTablePage(wrapper, state)
}

/** __tr opens the row-detail modal for a clicked table row. */
window.__tr = (rowEl) => {
  const wrapper = rowEl.closest('.table-wrapper')
  if (!wrapper) return
  const rowIdx = parseInt(rowEl.dataset.rowIdx, 10)
  const state = wrapper.id ? window.__tableState?.[wrapper.id] : null
  const headers = state?.headers ?? JSON.parse(decodeURIComponent(atob(wrapper.dataset.headers)))
  const rawRow = state?.rawRows[rowIdx] ?? JSON.parse(decodeURIComponent(atob(wrapper.dataset.raw)))[rowIdx]
  tableDetailRow.value = headers.map((key, i) => ({ key, value: rawRow[i] ?? '' }))
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

const PAGE_SIZE = 10

const messages = ref([])
/** oldestLoadedID is the smallest message ID currently in the list; used for lazy-loading older pages. */
let oldestLoadedID = null
/** allLoaded is true when there are no more older messages to fetch. */
const allLoaded = ref(false)
/** loadingHistory prevents concurrent history fetches and drives the loading indicator. */
const loadingHistory = ref(false)

/** inputEmpty is true when the textarea has no content (reactive only on empty↔non-empty). */
const inputEmpty = ref(true)
/** getInput reads the textarea value directly from the DOM — avoids per-keystroke Vue re-renders. */
function getInput() { return textareaEl.value?.value ?? '' }
/** setInputDOM writes to the DOM textarea and syncs inputEmpty. */
function setInputDOM(text) {
  if (textareaEl.value) textareaEl.value.value = text
  inputEmpty.value = !text.trim()
}
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
const codeMaxWidth = ref(0)
const copiedIdx = ref(null)
const showClearConfirm = ref(false)
/** tableDetailRow holds the key-value pairs for the row-detail modal; null when hidden. */
const tableDetailRow = ref(null)

/** collapsedIds holds message keys that should render in collapsed state. */
const collapsedIds = ref(new Set())
/** expandedIds holds message keys the user has manually expanded. */
const expandedIds = ref(new Set())

/** msgKey returns a stable key for a message — prefers persisted id, falls back to index. */
function msgKey(m, i) { return m.id != null ? `id:${m.id}` : `i:${i}` }

/** isCollapsed returns true when a message is tall enough to collapse and not yet expanded. */
function isCollapsed(m, i) {
  const k = msgKey(m, i)
  return collapsedIds.value.has(k) && !expandedIds.value.has(k)
}

/** isEverCollapsed returns true when a message has been registered for collapsing (expanded or not). */
function isEverCollapsed(m, i) {
  return collapsedIds.value.has(msgKey(m, i))
}

/** toggleExpand expands or re-collapses a message. */
function toggleExpand(m, i) {
  const k = msgKey(m, i)
  const next = new Set(expandedIds.value)
  if (next.has(k)) next.delete(k)
  else next.add(k)
  expandedIds.value = next
}

const COLLAPSE_HEIGHT = 350

/** pendingCollapseChecks queues history messages waiting for the panel to become visible. */
const pendingCollapseChecks = []

/** runPendingCollapseChecks processes all queued messages once the panel has a real layout. */
function runPendingCollapseChecks() {
  if (!messagesEl.value || messagesEl.value.clientHeight === 0) return
  const checks = pendingCollapseChecks.splice(0)
  for (const { m, i } of checks) {
    const k = msgKey(m, i)
    if (expandedIds.value.has(k)) continue
    const bubbleEl = messagesEl.value.querySelector(`[data-msg-key="${CSS.escape(k)}"]`)
    if (bubbleEl && bubbleEl.scrollHeight > COLLAPSE_HEIGHT) {
      const next = new Set(collapsedIds.value)
      next.add(k)
      collapsedIds.value = next
    }
  }
}

/** checkBubbleCollapse queues a history message for collapse measurement.
 *  If the panel is already visible, processes immediately; otherwise defers until visible. */
function checkBubbleCollapse(m, i, fromHistory = false) {
  if (!fromHistory) return
  if (m.streaming || m.thinking) return
  pendingCollapseChecks.push({ m, i })
  if (messagesEl.value && messagesEl.value.clientHeight > 0) {
    requestAnimationFrame(runPendingCollapseChecks)
  }
}
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
let offSoundsChanged
let offVoiceStart, offVoiceTranscript, offVoiceEnd, offVoiceFinal, offVoiceError, offVoiceAutoSend
/** @type {HTMLAudioElement|null} 当前正在播放的 TTS Audio 实例，用于暂停 */
let currentTTSAudio = null
let resizeObserver = null

/** mapMsg converts a backend Message to the frontend shape. */
function mapMsg(m) {
  return { id: m.ID, role: m.Role, content: m.Content, time: m.CreatedAt, images: m.Images || [], files: m.Files || [] }
}

/** loadOlderMessages fetches the next page of older messages and prepends them. */
async function loadOlderMessages() {
  if (loadingHistory.value || allLoaded.value || oldestLoadedID === null) return
  loadingHistory.value = true
  // Double-rAF: Vue flushes the DOM in the first frame, browser paints in the second.
  // This guarantees the loading dots are on screen before the IPC call starts.
  await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
  try {
    // Fetch and minimum display timer run in parallel — no artificial lag on slow connections.
    const [older] = await Promise.all([
      GetMessagesBeforeID(oldestLoadedID, PAGE_SIZE),
      new Promise(r => setTimeout(r, 300)),
    ])
    if (!older || older.length === 0) {
      allLoaded.value = true
      return
    }
    if (older.length < PAGE_SIZE) allLoaded.value = true
    const el = messagesEl.value
    // Record which index the current first message will land at after prepend.
    const firstOldIdx = older.length
    const olderMapped = older.map(mapMsg)
    messages.value = olderMapped.concat(messages.value)
    oldestLoadedID = older[0].ID
    olderMapped.forEach((m, i) => checkBubbleCollapse(m, i, true))
    // Wait for Vue to flush the DOM, then one rAF so the browser finishes layout.
    await nextTick()
    await new Promise(resolve => requestAnimationFrame(resolve))
    if (el) {
      // Anchor to the first "old" message element: scroll it to the top of the
      // viewport. getBoundingClientRect() forces a synchronous layout so the
      // measurement is always accurate, unlike scrollHeight which may be stale.
      const msgEls = el.querySelectorAll(':scope > .msg')
      const anchor = msgEls[firstOldIdx]
      if (anchor) {
        el.scrollTop += anchor.getBoundingClientRect().top - el.getBoundingClientRect().top
      }
    }
  } finally {
    loadingHistory.value = false
  }
}

onMounted(async () => {
  const history = await GetMessages(PAGE_SIZE)
  const mapped = (history || []).map(mapMsg)
  messages.value = mapped
  if (mapped.length > 0) oldestLoadedID = mapped[0].id
  if ((history || []).length < PAGE_SIZE) allLoaded.value = true
  scrollToBottom()
  // Check loaded history messages for collapse after DOM paints.
  mapped.forEach((m, i) => checkBubbleCollapse(m, i, true))

  // Sentinel element at top of list triggers lazy-load via IntersectionObserver.
  const sentinel = document.getElementById('msg-load-sentinel')
  if (sentinel) {
    const io = new IntersectionObserver(async (entries) => {
      if (entries[0].isIntersecting) await loadOlderMessages()
    }, { root: messagesEl.value, threshold: 0 })
    io.observe(sentinel)
  }

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

  offClear = EventsOn('chat:clear', () => {
    showClearConfirm.value = true
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

  offSoundsChanged = EventsOn('config:sounds:changed', (val) => {
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

  offVoiceStart = EventsOn('voice:start', () => {
    isRecording.value = true
    voiceHint.value = ''
    setInputDOM('')
    nextTick(() => textareaEl.value?.focus())
  })

  offVoiceTranscript = EventsOn('voice:transcript', (text) => {
    setInputDOM(text)
    voiceHint.value = text
  })

  offVoiceEnd = EventsOn('voice:end', () => {
    isRecording.value = false
    voiceHint.value = ''
  })

  offVoiceFinal = EventsOn('voice:final', (text) => {
    setInputDOM(text)
    voiceHint.value = ''
    if (voiceAutoSend.value && text.trim()) {
      send()
    }
  })

  offVoiceError = EventsOn('voice:error', (errMsg) => {
    isRecording.value = false
    voiceHint.value = ''
    setInputDOM('')
    EventsEmit('notification:show', {
      title: '🎙️ 语音识别失败',
      message: errMsg === 'mic_denied'
        ? '请在系统偏好设置中允许 Aiko 使用麦克风。'
        : errMsg === 'speech_denied'
          ? '请在系统偏好设置中允许 Aiko 使用语音识别。'
          : `语音识别出错：${errMsg}`,
    })
  })

  offVoiceAutoSend = EventsOn('config:voice:auto-send:changed', (val) => {
    voiceAutoSend.value = val
  })

  // Observe message container width for code block max-width.
  if (messagesEl.value) {
    resizeObserver = new ResizeObserver(([entry]) => {
      codeMaxWidth.value = entry.contentRect.width - 28 - 68
      // Panel just became visible — process any queued collapse checks.
      if (pendingCollapseChecks.length > 0 && entry.contentRect.height > 0) {
        requestAnimationFrame(runPendingCollapseChecks)
      }
    })
    resizeObserver.observe(messagesEl.value)
  }
})

onUnmounted(() => {
  // Invoke every EventsOn teardown; undefined entries are safely skipped via
  // optional chaining so a partial mount (e.g. early error) does not throw here.
  offToken?.(); offDone?.(); offError?.(); offClear?.()
  offProactiveStart?.(); offProactiveMessage?.()
  offTTSDone?.(); offTTSError?.(); offTTSAudio?.()
  offSoundsChanged?.()
  offVoiceStart?.(); offVoiceTranscript?.(); offVoiceEnd?.(); offVoiceFinal?.(); offVoiceError?.(); offVoiceAutoSend?.()
  // Stop any in-flight TTS playback so detached <audio> elements can be GC'd.
  if (currentTTSAudio) { try { currentTTSAudio.pause() } catch {} ; currentTTSAudio = null }
  resizeObserver?.disconnect()
})

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

/** extractUrls returns deduplicated http(s) URLs found in plain text, skipping markdown image syntax. */
function extractUrls(text) {
  if (!text) return []
  // Remove markdown image syntax ![...](...) so image URLs are not previewed.
  const noImages = text.replace(/!\[[^\]]*\]\([^)]+\)/g, '')
  const matches = noImages.match(/https?:\/\/[^\s)>\]"']+/g) || []
  // Deduplicate while preserving order.
  return [...new Set(matches)]
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

/** confirmClearHistory clears chat history and closes the confirm dialog. */
async function confirmClearHistory() {
  showClearConfirm.value = false
  try {
    await ClearChatHistory()
    messages.value = []
    oldestLoadedID = null
    allLoaded.value = true
    collapsedIds.value = new Set()
    expandedIds.value = new Set()
    pendingCollapseChecks.splice(0)
  } catch (e) {
    console.error('clear chat history failed:', e)
  }
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
  const text = getInput().trim()
  if ((!text && pendingImages.value.length === 0 && pendingFiles.value.length === 0) || loading.value) return
  setInputDOM('')
  resetTextareaHeight()
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

/** insertNewline inserts a newline at the current cursor position (⌘+Enter). */
function insertNewline() {
  const el = textareaEl.value
  if (!el) return
  const start = el.selectionStart
  const end = el.selectionEnd
  el.value = el.value.slice(0, start) + '\n' + el.value.slice(end)
  el.selectionStart = el.selectionEnd = start + 1
  autoResize()
}

/** autoResize adjusts the textarea height to fit its content; syncs inputEmpty on transitions only. */
function autoResize() {
  const el = textareaEl.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = el.scrollHeight + 'px'
  const empty = !el.value.trim()
  if (inputEmpty.value !== empty) inputEmpty.value = empty
}

/** resetTextareaHeight resets the textarea to single-line height after send. */
function resetTextareaHeight() {
  const el = textareaEl.value
  if (el) el.style.height = 'auto'
}

defineExpose({ focusInput, scrollToBottom })
</script>

<template>
  <div class="chat-panel" :style="{ '--code-max-width': codeMaxWidth > 0 ? codeMaxWidth + 'px' : 'none' }">
    <div class="messages" ref="messagesEl" @click="onMessagesClick">
      <!-- Lazy-load sentinel: entering viewport triggers loading older messages -->
      <div id="msg-load-sentinel" class="load-sentinel">
        <div v-if="loadingHistory" class="history-loading">
          <span class="h-dot" /><span class="h-dot" /><span class="h-dot" />
        </div>
        <span v-else-if="!allLoaded" class="load-sentinel-dot" />
      </div>
      <div v-for="(m, i) in messages" :key="i" :class="['msg', m.role]">
        <div class="bubble-wrap" :class="{ ghost: m.ghost }">
          <!-- Collapsible wrapper -->
          <div
            class="bubble-collapse-wrap"
            :class="{ 'is-collapsed': isCollapsed(m, i) }"
            :data-msg-key="msgKey(m, i)"
          >
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

            <!-- Collapse fade overlay + expand button -->
            <div v-if="isCollapsed(m, i)" class="collapse-fade" @click.stop="toggleExpand(m, i)">
              <button class="collapse-btn" @click.stop="toggleExpand(m, i)">
                <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                展开
              </button>
            </div>
          </div>

          <!-- Link preview cards — shown below the bubble once streaming is done -->
          <template v-if="!m.streaming && !m.thinking && m.content">
            <LinkPreview
              v-for="u in extractUrls(m.content)"
              :key="u"
              :url="u"
            />
          </template>
          <div v-if="(m.time && !m.streaming && !m.thinking) || (isEverCollapsed(m, i) && !isCollapsed(m, i))" class="msg-meta-row">
            <span v-if="m.time && !m.streaming && !m.thinking" class="msg-time">{{ formatTime(m.time) }}</span>
            <button v-if="isEverCollapsed(m, i) && !isCollapsed(m, i)" class="recollapse-btn" @click.stop="toggleExpand(m, i)">
              <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="18 15 12 9 6 15"/></svg>
              收起
            </button>
          </div>
        </div>
      </div>
    </div>
    <!-- Image lightbox -->
    <div v-if="lightboxSrc" class="lightbox" @click="lightboxSrc = null">
      <img :src="lightboxSrc" class="lightbox-img" @click.stop />
    </div>

    <!-- Tool execution confirmation modal -->
    <ToolConfirmModal />

    <!-- Clear chat confirmation dialog -->
    <Transition name="confirm-pop">
    <div v-if="showClearConfirm" class="clear-confirm-overlay">
      <div class="clear-confirm-backdrop" @click="showClearConfirm = false" />
      <div class="clear-confirm-box">
        <p class="clear-confirm-title">清空聊天记录</p>
        <p class="clear-confirm-text">确定要清空所有聊天记录吗？此操作不可撤销。</p>
        <div class="clear-confirm-actions">
          <button class="clear-confirm-cancel" @click="showClearConfirm = false">取消</button>
          <button class="clear-confirm-ok" @click="confirmClearHistory">确认清空</button>
        </div>
      </div>
    </div>
    </Transition>

    <!-- Table row detail modal -->
    <Transition name="tbl-detail-pop">
      <div v-if="tableDetailRow" class="tbl-detail-overlay">
        <div class="tbl-detail-backdrop" @click="tableDetailRow = null" />
        <div class="tbl-detail-box">
          <div class="tbl-detail-header">
            <span class="tbl-detail-title">行详情</span>
            <button class="tbl-detail-close" @click="tableDetailRow = null">✕</button>
          </div>
          <div class="tbl-detail-body">
            <div v-for="pair in tableDetailRow" :key="pair.key" class="tbl-detail-pair">
              <span class="tbl-detail-key">{{ pair.key }}</span>
              <span class="tbl-detail-value">{{ pair.value }}</span>
            </div>
          </div>
        </div>
      </div>
    </Transition>

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
    <div class="input-area">
      <input
        ref="fileInputEl"
        type="file"
        multiple
        style="display:none"
        @change="onFileInputChange"
      />
      <textarea
        ref="textareaEl"
        placeholder="发消息..."
        rows="1"
        spellcheck="false"
        autocorrect="off"
        autocomplete="off"
        @input="autoResize"
        @keydown.enter.exact.prevent="send"
        @keydown.meta.enter.prevent="insertNewline"
        @paste="onPaste"
        :disabled="loading"
      />
      <div class="input-toolbar">
        <div class="toolbar-spacer" />
        <button
          class="attach-btn"
          title="附加文件"
          :disabled="loading"
          @click="fileInputEl.click()"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>
        </button>
        <button v-if="isStreaming" class="stop-btn" @click="stopGeneration">⏹ 停止</button>
        <button v-else class="send-btn" @click="send" :disabled="loading || (inputEmpty && pendingImages.length === 0 && pendingFiles.length === 0)">
          <svg xmlns="http://www.w3.org/2000/svg" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
        </button>
      </div>
    </div>
    <div class="input-hint">Enter 发送 · ⌘↩ 换行</div>
  </div>
</template>

<style scoped>
.chat-panel { display: flex; flex-direction: column; height: 100%; position: relative; }

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

/* Lazy-load sentinel */
.load-sentinel { height: 24px; display: flex; align-items: center; justify-content: center; }
.load-sentinel-dot {
  display: block;
  width: 6px; height: 6px;
  border-radius: 50%;
  background: rgba(255,255,255,0.15);
  animation: dot-bounce 1.2s ease-in-out infinite;
}
.history-loading { display: flex; gap: 5px; align-items: center; }
.h-dot {
  display: block;
  width: 5px; height: 5px;
  border-radius: 50%;
  background: rgba(3,105,161,0.6);
  animation: dot-bounce 1.2s ease-in-out infinite;
}
.h-dot:nth-child(2) { animation-delay: 0.2s; }
.h-dot:nth-child(3) { animation-delay: 0.4s; }

/* Row */
.msg { display: flex; }
.msg.user { justify-content: flex-end; }
.msg.assistant, .msg.system { justify-content: flex-start; }

/* Wrap */
.bubble-wrap { max-width: 88%; display: flex; flex-direction: column; }
.msg.user .bubble-wrap { align-items: flex-end; }

/* Collapsible wrapper */
.bubble-collapse-wrap {
  position: relative;
}
.bubble-collapse-wrap.is-collapsed {
  max-height: 350px;
  overflow: hidden;
}

/* Gradient fade at the bottom of a collapsed bubble */
.collapse-fade {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 80px;
  background: linear-gradient(to bottom, transparent, rgba(5, 6, 12, 0.96));
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding-bottom: 10px;
  cursor: pointer;
}
/* For user bubbles the gradient should match the bubble background */
.msg.user .collapse-fade {
  background: linear-gradient(to bottom, transparent, rgba(5, 6, 12, 0.96));
}

.collapse-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 14px;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 20px;
  color: rgba(255, 255, 255, 0.7);
  font-size: 12px;
  cursor: pointer;
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  transition: background 0.15s, color 0.15s, border-color 0.15s;
  box-shadow: none;
  font-family: inherit;
}
.collapse-btn:hover {
  background: rgba(59, 130, 246, 0.15);
  border-color: rgba(59, 130, 246, 0.3);
  color: #93c5fd;
}

/* Re-collapse button */
.recollapse-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0;
  background: none;
  border: none;
  color: rgba(255, 255, 255, 0.28);
  font-size: 11px;
  cursor: pointer;
  transition: color 0.15s;
  font-family: inherit;
  box-shadow: none;
  line-height: 1;
}
.recollapse-btn:hover { color: rgba(255, 255, 255, 0.6); }

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
  background: rgba(255,255,255,0.04);
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

/* Meta row: timestamp at its side, recollapse button centered */
.msg-meta-row {
  position: relative;
  display: flex;
  align-items: center;
  min-height: 18px;
  margin-top: 3px;
  padding: 0 4px;
}
.msg.user .msg-meta-row { justify-content: flex-end; }
.msg.assistant .msg-meta-row { justify-content: flex-start; }
.msg-meta-row .recollapse-btn {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
}

/* Timestamp */
.msg-time {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.28);
  user-select: none;
}

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
  background: rgba(15, 20, 38, 0.65);
  border: 1px solid rgba(255,255,255,0.08);
  color: rgba(156, 163, 175, 0.8);
  border-radius: 6px;
  width: 28px;
  height: 28px;
  cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  outline: none;
  padding: 0;
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.msg-action-btn:hover {
  background: rgba(3, 105, 161, 0.2);
  border-color: rgba(3, 105, 161, 0.4);
  color: #7dd3fc;
}

/* Markdown prose */
.bubble.markdown :deep(p) { margin: 0 0 8px; }
.bubble.markdown :deep(p:last-child) { margin-bottom: 0; }

/* Code blocks */
.bubble.markdown :deep(.code-block) {
  margin: 8px 0;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid rgba(255,255,255,0.08);
  max-width: var(--code-max-width, 100%);
}
.bubble.markdown :deep(.code-header) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  background: rgba(255,255,255,0.04);
  border-bottom: 1px solid rgba(255,255,255,0.06);
}
.bubble.markdown :deep(.code-lang) {
  font-size: 11px;
  color: rgba(125,211,252,0.6);
  font-family: 'Fira Code', monospace;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.bubble.markdown :deep(.code-copy) {
  font-size: 11px;
  padding: 2px 10px;
  background: rgba(3,105,161,0.15);
  color: #7dd3fc;
  border: 1px solid rgba(3,105,161,0.25);
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.15s;
}
.bubble.markdown :deep(.code-copy:hover) { background: rgba(3,105,161,0.3); }
.bubble.markdown :deep(pre) {
  background: rgba(10, 10, 20, 0.6);
  padding: 12px 14px;
  overflow-x: auto;
  margin: 0;
  max-width: 100%;
}
.bubble.markdown :deep(pre code) { white-space: pre-wrap; word-break: break-word; background: none; padding: 0; }
.bubble.markdown :deep(.code-line) {
  display: flex;
  align-items: flex-start;
  line-height: 1.65;
}
.bubble.markdown :deep(.line-nr) {
  flex-shrink: 0;
  align-self: stretch;
  display: flex;
  align-items: flex-start;
  justify-content: flex-end;
  min-width: 3ch;
  padding-right: 0.8ch;
  margin-right: 1.2ch;
  color: rgba(148, 163, 184, 0.35);
  font-size: 11px;
  -webkit-user-select: none;
  user-select: none;
  border-right: 1px solid rgba(255, 255, 255, 0.08);
  transition: color 0.15s;
}
.bubble.markdown :deep(.line-code) {
  flex: 1;
  min-width: 0;
  white-space: pre-wrap;
  word-break: break-word;
}
.bubble.markdown :deep(.code-line:hover .line-nr) {
  color: rgba(148, 163, 184, 0.7);
}
.bubble.markdown :deep(code) { font-family: 'Fira Code', 'JetBrains Mono', monospace; font-size: 12px; }
.bubble.markdown :deep(:not(pre) > code) {
  background: rgba(3, 105, 161, 0.18);
  color: #7dd3fc;
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
  background: rgba(3, 105, 161, 0.06);
  border-radius: 0 6px 6px 0;
}

/* Headings */
.bubble.markdown :deep(h1) { margin: 10px 0 6px; font-size: 16px; color: #f9fafb; font-weight: 700; }
.bubble.markdown :deep(h2) { margin: 10px 0 5px; font-size: 15px; color: #f9fafb; font-weight: 700; }
.bubble.markdown :deep(h3) { margin: 8px 0 4px; font-size: 14px; color: #e5e7eb; font-weight: 600; }
.bubble.markdown :deep(hr) { border: none; border-top: 1px solid rgba(255,255,255,0.1); margin: 10px 0; }

/* Links — break long URLs so they don't overflow the bubble */
.bubble.markdown :deep(a) {
  color: #38bdf8;
  text-decoration: none;
  word-break: break-all;
}
.bubble.markdown :deep(a:hover) { text-decoration: underline; color: #7dd3fc; }

/* Tables */
.bubble.markdown :deep(.table-wrapper) {
  margin: 10px 0;
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.1);
  max-width: calc(var(--code-max-width, 100%) - 32px);
}
.bubble.markdown :deep(.tbl-scroll) {
  overflow-x: auto;
}
.bubble.markdown :deep(table) {
  border-collapse: collapse;
  font-size: 13px;
  width: max-content;
  min-width: 100%;
}
.bubble.markdown :deep(thead tr) {
  background: rgba(255,255,255,0.07);
}
.bubble.markdown :deep(th) {
  font-weight: 600;
  white-space: nowrap;
  color: rgba(255,255,255,0.9);
}
.bubble.markdown :deep(.sortable-th) {
  cursor: pointer;
  user-select: none;
  transition: background 0.12s, color 0.12s;
}
.bubble.markdown :deep(.sortable-th:hover) {
  background: rgba(255,255,255,0.08);
}
.bubble.markdown :deep(.sortable-th.sorted) {
  color: #60a5fa;
}
.bubble.markdown :deep(.sort-indicator) {
  font-size: 11px;
  margin-left: 3px;
  opacity: 0.75;
}
.bubble.markdown :deep(th), .bubble.markdown :deep(td) {
  border: none;
  border-bottom: 1px solid rgba(255,255,255,0.07);
  padding: 8px 14px;
  text-align: left;
  vertical-align: middle;
  white-space: nowrap;
}
.bubble.markdown :deep(tbody tr:last-child td) {
  border-bottom: none;
}
.bubble.markdown :deep(tbody tr:nth-child(even)) {
  background: rgba(255,255,255,0.03);
}
.bubble.markdown :deep(tbody tr:hover) {
  background: rgba(59, 130, 246, 0.12);
}
.bubble.markdown :deep(.tbl-row) {
  cursor: pointer;
}
.bubble.markdown :deep(.tbl-filter-bar) {
  padding: 7px 10px 6px;
  border-bottom: 1px solid rgba(255,255,255,0.06);
  background: rgba(255,255,255,0.015);
}
.bubble.markdown :deep(.tbl-filter-input) {
  width: 100%;
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.09);
  border-radius: 6px;
  color: rgba(255,255,255,0.85);
  font-size: 12px;
  font-family: inherit;
  padding: 4px 9px;
  outline: none;
  box-sizing: border-box;
  transition: border-color 0.15s;
}
.bubble.markdown :deep(.tbl-filter-input::placeholder) {
  color: rgba(255,255,255,0.28);
}
.bubble.markdown :deep(.tbl-filter-input:focus) {
  border-color: rgba(59,130,246,0.4);
}
.bubble.markdown :deep(.table-pagination) {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 7px 12px;
  border-top: 1px solid rgba(255,255,255,0.07);
  background: rgba(255,255,255,0.02);
}
.bubble.markdown :deep(.tbl-page-btn) {
  background: none;
  border: 1px solid rgba(255,255,255,0.1);
  color: rgba(255,255,255,0.6);
  border-radius: 5px;
  width: 28px;
  height: 26px;
  cursor: pointer;
  font-size: 15px;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  padding: 0;
  transition: background 0.15s, border-color 0.15s, color 0.15s;
}
.bubble.markdown :deep(.tbl-page-btn:hover:not(:disabled)) {
  background: rgba(59, 130, 246, 0.2);
  border-color: rgba(59, 130, 246, 0.4);
  color: #fff;
}
.bubble.markdown :deep(.tbl-page-btn:disabled) {
  opacity: 0.25;
  cursor: not-allowed;
}
.bubble.markdown :deep(.tbl-page-info) {
  font-size: 12px;
  color: rgba(255,255,255,0.45);
  user-select: none;
  min-width: 80px;
  text-align: center;
  white-space: nowrap;
}

/* Table row detail modal */
.tbl-detail-overlay {
  position: absolute;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: auto;
}
.tbl-detail-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
}
.tbl-detail-box {
  position: relative;
  background: rgb(5, 6, 12);
  backdrop-filter: blur(24px) saturate(140%);
  -webkit-backdrop-filter: blur(24px) saturate(140%);
  border: 1px solid rgba(255, 255, 255, 0.07);
  border-radius: 16px;
  width: 420px;
  max-width: 90vw;
  max-height: 70vh;
  box-shadow:
    0 16px 48px rgba(0, 0, 0, 0.65),
    0 1px 0 rgba(255, 255, 255, 0.05) inset;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.tbl-detail-header {
  display: flex;
  align-items: center;
  padding: 14px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  flex-shrink: 0;
}
.tbl-detail-title {
  flex: 1;
  font-size: 14px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.9);
}
.tbl-detail-close {
  background: none;
  border: none;
  color: rgba(255, 255, 255, 0.3);
  font-size: 13px;
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 5px;
  line-height: 1;
  transition: color 0.15s, background 0.15s;
}
.tbl-detail-close:hover {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.12);
}
.tbl-detail-body {
  overflow-y: auto;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 1px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.08) transparent;
}
.tbl-detail-pair {
  display: flex;
  gap: 14px;
  align-items: baseline;
  padding: 8px 10px;
  border-radius: 8px;
  transition: background 0.12s;
}
.tbl-detail-pair:hover {
  background: rgba(255, 255, 255, 0.04);
}
.tbl-detail-key {
  flex-shrink: 0;
  width: 120px;
  font-size: 11px;
  font-weight: 500;
  color: rgba(125, 211, 252, 0.75);
  word-break: break-word;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.tbl-detail-value {
  flex: 1;
  font-size: 13px;
  color: rgba(229, 231, 235, 0.9);
  word-break: break-word;
  line-height: 1.5;
}
.tbl-detail-pop-enter-active { transition: opacity 0.22s ease; }
.tbl-detail-pop-leave-active { transition: opacity 0.14s ease-in; }
.tbl-detail-pop-enter-from,
.tbl-detail-pop-leave-to { opacity: 0; }
.tbl-detail-pop-enter-active .tbl-detail-box {
  transition: transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.tbl-detail-pop-leave-active .tbl-detail-box {
  transition: transform 0.14s ease-in;
}
.tbl-detail-pop-enter-from .tbl-detail-box,
.tbl-detail-pop-leave-to .tbl-detail-box {
  transform: scale(0.90);
}

/* KaTeX math — adapt to dark theme */
.bubble.markdown :deep(.katex) { font-size: 1em; color: #e2e8f0; }
.bubble.markdown :deep(.katex-display) {
  margin: 8px 0;
  overflow-x: auto;
  overflow-y: hidden;
}
.bubble.markdown :deep(.katex-html) { color: #e2e8f0; }

/* ── Composer card ─────────────────────────────────────────── */
.input-area {
  margin: 12px 10px 10px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  flex-shrink: 0;
  transition: border-color 0.2s;
  overflow: hidden;
}
.input-area:focus-within {
  border-color: rgba(3, 105, 161, 0.55);
}
.input-area textarea {
  display: block;
  width: 100%;
  box-sizing: border-box;
  background: transparent;
  border: none;
  padding: 10px 12px 6px;
  color: #f9fafb;
  font-size: 13px;
  outline: none;
  resize: none;
  font-family: inherit;
  line-height: 1.6;
  min-height: 40px;
  max-height: 120px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.1) transparent;
}
.input-area textarea::placeholder { color: rgba(156, 163, 175, 0.45); }
.input-toolbar {
  display: flex;
  align-items: center;
  padding: 4px 8px 6px;
  gap: 6px;
  border-top: none;
}
.toolbar-spacer { flex: 1; }
.input-hint {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.2);
  user-select: none;
  text-align: right;
  padding: 0 12px 6px;
}
.send-btn {
  background: linear-gradient(135deg, #0369a1, #0c4a6e);
  color: #fff;
  border: none;
  border-radius: 8px;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: opacity 0.15s, transform 0.1s;
  box-shadow: 0 2px 6px rgba(3, 105, 161, 0.35);
  flex-shrink: 0;
}
.send-btn:hover:not(:disabled) { opacity: 0.88; transform: translateY(-1px); }
.send-btn:active:not(:disabled) { transform: translateY(0); }
.send-btn:disabled { opacity: 0.3; cursor: not-allowed; box-shadow: none; }

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
  background: #0369a1;
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
  position: absolute;
  inset: 0;
  z-index: 200;
  background: rgba(0,0,0,0.85);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
  pointer-events: auto;
}
.lightbox-img {
  max-width: 90%;
  max-height: 90%;
  border-radius: 10px;
  box-shadow: 0 8px 40px rgba(0,0,0,0.6);
  object-fit: contain;
  cursor: default;
}

/* Attach file button */
.attach-btn {
  flex-shrink: 0;
  background: transparent;
  border: none;
  border-radius: 8px;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(156,163,175,0.6);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  padding: 0;
}
.attach-btn:hover:not(:disabled) { background: rgba(255,255,255,0.08); color: #f9fafb; }
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
  background: rgba(3,105,161,0.15);
  border: 1px solid rgba(3,105,161,0.3);
  border-radius: 8px;
  padding: 4px 8px;
  font-size: 12px;
  color: #7dd3fc;
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
  color: rgba(125,211,252,0.7);
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

<style>
/* Clear chat confirmation dialog (non-scoped — teleported to body) */
.clear-confirm-overlay {
  position: absolute;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: auto;
}
.clear-confirm-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
}
.clear-confirm-box {
  position: relative;
  background: rgb(5, 6, 12);
  backdrop-filter: blur(24px) saturate(140%);
  -webkit-backdrop-filter: blur(24px) saturate(140%);
  border: 1px solid rgba(255, 255, 255, 0.07);
  border-radius: 16px;
  padding: 24px;
  width: 380px;
  max-width: 90vw;
  box-shadow:
    0 16px 48px rgba(0, 0, 0, 0.65),
    0 1px 0 rgba(255, 255, 255, 0.05) inset;
}
.confirm-pop-enter-active {
  transition: opacity 0.22s ease;
}
.confirm-pop-leave-active {
  transition: opacity 0.14s ease-in;
}
.confirm-pop-enter-from,
.confirm-pop-leave-to {
  opacity: 0;
}
.confirm-pop-enter-active .clear-confirm-box {
  transition: transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.confirm-pop-leave-active .clear-confirm-box {
  transition: transform 0.14s ease-in;
}
.confirm-pop-enter-from .clear-confirm-box,
.confirm-pop-leave-to .clear-confirm-box {
  transform: scale(0.90);
}
.clear-confirm-title {
  font-size: 15px;
  font-weight: 600;
  color: #e2e8f0;
  margin: 0 0 12px;
}
.clear-confirm-text {
  font-size: 13px;
  color: rgba(255,255,255,0.55);
  margin: 0 0 20px;
  line-height: 1.6;
}
.clear-confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
.clear-confirm-cancel {
  padding: 8px 18px;
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.12);
  background: rgba(255,255,255,0.06);
  color: #e2e8f0;
  font-size: 13px;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}
.clear-confirm-cancel:hover { background: rgba(255,255,255,0.1); }
.clear-confirm-ok {
  padding: 8px 18px;
  border-radius: 8px;
  border: none;
  background: linear-gradient(135deg, #dc2626, #991b1b);
  color: #fff;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: opacity 0.15s;
}
.clear-confirm-ok:hover { opacity: 0.9; }
</style>

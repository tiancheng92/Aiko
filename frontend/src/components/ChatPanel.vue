<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, SendMessageWithImages, SendMessageWithFiles, GetMessages, GetMessagesBeforeID, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend, StopGeneration, SpeakText, StopTTS, GetConfig, RegenerateLastReply, GetSoundsEnabled } from '../../wailsjs/go/main/App'
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
import { useEscapeKey } from '../composables/useEscapeKey'
import ToolConfirmModal from './ToolConfirmModal.vue'
import ExecutionProgress from './ExecutionProgress.vue'
import LinkPreview from './LinkPreview.vue'
import ContextMenu from './ContextMenu.vue'

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
  // Split HLJS output into per-line HTML while preserving span balance.
  // A naive split('\n') breaks multi-line spans (e.g. Go raw string literals
  // highlighted as one <span class="hljs-string">…</span>), producing malformed
  // HTML that the browser renders incorrectly. Instead we scan the HLJS HTML,
  // and at each newline we close all open spans then re-open them on the next
  // line so every line has balanced HTML.
  const lineHtmls = []
  let cur = ''
  const stack = [] // opening tags currently open
  for (let i = 0; i < highlighted.length; i++) {
    if (highlighted[i] === '\n') {
      lineHtmls.push(cur + stack.map(() => '</span>').join(''))
      cur = stack.join('') // re-open on next line
    } else if (highlighted[i] === '<') {
      const gt = highlighted.indexOf('>', i)
      const tag = highlighted.slice(i, gt + 1)
      cur += tag
      if (tag.startsWith('<span')) stack.push(tag)
      else if (tag.startsWith('</span')) stack.pop()
      i = gt
    } else {
      cur += highlighted[i]
    }
  }
  if (cur) lineHtmls.push(cur + stack.map(() => '</span>').join(''))
  // Drop trailing empty line that appears when code ends with \n.
  if (lineHtmls.length > 1 && lineHtmls[lineHtmls.length - 1] === '') lineHtmls.pop()
  const digits = String(lineHtmls.length).length
  const numbered = lineHtmls.map((line, i) =>
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
    filterCol: -1,
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

  const chevronSvg = `<svg class="tbl-col-chevron" xmlns="http://www.w3.org/2000/svg" width="10" height="6" viewBox="0 0 10 6"><path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`
  const colItems = [`<li class="tbl-col-opt tbl-col-opt--sel" onclick="window.__selectCol('${id}',-1,this)">全表</li>`, ...headers.map((h, i) => `<li class="tbl-col-opt" onclick="window.__selectCol('${id}',${i},this)">${h}</li>`)].join('')
  const filterBar = `<div class="tbl-filter-bar"><div class="tbl-col-select" id="${id}-col"><button class="tbl-col-btn" onclick="window.__toggleColDrop(event,'${id}')"><span class="tbl-col-label">全表</span>${chevronSvg}</button><ul class="tbl-col-drop">${colItems}</ul></div><input class="tbl-filter-input" type="text" placeholder="筛选..." oninput="window.__tf('${id}',this.value)"></div>`

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
  applyFilterSort(state)
  renderTablePage(wrapper, state)
  updateSortHeaders(wrapper, state)
}

/** applyFilterSort recomputes sortedIndices from current filterQuery/filterCol/sortCol/sortDir. */
function applyFilterSort(state) {
  const q = state.filterQuery
  const fc = state.filterCol
  let indices = state.rawRows.map((_, i) => i)
  if (q) {
    indices = fc >= 0
      ? indices.filter(i => (state.rawRows[i][fc] ?? '').toLowerCase().includes(q))
      : indices.filter(i => state.rawRows[i].some(cell => cell.toLowerCase().includes(q)))
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
}

/** __fc changes the active filter column then re-runs the current filter query. */
window.__fc = (id, colIdx) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  state.filterCol = colIdx
  applyFilterSort(state)
  renderTablePage(wrapper, state)
}

/** __tf filters table rows by a case-insensitive substring, scoped to the selected column or all columns. */
window.__tf = (id, query) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  state.filterQuery = query.trim().toLowerCase()
  applyFilterSort(state)
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

/** __toggleColDrop opens or closes the custom column-select dropdown. */
window.__toggleColDrop = (e, id) => {
  e.stopPropagation()
  const el = document.getElementById(id + '-col')
  if (!el) return
  const drop = el.querySelector('.tbl-col-drop')
  const isOpen = drop.classList.contains('tbl-col-drop--open')
  document.querySelectorAll('.tbl-col-drop--open').forEach(d => d.classList.remove('tbl-col-drop--open'))
  if (!isOpen) drop.classList.add('tbl-col-drop--open')
}

/** __selectCol updates the button label and fires the column-filter change. */
window.__selectCol = (id, val, optEl) => {
  const state = window.__tableState?.[id]
  const label = val === -1 ? '全表' : (state?.headers[val] ?? '')
  const el = document.getElementById(id + '-col')
  if (!el) return
  el.querySelector('.tbl-col-label').textContent = label
  el.querySelectorAll('.tbl-col-opt').forEach(o => o.classList.remove('tbl-col-opt--sel'))
  optEl.classList.add('tbl-col-opt--sel')
  el.querySelector('.tbl-col-drop').classList.remove('tbl-col-drop--open')
  window.__fc(id, val)
}

const closeColDrops = () => document.querySelectorAll('.tbl-col-drop--open').forEach(d => d.classList.remove('tbl-col-drop--open'))

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
/** lightboxFullscreen tracks whether the lightbox is in full-window mode. */
const lightboxFullscreen = ref(false)

/** previewImage opens the lightbox for the given image src. */
function previewImage(src) {
  lightboxSrc.value = src
  lightboxFullscreen.value = false
}

/** closeLightbox closes the lightbox and resets fullscreen state. */
function closeLightbox() {
  lightboxSrc.value = null
  lightboxFullscreen.value = false
}
const loading = ref(false)
const messagesEl = ref(null)
/** sentinelObserver watches the top-of-list sentinel for infinite-scroll lazy loading. */
let sentinelObserver = null
const codeMaxWidth = ref(0)
const copiedIdx = ref(null)
const showClearConfirm = ref(false)
const msgMenuRef = ref(null)
const msgMenuItems = ref([])
/** tableDetailRow holds the key-value pairs for the row-detail modal; null when hidden. */
const tableDetailRow = ref(null)

useEscapeKey(() => { showClearConfirm.value = false }, showClearConfirm)
useEscapeKey(() => { tableDetailRow.value = null }, () => tableDetailRow.value !== null)

/** collapsedIds holds message keys that should render in collapsed state. */
const collapsedIds = ref(new Set())
/** expandedIds holds message keys the user has manually expanded. */
const expandedIds = ref(new Set())
/** suppressAnimation disables enter transitions when prepending history messages. */
const suppressAnimation = ref(false)

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
/** lpExpanded tracks whether the extra link previews are shown for each message (keyed by msgKey). */
const lpExpanded = ref({})
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
    last.content += token  // direct mutation — Vue Proxy tracks this, no object copy needed
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
let offToken, offDone, offError, offClear, offProactiveStart, offProactiveMessage, offCronStart, offImage
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
    suppressAnimation.value = true
    messages.value = olderMapped.concat(messages.value)
    await nextTick()
    suppressAnimation.value = false
    oldestLoadedID = older[0].ID
    olderMapped.forEach((m, i) => checkBubbleCollapse(m, i, true))
    // Wait for Vue to flush the DOM, then one rAF so the browser finishes layout.
    await nextTick()
    await new Promise(resolve => requestAnimationFrame(resolve))
    if (el) {
      // Anchor to the first "old" message element: scroll it to the top of the
      // viewport. getBoundingClientRect() forces a synchronous layout so the
      // measurement is always accurate, unlike scrollHeight which may be stale.
      const msgEls = el.querySelectorAll('.messages-inner > .msg')
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
  document.addEventListener('click', closeColDrops)
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
    sentinelObserver = new IntersectionObserver(async (entries) => {
      if (entries[0].isIntersecting) await loadOlderMessages()
    }, { root: messagesEl.value, threshold: 0 })
    sentinelObserver.observe(sentinel)
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

  offCronStart = EventsOn('chat:cron:start', ({ name, prompt }) => {
    // Push a user-side trigger label followed by a streaming assistant placeholder.
    messages.value.push({ role: 'user', content: `⏰ **${name}**\n${prompt}`, isCron: true })
    messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, isCron: true })
    loading.value = true
    isStreaming.value = true
    firstTokenThisTurn = true
    EventsEmit('pet:state:change', 'thinking')
    scrollToBottom()
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

  offImage = EventsOn('chat:image', (imgs) => {
    const idx = messages.value.findLastIndex(m => m.role === 'assistant')
    if (idx < 0 || !Array.isArray(imgs)) return
    const existing = messages.value[idx].images || []
    messages.value[idx] = { ...messages.value[idx], images: [...existing, ...imgs] }
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
  offToken?.(); offDone?.(); offError?.(); offClear?.(); offImage?.()
  offProactiveStart?.(); offProactiveMessage?.(); offCronStart?.()
  offTTSDone?.(); offTTSError?.(); offTTSAudio?.()
  offSoundsChanged?.()
  offVoiceStart?.(); offVoiceTranscript?.(); offVoiceEnd?.(); offVoiceFinal?.(); offVoiceError?.(); offVoiceAutoSend?.()
  document.removeEventListener('click', closeColDrops)
  sentinelObserver?.disconnect()
  sentinelObserver = null
  // Stop any in-flight TTS playback so detached <audio> elements can be GC'd.
  if (currentTTSAudio) { try { currentTTSAudio.pause() } catch {} ; currentTTSAudio = null }
  resizeObserver?.disconnect()
})

// CommonMark flanking-delimiter rules break ** adjacent to CJK/fullwidth chars.
// Insert a zero-width joiner (U+200D, not whitespace/punctuation) to satisfy
// the rules, then strip it from the output so it doesn't appear in copy-paste.
const ZWJ = '‍'
// Closing ** fails when preceded by CJK/fullwidth (punctuation) and followed by
// a non-whitespace non-punctuation char — insert ZWJ before the closing **.
const BOLD_CLOSE_FIX = new RegExp('([　-鿿＀-￯])\\*\\*(?=[^\\s\\p{P}])', 'gu')
// Opening ** fails when preceded by a letter (Lo) and followed by CJK/fullwidth
// punctuation — insert ZWJ after the opening **.
const BOLD_OPEN_FIX = /(\p{Lo})\*\*(?=[　-〿＀-￯'-‟])/gu

const _mdCache = new Map()
const _MD_CACHE_MAX = 200

/**
 * closeUnclosedFences appends a closing fence for any unclosed fenced code
 * block so that `marked` does not misparse pipe characters inside the block
 * as table separators during streaming.
 */
function closeUnclosedFences(text) {
  const lines = text.split('\n')
  let inFence = false
  let fenceChar = ''
  for (const line of lines) {
    const m = line.match(/^(`{3,}|~{3,})/)
    if (m) {
      if (!inFence) {
        inFence = true
        fenceChar = m[1][0]
      } else if (m[1][0] === fenceChar) {
        inFence = false
      }
    }
  }
  return inFence ? text + '\n' + fenceChar.repeat(3) : text
}

/** renderMarkdown converts markdown text to sanitized HTML, caching results to avoid re-parsing. */
function renderMarkdown(text) {
  if (!text) return ''
  const cached = _mdCache.get(text)
  if (cached !== undefined) return cached
  // Strip LLM thinking blocks before rendering.
  const stripped = text.replace(/<thinking>[\s\S]*?<\/thinking>/gi, '').trim()
  if (!stripped) { _mdCache.set(text, ''); return '' }
  // Replace bare DDG redirect URLs with the real destination so marked's
  // autolink / link renderer can display them cleanly.
  const ddgFixed = stripped.replace(
    /(?<![(\[])(?:https?:)?\/\/(?:html\.)?duckduckgo\.com\/l\/\?[^\s)>\]]+/g,
    (match) => {
      const real = extractRealUrl(match.startsWith('//') ? 'https:' + match : match)
      return real || match
    }
  )
  const processed = closeUnclosedFences(ddgFixed)
    .replace(BOLD_CLOSE_FIX, `$1${ZWJ}**`)
    .replace(BOLD_OPEN_FIX, `$1**${ZWJ}`)
  const html = marked(processed).replace(/‍/g, '')
  // Evict oldest entry when cache exceeds limit to bound memory usage.
  if (_mdCache.size >= _MD_CACHE_MAX) _mdCache.delete(_mdCache.keys().next().value)
  _mdCache.set(text, html)
  return html
}

/** extractUrls returns deduplicated http(s) URLs found in plain text, skipping markdown image syntax. */
function extractUrls(text) {
  if (!text) return []
  // Remove markdown image syntax ![...](...) so image URLs are not previewed.
  const noImages = text.replace(/!\[[^\]]*\]\([^)]+\)/g, '')
  const matches = noImages.match(/https?:\/\/[^\s)>\]"']+/g) || []
  // Strip trailing punctuation that is not part of the URL (e.g. `.`, `,`, `` ` ``, `。`, `、`).
  const stripped = matches.map(u => u.replace(/[.,;:!?`'"。、…）\]]+$/, ''))
  // Deduplicate while preserving order.
  return [...new Set(stripped)]
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

/** onAssistantBubbleContextMenu shows the per-message right-click menu. */
function onAssistantBubbleContextMenu(e, i) {
  const m = messages.value[i]
  if (!m || m.role !== 'assistant' || m.streaming || m.thinking) return
  // Only allow regen on the last assistant message.
  const lastAssistantIdx = messages.value.reduce((last, msg, idx) =>
    msg.role === 'assistant' && !msg.streaming && !msg.thinking ? idx : last, -1)
  if (i !== lastAssistantIdx) return
  e.preventDefault()
  msgMenuItems.value = [
    {
      iconSvg: '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 .49-3.5"/></svg>',
      label: '重新生成',
      action: () => regenLastReply(i),
    },
  ]
  msgMenuRef.value?.show(e.clientX, e.clientY)
}

/** regenLastReply removes the last assistant + user bubble and re-requests. */
async function regenLastReply(assistantIdx) {
  if (loading.value) return
  // Remove the assistant bubble at assistantIdx.
  messages.value.splice(assistantIdx, 1)
  // Remove the preceding user bubble (last user before assistantIdx).
  const userIdx = messages.value.slice(0, assistantIdx).reduce(
    (last, m, idx) => m.role === 'user' ? idx : last, -1)
  if (userIdx >= 0) messages.value.splice(userIdx, 1)

  loading.value = true
  isStreaming.value = true
  firstTokenThisTurn = true
  messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  try {
    await RegenerateLastReply()
  } catch (e) {
    const thinkIdx = messages.value.findLastIndex(m => m.thinking)
    if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)
    messages.value.push({ role: 'system', content: '重新生成失败: ' + e })
    loading.value = false
    isStreaming.value = false
    EventsEmit('pet:state:change', 'error')
  }
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

/** onEnterKey sends the message on Enter, but ignores Enter presses that
 * commit an IME composition (Chinese / Japanese / Korean input). Without
 * this guard, the Enter that closes the IME candidate panel would also
 * send a half-composed message. */
function onEnterKey(e) {
  if (e.isComposing || e.keyCode === 229) return
  e.preventDefault()
  send()
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
let _scrollRafPending = false
/** scrollToBottom scrolls to the latest message; coalesced via rAF to avoid redundant layout reads. */
function scrollToBottom() {
  if (_scrollRafPending) return
  _scrollRafPending = true
  requestAnimationFrame(() => {
    _scrollRafPending = false
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

let _autoResizePending = false
/** autoResize adjusts the textarea height to fit its content; batched via rAF to avoid per-keystroke reflow. */
function autoResize() {
  if (_autoResizePending) return
  _autoResizePending = true
  requestAnimationFrame(() => {
    _autoResizePending = false
    const el = textareaEl.value
    if (!el) return
    el.style.height = 'auto'
    el.style.height = el.scrollHeight + 'px'
    const empty = !el.value.trim()
    if (inputEmpty.value !== empty) inputEmpty.value = empty
  })
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
      <TransitionGroup name="msg-slide" tag="div" class="messages-inner" :class="{ 'suppress-anim': suppressAnimation }">
      <div v-for="(m, i) in messages" :key="msgKey(m, i)" :class="['msg', m.role]">
        <div class="bubble-wrap" :class="{ ghost: m.ghost }">
          <!-- Collapsible wrapper -->
          <div
            class="bubble-collapse-wrap"
            :class="{ 'is-collapsed': isCollapsed(m, i) }"
            :data-msg-key="msgKey(m, i)"
          >
            <div class="bubble-row" :class="{ 'is-collapsed': isCollapsed(m, i) }"
              @contextmenu="m.role === 'assistant' ? onAssistantBubbleContextMenu($event, i) : undefined">
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
                <template v-if="!m.streaming && !m.thinking && m.content">
                  <template v-if="extractUrls(m.content).length <= 1">
                    <LinkPreview v-for="u in extractUrls(m.content)" :key="u" :url="u" />
                  </template>
                  <template v-else>
                    <LinkPreview :url="extractUrls(m.content)[0]" :key="extractUrls(m.content)[0]" />
                    <template v-if="lpExpanded[msgKey(m, i)]">
                      <LinkPreview v-for="u in extractUrls(m.content).slice(1)" :key="u" :url="u" />
                    </template>
                    <button class="lp-toggle-btn" @click="lpExpanded[msgKey(m, i)] = !lpExpanded[msgKey(m, i)]">
                      {{ lpExpanded[msgKey(m, i)] ? '收起链接 ↑' : `展开另外 ${extractUrls(m.content).length - 1} 个链接 ↓` }}
                    </button>
                  </template>
                </template>
              </div>
              <template v-else>
                <div v-if="m.thinking || (m.streaming && !renderMarkdown(m.content))" :class="['bubble', 'thinking-bubble', { proactive: m.isProactive }]">
                  <span class="dot" /><span class="dot" /><span class="dot" />
                </div>
                <div v-else :class="['bubble', 'markdown', { proactive: m.isProactive }]">
                  <div v-html="renderMarkdown(m.content) + (m.streaming ? '<span class=\'cursor\'>▋</span>' : '')" />
                  <template v-if="!m.streaming && !m.thinking && m.content">
                    <template v-if="extractUrls(m.content).length <= 1">
                      <LinkPreview v-for="u in extractUrls(m.content)" :key="u" :url="u" />
                    </template>
                    <template v-else>
                      <LinkPreview :url="extractUrls(m.content)[0]" :key="extractUrls(m.content)[0]" />
                      <template v-if="lpExpanded[msgKey(m, i)]">
                        <LinkPreview v-for="u in extractUrls(m.content).slice(1)" :key="u" :url="u" />
                      </template>
                      <button class="lp-toggle-btn" @click="lpExpanded[msgKey(m, i)] = !lpExpanded[msgKey(m, i)]">
                        {{ lpExpanded[msgKey(m, i)] ? '收起链接 ↑' : `展开另外 ${extractUrls(m.content).length - 1} 个链接 ↓` }}
                      </button>
                    </template>
                  </template>
                  <div v-if="m.images && m.images.length > 0" class="msg-images">
                    <img v-for="(img, imgIdx) in m.images" :key="imgIdx" :src="img" class="msg-img" @click.stop="previewImage(img)" />
                  </div>
                </div>
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

              <!-- Collapse fade overlay + expand button (inside bubble-row so width matches bubble) -->
              <div v-if="isCollapsed(m, i)" class="collapse-fade" :class="m.role" @click.stop="toggleExpand(m, i)">
                <button class="collapse-btn" @click.stop="toggleExpand(m, i)">
                  <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                  展开
                </button>
              </div>
            </div>
          </div>

          <div v-if="(m.time && !m.streaming && !m.thinking) || (isEverCollapsed(m, i) && !isCollapsed(m, i))" class="msg-meta-row">
            <!-- user: recollapse left of timestamp; assistant: recollapse right of timestamp -->
            <button v-if="m.role === 'user' && isEverCollapsed(m, i) && !isCollapsed(m, i)" class="recollapse-btn" @click.stop="toggleExpand(m, i)">
              <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="18 15 12 9 6 15"/></svg>
              收起
            </button>
            <span v-if="m.time && !m.streaming && !m.thinking" class="msg-time">{{ formatTime(m.time) }}</span>
            <button v-if="m.role !== 'user' && isEverCollapsed(m, i) && !isCollapsed(m, i)" class="recollapse-btn" @click.stop="toggleExpand(m, i)">
              <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="18 15 12 9 6 15"/></svg>
              收起
            </button>
          </div>
        </div>
      </div>
      </TransitionGroup>
    </div>
    <!-- Image lightbox (normal: inside panel; fullscreen: teleported to body to escape backdrop-filter stacking context) -->
    <template v-if="lightboxSrc">
      <Teleport to="body" :disabled="!lightboxFullscreen">
        <div :class="['lightbox', { 'lightbox-fullscreen': lightboxFullscreen }]" @click="closeLightbox">
          <img :src="lightboxSrc" class="lightbox-img" @click.stop />
          <button class="lightbox-fullscreen-btn" @click.stop="lightboxFullscreen = !lightboxFullscreen" title="全屏预览">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/>
              <line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/>
            </svg>
            {{ lightboxFullscreen ? '退出全屏' : '全屏预览' }}
          </button>
        </div>
      </Teleport>
    </template>

    <!-- Tool execution confirmation modal -->
    <ToolConfirmModal />

    <!-- Per-message right-click context menu -->
    <ContextMenu ref="msgMenuRef" :items="msgMenuItems" />

    <!-- Clear chat confirmation dialog -->
    <Transition name="confirm-pop">
    <div v-if="showClearConfirm" class="clear-confirm-overlay" role="dialog" aria-modal="true" aria-labelledby="clear-confirm-title">
      <div class="clear-confirm-backdrop" @click="showClearConfirm = false" />
      <div class="clear-confirm-box">
        <p id="clear-confirm-title" class="clear-confirm-title">清空聊天记录</p>
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
      <div v-if="tableDetailRow" class="tbl-detail-overlay" role="dialog" aria-modal="true" aria-labelledby="tbl-detail-title">
        <div class="tbl-detail-backdrop" @click="tableDetailRow = null" />
        <div class="tbl-detail-box">
          <div class="tbl-detail-header">
            <span id="tbl-detail-title" class="tbl-detail-title">行详情</span>
            <button class="tbl-detail-close" aria-label="关闭" @click="tableDetailRow = null">
              <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="tbl-detail-body">
            <div v-for="pair in tableDetailRow" :key="pair.key" class="tbl-detail-pair">
              <span class="tbl-detail-key">{{ pair.key }}</span>
              <span class="tbl-detail-value markdown" v-html="renderMarkdown(pair.value)"></span>
            </div>
          </div>
        </div>
      </div>
    </Transition>

      <!-- In-chat progress indicators for running tools -->
      <ExecutionProgress />

      <!-- Voice recording status bar -->
      <div v-if="isRecording" class="voice-hint-bar" role="status" aria-live="polite">
        <span class="voice-hint-icon" aria-hidden="true">
          <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>
        </span>
        <span class="voice-hint-text">
          {{ voiceHint ? `"${voiceHint}"` : '正在聆听...' }}
        </span>
        <span class="voice-hint-dots" aria-hidden="true">
          <span />
          <span />
          <span />
        </span>
      </div>
    <!-- Pending image previews shown above the input row -->
    <div v-if="pendingImages.length > 0" class="pending-images">
      <div v-for="(img, idx) in pendingImages" :key="idx" class="pending-img-wrap">
        <img :src="img" class="pending-img" :alt="`待发送图片 ${idx + 1}`" />
        <button class="pending-img-remove" aria-label="移除图片" @click="removeImage(idx)">
          <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>
    </div>
    <!-- Pending file chips shown above the input row -->
    <div v-if="pendingFiles.length > 0" class="pending-files">
      <div v-for="(f, idx) in pendingFiles" :key="idx" class="pending-file-chip" :title="f.name">
        <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
        <span class="pending-file-name">{{ f.name }}</span>
        <button class="pending-file-remove" aria-label="移除文件" @click="removeFile(idx)">
          <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
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
        @keydown.enter.exact="onEnterKey"
        @keydown.meta.enter.prevent="insertNewline"
        @paste="onPaste"
        :disabled="loading"
      />
      <div class="input-toolbar">
        <div class="toolbar-spacer" />
        <button
          class="attach-btn"
          title="附加文件"
          aria-label="附加文件"
          :disabled="loading"
          @click="fileInputEl.click()"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>
        </button>
        <button v-if="isStreaming" class="stop-btn" aria-label="停止生成" @click="stopGeneration">⏹ 停止</button>
        <button v-else class="send-btn" title="发送 (Enter)" aria-label="发送" @click="send" :disabled="loading || (inputEmpty && pendingImages.length === 0 && pendingFiles.length === 0)">
          <svg xmlns="http://www.w3.org/2000/svg" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
        </button>
      </div>
    </div>
    <div class="input-hint">Enter 发送 · ⌘↩ 换行</div>
  </div>
</template>

<style scoped>
.chat-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  position: relative;
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', sans-serif;
  -webkit-font-smoothing: antialiased;
}

/* Messages list */
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 14px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.1) transparent;
}

.messages-inner {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* Slide-up + fade-in for newly appended messages */
.msg-slide-enter-active {
  transition: opacity 0.22s ease, transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.msg-slide-enter-from {
  opacity: 0;
  transform: translateY(10px);
}
/* Disable move transitions (repositioning during prepend) */
.msg-slide-move { transition: none; }
/* Suppress animation when loading older history */
.suppress-anim .msg-slide-enter-active { transition: none; }
.messages::-webkit-scrollbar { width: 4px; background: transparent; }
.messages::-webkit-scrollbar-track { background: transparent; }
.messages::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.12); border-radius: 2px; }
.messages::-webkit-scrollbar-thumb:hover { background: rgba(255,255,255,0.22); }

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
  background: var(--accent);
  opacity: 0.7;
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
/* Clip on `.bubble-row` (inline-flex → bubble width) so the fade anchors
   to the visible edge and respects the bubble's rounded corners. */
.bubble-row.is-collapsed {
  max-height: 350px;
  overflow: hidden;
  border-radius: 16px 16px 16px 4px;
}
.msg.user .bubble-row.is-collapsed { border-radius: 16px 16px 4px 16px; }

.collapse-fade {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 96px;
  background: linear-gradient(
    to bottom,
    rgba(0, 0, 0, 0)    0%,
    rgba(0, 0, 0, 0.38) 55%,
    rgba(0, 0, 0, 0.72) 100%
  );
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding-bottom: 10px;
  cursor: pointer;
}
/* User bubble fade: blend into the solid accent background. */
.collapse-fade.user {
  background: linear-gradient(
    to bottom,
    rgba(0, 122, 255, 0)    0%,
    rgba(0, 122, 255, 0.45) 55%,
    rgba(0, 122, 255, 0.95) 100%
  );
}

.collapse-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 14px;
  background: rgba(255, 255, 255, 0.14);
  border: 1px solid rgba(255, 255, 255, 0.22);
  border-radius: 20px;
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  backdrop-filter: var(--lg-blur-sm);
  -webkit-backdrop-filter: var(--lg-blur-sm);
  transition: background 0.12s, color 0.12s, border-color 0.12s;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.35);
  font-family: inherit;
}
.collapse-btn:hover {
  background: var(--accent);
  border-color: transparent;
  color: #fff;
}

/* Re-collapse button */
.recollapse-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0;
  background: none;
  border: none;
  color: var(--text-tertiary);
  font-size: 11px;
  cursor: pointer;
  transition: color 0.12s;
  font-family: inherit;
  box-shadow: none;
  line-height: 1;
}
.recollapse-btn:hover { color: var(--text-secondary); }

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
  user-select: text;
  -webkit-user-select: text;
}

/* User bubble — solid accent (macOS iMessage-like) */
.user .bubble {
  background: var(--accent);
  color: #fff;
  border-radius: 16px 16px 4px 16px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2), 0 0 0 0.5px rgba(0, 0, 0, 0.1);
}
.user .bubble.has-images {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Assistant bubble — glass surface */
.assistant .bubble {
  background: var(--lg-surface);
  color: var(--text-primary);
  border-radius: 16px 16px 16px 4px;
  border: 1px solid var(--lg-border-subtle);
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.08) inset,
    0 2px 8px rgba(0, 0, 0, 0.18);
}

/* System / error bubble */
.system .bubble {
  background: var(--danger-bg);
  color: #ffb3ad;
  border: 1px solid rgba(255, 69, 58, 0.28);
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
  border-left: 3px solid var(--accent);
  padding-left: 13px;
}

/* Stop button */
.stop-btn {
  background: var(--danger-bg);
  color: var(--danger);
  border: 1px solid rgba(255, 69, 58, 0.3);
  border-radius: 7px;
  padding: 6px 14px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s, transform 0.1s cubic-bezier(0.34, 1.56, 0.64, 1), box-shadow 0.15s;
}
.stop-btn:hover {
  background: rgba(255, 69, 58, 0.22);
  box-shadow: 0 0 0 3px rgba(255, 69, 58, 0.14);
}
.stop-btn:active { transform: scale(0.95); }
.stop-btn:focus-visible { outline: 2px solid var(--danger); outline-offset: 2px; }

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
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 18px;
  margin-top: 3px;
  padding: 0 4px;
}
.msg.user .msg-meta-row { justify-content: flex-end; }
.msg.assistant .msg-meta-row { justify-content: flex-start; }

/* Timestamp */
.msg-time {
  font-size: 11px;
  color: var(--text-label-muted);
  font-variant-numeric: tabular-nums;
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
  background: var(--lg-surface-elevated);
  border: 1px solid var(--lg-border-subtle);
  color: var(--text-secondary);
  border-radius: 6px;
  width: 26px;
  height: 26px;
  cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  outline: none;
  padding: 0;
  backdrop-filter: var(--lg-blur-sm);
  -webkit-backdrop-filter: var(--lg-blur-sm);
  transition: background 0.12s, color 0.12s, border-color 0.12s, transform 0.08s;
}
.msg-action-btn:hover {
  background: var(--accent);
  border-color: transparent;
  color: #fff;
}
.msg-action-btn:active { transform: scale(0.94); }
.msg-action-btn:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
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
  padding: 3px 10px;
  background: var(--accent-alpha-12);
  color: var(--accent);
  border: 1px solid var(--accent-alpha-20);
  border-radius: 5px;
  cursor: pointer;
  font-weight: 500;
  transition: background 0.12s, color 0.12s;
}
.bubble.markdown :deep(.code-copy:hover) {
  background: var(--accent);
  color: #fff;
  border-color: transparent;
}
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
.bubble.markdown :deep(code) { font-family: 'SF Mono', ui-monospace, 'JetBrains Mono', 'Fira Code', Menlo, monospace; font-size: 12px; }
.bubble.markdown :deep(:not(pre) > code) {
  background: var(--accent-alpha-12);
  color: var(--accent);
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 12px;
}
/* Inline code inside user bubble (solid accent background) — flip to white */
.user .bubble.markdown :deep(:not(pre) > code) {
  background: rgba(255, 255, 255, 0.18);
  color: #fff;
}
.user .bubble.markdown :deep(a) { color: #fff; text-decoration: underline; }

/* Lists */
.bubble.markdown :deep(ul), .bubble.markdown :deep(ol) { padding-left: 20px; margin: 4px 0 8px; }
.bubble.markdown :deep(li) { margin: 3px 0; line-height: 1.6; }
.bubble.markdown :deep(li > ul), .bubble.markdown :deep(li > ol) { margin: 2px 0; }

/* Blockquote */
.bubble.markdown :deep(blockquote) {
  border-left: 3px solid var(--accent);
  margin: 8px 0;
  padding: 6px 12px;
  color: var(--text-secondary);
  background: var(--accent-alpha-08);
  border-radius: 0 6px 6px 0;
}

/* Headings */
.bubble.markdown :deep(h1) { margin: 10px 0 6px; font-size: 16px; color: #f9fafb; font-weight: 700; }
.bubble.markdown :deep(h2) { margin: 10px 0 5px; font-size: 15px; color: #f9fafb; font-weight: 700; }
.bubble.markdown :deep(h3) { margin: 8px 0 4px; font-size: 14px; color: #e5e7eb; font-weight: 600; }
.bubble.markdown :deep(hr) { border: none; border-top: 1px solid rgba(255,255,255,0.1); margin: 10px 0; }

/* Links — break long URLs so they don't overflow the bubble */
.bubble.markdown :deep(a) {
  color: var(--accent);
  text-decoration: none;
  word-break: break-all;
}
.bubble.markdown :deep(a:hover) { text-decoration: underline; }

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
  background: var(--accent-alpha-08);
}
.bubble.markdown :deep(.tbl-row) { cursor: pointer; }

/* ── Table filter bar ─────────────────────────────────────── */
.bubble.markdown :deep(.tbl-filter-bar) {
  padding: 8px 10px;
  border-bottom: 1px solid var(--lg-border-subtle);
  background: rgba(255, 255, 255, 0.02);
  display: flex;
  gap: 6px;
  align-items: center;
}
.bubble.markdown :deep(.tbl-col-select) {
  position: relative;
  flex-shrink: 0;
}

/* Column-select trigger button (macOS pull-down style) */
.bubble.markdown :deep(.tbl-col-btn) {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 500;
  font-family: inherit;
  padding: 4px 8px 4px 10px;
  outline: none;
  cursor: pointer;
  white-space: nowrap;
  max-width: 120px;
  transition: background 0.12s, border-color 0.12s;
  height: 26px;
  box-shadow: none;
}
.bubble.markdown :deep(.tbl-col-btn:hover) { background: var(--lg-surface-input-h); }
.bubble.markdown :deep(.tbl-col-btn:focus-visible) {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
.bubble.markdown :deep(.tbl-col-chevron) {
  opacity: 0.55;
  flex-shrink: 0;
  width: 10px;
  height: 10px;
}

/* Dropdown popover — macOS menu style */
.bubble.markdown :deep(.tbl-col-drop) {
  display: none;
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  min-width: 140px;
  max-width: 240px;
  max-height: 240px;
  overflow-y: auto;
  background: var(--lg-surface-elevated);
  border: 1px solid var(--lg-border);
  border-radius: 9px;
  padding: 4px;
  z-index: 100;
  list-style: none;
  margin: 0;
  backdrop-filter: var(--lg-blur-sm);
  -webkit-backdrop-filter: var(--lg-blur-sm);
  box-shadow: var(--lg-shadow-sm);
}
.bubble.markdown :deep(.tbl-col-drop--open) { display: block; }
.bubble.markdown :deep(.tbl-col-drop)::-webkit-scrollbar { width: 6px; }
.bubble.markdown :deep(.tbl-col-drop)::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.14);
  border-radius: 3px;
}

.bubble.markdown :deep(.tbl-col-opt) {
  padding: 5px 10px;
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 400;
  border-radius: 5px;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  list-style: none;
  transition: background 0.08s, color 0.08s;
}
.bubble.markdown :deep(.tbl-col-opt:hover) {
  background: var(--accent);
  color: #fff;
}
.bubble.markdown :deep(.tbl-col-opt--sel) {
  color: #fff;
  background: var(--accent);
  font-weight: 500;
}

/* Filter search input */
.bubble.markdown :deep(.tbl-filter-input) {
  flex: 1;
  min-width: 0;
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 12px;
  font-family: inherit;
  padding: 4px 10px;
  outline: none;
  box-sizing: border-box;
  height: 26px;
  transition: background 0.12s, border-color 0.12s, box-shadow 0.12s;
}
.bubble.markdown :deep(.tbl-filter-input:hover:not(:focus)) { background: var(--lg-surface-input-h); }
.bubble.markdown :deep(.tbl-filter-input::placeholder) { color: var(--text-tertiary); }
.bubble.markdown :deep(.tbl-filter-input:focus) {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}

/* Pagination */
.bubble.markdown :deep(.table-pagination) {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 8px 12px;
  border-top: 1px solid var(--lg-border-subtle);
  background: rgba(255, 255, 255, 0.02);
}
.bubble.markdown :deep(.tbl-page-btn) {
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border);
  color: var(--text-secondary);
  border-radius: 6px;
  width: 28px;
  height: 26px;
  cursor: pointer;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  padding: 0;
  transition: background 0.12s, border-color 0.12s, color 0.12s, transform 0.08s;
}
.bubble.markdown :deep(.tbl-page-btn:hover:not(:disabled)) {
  background: var(--accent);
  border-color: transparent;
  color: #fff;
}
.bubble.markdown :deep(.tbl-page-btn:active:not(:disabled)) { transform: scale(0.94); }
.bubble.markdown :deep(.tbl-page-btn:disabled) {
  opacity: 0.3;
  cursor: not-allowed;
}
.bubble.markdown :deep(.tbl-page-info) {
  font-size: 12px;
  color: var(--text-secondary);
  user-select: none;
  min-width: 80px;
  text-align: center;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
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
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}
.tbl-detail-box {
  position: relative;
  background: var(--lg-surface-modal);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 14px;
  width: 460px;
  max-width: 90vw;
  max-height: 70vh;
  box-shadow: var(--lg-shadow);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.tbl-detail-header {
  display: flex;
  align-items: center;
  padding: 14px 16px;
  border-bottom: 1px solid var(--lg-border-subtle);
  flex-shrink: 0;
}
.tbl-detail-title {
  flex: 1;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  letter-spacing: -0.01em;
}
.tbl-detail-close {
  background: transparent;
  border: none;
  color: var(--text-tertiary);
  width: 24px;
  height: 24px;
  padding: 0;
  cursor: pointer;
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  font-size: 13px;
  transition: color 0.12s, background 0.12s;
}
.tbl-detail-close:hover {
  color: var(--danger);
  background: rgba(255, 69, 58, 0.14);
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
.tbl-detail-body::-webkit-scrollbar { width: 4px; background: transparent; }
.tbl-detail-body::-webkit-scrollbar-track { background: transparent; }
.tbl-detail-body::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.08); border-radius: 2px; }
.tbl-detail-body::-webkit-scrollbar-thumb:hover { background: rgba(255,255,255,0.18); }
.tbl-detail-pair {
  display: flex;
  gap: 14px;
  align-items: baseline;
  padding: 10px 12px;
  border-radius: 8px;
  transition: background 0.12s;
}
.tbl-detail-pair:hover { background: var(--accent-alpha-08); }
.tbl-detail-key {
  flex-shrink: 0;
  width: 120px;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-tertiary);
  word-break: break-word;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.tbl-detail-value {
  flex: 1;
  font-size: 13px;
  color: var(--text-primary);
  word-break: break-word;
  line-height: 1.55;
}
.tbl-detail-value.markdown :deep(p) { margin: 0; }
.tbl-detail-value.markdown :deep(p + p) { margin-top: 4px; }
.tbl-detail-value.markdown :deep(strong) { color: #e5e7eb; }
.tbl-detail-value.markdown :deep(code) { font-size: 12px; }
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
  margin: 10px 12px;
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 12px;
  flex-shrink: 0;
  transition: border-color 0.15s, box-shadow 0.15s, background 0.15s;
  overflow: hidden;
}
.input-area:hover:not(:focus-within) { background: var(--lg-surface-input-h); }
.input-area:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
.input-area textarea {
  display: block;
  width: 100%;
  box-sizing: border-box;
  background: transparent;
  border: none;
  padding: 10px 12px 6px;
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  resize: none;
  font-family: inherit;
  line-height: 1.55;
  min-height: 40px;
  max-height: 120px;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.14) transparent;
  user-select: text;
  -webkit-user-select: text;
}
.input-area textarea::-webkit-scrollbar { width: 4px; background: transparent; }
.input-area textarea::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.14); border-radius: 2px; }
.input-area textarea::placeholder { color: var(--text-tertiary); }
.input-toolbar {
  display: flex;
  align-items: center;
  padding: 4px 8px 6px;
  gap: 4px;
  border-top: none;
}
.toolbar-spacer { flex: 1; }
.input-hint {
  font-size: 11px;
  color: var(--text-label-muted);
  user-select: none;
  text-align: right;
  padding: 0 14px 8px;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.02em;
}
.send-btn {
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: 7px;
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s, transform 0.12s cubic-bezier(0.34, 1.56, 0.64, 1), box-shadow 0.15s;
  flex-shrink: 0;
  box-shadow: 0 2px 6px rgba(0, 122, 255, 0.35);
}
.send-btn:hover:not(:disabled) {
  background: var(--accent-hover);
  box-shadow: 0 3px 10px rgba(0, 122, 255, 0.5);
  transform: scale(1.04);
}
.send-btn:active:not(:disabled) {
  transform: scale(0.93);
  transition-duration: 0.08s;
}
.send-btn:disabled { opacity: 0.35; cursor: not-allowed; box-shadow: none; }
.send-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

/* ── Voice hint status bar ─────────────────────────────────── */
.voice-hint-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 12px 8px;
  padding: 10px 14px;
  border-radius: 10px;
  background: var(--accent-alpha-12);
  border: 1px solid var(--accent-alpha-20);
  font-size: 12px;
  color: var(--accent);
}

.voice-hint-icon {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  line-height: 1;
}

.voice-hint-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
  font-weight: 500;
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
  background: var(--accent);
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
  top: -7px;
  right: -7px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--lg-surface-elevated);
  color: #fff;
  border: 1px solid var(--lg-border);
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.4);
  transition: background 0.12s, transform 0.08s;
}
.pending-img-remove:hover { background: var(--danger); border-color: transparent; }
.pending-img-remove:active { transform: scale(0.92); }
.pending-img-remove:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

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

/* Lightbox — inside panel (normal mode) */
.lightbox {
  position: absolute;
  inset: 0;
  z-index: 200;
  background: rgba(0,0,0,0.85);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  cursor: zoom-out;
  pointer-events: auto;
}
/* Fullscreen mode: teleported to body so position:fixed covers the full window */
body > .lightbox {
  position: fixed;
  inset: 0;
  z-index: 9999;
}
body > .lightbox .lightbox-img {
  max-width: 98%;
  max-height: calc(100% - 60px);
  border-radius: 4px;
}
.lightbox-img {
  max-width: 90%;
  max-height: 80%;
  border-radius: 10px;
  box-shadow: 0 8px 40px rgba(0,0,0,0.6);
  object-fit: contain;
  cursor: default;
}
.lightbox-fullscreen-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 18px;
  border-radius: 20px;
  border: 1px solid rgba(255,255,255,0.25);
  background: rgba(255,255,255,0.1);
  color: rgba(255,255,255,0.85);
  font-size: 13px;
  cursor: pointer;
  backdrop-filter: blur(6px);
  transition: background 0.15s;
}
.lightbox-fullscreen-btn:hover {
  background: rgba(255,255,255,0.2);
}

/* Attach file button */
.attach-btn {
  flex-shrink: 0;
  background: transparent;
  border: none;
  border-radius: 6px;
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-tertiary);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
  padding: 0;
}
.attach-btn:hover:not(:disabled) { background: var(--lg-surface-hover); color: var(--text-primary); }
.attach-btn:disabled { opacity: 0.35; cursor: not-allowed; }
.attach-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }

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
  gap: 6px;
  background: var(--accent-alpha-12);
  border: 1px solid var(--accent-alpha-20);
  border-radius: 7px;
  padding: 4px 10px;
  font-size: 12px;
  color: var(--accent);
  max-width: 220px;
}
.pending-file-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.pending-file-remove {
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  color: var(--accent);
  opacity: 0.7;
  cursor: pointer;
  line-height: 1;
  padding: 0;
  flex-shrink: 0;
  box-shadow: none;
  border-radius: 4px;
  transition: opacity 0.12s, background 0.12s;
}
.pending-file-remove:hover { opacity: 1; background: var(--lg-surface-hover); }
.pending-file-remove:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }

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

/* Link preview inside user bubble: keep dark bg on hover so text stays readable */
.msg.user :deep(.link-preview) {
  background: rgba(0, 0, 0, 0.35);
  border-color: rgba(255, 255, 255, 0.15);
}
.msg.user :deep(.link-preview:hover) {
  background: rgba(0, 0, 0, 0.5);
  border-color: rgba(255, 255, 255, 0.28);
}

.lp-toggle-btn {
  display: inline-block;
  margin-top: 5px;
  padding: 0;
  background: none;
  border: none;
  font-size: 11px;
  color: rgba(3, 105, 161, 0.8);
  cursor: pointer;
  user-select: none;
  font-family: inherit;
}
.lp-toggle-btn:hover { color: rgba(3, 105, 161, 1); }
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
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}
.clear-confirm-box {
  position: relative;
  background: var(--lg-surface-modal);
  backdrop-filter: var(--lg-blur);
  -webkit-backdrop-filter: var(--lg-blur);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 14px;
  padding: 24px;
  width: 380px;
  max-width: 90vw;
  box-shadow: var(--lg-shadow);
  font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', sans-serif;
}
.confirm-pop-enter-active { transition: opacity 0.22s ease; }
.confirm-pop-leave-active { transition: opacity 0.14s ease-in; }
.confirm-pop-enter-from,
.confirm-pop-leave-to { opacity: 0; }
.confirm-pop-enter-active .clear-confirm-box {
  transition: transform 0.24s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.confirm-pop-leave-active .clear-confirm-box {
  transition: transform 0.14s ease-in;
}
.confirm-pop-enter-from .clear-confirm-box { transform: scale(0.92); }
.confirm-pop-leave-to .clear-confirm-box { transform: scale(0.96); }

.clear-confirm-title {
  font-size: 15px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.94);
  letter-spacing: -0.01em;
  margin: 0 0 10px;
}
.clear-confirm-text {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.66);
  margin: 0 0 20px;
  line-height: 1.6;
}
.clear-confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.clear-confirm-cancel,
.clear-confirm-ok {
  padding: 7px 18px;
  border-radius: 7px;
  font-size: 13px;
  font-weight: 500;
  letter-spacing: -0.01em;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.12s, transform 0.08s;
  -webkit-appearance: none;
  appearance: none;
}
.clear-confirm-cancel:active,
.clear-confirm-ok:active { transform: scale(0.97); }

.clear-confirm-cancel {
  border: 1px solid rgba(255, 255, 255, 0.14);
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.94);
}
.clear-confirm-cancel:hover { background: rgba(255, 255, 255, 0.10); }

.clear-confirm-ok {
  border: 1px solid transparent;
  background: #ff453a;
  color: #fff;
  font-weight: 600;
}
.clear-confirm-ok:hover { background: #ff5e55; }
</style>

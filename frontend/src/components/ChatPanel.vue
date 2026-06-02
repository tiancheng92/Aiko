<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { SendMessage, SendMessageWithImages, SendMessageWithFiles, GetMessages, GetMessagesBeforeID, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend, StopGeneration, SpeakText, StopTTS, GetConfig, SaveConfig, RegenerateLastReply, GetSoundsEnabled, ReadClipboard, SearchMessages, GetMessagesFromNewestToID } from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit, BrowserOpenURL } from '../../wailsjs/runtime/runtime'
import { throttle, debounce } from '../utils/timing.js'
import { ICON_THINKING, ICON_KNOWLEDGE, ICON_MEMORY } from '../utils/icons.js'
import { renderMarkdown, extractRealUrl, shortenUrl, stripEmotionTags, stripToolCallTags, closeUnclosedFences } from '../composables/useMarkdown.js'
import { useSounds } from '../composables/useSounds'
import { useTypingScheduler } from '../composables/useTypingScheduler'
import { useEscapeKey } from '../composables/useEscapeKey'
import { useI18n } from 'vue-i18n'
import { springAnimate } from '../composables/useSpring'
import ToolConfirmModal from './ToolConfirmModal.vue'
import ExecutionProgress from './ExecutionProgress.vue'
import LinkPreview from './LinkPreview.vue'
import ContextMenu from './ContextMenu.vue'
import { Crepe } from '@milkdown/crepe'
import '@milkdown/crepe/theme/common/style.css'
import '@milkdown/crepe/theme/frame-dark.css'
import { replaceAll } from '@milkdown/kit/utils'
import { editorViewCtx } from '@milkdown/kit/core'

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

const closeColDrops = () => document.querySelectorAll('.tbl-col-drop--open').forEach(d => d.classList.remove('tbl-col-drop--open'))

const PAGE_SIZE = 10

const messages = ref([])
/** streamingFading holds message keys that are playing the shimmer fade-out animation after streaming ends. */
const streamingFading = reactive(new Set())
let settleIntervalId = null
/** oldestLoadedID is the smallest message ID currently in the list; used for lazy-loading older pages. */
let oldestLoadedID = null
/** allLoaded is true when there are no more older messages to fetch. */
const allLoaded = ref(false)
/** searchQuery is the current search input text. */
const searchQuery = ref('')
/** isSearching is true when the search bar is visible and active. */
const isSearching = ref(false)
/** searchResults holds messages returned from FTS5 search, or null when not searching. */
const searchResults = ref(null)
/** searchSnapshot saves normal state before entering search for restore on exit. */
let searchSnapshot = null
/** searchDebounceTimer holds the active debounce timer for search input. */
let searchDebounceTimer = null
const searchInputEl = ref(null)
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
/** lightboxZoom is the current scale factor (1 = 100%). */
const lightboxZoom = ref(1)
/** lightboxPan is the current x/y offset in pixels when panning a zoomed image. */
const lightboxPan = ref({ x: 0, y: 0 })
/** lbDragging is true while the user is panning the image. */
const lbDragging = ref(false)
let _lbDragStart = { x: 0, y: 0 }
let _lbPanStart = { x: 0, y: 0 }
let _lbDidDrag = false

/** previewImage opens the lightbox for the given image src. */
function previewImage(src) {
  lightboxSrc.value = src
  lightboxFullscreen.value = false
  lightboxZoom.value = 1
  lightboxPan.value = { x: 0, y: 0 }
}

/** closeLightbox closes the lightbox and resets all state. */
function closeLightbox() {
  lightboxSrc.value = null
  lightboxFullscreen.value = false
  lightboxZoom.value = 1
  lightboxPan.value = { x: 0, y: 0 }
}

/** onLightboxBgClick closes the lightbox on background click, but not after a drag. */
function onLightboxBgClick() {
  if (_lbDidDrag) { _lbDidDrag = false; return }
  closeLightbox()
}

/** onLightboxWheel zooms in/out with the scroll wheel. */
function onLightboxWheel(e) {
  e.preventDefault()
  const factor = e.deltaY > 0 ? 0.88 : 1.14
  lightboxZoom.value = Math.min(10, Math.max(0.1, lightboxZoom.value * factor))
}

/** onLbImgMousedown starts a pan drag. */
function onLbImgMousedown(e) {
  if (e.button !== 0) return
  lbDragging.value = true
  _lbDidDrag = false
  _lbDragStart = { x: e.clientX, y: e.clientY }
  _lbPanStart = { ...lightboxPan.value }
  e.preventDefault()
}

/** onLbMousemove updates pan offset while dragging. */
function onLbMousemove(e) {
  if (!lbDragging.value) return
  const dx = e.clientX - _lbDragStart.x
  const dy = e.clientY - _lbDragStart.y
  if (Math.abs(dx) > 2 || Math.abs(dy) > 2) _lbDidDrag = true
  lightboxPan.value = { x: _lbPanStart.x + dx, y: _lbPanStart.y + dy }
}

/** onLbMouseup ends a pan drag. */
function onLbMouseup() { lbDragging.value = false }

/** lbZoomIn increases zoom by 25%. */
function lbZoomIn() { lightboxZoom.value = Math.min(10, lightboxZoom.value * 1.25) }
/** lbZoomOut decreases zoom by 20%. */
function lbZoomOut() { lightboxZoom.value = Math.max(0.1, lightboxZoom.value / 1.25) }
/** lbReset resets zoom and pan to defaults. */
function lbReset() { lightboxZoom.value = 1; lightboxPan.value = { x: 0, y: 0 } }
const loading = ref(false)
const messagesEl = ref(null)
const chatPanelEl = ref(null)
const spotlightEl = ref(null)
let _rafPending = false
/** onChatPanelMousemove updates the spotlight position via GPU-composited transform (no repaint). */
function onChatPanelMousemove(e) {
  if (_rafPending) return
  _rafPending = true
  requestAnimationFrame(() => {
    _rafPending = false
    const panel = chatPanelEl.value
    const spot = spotlightEl.value
    if (!panel || !spot) return
    const rect = panel.getBoundingClientRect()
    const x = e.clientX - rect.left - 260
    const y = e.clientY - rect.top - 260
    spot.style.transform = `translate(${x}px, ${y}px)`
  })
}
/** sentinelObserver watches the top-of-list sentinel for infinite-scroll lazy loading. */
let sentinelObserver = null
const codeMaxWidth = ref(0)
const copiedIdx = ref(null)
const showClearConfirm = ref(false)
const msgMenuRef = ref(null)
const msgMenuItems = ref([])
const inputMenuRef = ref(null)
const inputMenuItems = ref([])
/** tableDetailRow holds the key-value pairs for the row-detail modal; null when hidden. */
const tableDetailRow = ref(null)
/** copiedPairKey is the key of the pair whose value was most recently copied, or null. */
const copiedPairKey = ref(null)

/** copyPairValue copies the value of a single key-value pair to the clipboard. */
function copyPairValue(pair) {
  navigator.clipboard.writeText(pair.value).then(() => {
    copiedPairKey.value = pair.key
    setTimeout(() => { copiedPairKey.value = null }, 2000)
  })
}

/** toolArgsPopover holds { pairs, x, y } for the tool-args popover; null when hidden. */
const toolArgsPopover = ref(null)

window.__showToolArgs = (e) => {
  e.stopPropagation()
  const chip = e.currentTarget
  const b64 = chip.dataset.args || ''
  let pairs = []
  try {
    const bytes = Uint8Array.from(atob(b64), c => c.charCodeAt(0))
    const obj = JSON.parse(new TextDecoder().decode(bytes))
    pairs = Object.entries(obj).map(([k, v]) => ({
      key: k,
      value: typeof v === 'string' ? v : JSON.stringify(v, null, 2),
    }))
  } catch {}
  if (!pairs.length) return
  toolArgsPopover.value = { pairs }
}

/** __toggleChipGroup expands or collapses the extra chips in a tool-call-group. */
window.__toggleChipGroup = (btn) => {
  const group = btn.closest('.tool-call-group')
  if (!group) return
  const isNowCollapsed = group.classList.toggle('collapsed')
  const count = group.querySelectorAll('.tool-call-chip-extra').length
  btn.textContent = isNowCollapsed ? t('chat.more', { n: count }) : t('chat.collapse')
}

useEscapeKey(() => { showClearConfirm.value = false }, showClearConfirm)
useEscapeKey(() => { tableDetailRow.value = null }, () => tableDetailRow.value !== null)
useEscapeKey(() => { toolArgsPopover.value = null }, () => toolArgsPopover.value !== null)

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

/** springCancels maps message keys to in-flight spring cancel functions. */
const springCancels = new Map()


/** toggleExpand expands or collapses a message using spring physics.
 *
 *  Expand —  ζ ≈ 0.75 (underdamped): opens with a gentle overshoot so the
 *  bubble briefly shows a sliver of extra space, then settles back — giving a
 *  physical "inhale" feel that no cubic-bezier can replicate.
 *
 *  Collapse — ζ ≈ 1.02 (near-critical): closes decisively with no bounce; the
 *  spring decelerates exactly as it hits 350px and stops dead. */
function toggleExpand(m, i) {
  const k = msgKey(m, i)

  // Cancel any in-flight spring for this key before starting a new one.
  springCancels.get(k)?.()
  springCancels.delete(k)

  const wrapEl = messagesEl.value?.querySelector(`[data-msg-key="${CSS.escape(k)}"]`)
  const rowEl = wrapEl?.querySelector('.bubble-row')

  // No DOM — instant toggle (history prepend before paint, or unmounted).
  if (!rowEl) {
    const next = new Set(expandedIds.value)
    if (next.has(k)) next.delete(k); else next.add(k)
    expandedIds.value = next
    return
  }

  // Respect OS reduced-motion: instant toggle, no spring.
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    const next = new Set(expandedIds.value)
    if (next.has(k)) next.delete(k); else next.add(k)
    expandedIds.value = next
    return
  }

  if (!expandedIds.value.has(k)) {
    // ── EXPAND ──────────────────────────────────────────────────────────
    // Hold the element at COLLAPSE_HEIGHT via inline style so the visual
    // doesn't jump while Vue re-renders and removes `.is-collapsed`.
    rowEl.style.height = COLLAPSE_HEIGHT + 'px'
    rowEl.style.maxHeight = 'none'
    rowEl.style.overflow = 'hidden'
    rowEl.style.willChange = 'height'
    rowEl.style.transition = 'none'

    const next = new Set(expandedIds.value)
    next.add(k)
    expandedIds.value = next

    nextTick(() => {
      const targetH = rowEl.scrollHeight
      // ζ = 22 / (2·√180) ≈ 0.82 — gentle underdamping gives ~1.5% overshoot:
      // the bubble briefly grows ~5px past content height then snaps back.
      const cancel = springAnimate({
        from: COLLAPSE_HEIGHT,
        to: targetH,
        stiffness: 180,
        damping: 22,
        onUpdate: (h) => { rowEl.style.height = h + 'px' },
        onDone: () => {
          rowEl.style.cssText = ''
          springCancels.delete(k)
        },
      })
      springCancels.set(k, cancel)
    })
  } else {
    // ── COLLAPSE ─────────────────────────────────────────────────────────
    // ζ = 34 / (2·√280) ≈ 1.015 — near-critical: fast, zero bounce.
    const startH = rowEl.scrollHeight
    rowEl.style.height = startH + 'px'
    rowEl.style.maxHeight = 'none'
    rowEl.style.overflow = 'hidden'
    rowEl.style.willChange = 'height'
    rowEl.style.transition = 'none'

    const cancel = springAnimate({
      from: startH,
      to: COLLAPSE_HEIGHT,
      stiffness: 280,
      damping: 34,
      // Floor prevents the element from briefly clipping below 350px on
      // the negligible undershoot that occurs even at near-critical damping.
      onUpdate: (h) => { rowEl.style.height = Math.max(h, COLLAPSE_HEIGHT - 20) + 'px' },
      onDone: () => {
        rowEl.style.cssText = ''
        const next = new Set(expandedIds.value)
        next.delete(k)
        expandedIds.value = next
        springCancels.delete(k)
      },
    })
    springCancels.set(k, cancel)
  }
}

/** toggleThinkingExpanded toggles the thinking block's expand/collapse state for message at index i. */
function toggleThinkingExpanded(i) {
  const m = messages.value[i]
  if (m) messages.value[i] = { ...m, thinkingExpanded: !m.thinkingExpanded }
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
/** markdownMode is true when the Milkdown editor is active instead of the textarea. */
const markdownMode = ref(false)
/** milkdownEl is the DOM element Milkdown mounts into. */
const milkdownEl = ref(null)
/** milkdownInstance holds the active Crepe instance, or null when not in markdown mode. */
let milkdownInstance = null
/** milkdownEditorDom caches the ProseMirror DOM node for reliable keydown listener cleanup. */
let milkdownEditorDom = null
/** lpExpanded tracks whether the extra link previews are shown for each message (keyed by msgKey). */
const lpExpanded = ref({})
const isRecording = ref(false)
const voiceHint = ref('')
const voiceAutoSend = ref(false)
const isStreaming = ref(false)
const activeTTSMsgId = ref(null)  // id of the message currently being spoken
const cfg = ref(null)
const aiAvatar = ref('')    // data URL or '' (use default /logo.png)
const userAvatar = ref('')  // data URL or '' (use default SVG)

/** thinkingLevel is the current reasoning effort level for the next message. */
const thinkingLevel = ref('default')
const thinkingChipFired = ref(false)
const knowledgeChipFired = ref(false)
const memoryChipFired = ref(false)
/** useKnowledge controls whether the knowledge base is queried for the next message. */
const useKnowledge = ref(true)
/** useMemory controls whether long-term memory is queried for the next message. */
const useMemory = ref(true)

/** isOpenRouter is true when the active LLM provider is OpenRouter. */
const isOpenRouter = computed(() => cfg.value?.LLMProvider === 'openrouter')

/** thinkingLevels returns available thinking level options based on the current provider. */
const thinkingLevels = computed(() =>
  isOpenRouter.value
    ? ['default', 'off', 'low', 'medium', 'high']
    : ['default', 'low', 'medium', 'high']
)

/** thinkingLevelLabel returns the i18n label for the current thinking level. */
const thinkingLevelLabel = computed(() => {
  const key = { default: 'default', off: 'off', low: 'low', medium: 'medium', high: 'high' }[thinkingLevel.value] || 'default'
  return t('chat.thinkingLabel.' + key)
})

const { t } = useI18n()
const { playSend, playReceive, playError, playStop } = useSounds()
let soundsEnabled = false

/** settleMessage flushes pendingTokens into displayHtml for the message at idx. */
function settleMessage(idx) {
  const msg = messages.value[idx]
  if (!msg || !msg.pendingTokens?.length) return
  msg.displayHtml = renderMarkdown(msg.content)
  msg.pendingTokens = []
}

/** applyToken appends a token to the last streaming assistant message. */
function applyToken(token) {
  // Transition the thinking placeholder on first real token.
  const thinkIdx = messages.value.findLastIndex(m => m.thinking)
  if (thinkIdx >= 0) {
    if (messages.value[thinkIdx].thinkingContent) {
      messages.value[thinkIdx] = {
        ...messages.value[thinkIdx],
        thinking: false,
        content: token,
        displayHtml: '',
        pendingTokens: [{ text: token, key: 0 }],
        tokenKeyCounter: 1,
      }
      scrollToBottom()
      return
    }
    messages.value.splice(thinkIdx, 1)
  }

  const idx = messages.value.length - 1
  const last = messages.value[idx]
  if (last && last.role === 'assistant' && last.streaming) {
    last.content += token
    last.pendingTokens.push({ text: token, key: last.tokenKeyCounter++ })
    if (last.pendingTokens.length >= 40) settleMessage(idx)
  } else {
    messages.value.push({
      role: 'assistant',
      content: token,
      streaming: true,
      isProactive: proactiveStarted,
      thinkingContent: '',
      thinkingExpanded: false,
      displayHtml: '',
      pendingTokens: [{ text: token, key: 0 }],
      tokenKeyCounter: 1,
    })
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
let offToken, offDone, offError, offClear, offProactiveStart, offProactiveMessage, offCronStart, offImage, offThinking, offSystemInject
let offTTSDone, offTTSError, offTTSAudio
let offSoundsChanged
let offAvatarChanged
let offModelChanged
let offVoiceStart, offVoiceTranscript, offVoiceEnd, offVoiceFinal, offVoiceError, offVoiceAutoSend
let offUpdateProgress, offUpdateError
let voiceTranscriptHandler, updateProgressHandler

const updateProgress = ref(0)
const updateProgressMsg = ref('')
const isUpdating = ref(false)
/** @type {HTMLAudioElement|null} 当前正在播放的 TTS Audio 实例，用于暂停 */
let currentTTSAudio = null
let resizeObserver = null

/** mapMsg converts a backend Message to the frontend shape. */
function mapMsg(m) {
  return {
    id: m.ID,
    role: m.Role,
    content: stripEmotionTags(m.Content ?? ''),
    thinkingContent: m.ThinkingContent ?? '',
    thinkingExpanded: false,
    time: m.CreatedAt,
    images: m.Images || [],
    files: m.Files || []
  }
}

/** loadOlderMessages fetches the next page of older messages and prepends them. */
async function loadOlderMessages() {
  if (loadingHistory.value || allLoaded.value || oldestLoadedID === null) return
  loadingHistory.value = true
  // One rAF so Vue flushes the loading-dots before the IPC call starts.
  await new Promise(resolve => requestAnimationFrame(resolve))
  try {
    const older = await GetMessagesBeforeID(oldestLoadedID, PAGE_SIZE)
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
    // After prepend, index-based keys (i:N) shift by firstOldIdx.
    // Re-key collapsedIds and expandedIds to keep collapse state consistent.
    const rekey = (set) => {
      const next = new Set()
      for (const k of set) {
        if (k.startsWith('i:')) {
          next.add('i:' + (parseInt(k.slice(2), 10) + firstOldIdx))
        } else {
          next.add(k)
        }
      }
      return next
    }
    collapsedIds.value = rekey(collapsedIds.value)
    expandedIds.value = rekey(expandedIds.value)
    // One nextTick for Vue to flush the DOM, then one rAF for browser layout.
    await nextTick()
    await new Promise(resolve => requestAnimationFrame(resolve))
    suppressAnimation.value = false
    oldestLoadedID = older[0].ID
    olderMapped.forEach((m, i) => checkBubbleCollapse(m, i, true))
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

/** enterSearch saves current message state and activates search mode. */
function enterSearch() {
  if (isStreaming.value) return
  searchSnapshot = {
    messages: [...messages.value],
    oldestLoadedID,
    allLoaded: allLoaded.value,
  }
  isSearching.value = true
  searchQuery.value = ''
  searchResults.value = null
  selectedResultIndex.value = null
  // Disable infinite-scroll observer while searching.
  sentinelObserver?.disconnect()
  nextTick(() => searchInputEl.value?.focus())
}

/** exitSearch restores the pre-search message list and scrolls to bottom. */
function exitSearch() {
  if (!searchSnapshot) return
  isSearching.value = false
  searchQuery.value = ''
  searchResults.value = null
  clearTimeout(searchDebounceTimer)
  messages.value = searchSnapshot.messages
  oldestLoadedID = searchSnapshot.oldestLoadedID
  allLoaded.value = searchSnapshot.allLoaded
  searchSnapshot = null
  // Re-enable observer and scroll to bottom.
  nextTick(() => {
    const sentinel = document.getElementById('msg-load-sentinel')
    if (sentinel && sentinelObserver) sentinelObserver.observe(sentinel)
    scrollToBottom()
  })
}

/** doSearch calls the backend FTS5 search and updates results. */
async function doSearch(query) {
  const q = query.trim()
  if (!q) {
    searchResults.value = null
    return
  }
  try {
    const results = await SearchMessages(q)
    searchResults.value = (results || []).map(mapMsg)
    selectedResultIndex.value = null
  } catch (e) {
    console.warn('search failed:', e)
    searchResults.value = []
  }
}

/** onSearchInput handles debounced search input. */
function onSearchInput(e) {
  searchQuery.value = e.target.value
  clearTimeout(searchDebounceTimer)
  searchDebounceTimer = setTimeout(() => doSearch(searchQuery.value), 300)
}

/** selectedResultIndex tracks the keyboard-selected search result, or null. */
const selectedResultIndex = ref(null)

/** onSearchKeydown handles keyboard navigation in search input. */
function onSearchKeydown(e) {
  if (e.key === 'Escape') {
    exitSearch()
    return
  }
  if (!searchResults.value || searchResults.value.length === 0) return

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedResultIndex.value = selectedResultIndex.value === null
      ? 0
      : Math.min(selectedResultIndex.value + 1, searchResults.value.length - 1)
    scrollToSelectedResult()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedResultIndex.value = selectedResultIndex.value === null
      ? searchResults.value.length - 1
      : Math.max(selectedResultIndex.value - 1, 0)
    scrollToSelectedResult()
  } else if (e.key === 'Enter' && selectedResultIndex.value !== null) {
    e.preventDefault()
    const msg = searchResults.value[selectedResultIndex.value]
    if (msg) jumpToMessage(msg.id)
  }
}

/** scrollToSelectedResult brings the keyboard-selected result into view. */
function scrollToSelectedResult() {
  if (selectedResultIndex.value === null) return
  nextTick(() => {
    const el = document.querySelector(`[data-msg-key="id:${searchResults.value[selectedResultIndex.value].id}"]`)
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  })
}

/** jumpToMessage loads context from newest down to the page containing targetID, then scrolls to it. */
async function jumpToMessage(targetID) {
  if (isStreaming.value) return
  exitSearch()
  loadingHistory.value = true
  try {
    const msgs = await GetMessagesFromNewestToID(targetID)
    if (!msgs || msgs.length === 0) return
    suppressAnimation.value = true
    messages.value = msgs.map(mapMsg)
    if (msgs.length > 0) oldestLoadedID = msgs[0].ID
    allLoaded.value = false

    await nextTick()
    await new Promise(resolve => requestAnimationFrame(resolve))
    suppressAnimation.value = false

    // Re-enable observer.
    const sentinel = document.getElementById('msg-load-sentinel')
    if (sentinel && sentinelObserver) sentinelObserver.observe(sentinel)

    // Scroll to target message and flash highlight.
    const el = document.querySelector(`[data-msg-key="id:${targetID}"]`)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.classList.add('jump-flash')
      setTimeout(() => el.classList.remove('jump-flash'), 2000)
    }
  } catch (e) {
    console.warn('jump to message failed:', e)
  } finally {
    loadingHistory.value = false
  }
}

/** displayMessages returns search results when a query has been executed,
 *  or the normal message list when idle / before the first keystroke. */
const displayMessages = computed(() => {
  if (isSearching.value && searchResults.value !== null) return searchResults.value
  return messages.value
})

/** searchMatchIds is a Set of message IDs that match the search query. */
const searchMatchIds = computed(() => {
  if (!isSearching.value || !searchResults.value) return null
  return new Set(searchResults.value.map(m => m.id))
})

/** highlightMatches wraps occurrences of query terms in <mark> tags. */
function highlightMatches(text, query) {
  if (!query || !query.trim()) return text
  const escaped = query.trim().replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(`(${escaped})`, 'gi')
  return text.replace(re, '<mark class="search-highlight">$1</mark>')
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
          content: t('chat.system.welcome'),
          thinkingContent: '',
          thinkingExpanded: false,
        })
        scrollToBottom()
        await MarkWelcomeShown()
      }
    } catch (e) {
      console.warn('welcome check failed:', e)
    }
  }

  offSystemInject = EventsOn('chat:system:inject', (msg) => {
    loading.value = false
    isStreaming.value = false
    messages.value.push({ role: 'system', content: msg, isInfo: true })
    nextTick(scrollToBottom)
  })

  offClear = EventsOn('chat:clear', () => {
    showClearConfirm.value = true
  })

  offProactiveStart = EventsOn('chat:proactive:start', () => {
    proactiveStarted = true
    messages.value.push({ role: 'assistant', content: '', streaming: true, isProactive: true, thinkingContent: '', thinkingExpanded: false, displayHtml: '', pendingTokens: [], tokenKeyCounter: 0 })
    EventsEmit('pet:state:change', 'speaking')
    scrollToBottom()
  })

  offProactiveMessage = EventsOn('chat:proactive:message', (text) => {
    messages.value.push({ role: 'assistant', content: text, isProactive: true, thinkingContent: '', thinkingExpanded: false })
    EventsEmit('pet:state:change', 'speaking')
    scrollToBottom()
    setTimeout(() => EventsEmit('pet:state:change', 'idle'), 2000)
  })

  offCronStart = EventsOn('chat:cron:start', ({ name, prompt }) => {
    // Push a user-side trigger label followed by a streaming assistant placeholder.
    messages.value.push({ role: 'user', content: `⏰ **${name}**\n${prompt}`, isCron: true })
    messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, isCron: true, thinkingContent: '', thinkingExpanded: false, displayHtml: '', pendingTokens: [], tokenKeyCounter: 0 })
    loading.value = true
    isStreaming.value = true
    firstTokenThisTurn = true
    EventsEmit('pet:state:change', 'thinking')
    scrollToBottom()
  })

  offThinking = EventsOn('chat:thinking', (token) => {
    const last = messages.value[messages.value.length - 1]
    if (last && last.role === 'assistant') {
      if (last.thinkingContent === undefined) last.thinkingContent = ''
      last.thinkingContent += token
      last.thinkingExpanded = true
    }
    scrollToBottom()
    scrollThinkingToBottom()
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
    if (idx >= 0) {
      settleMessage(idx)
      const fadingKey = msgKey(messages.value[idx], idx)
      streamingFading.add(fadingKey)
      setTimeout(() => streamingFading.delete(fadingKey), 700)
      messages.value[idx] = { ...messages.value[idx], streaming: false, thinkingExpanded: false, time: new Date() }
    }
    loading.value = false
    isStreaming.value = false
    proactiveStarted = false
    EventsEmit('pet:state:change', 'idle')
    // Check if the newly completed message is tall enough to collapse.
    if (idx >= 0) {
      const m = messages.value[idx]
      const k = msgKey(m, idx)
      nextTick(() => requestAnimationFrame(() => {
        const bubbleEl = messagesEl.value?.querySelector(`[data-msg-key="${CSS.escape(k)}"]`)
        if (bubbleEl && bubbleEl.scrollHeight > COLLAPSE_HEIGHT) {
          const nextC = new Set(collapsedIds.value)
          nextC.add(k)
          collapsedIds.value = nextC
          const nextE = new Set(expandedIds.value)
          nextE.add(k)
          expandedIds.value = nextE
        }
      }))
    }
    // Auto-play TTS if enabled and this is not a voice-triggered response
    if (cfg.value?.TTSAutoPlay && lastMsg?.content && !isRecording.value) {
      activeTTSMsgId.value = idx
      SpeakText(stripToolCallTags(lastMsg.content)).catch(() => { activeTTSMsgId.value = null })
    }
  })

  offError = EventsOn('chat:error', (err) => {
    typingScheduler.clear()
    const errIdx = messages.value.findLastIndex(m => m.streaming)
    if (errIdx >= 0) {
      settleMessage(errIdx)
      messages.value[errIdx] = { ...messages.value[errIdx], streaming: false }
    }
    const thinkIdx = messages.value.findLastIndex(m => m.thinking)
    if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)
    messages.value.push({ role: 'system', content: t('chat.system.error', { error: err }) })
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
  try {
    cfg.value = await GetConfig()
    aiAvatar.value = cfg.value?.AIAvatar || ''
    userAvatar.value = cfg.value?.UserAvatar || ''
    thinkingLevel.value = cfg.value?.ThinkingLevel || 'default'
    useKnowledge.value = cfg.value?.UseKnowledge !== false
    useMemory.value = cfg.value?.UseMemory !== false
  } catch {}

  offAvatarChanged = EventsOn('config:avatar:changed', ({ role, dataURL }) => {
    if (role === 'ai') aiAvatar.value = dataURL || ''
    else if (role === 'user') userAvatar.value = dataURL || ''
  })

  offModelChanged = EventsOn('config:model:changed', async () => {
    try {
      const prevProvider = cfg.value?.LLMProvider
      cfg.value = await GetConfig()
      if (cfg.value?.LLMProvider !== prevProvider) {
        const validLevels = cfg.value?.LLMProvider === 'openrouter'
          ? ['default', 'off', 'low', 'medium', 'high']
          : ['default', 'low', 'medium', 'high']
        if (!validLevels.includes(thinkingLevel.value)) {
          thinkingLevel.value = 'default'
        }
      }
    } catch {}
  })

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
    if (!markdownMode.value) {
      setInputDOM('')
      nextTick(() => textareaEl.value?.focus())
    }
  })

  voiceTranscriptHandler = throttle((text) => {
    if (!markdownMode.value) {
      setInputDOM(text)
    }
    voiceHint.value = text
  }, 80)
  offVoiceTranscript = EventsOn('voice:transcript', voiceTranscriptHandler)

  offVoiceEnd = EventsOn('voice:end', () => {
    isRecording.value = false
    voiceHint.value = ''
  })

  offVoiceFinal = EventsOn('voice:final', (text) => {
    voiceHint.value = ''
    if (markdownMode.value) {
      milkdownInstance?.editor.action(replaceAll(text))
      inputEmpty.value = !text.trim()
    } else {
      setInputDOM(text)
      nextTick(() => textareaEl.value?.focus())
    }
    if (voiceAutoSend.value && text.trim()) {
      send()
    }
  })

  offVoiceError = EventsOn('voice:error', (errMsg) => {
    isRecording.value = false
    voiceHint.value = ''
    if (!markdownMode.value) {
      setInputDOM('')
    }
    EventsEmit('notification:show', {
      title: t('chat.system.voiceError'),
      message: errMsg === 'mic_denied'
        ? t('chat.system.voiceErrorMicDenied')
        : errMsg === 'speech_denied'
          ? t('chat.system.voiceErrorSpeechDenied')
          : t('chat.system.voiceErrorUnknown', { error: errMsg }),
    })
  })

  offVoiceAutoSend = EventsOn('config:voice:auto-send:changed', (val) => {
    voiceAutoSend.value = val
  })

  updateProgressHandler = throttle((data) => {
    isUpdating.value = true
    updateProgress.value = data.pct ?? 0
    updateProgressMsg.value = data.msg ?? ''
    if ((data.pct ?? 0) >= 100) {
      setTimeout(() => { isUpdating.value = false }, 2000)
    }
  }, 100)
  offUpdateProgress = EventsOn('update:progress', updateProgressHandler)
  offUpdateError = EventsOn('update:error', (msg) => {
    isUpdating.value = false
    updateProgress.value = 0
    updateProgressMsg.value = ''
    messages.value.push({ role: 'system', content: t('chat.system.updateFailed', { error: msg }) })
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

  settleIntervalId = setInterval(() => {
    const idx = messages.value.findLastIndex(m => m.streaming)
    if (idx >= 0) settleMessage(idx)
  }, 500)
})

onUnmounted(() => {
  destroyMilkdown()
  // Invoke every EventsOn teardown; undefined entries are safely skipped via
  // optional chaining so a partial mount (e.g. early error) does not throw here.
  offToken?.(); offDone?.(); offError?.(); offClear?.(); offImage?.(); offThinking?.()
  offProactiveStart?.(); offProactiveMessage?.(); offCronStart?.(); offSystemInject?.()
  offTTSDone?.(); offTTSError?.(); offTTSAudio?.()
  offSoundsChanged?.(); offAvatarChanged?.(); offModelChanged?.()
  offVoiceStart?.(); offVoiceTranscript?.(); offVoiceEnd?.(); offVoiceFinal?.(); offVoiceError?.(); offVoiceAutoSend?.()
  voiceTranscriptHandler?.cancel?.()
  offUpdateProgress?.(); offUpdateError?.()
  updateProgressHandler?.cancel?.()
  onMessagesScroll?.cancel?.()
  if (_smoothScrollRaf) { cancelAnimationFrame(_smoothScrollRaf); _smoothScrollRaf = null }
  document.removeEventListener('click', closeColDrops)
  sentinelObserver?.disconnect()
  sentinelObserver = null
  // Stop any in-flight TTS playback so detached <audio> elements can be GC'd.
  if (currentTTSAudio) { try { currentTTSAudio.pause() } catch {} ; currentTTSAudio = null }
  resizeObserver?.disconnect()
  // Cancel any in-flight spring animations.
  springCancels.forEach(cancel => cancel())
  springCancels.clear()
  clearInterval(settleIntervalId)
  // Release table state accumulated from rendered markdown tables.
  window.__tableState = {}
})

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

const ICON_COPY  = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>'
const ICON_REGEN = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 .49-3.5"/></svg>'
const ICON_SPEAK = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/></svg>'
const ICON_STOP_SPEAK = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/></svg>'

/** unlockScroll restores the messages container scroll after the context menu closes. */
function unlockScroll() {
  if (messagesEl.value) messagesEl.value.style.overflow = ''
}

/** onBubbleContextMenu shows the per-message right-click menu. */
function onBubbleContextMenu(e, i) {
  const m = messages.value[i]
  if (!m || m.streaming || m.thinking) return
  e.preventDefault()
  e.stopPropagation()
  if (messagesEl.value) messagesEl.value.style.overflow = 'hidden'
  const items = []
  if (m.content) {
    items.push({
      iconSvg: ICON_COPY,
      label: t('chat.copy'),
      action: () => navigator.clipboard.writeText(m.content),
    })
  }
  if (m.role === 'assistant' && m.content) {
    if (items.length) items.push({ divider: true })
    const isSpeaking = activeTTSMsgId.value === i
    items.push({
      iconSvg: isSpeaking ? ICON_STOP_SPEAK : ICON_SPEAK,
      label: isSpeaking ? t('chat.stopSpeak') : t('chat.speak'),
      action: () => speakMessage(i),
    })
    const lastAssistantIdx = messages.value.reduce((last, msg, idx) =>
      msg.role === 'assistant' && !msg.streaming && !msg.thinking ? idx : last, -1)
    if (i === lastAssistantIdx && !loading.value) {
      items.push({
        iconSvg: ICON_REGEN,
        label: t('chat.regenerate'),
        action: () => regenLastReply(i),
      })
    }
  }
  if (!items.length) return
  msgMenuItems.value = items
  msgMenuRef.value?.show(e.clientX, e.clientY)
}

const ICON_PASTE = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1" ry="1"/></svg>'
const ICON_CUT = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="20" r="2"/><circle cx="6" cy="4" r="2"/><line x1="6" y1="6" x2="6" y2="18"/><line x1="6" y1="12" x2="21" y2="3"/><line x1="6" y1="12" x2="21" y2="21"/></svg>'

/** onTextareaContextMenu shows paste-only or copy/paste/cut menu depending on selection. */
function onTextareaContextMenu(e) {
  e.preventDefault()
  e.stopPropagation()
  const el = textareaEl.value
  if (!el) return
  const hasSelection = el.selectionStart !== el.selectionEnd
  const items = []
  if (hasSelection) {
    items.push({
      iconSvg: ICON_COPY,
      label: t('chat.copy'),
      action: () => navigator.clipboard.writeText(el.value.slice(el.selectionStart, el.selectionEnd)),
    })
  }
  items.push({
    iconSvg: ICON_PASTE,
    label: t('chat.paste'),
    action: async () => {
      const text = await ReadClipboard().catch(() => '')
      if (!text) return
      const start = el.selectionStart
      const end = el.selectionEnd
      el.value = el.value.slice(0, start) + text + el.value.slice(end)
      el.selectionStart = el.selectionEnd = start + text.length
      autoResize()
    },
  })
  if (hasSelection) {
    items.push({
      iconSvg: ICON_CUT,
      label: t('chat.cut'),
      action: () => {
        navigator.clipboard.writeText(el.value.slice(el.selectionStart, el.selectionEnd))
        const start = el.selectionStart
        el.value = el.value.slice(0, start) + el.value.slice(el.selectionEnd)
        el.selectionStart = el.selectionEnd = start
        autoResize()
      },
    })
  }
  inputMenuItems.value = items
  inputMenuRef.value?.show(e.clientX, e.clientY)
}

/** onMilkdownContextMenu shows copy/paste/cut menu for the Milkdown editor, mirroring textarea behavior. */
function onMilkdownContextMenu(e) {
  e.preventDefault()
  e.stopPropagation()
  if (!milkdownInstance) return
  const selectedText = milkdownInstance.editor.action((ctx) => {
    const view = ctx.get(editorViewCtx)
    const { state } = view
    const { from, to } = state.selection
    return from === to ? '' : state.doc.textBetween(from, to, '\n')
  })
  const hasSelection = !!selectedText
  const items = []
  if (hasSelection) {
    items.push({
      iconSvg: ICON_COPY,
      label: t('chat.copy'),
      action: () => navigator.clipboard.writeText(selectedText),
    })
  }
  items.push({
    iconSvg: ICON_PASTE,
    label: t('chat.paste'),
    action: async () => {
      const text = await ReadClipboard().catch(() => '')
      if (!text) return
      milkdownInstance?.editor.action((ctx) => {
        const view = ctx.get(editorViewCtx)
        const { state, dispatch } = view
        dispatch(state.tr.replaceSelectionWith(state.schema.text(text)))
      })
    },
  })
  if (hasSelection) {
    items.push({
      iconSvg: ICON_CUT,
      label: t('chat.cut'),
      action: () => {
        navigator.clipboard.writeText(selectedText)
        milkdownInstance?.editor.action((ctx) => {
          const view = ctx.get(editorViewCtx)
          const { state, dispatch } = view
          dispatch(state.tr.deleteSelection())
        })
      },
    })
  }
  inputMenuItems.value = items
  inputMenuRef.value?.show(e.clientX, e.clientY)
}

/** regenLastReply removes the last user+assistant bubbles, re-appends the user bubble, then re-requests. */
async function regenLastReply(assistantIdx) {
  if (loading.value) return
  // Find and remove the preceding user bubble, saving its data to re-append below.
  const userIdx = messages.value.slice(0, assistantIdx).reduce(
    (last, m, idx) => m.role === 'user' ? idx : last, -1)
  const userMsg = userIdx >= 0 ? { ...messages.value[userIdx] } : null
  messages.value.splice(assistantIdx, 1)
  if (userIdx >= 0) messages.value.splice(userIdx, 1)
  if (userMsg) messages.value.push(userMsg)

  loading.value = true
  isStreaming.value = true
  firstTokenThisTurn = true
  messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, thinkingContent: '', thinkingExpanded: false })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  try {
    await RegenerateLastReply()
  } catch (e) {
    const thinkIdx = messages.value.findLastIndex(m => m.thinking)
    if (thinkIdx >= 0) messages.value.splice(thinkIdx, 1)
    messages.value.push({ role: 'system', content: t('chat.system.regenerateFailed', { error: e }) })
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
  SpeakText(stripToolCallTags(m.content)).catch(() => { activeTTSMsgId.value = null })
}

/** onPaste handles clipboard paste events on the textarea.
 *  If the clipboard contains an image, it is captured as a data URL and
 *  added to pendingImages for preview; the default paste action is suppressed. */
function onPaste(e) {
  if (!cfg.value?.SupportsVision) return
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
    messages.value.push({ role: 'system', content: t('chat.system.fileTooBig', { name: file.name }) })
    return
  }
  const mime = file.type || 'text/plain'
  if (!isReadableMime(mime)) {
    messages.value.push({ role: 'system', content: t('chat.system.fileUnsupportedType', { name: file.name }) })
    return
  }
  const reader = new FileReader()
  reader.onload = (ev) => {
    pendingFiles.value.push({ name: file.name, mimeType: mime, content: ev.target.result })
  }
  reader.onerror = () => {
    messages.value.push({ role: 'system', content: t('chat.system.fileReadError', { name: file.name }) })
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

/** onEnterKey sends the message on ⌘+Enter, but ignores Enter presses that
 * commit an IME composition (Chinese / Japanese / Korean input). Without
 * this guard, the Enter that closes the IME candidate panel would also
 * send a half-composed message. */
function onEnterKey(e) {
  if (e.isComposing || e.keyCode === 229) return
  e.preventDefault()
  send()
}

/** Cycles thinking level through available levels and persists. */
async function cycleThinkingLevel() {
  const levels = thinkingLevels.value
  const idx = levels.indexOf(thinkingLevel.value)
  thinkingLevel.value = levels[(idx + 1) % levels.length]
  thinkingChipFired.value = false
  await nextTick()
  thinkingChipFired.value = true
  setTimeout(() => { thinkingChipFired.value = false }, 450)
  await persistChatOptions()
}

/** Toggles knowledge base flag and persists. */
async function toggleKnowledge() {
  useKnowledge.value = !useKnowledge.value
  knowledgeChipFired.value = false
  await nextTick()
  knowledgeChipFired.value = true
  setTimeout(() => { knowledgeChipFired.value = false }, 450)
  await persistChatOptions()
}

/** Toggles long-term memory flag and persists. */
async function toggleMemory() {
  useMemory.value = !useMemory.value
  memoryChipFired.value = false
  await nextTick()
  memoryChipFired.value = true
  setTimeout(() => { memoryChipFired.value = false }, 450)
  await persistChatOptions()
}

/** Saves the three chat option fields to config. */
async function persistChatOptions() {
  try {
    await SaveConfig({ ...cfg.value, ThinkingLevel: thinkingLevel.value, UseKnowledge: useKnowledge.value, UseMemory: useMemory.value })
  } catch (e) {
    console.error('persistChatOptions failed', e)
  }
}

/** send submits the current input as a user message. */
async function send() {
  const text = markdownMode.value
    ? (milkdownInstance?.getMarkdown().trim() ?? '')
    : getInput().trim()
  if ((!text && pendingImages.value.length === 0 && pendingFiles.value.length === 0) || loading.value) return
  if (markdownMode.value) {
    await destroyMilkdown()
  } else {
    setInputDOM('')
    resetTextareaHeight()
  }
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
  messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true, thinkingContent: '', thinkingExpanded: false })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  const chatOpts = { thinkingLevel: thinkingLevel.value, useKnowledge: useKnowledge.value, useMemory: useMemory.value }
  try {
    if (imgs.length > 0 || fileAttachments.length > 0) {
      await SendMessageWithFiles(text, imgs, fileAttachments, chatOpts)
    } else {
      await SendMessage(text, chatOpts)
    }
  } catch (e) {
    const idx = messages.value.findLastIndex(m => m.thinking)
    if (idx >= 0) messages.value.splice(idx, 1)
    messages.value.push({ role: 'system', content: t('chat.system.sendFailed', { error: e }) })
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
/** isAtBottom tracks whether the messages list is scrolled to (or near) the bottom. */
const isAtBottom = ref(true)

/** onMessagesScroll updates isAtBottom; throttled to avoid per-pixel overhead. */
const onMessagesScroll = throttle(() => {
  const el = messagesEl.value
  if (!el) return
  isAtBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < 60
}, 100)

let _scrollRafPending = false
/** scrollToBottom scrolls to the latest message; coalesced via rAF to avoid redundant layout reads. */
function scrollToBottom() {
  if (_scrollRafPending) return
  _scrollRafPending = true
  requestAnimationFrame(() => {
    _scrollRafPending = false
    if (messagesEl.value) {
      messagesEl.value.scrollTop = messagesEl.value.scrollHeight
      isAtBottom.value = true
    }
  })
}

let _smoothScrollRaf = null
/** smoothScrollToBottom animates a 1-second ease-in-out scroll to the bottom; used by the floating button. */
function smoothScrollToBottom() {
  const el = messagesEl.value
  if (!el) return
  if (_smoothScrollRaf) { cancelAnimationFrame(_smoothScrollRaf); _smoothScrollRaf = null }
  const start = el.scrollTop
  const end = el.scrollHeight - el.clientHeight
  if (end - start < 2) { isAtBottom.value = true; return }
  const duration = 1000
  const t0 = performance.now()
  function step(now) {
    const p = Math.min((now - t0) / duration, 1)
    // ease-in-out cubic
    const e = p < 0.5 ? 4 * p * p * p : 1 - (-2 * p + 2) ** 3 / 2
    el.scrollTop = start + (end - start) * e
    if (p < 1) {
      _smoothScrollRaf = requestAnimationFrame(step)
    } else {
      _smoothScrollRaf = null
      isAtBottom.value = true
    }
  }
  _smoothScrollRaf = requestAnimationFrame(step)
}

let _thinkingScrollRafPending = false
/** scrollThinkingToBottom scrolls the expanded thinking block body to its bottom; waits for Vue DOM flush then rAF. */
function scrollThinkingToBottom() {
  if (_thinkingScrollRafPending) return
  _thinkingScrollRafPending = true
  nextTick(() => {
    requestAnimationFrame(() => {
      _thinkingScrollRafPending = false
      const body = messagesEl.value?.querySelector('.thinking-block-body.expanded')
      if (body) body.scrollTop = body.scrollHeight
    })
  })
}

/** focusInput focuses the textarea input. */
function focusInput() {
  nextTick(() => { textareaEl.value?.focus() })
}

/** insertNewline inserts a newline at the current cursor position (Enter). */
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
  if (markdownMode.value) return
  const el = textareaEl.value
  if (el) el.style.height = 'auto'
}

/** initMilkdown creates a Crepe instance mounted on milkdownEl. */
async function initMilkdown(initialContent = '') {
  if (!milkdownEl.value || milkdownInstance) return
  milkdownInstance = new Crepe({
    root: milkdownEl.value,
    defaultValue: initialContent,
    features: {
      [Crepe.Feature.Toolbar]: false,
      [Crepe.Feature.BlockEdit]: false,
      [Crepe.Feature.ImageBlock]: false,
      [Crepe.Feature.Latex]: false,
      [Crepe.Feature.TopBar]: false,
      [Crepe.Feature.AI]: false,
      [Crepe.Feature.Cursor]: true,
      [Crepe.Feature.ListItem]: true,
      [Crepe.Feature.LinkTooltip]: true,
      [Crepe.Feature.Placeholder]: true,
      [Crepe.Feature.CodeMirror]: true,
      [Crepe.Feature.Table]: true,
    },
    featureConfigs: {
      [Crepe.Feature.Placeholder]: {
        text: t('chat.placeholder'),
        mode: 'doc',
      },
    },
  })
  milkdownInstance.on((api) => {
    api.markdownUpdated((ctx, md) => {
      const empty = !md.trim()
      if (inputEmpty.value !== empty) inputEmpty.value = empty
    })
  })
  await milkdownInstance.create()
  milkdownEditorDom = milkdownEl.value?.querySelector('.ProseMirror') ?? null
  if (milkdownEditorDom) {
    milkdownEditorDom.setAttribute('spellcheck', 'false')
    milkdownEditorDom.setAttribute('autocorrect', 'off')
    milkdownEditorDom.setAttribute('autocomplete', 'off')
    milkdownEditorDom.setAttribute('autocapitalize', 'off')
    milkdownEditorDom.setAttribute('writingsuggestions', 'false')
    milkdownEditorDom.addEventListener('keydown', onMilkdownKeydown)
    milkdownEditorDom.focus()
  }
}

/** onMilkdownKeydown intercepts Cmd+Enter to send the message. */
function onMilkdownKeydown(e) {
  if (!e.isComposing && e.keyCode !== 229 && e.key === 'Enter' && e.metaKey) {
    e.preventDefault()
    e.stopPropagation()
    send()
  }
}

/** destroyMilkdown tears down the Crepe instance and resets state. Returns the current markdown content. */
async function destroyMilkdown() {
  const md = milkdownInstance?.getMarkdown() ?? ''
  milkdownEditorDom?.removeEventListener('keydown', onMilkdownKeydown)
  milkdownEditorDom = null
  const inst = milkdownInstance
  milkdownInstance = null
  markdownMode.value = false
  inputEmpty.value = true
  await inst?.destroy()
  return md
}

/** toggleMarkdownMode switches between the textarea and Milkdown editor, carrying content across. */
async function toggleMarkdownMode() {
  if (markdownMode.value) {
    const md = await destroyMilkdown()
    await nextTick()
    if (md.trim()) setInputDOM(md)
    autoResize()
    textareaEl.value?.focus()
  } else {
    const text = getInput()
    setInputDOM('')
    markdownMode.value = true
    await nextTick()
    await initMilkdown(text)
  }
}

defineExpose({ enterSearch, focusInput, scrollToBottom })
</script>

<template>
  <div class="chat-panel" ref="chatPanelEl" @mousemove="onChatPanelMousemove" :style="{ '--code-max-width': codeMaxWidth > 0 ? codeMaxWidth + 'px' : 'none' }">
    <!-- Search bar -->
    <div v-if="isSearching" class="search-bar" role="search" :aria-label="$t('chat.searchPlaceholder')">
      <div class="search-input-wrap">
        <svg class="search-input-icon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
        </svg>
        <input
          class="search-input"
          type="search"
          :placeholder="$t('chat.searchPlaceholder')"
          :aria-label="$t('chat.searchPlaceholder')"
          :value="searchQuery"
          spellcheck="false"
          autocorrect="off"
          autocomplete="off"
          autocapitalize="off"
          writingsuggestions="false"
          @input="onSearchInput"
          @keydown="onSearchKeydown"
          ref="searchInputEl"
        />
        <span v-if="searchResults" class="search-count" aria-live="polite">{{ searchResults.length }} {{ $t('chat.searchMatches') }}</span>
        <button class="search-close-btn" @click="exitSearch" :aria-label="$t('chat.searchClose')">
          <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
    </div>
    <div class="chat-spotlight" ref="spotlightEl" aria-hidden="true" />
    <div class="messages" ref="messagesEl" @click="onMessagesClick" @scroll="onMessagesScroll">
      <!-- Lazy-load sentinel: entering viewport triggers loading older messages -->
      <div id="msg-load-sentinel" class="load-sentinel">
        <div v-if="loadingHistory" class="history-loading">
          <span class="h-dot" /><span class="h-dot" /><span class="h-dot" />
        </div>
        <span v-else-if="!allLoaded" class="load-sentinel-dot" />
      </div>
      <div v-if="isSearching && searchResults && searchResults.length === 0" class="search-empty">
        <div class="search-empty-title">{{ $t('chat.searchNoResults') }}</div>
        <div class="search-empty-hint">{{ $t('chat.searchNoResultsHint') }}</div>
      </div>
      <TransitionGroup name="msg-slide" tag="div" class="messages-inner" :class="{ 'suppress-anim': suppressAnimation }">
      <div v-for="(m, i) in displayMessages" :key="msgKey(m, i)" :class="['msg', m.role, { 'is-info': m.isInfo, 'search-dimmed': searchMatchIds && !searchMatchIds.has(m.id) && !m.isInfo, 'search-result-selected': isSearching && selectedResultIndex === i }]" :data-msg-key="msgKey(m, i)" @click="isSearching && searchMatchIds && searchMatchIds.has(m.id) && jumpToMessage(m.id)">
        <img v-if="m.role === 'assistant'" class="msg-avatar" :src="aiAvatar || '/logo.png'" alt="AI" draggable="false" />
        <div class="bubble-wrap" :class="{ ghost: m.ghost, 'has-recollapse': isEverCollapsed(m, i) && !isCollapsed(m, i) }">
          <!-- Collapsible wrapper -->
          <div
            class="bubble-collapse-wrap"
            :class="{ 'is-collapsed': isCollapsed(m, i) }"
            :data-msg-key="msgKey(m, i)"
          >
            <div class="bubble-row" :class="{ 'is-collapsed': isCollapsed(m, i) }"
              @contextmenu="onBubbleContextMenu($event, i)">
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
                <div v-if="m.content" v-html="isSearching ? highlightMatches(renderMarkdown(m.content), searchQuery) : renderMarkdown(m.content)"></div>
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
                      {{ lpExpanded[msgKey(m, i)] ? $t('chat.collapseLinks') : $t('chat.expandLinks', { n: extractUrls(m.content).length - 1 }) }}
                    </button>
                  </template>
                </template>
              </div>
              <template v-else>
                <div v-if="!m.thinkingContent && (m.thinking || (m.streaming && !renderMarkdown(m.content)))" :class="['bubble', 'thinking-bubble', { proactive: m.isProactive, streaming: m.streaming, 'streaming-fading': streamingFading.has(msgKey(m, i)) }]">
                  <span class="dot" /><span class="dot" /><span class="dot" />
                </div>
                <div v-if="!m.thinking || m.content || m.thinkingContent" :class="['bubble', 'markdown', { proactive: m.isProactive, streaming: m.streaming, 'streaming-fading': streamingFading.has(msgKey(m, i)) }]">
                  <!-- ThinkingBlock: inside the bubble, at the top -->
                  <div v-if="m.thinkingContent" :class="['thinking-block', { 'thinking-streaming': m.streaming && !m.content, expanded: m.thinkingExpanded }]">
                    <div class="thinking-block-header" @click="toggleThinkingExpanded(i)">
                      <div class="thinking-icon">
                        <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z"/><path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z"/><path d="M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4"/><path d="M17.599 6.5a3 3 0 0 0 .399-1.375"/><path d="M6.003 5.125A3 3 0 0 0 6.401 6.5"/><path d="M3.477 10.896a4 4 0 0 1 .585-.396"/><path d="M19.938 10.5a4 4 0 0 1 .585.396"/><path d="M6 18a4 4 0 0 1-1.967-.516"/><path d="M19.967 17.484A4 4 0 0 1 18 18"/></svg>
                      </div>
                      <span class="thinking-label">{{ $t('chat.thinkingProcess') }}</span>
                      <span v-if="m.streaming && !m.content" class="thinking-streaming-badge">
                        <span class="thinking-dot" /><span class="thinking-dot" /><span class="thinking-dot" />
                      </span>
                      <div class="thinking-toggle-icon" :class="{ expanded: m.thinkingExpanded }">
                        <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                      </div>
                    </div>
                    <div class="thinking-block-body" :class="{ expanded: m.thinkingExpanded }">
                      <div class="thinking-block-text">{{ m.thinkingContent }}<span v-if="m.streaming && m.thinkingExpanded" class="stream-cursor">▋</span></div>
                    </div>
                  </div>
                  <div v-if="m.thinkingContent && (m.content || m.streaming)" class="thinking-divider" />
                  <div v-if="m.displayHtml || (!m.streaming && m.content)" v-html="m.displayHtml || (isSearching ? highlightMatches(renderMarkdown(m.content), searchQuery) : renderMarkdown(m.content))" />
                  <template v-if="m.pendingTokens && m.pendingTokens.length">
                    <span v-for="tok in m.pendingTokens" :key="tok.key" class="token-word">{{ tok.text }}</span>
                  </template>
                  <span v-if="m.streaming" class="stream-cursor">▋</span>
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
                        {{ lpExpanded[msgKey(m, i)] ? $t('chat.collapseLinks') : $t('chat.expandLinks', { n: extractUrls(m.content).length - 1 }) }}
                      </button>
                    </template>
                  </template>
                  <div v-if="m.images && m.images.length > 0" class="msg-images">
                    <img v-for="(img, imgIdx) in m.images" :key="imgIdx" :src="img" class="msg-img" @click.stop="previewImage(img)" />
                  </div>
                </div>
              </template>

              <!-- Collapse fade overlay + expand button (inside bubble-row so width matches bubble) -->
              <Transition name="coll-fade">
                <div v-if="isCollapsed(m, i)" class="collapse-fade" :class="m.role" @click.stop="toggleExpand(m, i)">
                  <button class="collapse-btn" @click.stop="toggleExpand(m, i)">
                    <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                    {{ $t('chat.expand') }}
                  </button>
                </div>
              </Transition>
            </div>
            <!-- Action buttons: sibling of bubble-row, inside bubble-collapse-wrap so right:100% anchors to bubble width -->
            <div
              v-if="!m.streaming && !m.thinking"
              :class="['msg-actions', m.role]"
            >
              <button
                class="msg-action-btn"
                @click="copyMessage(i)"
                :title="copiedIdx === i ? $t('chat.copied') : $t('chat.copy')"
              >
                <svg v-if="copiedIdx !== i" xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              </button>
              <button
                v-if="m.role === 'assistant'"
                class="msg-action-btn"
                :title="activeTTSMsgId === i ? $t('chat.stopSpeak') : $t('chat.speak')"
                @click="speakMessage(i)"
              >
                <svg v-if="activeTTSMsgId !== i" xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/></svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>
              </button>
            </div>
          </div>

          <div v-if="(m.time && !m.streaming && !m.thinking) || (isEverCollapsed(m, i) && !isCollapsed(m, i))" class="msg-meta-row">
            <!-- user: recollapse left of timestamp; assistant: recollapse right of timestamp -->
            <button v-if="m.role === 'user' && isEverCollapsed(m, i) && !isCollapsed(m, i)" class="recollapse-btn" @click.stop="toggleExpand(m, i)">
              <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="18 15 12 9 6 15"/></svg>
              {{ $t('chat.collapse') }}
            </button>
            <span v-if="m.time && !m.streaming && !m.thinking" class="msg-time">{{ formatTime(m.time) }}</span>
            <button v-if="m.role !== 'user' && isEverCollapsed(m, i) && !isCollapsed(m, i)" class="recollapse-btn" @click.stop="toggleExpand(m, i)">
              <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="18 15 12 9 6 15"/></svg>
              {{ $t('chat.collapse') }}
            </button>
          </div>
        </div>
        <div v-if="m.role === 'user' && !userAvatar" class="msg-avatar user-avatar" aria-hidden="true">
          <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.7 0 4.8-2.1 4.8-4.8S14.7 2.4 12 2.4 7.2 4.5 7.2 7.2 9.3 12 12 12zm0 2.4c-3.2 0-9.6 1.6-9.6 4.8v2.4h19.2v-2.4c0-3.2-6.4-4.8-9.6-4.8z"/></svg>
        </div>
        <img v-else-if="m.role === 'user' && userAvatar" class="msg-avatar" :src="userAvatar" :alt="$t('chat.userAlt')" draggable="false" />
      </div>
      </TransitionGroup>
      <!-- Scroll-to-bottom floating button -->
      <Transition name="scroll-btn">
        <button v-if="!isAtBottom" class="scroll-to-bottom-btn" :aria-label="$t('chat.scrollToBottomAria')" @click="smoothScrollToBottom">
          <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
        </button>
      </Transition>
    </div>
    <!-- Image lightbox (normal: inside panel; fullscreen: teleported to body to escape backdrop-filter stacking context) -->
    <template v-if="lightboxSrc">
      <Teleport to="body" :disabled="!lightboxFullscreen">
        <div
          :class="['lightbox', { 'lightbox-fullscreen': lightboxFullscreen }]"
          @click="onLightboxBgClick"
          @wheel.prevent="onLightboxWheel"
          @mousemove="onLbMousemove"
          @mouseup="onLbMouseup"
          @mouseleave="onLbMouseup"
        >
          <img
            :src="lightboxSrc"
            class="lightbox-img"
            :style="{
              transform: `translate(${lightboxPan.x}px, ${lightboxPan.y}px) scale(${lightboxZoom})`,
              cursor: lightboxZoom > 1 ? (lbDragging ? 'grabbing' : 'grab') : 'default',
              transition: lbDragging ? 'none' : 'transform 0.18s cubic-bezier(0.16,1,0.3,1)',
            }"
            draggable="false"
            @click.stop
            @mousedown="onLbImgMousedown"
            @dblclick="lbReset"
          />
          <!-- Toolbar: zoom controls + fullscreen -->
          <div class="lightbox-toolbar" @click.stop>
            <button class="lb-btn" @click="lbZoomOut" :title="$t('chat.lightbox.zoomOut')">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="5" y1="12" x2="19" y2="12"/></svg>
            </button>
            <span class="lb-zoom-label">{{ Math.round(lightboxZoom * 100) }}%</span>
            <button class="lb-btn" @click="lbZoomIn" :title="$t('chat.lightbox.zoomIn')">
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
            </button>
            <div class="lb-sep" />
            <button class="lb-btn" @click="lbReset" :title="$t('chat.lightbox.reset')">
              <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
            </button>
            <div class="lb-sep" />
            <button class="lb-btn" @click="lightboxFullscreen = !lightboxFullscreen" :title="lightboxFullscreen ? $t('chatBubble.exitFullscreen') : $t('chatBubble.fullscreen')">
              <svg v-if="!lightboxFullscreen" xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>
              <svg v-else xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="10" y1="14" x2="3" y2="21"/><line x1="21" y1="3" x2="14" y2="10"/></svg>
            </button>
          </div>
        </div>
      </Teleport>
    </template>

    <!-- Tool execution confirmation modal -->
    <ToolConfirmModal />

    <!-- Per-message right-click context menu -->
    <ContextMenu ref="msgMenuRef" :items="msgMenuItems" @close="unlockScroll" />
    <!-- Textarea right-click context menu -->
    <ContextMenu ref="inputMenuRef" :items="inputMenuItems" />

    <!-- Clear chat confirmation dialog -->
    <Transition name="confirm-pop">
    <div v-if="showClearConfirm" class="clear-confirm-overlay" role="dialog" aria-modal="true" aria-labelledby="clear-confirm-title">
      <div class="clear-confirm-backdrop" @click="showClearConfirm = false" />
      <div class="clear-confirm-box">
        <p id="clear-confirm-title" class="clear-confirm-title">{{ $t('chat.clearHistoryTitle') }}</p>
        <p class="clear-confirm-text">{{ $t('chat.clearConfirm') }}</p>
        <div class="clear-confirm-actions">
          <button class="clear-confirm-cancel" @click="showClearConfirm = false">{{ $t('chat.clearConfirmCancel') }}</button>
          <button class="clear-confirm-ok" @click="confirmClearHistory">{{ $t('chat.clearConfirmOk') }}</button>
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
            <span id="tbl-detail-title" class="tbl-detail-title">{{ $t('chat.rowDetail') }}</span>
            <button class="tbl-detail-close" :aria-label="$t('chatBubble.close')" @click="tableDetailRow = null">
              <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="tbl-detail-body">
            <div v-for="pair in tableDetailRow" :key="pair.key" class="tbl-detail-pair">
              <span class="tbl-detail-key">{{ pair.key }}</span>
              <span class="tbl-detail-value markdown" v-html="renderMarkdown(pair.value)"></span>
              <button class="tbl-detail-pair-copy" :class="{ copied: copiedPairKey === pair.key }" :aria-label="$t('chat.copy') + ' ' + pair.key" @click.stop="copyPairValue(pair)">
                <svg v-if="copiedPairKey !== pair.key" xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              </button>
            </div>
          </div>
        </div>
      </div>
    </Transition>

    <!-- Tool args modal (same style as table row detail) -->
    <Transition name="tbl-detail-pop">
      <div v-if="toolArgsPopover" class="tbl-detail-overlay" role="dialog" aria-modal="true" aria-labelledby="tool-args-title">
        <div class="tbl-detail-backdrop" @click="toolArgsPopover = null" />
        <div class="tbl-detail-box">
          <div class="tbl-detail-header">
            <span id="tool-args-title" class="tbl-detail-title">{{ $t('chat.toolArgs') }}</span>
            <button class="tbl-detail-close" :aria-label="$t('chatBubble.close')" @click="toolArgsPopover = null">
              <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="tbl-detail-body">
            <div v-for="pair in toolArgsPopover.pairs" :key="pair.key" class="tbl-detail-pair">
              <span class="tbl-detail-key">{{ pair.key }}</span>
              <span class="tbl-detail-value markdown" v-html="renderMarkdown(pair.value)"></span>
              <button class="tbl-detail-pair-copy" :class="{ copied: copiedPairKey === pair.key }" :aria-label="$t('chat.copy') + ' ' + pair.key" @click.stop="copyPairValue(pair)">
                <svg v-if="copiedPairKey !== pair.key" xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              </button>
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
          {{ voiceHint ? `"${voiceHint}"` : $t('chat.listening') }}
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
        <img :src="img" class="pending-img" :alt="$t('chat.pendingImage', { n: idx + 1 })" />
        <button class="pending-img-remove" :aria-label="$t('chat.removeImage')" @click="removeImage(idx)">
          <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>
    </div>
    <!-- Pending file chips shown above the input row -->
    <div v-if="pendingFiles.length > 0" class="pending-files">
      <div v-for="(f, idx) in pendingFiles" :key="idx" class="pending-file-chip" :title="f.name">
        <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
        <span class="pending-file-name">{{ f.name }}</span>
        <button class="pending-file-remove" :aria-label="$t('chat.removeFile')" @click="removeFile(idx)">
          <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>
    </div>
    <Transition name="update-bar">
      <div v-if="isUpdating" class="update-progress-bar-wrap">
        <div
          class="update-progress-bar"
          role="progressbar"
          :aria-valuenow="updateProgress"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-label="$t('chat.updateProgress', { pct: updateProgress })"
        >
          <div class="update-progress-fill" :style="{ width: updateProgress + '%' }"></div>
        </div>
        <span class="update-progress-msg">{{ updateProgressMsg || $t('chat.preparing') }}（{{ updateProgress }}%）</span>
      </div>
    </Transition>

    <div class="input-area" :class="{ 'md-mode': markdownMode }">
      <input
        ref="fileInputEl"
        type="file"
        multiple
        style="display:none"
        @change="onFileInputChange"
      />
      <textarea
        v-show="!markdownMode"
        ref="textareaEl"
        :placeholder="$t('chat.placeholder')"
        rows="1"
        spellcheck="false"
        autocorrect="off"
        autocomplete="off"
        autocapitalize="off"
        writingsuggestions="false"
        @input="autoResize"
        @keydown.meta.enter.prevent="onEnterKey"
        @keydown.enter.exact.prevent="insertNewline"
        @paste="onPaste"
        @contextmenu.prevent="onTextareaContextMenu"
        :disabled="loading"
      />
      <div
        v-show="markdownMode"
        ref="milkdownEl"
        class="milkdown-wrap"
        @contextmenu.prevent="onMilkdownContextMenu"
      />
      <div class="input-toolbar" @contextmenu.stop.prevent>
        <div class="chat-opts-chips">
          <button
            class="chat-opt-chip"
            :class="['thinking-' + thinkingLevel]"
            :disabled="loading"
            @click="cycleThinkingLevel"
            :title="$t('chat.thinkingLevel')"
          ><span class="chip-icon" :class="{ 'chip-icon--fired': thinkingChipFired }" v-html="ICON_THINKING"></span><span class="chip-label" :class="{ 'chip-label--fired': thinkingChipFired }">{{ thinkingLevelLabel }}</span></button>
          <button
            v-if="cfg?.EmbeddingModel"
            class="chat-opt-chip"
            :class="{ active: useKnowledge }"
            :aria-pressed="useKnowledge"
            :disabled="loading"
            @click="toggleKnowledge"
            :title="$t('chat.useKnowledge')"
          ><span class="chip-icon" :class="{ 'chip-icon--fired': knowledgeChipFired }" v-html="ICON_KNOWLEDGE"></span><span class="chip-label" :class="{ 'chip-label--fired': knowledgeChipFired }">{{ $t('chat.knowledgeChip') }}</span></button>
          <button
            v-if="cfg?.EmbeddingModel"
            class="chat-opt-chip"
            :class="{ active: useMemory }"
            :aria-pressed="useMemory"
            :disabled="loading"
            @click="toggleMemory"
            :title="$t('chat.useMemory')"
          ><span class="chip-icon" :class="{ 'chip-icon--fired': memoryChipFired }" v-html="ICON_MEMORY"></span><span class="chip-label" :class="{ 'chip-label--fired': memoryChipFired }">{{ $t('chat.memoryChip') }}</span></button>
        </div>
        <button
          class="md-btn"
          :class="{ active: markdownMode }"
          :title="$t('chat.markdownMode')"
          :aria-label="$t('chat.markdownMode')"
          :disabled="loading"
          @click="toggleMarkdownMode"
        >M↓</button>
        <button
          class="attach-btn"
          :title="$t('chat.attachFile')"
          :aria-label="$t('chat.attachFile')"
          :disabled="loading"
          @click="fileInputEl.click()"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>
        </button>
        <button v-if="isStreaming" class="stop-btn" :aria-label="$t('chat.stopAriaLabel')" @click="stopGeneration">⏹ {{ $t('chat.stop') }}</button>
        <button v-else class="send-btn" :title="$t('chat.sendTitle')" :aria-label="$t('chat.sendAriaLabel')" @click="send" :disabled="loading || (inputEmpty && pendingImages.length === 0 && pendingFiles.length === 0)">
          <svg xmlns="http://www.w3.org/2000/svg" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
        </button>
      </div>
    </div>
    <div class="input-hint">{{ $t('chat.sendHint') }}</div>
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

/* Spotlight glow: a fixed-size orb moved via transform (GPU-composited, no repaint). */
.chat-spotlight {
  pointer-events: none;
  position: absolute;
  top: 0;
  left: 0;
  width: 520px;
  height: 520px;
  border-radius: 50%;
  z-index: 0;
  background: radial-gradient(circle, rgba(70, 140, 255, 0.07) 0%, transparent 70%);
  will-change: transform;
  transform: translate(-260px, -260px);
  transition: opacity 0.35s ease;
  opacity: 0;
}
.chat-panel:hover .chat-spotlight {
  opacity: 1;
}
@media (prefers-reduced-motion: reduce) {
  .chat-spotlight { display: none; }
}

/* Messages list */
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 14px;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.1) transparent;
  position: relative;
  z-index: 1;
}

.messages-inner {
  display: flex;
  flex-direction: column;
  gap: 14px;
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

/* Scroll-to-bottom button */
.scroll-to-bottom-btn {
  position: sticky;
  bottom: 12px;
  float: right;
  margin-right: 10px;
  clear: both;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--lg-surface-elevated);
  border: 1px solid var(--lg-border-subtle);
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: var(--lg-blur-sm);
  -webkit-backdrop-filter: var(--lg-blur-sm);
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.3);
  transition: background 0.12s, color 0.12s, transform 0.1s cubic-bezier(0.34, 1.56, 0.64, 1);
  z-index: 5;
}
.scroll-to-bottom-btn:hover {
  background: var(--accent);
  border-color: transparent;
  color: #fff;
}
.scroll-to-bottom-btn:active { transform: scale(0.9); }
.scroll-btn-enter-active { transition: opacity 0.18s ease, transform 0.18s cubic-bezier(0.34, 1.56, 0.64, 1); }
.scroll-btn-leave-active { transition: opacity 0.14s ease, transform 0.14s ease; }
.scroll-btn-enter-from { opacity: 0; transform: scale(0.7); }
.scroll-btn-leave-to { opacity: 0; transform: scale(0.7); }

/* Lazy-load sentinel */
.load-sentinel { min-height: 0; display: flex; align-items: center; justify-content: center; }
.load-sentinel:has(> *) { height: 24px; }
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
.msg { display: flex; align-items: flex-start; }
.msg.user { justify-content: flex-end; gap: 8px; }
.msg.assistant { justify-content: flex-start; gap: 8px; }
/* system messages have no avatar — add left padding to align with assistant bubbles (28px avatar + 8px gap) */
.msg.system { justify-content: flex-start; padding-left: 36px; }

.msg-avatar {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  flex-shrink: 0;
  margin-top: 2px;
  user-select: none;
}
img.msg-avatar {
  object-fit: cover;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.35);
  -webkit-user-drag: none;
}
.user-avatar {
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.12);
  border: 1px solid rgba(255, 255, 255, 0.14);
  color: rgba(255, 255, 255, 0.6);
}

/* Wrap */
.bubble-wrap { max-width: 82%; min-width: 0; display: flex; flex-direction: column; position: relative; }
.msg.user .bubble-wrap { align-items: flex-end; max-width: 72%; }
.msg.user .bubble { word-break: break-word; overflow-wrap: anywhere; min-width: 0; max-width: 100%; }

/* Collapsible wrapper */
.bubble-collapse-wrap {
  position: relative;
  max-width: 100%;
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
/* Collapse-fade Transition: fade in when collapsed, fade out quickly when expanding. */
.coll-fade-enter-active { transition: opacity 0.22s var(--ease-enter) 0.1s; }
.coll-fade-leave-active { transition: opacity 0.12s var(--ease-exit); }
.coll-fade-enter-from, .coll-fade-leave-to { opacity: 0; }

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
  padding: 2px 8px;
  background: var(--lg-surface-input);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 5px;
  color: var(--text-tertiary);
  font-size: 11px;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
  font-family: inherit;
  box-shadow: none;
  line-height: 1;
}
.recollapse-btn:hover {
  color: var(--text-primary);
  background: var(--lg-surface-input-h);
  border-color: var(--lg-border);
}

/* Bubble row: relative container，按钮绝对定位不占空间 */
.bubble-row { position: relative; display: inline-flex; max-width: 100%; min-width: 0; }
.msg.user .bubble-row { justify-content: flex-end; }
.msg.assistant .bubble-row { justify-content: flex-start; flex-direction: column; align-items: flex-start; }

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

/* User bubble — solid accent */
.user .bubble {
  background: var(--accent);
  color: #fff;
  border-radius: 16px 16px 4px 16px;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.18),
    0 1px 3px rgba(0, 0, 0, 0.22),
    0 2px 8px rgba(0, 0, 0, 0.14);
  transition: transform 0.28s cubic-bezier(0.34, 1.3, 0.64, 1),
              box-shadow 0.28s cubic-bezier(0.34, 1.3, 0.64, 1);
}
.user .bubble.has-images {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.user .bubble-wrap:hover .bubble {
  transform: translateY(-1.5px);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.18),
    0 4px 18px rgba(0, 0, 0, 0.24),
    0 1px 4px rgba(0, 0, 0, 0.18);
}
@media (prefers-reduced-motion: reduce) {
  .user .bubble { transition: none; }
  .user .bubble-wrap:hover .bubble { transform: none; }
}

/* Assistant bubble — glass surface */
.assistant .bubble {
  background: var(--lg-surface);
  backdrop-filter: blur(12px) saturate(160%);
  -webkit-backdrop-filter: blur(12px) saturate(160%);
  color: var(--text-primary);
  border-radius: 16px 16px 16px 4px;
  border: 1px solid var(--lg-border-subtle);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08),
    0 2px 8px rgba(0, 0, 0, 0.18);
  transition: transform 0.28s cubic-bezier(0.34, 1.3, 0.64, 1),
              box-shadow 0.28s cubic-bezier(0.34, 1.3, 0.64, 1),
              border-color 0.2s ease;
}
.assistant .bubble-wrap:hover .bubble {
  transform: translateY(-1.5px);
  border-color: rgba(80, 150, 255, 0.28);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08),
    0 4px 18px rgba(0, 0, 0, 0.22),
    0 0 0 1px rgba(60, 130, 255, 0.12);
}
@media (prefers-reduced-motion: reduce) {
  .assistant .bubble { transition: none; }
  .assistant .bubble-wrap:hover .bubble { transform: none; }
}

/* Streaming shimmer border — rotating conic-gradient hollow border */
.assistant .bubble.streaming {
  position: relative;
  border-color: transparent;
}
.assistant .bubble.streaming::before {
  content: '';
  position: absolute;
  inset: -1px;
  border-radius: inherit;
  background: conic-gradient(
    from var(--shimmer-angle),
    rgba(80,  200, 255, 0.90)   0%,
    rgba(140, 100, 255, 0.85)  20%,
    rgba(240,  80, 200, 0.80)  40%,
    rgba(255, 160,  60, 0.80)  60%,
    rgba(80,  230, 180, 0.85)  80%,
    rgba(80,  200, 255, 0.90) 100%
  );
  -webkit-mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  mask-composite: exclude;
  padding: 1px;
  animation: shimmer-spin 2s linear infinite;
  pointer-events: none;
  z-index: 0;
}
.assistant .bubble.streaming > * { position: relative; z-index: 1; }
@media (prefers-reduced-motion: reduce) {
  .assistant .bubble.streaming::before { animation: none; }
  .assistant .bubble.streaming { border-color: var(--thinking-border-on); }
}

/* Shimmer fade-out — plays after streaming ends */
.assistant .bubble.streaming-fading {
  position: relative;
  border-color: transparent;
}
.assistant .bubble.streaming-fading::before {
  content: '';
  position: absolute;
  inset: -1px;
  border-radius: inherit;
  background: conic-gradient(
    from var(--shimmer-angle),
    rgba(80,  200, 255, 0.90)   0%,
    rgba(140, 100, 255, 0.85)  20%,
    rgba(240,  80, 200, 0.80)  40%,
    rgba(255, 160,  60, 0.80)  60%,
    rgba(80,  230, 180, 0.85)  80%,
    rgba(80,  200, 255, 0.90) 100%
  );
  -webkit-mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  mask-composite: exclude;
  padding: 1px;
  pointer-events: none;
  z-index: 0;
  animation: shimmer-fadeout 0.7s ease-out forwards;
}
.assistant .bubble.streaming-fading > * { position: relative; z-index: 1; }
@media (prefers-reduced-motion: reduce) {
  .assistant .bubble.streaming-fading::before { display: none; }
}

/* Thinking-bubble shimmer (same technique) */
.assistant .thinking-bubble.streaming {
  position: relative;
  border-color: transparent;
}
.assistant .thinking-bubble.streaming::before {
  content: '';
  position: absolute;
  inset: -1px;
  border-radius: inherit;
  background: conic-gradient(
    from var(--shimmer-angle),
    rgba(80,  200, 255, 0.90)   0%,
    rgba(140, 100, 255, 0.85)  20%,
    rgba(240,  80, 200, 0.80)  40%,
    rgba(255, 160,  60, 0.80)  60%,
    rgba(80,  230, 180, 0.85)  80%,
    rgba(80,  200, 255, 0.90) 100%
  );
  -webkit-mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  mask-composite: exclude;
  padding: 1px;
  animation: shimmer-spin 2s linear infinite;
  pointer-events: none;
  z-index: 0;
}
@media (prefers-reduced-motion: reduce) {
  .assistant .thinking-bubble.streaming::before { animation: none; }
  .assistant .thinking-bubble.streaming { border-color: var(--thinking-border-on); }
}
.assistant .thinking-bubble.streaming-fading {
  position: relative;
  border-color: transparent;
}
.assistant .thinking-bubble.streaming-fading::before {
  content: '';
  position: absolute;
  inset: -1px;
  border-radius: inherit;
  background: conic-gradient(
    from var(--shimmer-angle),
    rgba(80,  200, 255, 0.90)   0%,
    rgba(140, 100, 255, 0.85)  20%,
    rgba(240,  80, 200, 0.80)  40%,
    rgba(255, 160,  60, 0.80)  60%,
    rgba(80,  230, 180, 0.85)  80%,
    rgba(80,  200, 255, 0.90) 100%
  );
  -webkit-mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  mask-composite: exclude;
  padding: 1px;
  pointer-events: none;
  z-index: 0;
  animation: shimmer-fadeout 0.7s ease-out forwards;
}
@media (prefers-reduced-motion: reduce) {
  .assistant .thinking-bubble.streaming-fading::before { display: none; }
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
/* Info system bubble (e.g. app:restarting) — blue instead of red */
.system.is-info .bubble {
  background: var(--accent-alpha-08);
  color: var(--accent-hover);
  border-color: var(--accent-alpha-20);
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
  transition: background 0.15s var(--ease-enter), transform 0.15s var(--ease-spring), box-shadow 0.2s var(--ease-enter);
}
.stop-btn:hover {
  background: rgba(255, 69, 58, 0.22);
  box-shadow: 0 0 0 4px rgba(255, 69, 58, 0.12);
}
.stop-btn:active { transform: scale(0.94); }
.stop-btn:focus-visible { outline: 2px solid var(--danger); outline-offset: 2px; }

/* Per-token pop animation */
.token-word {
  display: inline;
  animation: token-word-appear 0.12s cubic-bezier(0.34, 1.4, 0.64, 1) both;
}

/* Rainbow pulse cursor — replaces old .cursor */
@keyframes cursor-color {
  0%   { color: rgba(80,  200, 255, 0.95); filter: drop-shadow(0 0 5px rgba(80,  200, 255, 0.65)); }
  25%  { color: rgba(160, 100, 255, 0.95); filter: drop-shadow(0 0 5px rgba(160, 100, 255, 0.60)); }
  50%  { color: rgba(240, 80,  200, 0.95); filter: drop-shadow(0 0 5px rgba(240, 80,  200, 0.60)); }
  75%  { color: rgba(255, 160, 60,  0.95); filter: drop-shadow(0 0 5px rgba(255, 160, 60,  0.50)); }
  100% { color: rgba(80,  200, 255, 0.95); filter: drop-shadow(0 0 5px rgba(80,  200, 255, 0.65)); }
}
@keyframes cursor-pulse {
  0%, 100% { transform: scaleY(1);    opacity: 0.9; }
  50%      { transform: scaleY(0.65); opacity: 0.5; }
}
.stream-cursor {
  display: inline-block;
  vertical-align: text-bottom;
  margin-left: 1px;
  transform-origin: 50% 85%;
  animation:
    cursor-color 2s linear infinite,
    cursor-pulse 0.8s ease-in-out infinite;
}
@media (prefers-reduced-motion: reduce) {
  .token-word    { animation: none; }
  .stream-cursor { animation: none; color: rgba(255, 255, 255, 0.7); filter: none; }
}

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
  min-height: 20px;
  margin-top: 4px;
  padding: 0 4px;
  opacity: 0;
  transition: opacity 0.2s var(--ease-enter);
}
.msg.user .msg-meta-row { justify-content: flex-end; }
.msg.assistant .msg-meta-row { justify-content: flex-start; }
.bubble-wrap:hover .msg-meta-row,
.bubble-wrap.has-recollapse .msg-meta-row { opacity: 1; }

/* Timestamp */
.msg-time {
  font-size: 11px;
  color: var(--text-label-muted);
  font-variant-numeric: tabular-nums;
  user-select: none;
  line-height: 1;
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
.msg-actions.assistant,
.msg-actions.system { left: 100%; padding-left: 6px; }
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

.bubble.markdown { max-width: 100%; }

/* Markdown prose */
.bubble.markdown :deep(p) { margin: 0 0 8px; }
.bubble.markdown :deep(p:last-child) { margin-bottom: 0; }

/* Tool call chips */
.bubble.markdown :deep(.tool-call-chip) {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 9px 3px 7px;
  border-radius: 6px;
  font-size: 11px;
  font-family: 'SF Mono', ui-monospace, monospace;
  vertical-align: middle;
  line-height: 1.4;
  white-space: nowrap;
  cursor: pointer;
  transition: opacity 0.12s;
}
.bubble.markdown :deep(.tool-call-chip:hover) { opacity: 0.75; }
.bubble.markdown :deep(.tool-call-chip--tool) {
  background: rgba(234, 179, 8, 0.1);
  border: 1px solid rgba(234, 179, 8, 0.28);
  color: rgba(253, 224, 71, 0.9);
}
.bubble.markdown :deep(.tool-call-chip--skill) {
  background: rgba(34, 197, 94, 0.1);
  border: 1px solid rgba(34, 197, 94, 0.28);
  color: rgba(74, 222, 128, 0.9);
}
.bubble.markdown :deep(.tool-call-chip svg) {
  flex-shrink: 0;
  opacity: 0.8;
}
.bubble.markdown :deep(.tool-call-group) {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  margin: 6px 0;
}
.bubble.markdown :deep(.tool-call-group.collapsed .tool-call-chip-extra) {
  display: none;
}
.bubble.markdown :deep(.tool-call-toggle) {
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.12);
  color: var(--text-tertiary);
  padding: 2px 8px;
  border-radius: 5px;
  font-size: 11px;
  font-family: 'SF Mono', ui-monospace, monospace;
  cursor: pointer;
  white-space: nowrap;
  line-height: 1.4;
  transition: background 0.12s, color 0.12s;
  flex-shrink: 0;
}
.bubble.markdown :deep(.tool-call-toggle:hover) {
  background: rgba(255, 255, 255, 0.08);
  color: var(--text-primary);
}


/* Code blocks */
.bubble.markdown :deep(.code-block) {
  margin: 8px 0;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid rgba(255,255,255,0.08);
  max-width: var(--code-max-width, 100%);
}
/* User bubbles: reset code-block max-width so it's bounded by the bubble's own 72% width; align code blocks to the right */

.msg.user .bubble.markdown :deep(.code-block) { margin-left: auto; }

.bubble.markdown :deep(.code-lang-icon) {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  margin-right: 6px;
}
.bubble.markdown :deep(.code-lang-icon svg) { width: 16px; height: 16px; }
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

/* ── User bubble markdown overrides (accent #2563EB background) ──────────── */
/* Headings: pure white for max contrast */
.user .bubble.markdown :deep(h1),
.user .bubble.markdown :deep(h2) { color: #fff; }
.user .bubble.markdown :deep(h3) { color: rgba(255,255,255,0.92); }
/* HR: more visible divider */
.user .bubble.markdown :deep(hr) { border-top-color: rgba(255,255,255,0.28); }
/* Blockquote: white left bar + dark glass background */
.user .bubble.markdown :deep(blockquote) {
  border-left-color: rgba(255,255,255,0.55);
  background: rgba(0,0,0,0.18);
  color: rgba(255,255,255,0.88);
}
/* Code block wrapper */
.user .bubble.markdown :deep(.code-block) {
  border-color: rgba(255,255,255,0.22);
}
/* Code header: dark glass on blue */
.user .bubble.markdown :deep(.code-header) {
  background: rgba(0,0,0,0.22);
  border-bottom-color: rgba(0,0,0,0.22);
}
/* Language label: muted white */
.user .bubble.markdown :deep(.code-lang) { color: rgba(255,255,255,0.6); }
/* Line number gutter */
.user .bubble.markdown :deep(.line-nr) {
  color: rgba(255,255,255,0.3);
  border-right-color: rgba(255,255,255,0.15);
}
/* Copy button: white glass style */
.user .bubble.markdown :deep(.code-copy) {
  background: rgba(255,255,255,0.14);
  color: #fff;
  border-color: rgba(255,255,255,0.28);
}
.user .bubble.markdown :deep(.code-copy:hover) {
  background: rgba(255,255,255,0.88);
  color: #1d4ed8;
  border-color: transparent;
}
/* Table: stronger borders & contrast on blue */
.user .bubble.markdown :deep(.table-wrapper) {
  border-color: rgba(255,255,255,0.28);
}
.user .bubble.markdown :deep(.tbl-filter-bar) {
  background: rgba(0,0,0,0.14);
  border-bottom-color: rgba(255,255,255,0.15);
}
.user .bubble.markdown :deep(.tbl-col-btn) {
  background: rgba(0,0,0,0.2);
  border-color: rgba(255,255,255,0.25);
  color: rgba(255,255,255,0.9);
}
.user .bubble.markdown :deep(.tbl-col-btn:hover) { background: rgba(0,0,0,0.32); }
.user .bubble.markdown :deep(.tbl-filter-input) {
  background: rgba(0,0,0,0.2);
  border-color: rgba(255,255,255,0.22);
  color: #fff;
}
.user .bubble.markdown :deep(.tbl-filter-input::placeholder) { color: rgba(255,255,255,0.4); }
.user .bubble.markdown :deep(.tbl-filter-input:focus) {
  border-color: rgba(255,255,255,0.7);
  box-shadow: 0 0 0 3px rgba(255,255,255,0.15);
}
.user .bubble.markdown :deep(thead tr) { background: rgba(0,0,0,0.2); }
.user .bubble.markdown :deep(th) { color: #fff; }
.user .bubble.markdown :deep(th),
.user .bubble.markdown :deep(td) { border-bottom-color: rgba(255,255,255,0.14); }
.user .bubble.markdown :deep(tbody tr:nth-child(even)) { background: rgba(0,0,0,0.1); }
.user .bubble.markdown :deep(tbody tr:hover) { background: rgba(0,0,0,0.18); }
.user .bubble.markdown :deep(.sortable-th.sorted) { color: rgba(255,255,255,0.95); }
/* ─────────────────────────────────────────────────────────────────────────── */

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
  width: fit-content;
  max-width: 100%;
}
.bubble.markdown :deep(.tbl-scroll) {
  overflow-x: auto;
  max-width: 100%;
}
.bubble.markdown :deep(table) {
  border-collapse: collapse;
  font-size: 13px;
  width: max-content;
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
.tbl-detail-pair-copy {
  flex-shrink: 0;
  opacity: 0;
  background: transparent;
  border: none;
  color: var(--text-tertiary);
  width: 22px;
  height: 22px;
  padding: 0;
  cursor: pointer;
  border-radius: 4px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: opacity 0.12s, color 0.12s, background 0.12s;
}
.tbl-detail-pair:hover .tbl-detail-pair-copy { opacity: 1; }
.tbl-detail-pair-copy:hover { color: var(--accent); background: var(--accent-alpha-12); }
.tbl-detail-pair-copy.copied { opacity: 1; color: #4ade80; }
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
.tbl-detail-value.markdown :deep(a) { color: rgba(125, 183, 255, 0.65); text-decoration: none; }
.tbl-detail-value.markdown :deep(a:hover) { color: rgba(125, 183, 255, 0.9); text-decoration: underline; }
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
.update-progress-bar-wrap {
  margin: 0 12px 6px;
  flex-shrink: 0;
  overflow: hidden;
}
.update-bar-enter-active {
  transition: max-height 0.25s ease, opacity 0.25s ease;
}
.update-bar-leave-active {
  transition: max-height 0.2s ease, opacity 0.15s ease;
}
.update-bar-enter-from,
.update-bar-leave-to {
  max-height: 0;
  opacity: 0;
}
.update-bar-enter-to,
.update-bar-leave-from {
  max-height: 40px;
  opacity: 1;
}
.update-progress-bar {
  height: 4px;
  border-radius: 2px;
  background: var(--lg-border-subtle);
  overflow: hidden;
}
.update-progress-fill {
  height: 100%;
  border-radius: 2px;
  background: var(--accent);
  transition: width 0.3s ease;
}
.update-progress-msg {
  display: block;
  margin-top: 4px;
  font-size: 11px;
  color: var(--text-tertiary);
  text-align: center;
}

.input-area {
  margin: 10px 12px;
  background: var(--lg-surface-input);
  backdrop-filter: blur(12px) saturate(160%);
  -webkit-backdrop-filter: blur(12px) saturate(160%);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 12px;
  flex-shrink: 0;
  transition:
    border-color 0.2s var(--ease-enter),
    box-shadow 0.25s var(--ease-enter),
    background 0.2s var(--ease-enter);
  overflow: hidden;
  position: relative;
  z-index: 1;
}
.input-area.md-mode { overflow: visible; }
.input-area:hover:not(:focus-within) { background: var(--lg-surface-input-h); }
.input-area:focus-within {
  border-color: var(--accent);
  box-shadow:
    0 0 0 3px var(--accent-alpha-20),
    inset 0 1px 0 rgba(255, 255, 255, 0.05);
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
.chat-opts-chips {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
}

.chat-opt-chip {
  display: inline-flex;
  align-items: center;
  gap: 0;
  height: 30px;
  padding: 0 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 500;
  font-family: inherit;
  cursor: pointer;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-tertiary);
  transition: background 0.12s, color 0.12s, border-color 0.12s;
  user-select: none;
  white-space: nowrap;
  overflow: hidden;
  flex-shrink: 0;
}

.chip-label {
  max-width: 0;
  overflow: hidden;
  opacity: 0;
  transition: max-width 0.2s ease, opacity 0.15s ease, margin-left 0.2s ease;
  margin-left: 0;
  white-space: nowrap;
}

.chat-opt-chip:hover .chip-label {
  max-width: 60px;
  opacity: 1;
  margin-left: 4px;
}

.chat-opt-chip:hover {
  background: var(--lg-surface-hover);
  color: var(--text-secondary);
}

/* Toggle chips (knowledge / memory): active = accent-tinted */
.chat-opt-chip.active {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-alpha-20);
}
.chat-opt-chip.active:hover {
  background: var(--accent-alpha-20);
  color: var(--accent);
}

.chat-opt-chip:disabled { opacity: 0.35; cursor: not-allowed; pointer-events: none; }

/* Thinking level variants */
.chat-opt-chip.thinking-off    { color: var(--text-tertiary); }
.chat-opt-chip.thinking-default{ color: var(--text-secondary); border-color: rgba(255,255,255,0.18); background: rgba(255,255,255,0.06); }
.chat-opt-chip.thinking-low    { color: #4ade80; border-color: rgba(74,222,128,0.45);  background: rgba(74,222,128,0.08); }
.chat-opt-chip.thinking-medium { color: #facc15; border-color: rgba(250,204,21,0.45);  background: rgba(250,204,21,0.08); }
.chat-opt-chip.thinking-high   { color: #fb923c; border-color: rgba(251,146,60,0.45);  background: rgba(251,146,60,0.08); }

.chip-icon {
  display: inline-flex;
  align-items: center;
  width: 14px;
  height: 14px;
  flex-shrink: 0;
}
:deep(.chip-icon svg) {
  width: 14px;
  height: 14px;
}
.chip-icon--fired {
  animation: chip-icon-bounce 0.42s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.chip-label--fired {
  animation: chip-label-slide 0.22s ease-out both 0.08s;
}
@keyframes chip-icon-bounce {
  0%   { transform: scale(1)    rotate(0deg); }
  30%  { transform: scale(1.45) rotate(-12deg); }
  60%  { transform: scale(0.9)  rotate(6deg); }
  80%  { transform: scale(1.08) rotate(-3deg); }
  100% { transform: scale(1)    rotate(0deg); }
}
@keyframes chip-label-slide {
  from { opacity: 0; transform: translateX(-5px); }
  to   { opacity: 1; transform: translateX(0); }
}
@media (prefers-reduced-motion: reduce) {
  .chip-icon--fired, .chip-label--fired { animation: none; }
}
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
  border-radius: 8px;
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition:
    background 0.15s var(--ease-enter),
    transform 0.18s var(--ease-spring),
    box-shadow 0.2s var(--ease-enter);
  flex-shrink: 0;
  box-shadow:
    0 2px 6px rgba(37, 99, 235, 0.35),
    inset 0 1px 0 rgba(255, 255, 255, 0.15);
}
.send-btn:hover:not(:disabled) {
  background: var(--accent-hover);
  box-shadow:
    0 4px 16px rgba(37, 99, 235, 0.5),
    0 0 0 4px rgba(37, 99, 235, 0.12),
    inset 0 1px 0 rgba(255, 255, 255, 0.2);
  transform: scale(1.06);
}
.send-btn:active:not(:disabled) {
  transform: scale(0.92);
  box-shadow:
    0 1px 3px rgba(37, 99, 235, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
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
  animation: voice-bar-pulse 2s ease-in-out infinite;
}
@keyframes voice-bar-pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(37, 99, 235, 0.15); }
  50%      { box-shadow: 0 0 0 4px rgba(37, 99, 235, 0.06); }
}
@media (prefers-reduced-motion: reduce) {
  .voice-hint-bar { animation: none; }
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
@keyframes lightbox-in {
  from { opacity: 0; }
  to   { opacity: 1; }
}
@keyframes lightbox-img-in {
  from { opacity: 0; transform: scale(0.94); }
  to   { opacity: 1; transform: scale(1);    }
}
.lightbox {
  position: absolute;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.88);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  pointer-events: auto;
  animation: lightbox-in 0.18s var(--ease-enter) both;
}
/* Fullscreen mode: teleported to body so position:fixed covers the full window */
body > .lightbox {
  position: fixed;
  inset: 0;
  z-index: 9999;
}
.lightbox-img {
  max-width: 90%;
  max-height: calc(100% - 80px);
  border-radius: 10px;
  box-shadow: 0 12px 60px rgba(0, 0, 0, 0.7);
  object-fit: contain;
  transform-origin: center;
  will-change: transform;
  user-select: none;
  -webkit-user-drag: none;
  display: block;
  animation: lightbox-img-in 0.24s var(--ease-enter) both;
}
body > .lightbox .lightbox-img {
  max-width: 96%;
  max-height: calc(100% - 80px);
  border-radius: 6px;
}

/* Toolbar */
.lightbox-toolbar {
  position: absolute;
  bottom: 20px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 2px;
  background: rgba(18, 18, 22, 0.8);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 32px;
  padding: 4px 10px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.5);
  white-space: nowrap;
}
.lb-btn {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.7);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s var(--ease-enter), color 0.15s, transform 0.1s var(--ease-exit);
  flex-shrink: 0;
}
.lb-btn:hover {
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
}
.lb-btn:active {
  background: rgba(255, 255, 255, 0.2);
  transform: scale(0.9);
}
.lb-zoom-label {
  font-size: 11.5px;
  color: rgba(255, 255, 255, 0.6);
  min-width: 38px;
  text-align: center;
  font-variant-numeric: tabular-nums;
  user-select: none;
  letter-spacing: 0.02em;
}
.lb-sep {
  width: 1px;
  height: 16px;
  background: rgba(255, 255, 255, 0.12);
  margin: 0 4px;
  flex-shrink: 0;
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

/* Markdown mode toggle button */
.md-btn {
  flex-shrink: 0;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 6px;
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-tertiary);
  cursor: pointer;
  font-size: 11px;
  font-weight: 600;
  font-family: inherit;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
  padding: 0;
}
.md-btn:hover:not(:disabled) { background: var(--lg-surface-hover); color: var(--text-secondary); }
.md-btn.active { border-color: var(--accent); color: var(--accent); background: var(--accent-alpha-20); }
.md-btn:disabled { opacity: 0.35; cursor: not-allowed; }
.md-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }

/* Milkdown editor container */
.milkdown-wrap {
  width: 100%;
  box-sizing: border-box;
  min-height: 80px;
  max-height: 400px;
  overflow-y: auto;
  border-radius: 8px 8px 0 0;
  user-select: text;
  -webkit-user-select: text;
  cursor: text;
}

/* Override Milkdown/Crepe internals to match app theme */
.milkdown-wrap :deep(.milkdown) {
  background: transparent;
  box-shadow: none;
  padding: 10px 12px 6px;
  font-size: 13px;
  line-height: 1.55;
  color: var(--text-primary);
  min-height: 80px;
  user-select: text;
  -webkit-user-select: text;
  /* Prevent Milkdown from loading external fonts (Noto Sans/Serif, Space Mono) */
  --crepe-font-default: inherit;
  --crepe-font-title: inherit;
  --crepe-font-code: 'SFMono-Regular', 'Menlo', 'Monaco', 'Courier New', monospace;
}
.milkdown-wrap :deep(.ProseMirror) {
  background: transparent;
  outline: none;
  min-height: 80px;
  padding: 0;
  font-size: 13px;
  line-height: 1.55;
  font-family: inherit;
  user-select: text;
  -webkit-user-select: text;
  pointer-events: auto;
  cursor: text;
}
/* Compact prose font sizes — Milkdown's defaults are document-editor scale (16-42px) */
.milkdown-wrap :deep(.milkdown .ProseMirror p) { font-size: 13px; line-height: 1.55; padding: 2px 0; }
.milkdown-wrap :deep(.milkdown .ProseMirror h1) { font-size: 1.4em; line-height: 1.3; margin-top: 8px; }
.milkdown-wrap :deep(.milkdown .ProseMirror h2) { font-size: 1.2em; line-height: 1.3; margin-top: 6px; }
.milkdown-wrap :deep(.milkdown .ProseMirror h3) { font-size: 1.1em; line-height: 1.3; margin-top: 4px; }
.milkdown-wrap :deep(.milkdown .ProseMirror h4),
.milkdown-wrap :deep(.milkdown .ProseMirror h5),
.milkdown-wrap :deep(.milkdown .ProseMirror h6) { font-size: 1em; line-height: 1.4; margin-top: 4px; }
.milkdown-wrap :deep(.ProseMirror p.is-empty::before) {
  color: var(--text-placeholder);
}
/* Hide Crepe frame/toolbar decorations */
.milkdown-wrap :deep(.milkdown-toolbar),
.milkdown-wrap :deep(.milkdown-block-handle),
.milkdown-wrap :deep(.milkdown-top-bar),
.milkdown-wrap :deep(.milkdown-slash-menu),
.milkdown-wrap :deep(.milkdown-link-preview),
.milkdown-wrap :deep(.milkdown-link-edit) {
  display: none !important;
}
/* Code block (CodeMirror) — match app's chat code block aesthetic */
.milkdown-wrap :deep(.milkdown-code-block) {
  background: rgba(10, 10, 20, 0.55);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 8px;
  padding: 0;
  margin: 4px 0;
  overflow: visible;
  min-height: 220px;
  position: relative;
}
/* Tools bar (language badge + copy button) */
.milkdown-wrap :deep(.milkdown-code-block .tools) {
  padding: 4px 8px;
  background: rgba(255, 255, 255, 0.04);
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}
.milkdown-wrap :deep(.milkdown-code-block .language-button) {
  font-size: 11px;
  color: rgba(125, 211, 252, 0.7);
  background: transparent;
  border-radius: 4px;
  padding: 2px 6px;
  margin-bottom: 0;
  font-family: 'SFMono-Regular', 'Menlo', monospace;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  opacity: 1 !important;
}
.milkdown-wrap :deep(.milkdown-code-block .language-button:hover) {
  background: rgba(255, 255, 255, 0.08);
}
.milkdown-wrap :deep(.milkdown-code-block .expand-icon svg) {
  color: rgba(125, 211, 252, 0.5);
}
.milkdown-wrap :deep(.milkdown-code-block .tools-button-group button) {
  font-size: 11px;
  opacity: 1 !important;
  background: rgba(255, 255, 255, 0.06) !important;
  color: rgba(255, 255, 255, 0.6) !important;
  border-radius: 4px;
}
.milkdown-wrap :deep(.milkdown-code-block .tools-button-group button:hover) {
  background: rgba(255, 255, 255, 0.12) !important;
}
/* CodeMirror editor surface */
.milkdown-wrap :deep(.milkdown-code-block .cm-editor) {
  background: transparent !important;
}
.milkdown-wrap :deep(.milkdown-code-block .cm-scroller) {
  padding: 8px 0;
  font-family: 'SFMono-Regular', 'Menlo', 'Monaco', 'Courier New', monospace !important;
  font-size: 12px;
  line-height: 1.6;
}
.milkdown-wrap :deep(.milkdown-code-block .cm-gutters) {
  background: transparent !important;
  border-right: 1px solid rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.25);
  min-width: 2.5em;
}
.milkdown-wrap :deep(.milkdown-code-block .cm-lineNumbers .cm-gutterElement) {
  padding: 0 8px 0 4px;
  font-size: 11px;
}
.milkdown-wrap :deep(.milkdown-code-block .cm-content) {
  padding: 0 12px;
}
.milkdown-wrap :deep(.milkdown-code-block .cm-activeLine) {
  background: rgba(255, 255, 255, 0.03);
}
.milkdown-wrap :deep(.milkdown-code-block .cm-activeLineGutter) {
  background: rgba(255, 255, 255, 0.05);
  color: rgba(255, 255, 255, 0.5);
}
/* Language picker dropdown */
.milkdown-wrap :deep(.milkdown-code-block .language-picker) {
  z-index: 9999;
}
.milkdown-wrap :deep(.milkdown-code-block .list-wrapper) {
  background: rgba(30, 34, 48, 0.98);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.6);
  width: 200px;
  max-height: 190px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.milkdown-wrap :deep(.milkdown-code-block .language-list) {
  height: auto !important;
  flex: 1;
  overflow-y: auto;
}
.milkdown-wrap :deep(.milkdown-code-block .language-list-item) {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.75);
  padding: 3px 10px;
}
.milkdown-wrap :deep(.milkdown-code-block .language-list-item:hover) {
  background: rgba(255, 255, 255, 0.07);
}
.milkdown-wrap :deep(.milkdown-code-block .search-box) {
  outline: 1px solid rgba(255, 255, 255, 0.15);
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.05);
  margin: 0 8px 6px;
  padding: 3px 8px;
}
.milkdown-wrap :deep(.milkdown-code-block .search-box input) {
  color: rgba(255, 255, 255, 0.8);
  font-size: 11px;
}

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
  padding: 2px 0;
  background: none;
  border: none;
  border-radius: 3px;
  font-size: 11px;
  color: var(--accent-hover);
  cursor: pointer;
  user-select: none;
  font-family: inherit;
  transition: color 0.12s;
}
.lp-toggle-btn:hover { color: var(--text-primary); }
.lp-toggle-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

.search-bar {
  padding: 8px 12px;
  border-bottom: 1px solid var(--lg-border-subtle);
  animation: search-slide-down 0.18s var(--ease-enter);
}
@keyframes search-slide-down {
  from { opacity: 0; transform: translateY(-6px); }
  to { opacity: 1; transform: translateY(0); }
}
.search-input-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--lg-surface-elevated);
  border: 1px solid var(--lg-border-subtle);
  border-radius: 9px;
  padding: 5px 9px;
  transition: border-color 0.2s var(--ease-enter), box-shadow 0.2s var(--ease-enter);
}
.search-input-wrap:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
.search-input-icon {
  color: var(--text-secondary);
  flex-shrink: 0;
}
.search-input {
  flex: 1;
  background: transparent;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  padding: 5px 4px;
  outline: none;
}
.search-input::placeholder {
  color: var(--text-tertiary);
}
.search-input::-webkit-search-cancel-button {
  display: none;
}
.search-count {
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
}
.search-close-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  padding: 6px;
  border-radius: 5px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.12s, color 0.12s;
}
.search-close-btn:hover {
  color: var(--text-primary);
  background: var(--lg-surface-hover);
}
.search-close-btn:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
.search-empty {
  text-align: center;
  padding: 32px 16px;
}
.search-empty-title {
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 500;
  margin-bottom: 6px;
}
.search-empty-hint {
  color: var(--text-tertiary);
  font-size: 12px;
}
.search-highlight {
  background: rgba(240, 180, 41, 0.25);
  color: #f0b429;
  border-radius: 2px;
  padding: 0 1px;
}
.search-dimmed {
  opacity: 0.5;
}
.search-result-selected {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
  border-radius: 4px;
}
@keyframes jump-flash {
  0%, 100% { background: transparent; }
  50% { background: rgba(240, 180, 41, 0.12); }
}
:deep(.jump-flash) {
  animation: jump-flash 0.5s ease-in-out 4;
}
</style>

<style>
/* Shimmer border animation — must be global for @property + @keyframes */
@property --shimmer-angle {
  syntax: '<angle>';
  inherits: false;
  initial-value: 0deg;
}

@keyframes shimmer-spin {
  to { --shimmer-angle: 360deg; }
}

@keyframes token-word-appear {
  from { opacity: 0; transform: scale(0.88) translateY(2px); }
  to   { opacity: 1; transform: scale(1)    translateY(0);   }
}

@keyframes shimmer-fadeout {
  0%   { opacity: 1;   transform: scale(1);    }
  60%  { opacity: 0.6; transform: scale(1.012); }
  100% { opacity: 0;   transform: scale(1.025); }
}

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
  background: var(--danger);
  color: #fff;
  font-weight: 600;
}
.clear-confirm-ok:hover { background: #ff5e55; }

/* ── Design tokens ─────────────────────────────────────── */
/* expo-out: snappy enter;  ease-in-cubic: brisk exit (30% faster) */
:root {
  --ease-enter: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-exit:  cubic-bezier(0.4, 0, 0.9, 1);
  --ease-spring: cubic-bezier(0.34, 1.3, 0.64, 1);
}

/* ── ThinkingBlock ─────────────────────────────────────── */
/* Entrance: fade-in + slide-up from 6px */
@keyframes thinking-appear {
  from { opacity: 0; transform: translateY(6px); }
  to   { opacity: 1; transform: translateY(0);   }
}
@keyframes thinking-pulse {
  0%, 100% { box-shadow: var(--thinking-glow); }
  50%       { box-shadow: var(--thinking-glow-on); }
}
@keyframes thinking-bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.5; }
  40%           { transform: translateY(-4px); opacity: 1; }
}

.thinking-block {
  margin-bottom: 4px;
  border-radius: 10px;
  background: var(--thinking-bg);
  border: 1px solid var(--thinking-border);
  overflow: hidden;
  position: relative;
  animation: thinking-appear 0.22s var(--ease-enter) both;
  transition: border-color 0.25s var(--ease-enter), box-shadow 0.25s var(--ease-enter);
}

/* Ambient glow when streaming */
.thinking-block.thinking-streaming {
  border-color: var(--thinking-border-on);
  box-shadow: var(--thinking-glow);
  animation: thinking-appear 0.22s var(--ease-enter) both,
             thinking-pulse 2.4s ease-in-out infinite;
}

/* Top shimmer line */
.thinking-block::before {
  content: '';
  position: absolute;
  top: 0; left: 0; right: 0;
  height: 1px;
  background: var(--thinking-shimmer);
  opacity: 0.8;
}

.thinking-divider {
  border: none;
  border-top: 1px solid rgba(255, 255, 255, 0.07);
  margin: 8px 0;
}

.thinking-block-header {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 8px 11px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s var(--ease-enter);
}
.thinking-block-header:hover {
  background: rgba(80, 150, 255, 0.05);
}

.thinking-icon {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--thinking-icon-bg);
  border: 1px solid var(--thinking-icon-border);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--thinking-icon-fg);
  line-height: 0;
}
.thinking-icon svg { display: block; }

.thinking-label {
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.04em;
  color: var(--thinking-fg);
  flex: 1;
}

/* Streaming badge: three bouncing dots */
.thinking-streaming-badge {
  display: flex;
  align-items: center;
  gap: 3px;
  margin-right: 2px;
}

.thinking-dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--thinking-fg-dot);
  display: inline-block;
  animation: thinking-bounce 1.2s ease-in-out infinite;
}
.thinking-dot:nth-child(2) { animation-delay: 0.2s; }
.thinking-dot:nth-child(3) { animation-delay: 0.4s; }

/* Chevron: spring on expand, brisk on collapse */
.thinking-toggle-icon {
  color: var(--thinking-chevron);
  transition: transform 0.28s var(--ease-spring), color 0.15s;
  flex-shrink: 0;
  display: flex;
  align-items: center;
}
.thinking-toggle-icon.expanded {
  transform: rotate(180deg);
  color: var(--thinking-chevron-on);
  transition: transform 0.18s var(--ease-exit), color 0.15s;
}
.thinking-block-header:hover .thinking-toggle-icon {
  color: var(--thinking-chevron-on);
}

/* Body: expo-out open, faster close */
.thinking-block-body {
  max-height: 0;
  overflow: hidden;
  transition: max-height 0.2s var(--ease-exit);
}
.thinking-block-body.expanded {
  max-height: 190px;
  overflow-y: auto;
  transition: max-height 0.3s var(--ease-enter);
}

.thinking-block-body.expanded::-webkit-scrollbar {
  width: 3px;
}
.thinking-block-body.expanded::-webkit-scrollbar-track {
  background: transparent;
}
.thinking-block-body.expanded::-webkit-scrollbar-thumb {
  background: var(--thinking-border-on);
  border-radius: 2px;
}

.thinking-block-text {
  padding: 2px 12px 10px;
  font-size: 12px;
  font-weight: 400;
  color: var(--thinking-fg-dim);
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.65;
  font-family: inherit;
}

/* ── prefers-reduced-motion ─────────────────────────────── */
/* Respect user OS motion preferences: disable all decorative animations */
@media (prefers-reduced-motion: reduce) {
  .thinking-block,
  .thinking-block.thinking-streaming {
    animation: none;
    transition: border-color 0s, box-shadow 0s;
  }
  .thinking-block-body,
  .thinking-block-body.expanded { transition: none; }
  .thinking-toggle-icon,
  .thinking-toggle-icon.expanded { transition: color 0.15s; }
  .thinking-dot { animation: none; opacity: 0.7; }
  .lightbox { animation: none; }
  .lightbox-img { animation: none; }
  .lb-btn { transition: background 0.1s, color 0.1s; }
  /* Collapse/expand: no height animation, no fade */
  .coll-fade-enter-active,
  .coll-fade-leave-active { transition: none; }
}
</style>

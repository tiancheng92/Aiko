import { marked, Renderer } from 'marked'
import markedKatex from 'marked-katex-extension'
import 'katex/dist/katex.min.css'
import { createHighlighterCore } from 'shiki/core'
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript'

const SHIKI_THEME = 'github-dark'

let _hl = null
/** highlighterReady resolves once Shiki is initialized. */
export const highlighterReady = createHighlighterCore({
  themes: [import('shiki/themes/github-dark.mjs')],
  langs: [
    import('shiki/langs/javascript.mjs'),
    import('shiki/langs/typescript.mjs'),
    import('shiki/langs/python.mjs'),
    import('shiki/langs/bash.mjs'),
    import('shiki/langs/shell.mjs'),
    import('shiki/langs/go.mjs'),
    import('shiki/langs/json.mjs'),
    import('shiki/langs/css.mjs'),
    import('shiki/langs/html.mjs'),
    import('shiki/langs/xml.mjs'),
    import('shiki/langs/vue.mjs'),
    import('shiki/langs/sql.mjs'),
    import('shiki/langs/yaml.mjs'),
    import('shiki/langs/rust.mjs'),
  ],
  engine: createJavaScriptRegexEngine(),
}).then(hl => { _hl = hl }).catch(() => {})

/** escapeHtml escapes HTML special characters in a raw string. */
function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/** tokenToSpan converts a Shiki ThemedToken to an HTML span string. */
function tokenToSpan(t) {
  const content = escapeHtml(t.content)
  let style = ''
  if (t.color) style += `color:${t.color};`
  if (t.fontStyle & 1) style += 'font-style:italic;'
  if (t.fontStyle & 2) style += 'font-weight:bold;'
  return style ? `<span style="${style}">${content}</span>` : content
}

const _codeCache = new Map()
const _CODE_CACHE_MAX = 100

const renderer = new Renderer()
renderer.code = ({ text, lang }) => {
  const cacheKey = lang + '\x00' + text
  const cached = _codeCache.get(cacheKey)
  if (cached !== undefined) return cached
  const language = lang || null
  let lineHtmls
  if (_hl) {
    try {
      const tokens = _hl.codeToTokensBase(text, { lang: language || 'plaintext', theme: SHIKI_THEME })
      lineHtmls = tokens.map(line => line.map(tokenToSpan).join(''))
    } catch {
      lineHtmls = escapeHtml(text).split('\n')
    }
  } else {
    lineHtmls = escapeHtml(text).split('\n')
  }
  if (lineHtmls.length > 1 && lineHtmls[lineHtmls.length - 1] === '') lineHtmls.pop()
  const digits = String(lineHtmls.length).length
  const numbered = lineHtmls.map((line, i) =>
    `<span class="code-line"><span class="line-nr">${String(i + 1).padStart(digits)}</span><span class="line-code">${line || ' '}</span></span>`
  ).join('')
  const result = `<div class="code-block"><div class="code-header"><span class="code-lang">${language || 'text'}</span><button class="code-copy" onclick="navigator.clipboard.writeText(decodeURIComponent(atob(this.dataset.code)));this.textContent='✓';setTimeout(()=>this.textContent='复制',2000)" data-code="${btoa(encodeURIComponent(text))}">复制</button></div><pre><code>${numbered}</code></pre></div>`
  if (_codeCache.size >= _CODE_CACHE_MAX) _codeCache.delete(_codeCache.keys().next().value)
  _codeCache.set(cacheKey, result)
  return result
}

const TABLE_PAGE_SIZE = 10

renderer.table = (token) => {
  const aligns = token.header.map(c => c.align || '')
  const rawRows = token.rows.map(row => row.map(c => c.text))
  const headers = token.header.map(c => c.text)
  const encodedHeaders = btoa(encodeURIComponent(JSON.stringify(headers)))
  const encodedRaw = btoa(encodeURIComponent(JSON.stringify(rawRows)))
  const id = 'tbl-' + Math.random().toString(36).slice(2, 9)

  window.__tableState = window.__tableState || {}
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

/** buildRowHtml renders one <tr> from raw text cells. */
function buildRowHtml(cells, origIdx, aligns) {
  const tds = cells.map((cell, j) => {
    const align = aligns[j] ? ` style="text-align:${aligns[j]}"` : ''
    return `<td${align}>${marked.parseInline(cell)}</td>`
  }).join('')
  return `<tr class="tbl-row" onclick="window.__tr(this)" data-row-idx="${origIdx}">${tds}</tr>`
}

/** renderTablePage rebuilds tbody and pagination controls. */
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

/** updateSortHeaders updates <th> sort indicators. */
function updateSortHeaders(wrapper, state) {
  wrapper.querySelectorAll('thead th').forEach((th, i) => {
    const ind = th.querySelector('.sort-indicator')
    if (!ind) return
    const active = i === state.sortCol && state.sortDir !== 'none'
    ind.textContent = active ? (state.sortDir === 'asc' ? ' ↑' : ' ↓') : ''
    th.classList.toggle('sorted', active)
  })
}

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

// Register global table interaction handlers once at module level.
window.__tp = (id, page) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  const totalPages = Math.ceil(state.sortedIndices.length / TABLE_PAGE_SIZE)
  state.currentPage = Math.max(1, Math.min(page, totalPages))
  renderTablePage(wrapper, state)
}
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
window.__fc = (id, colIdx) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  state.filterCol = colIdx
  applyFilterSort(state)
  renderTablePage(wrapper, state)
}
window.__tf = (id, query) => {
  const wrapper = document.getElementById(id)
  const state = window.__tableState?.[id]
  if (!wrapper || !state) return
  state.filterQuery = query.trim().toLowerCase()
  applyFilterSort(state)
  renderTablePage(wrapper, state)
}
window.__toggleColDrop = (e, id) => {
  e.stopPropagation()
  const el = document.getElementById(id + '-col')
  if (!el) return
  const drop = el.querySelector('.tbl-col-drop')
  const isOpen = drop.classList.contains('tbl-col-drop--open')
  document.querySelectorAll('.tbl-col-drop--open').forEach(d => d.classList.remove('tbl-col-drop--open'))
  if (!isOpen) drop.classList.add('tbl-col-drop--open')
}
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

renderer.link = ({ href, title, text }) => {
  const realHref = extractRealUrl(href) || href
  const display = text && text !== href ? text : shortenUrl(realHref)
  const safeHref = realHref.replace(/"/g, '&quot;')
  const titleAttr = title ? ` title="${title}"` : ''
  return `<a href="${safeHref}"${titleAttr} target="_blank" rel="noopener">${display}</a>`
}

marked.use(markedKatex({ throwOnError: false, output: 'html' }))
marked.use({ renderer, breaks: true, gfm: true })

/** extractRealUrl unwraps DuckDuckGo redirect URLs. */
export function extractRealUrl(href) {
  if (!href) return null
  const full = href.startsWith('//') ? 'https:' + href : href
  try {
    const u = new URL(full)
    if (u.hostname.includes('duckduckgo.com') && u.searchParams.has('uddg')) {
      return decodeURIComponent(u.searchParams.get('uddg'))
    }
  } catch {}
  return null
}

/** shortenUrl returns a readable short form of a URL. */
export function shortenUrl(url) {
  try {
    const u = new URL(url.startsWith('//') ? 'https:' + url : url)
    const path = u.pathname.length > 30 ? u.pathname.slice(0, 28) + '…' : u.pathname
    return u.hostname + (path !== '/' ? path : '')
  } catch {
    return url.length > 50 ? url.slice(0, 48) + '…' : url
  }
}

/** stripEmotionTags removes [情绪:xxx/0.0] tags from a string. */
export function stripEmotionTags(s) { return s.replace(/\[情绪:\w+\/[\d.]+\]\n?/g, '') }

/** closeUnclosedFences closes any unclosed fenced code blocks to avoid broken rendering. */
export function closeUnclosedFences(text) {
  const lines = text.split('\n')
  let inFence = false
  let fenceChar = ''
  for (const line of lines) {
    const m = line.match(/^(`{3,}|~{3,})/)
    if (m) {
      if (!inFence) { inFence = true; fenceChar = m[1][0] }
      else if (m[1][0] === fenceChar) inFence = false
    }
  }
  return inFence ? text + '\n' + fenceChar.repeat(3) : text
}

const ZWJ = '‍'
const BOLD_CLOSE_FIX = new RegExp('([　-鿿＀-￯])\\*\\*(?=[^\\s\\p{P}])', 'gu')
const BOLD_OPEN_FIX = /(\p{Lo})\*\*(?=[　-〿＀-￯'-‟])/gu

const _mdCache = new Map()
const _MD_CACHE_MAX = 200

/** renderMarkdown converts markdown text to sanitized HTML with caching. */
export function renderMarkdown(text) {
  if (!text) return ''
  const cached = _mdCache.get(text)
  if (cached !== undefined) return cached
  const stripped = stripEmotionTags(text.replace(/<thinking>[\s\S]*?<\/thinking>/gi, '')).trim()
  if (!stripped) { _mdCache.set(text, ''); return '' }
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
  if (_mdCache.size >= _MD_CACHE_MAX) _mdCache.delete(_mdCache.keys().next().value)
  _mdCache.set(text, html)
  return html
}

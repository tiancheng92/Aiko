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

/** LANG_ICONS maps language names to inline SVG logos shown in the code block header. */
const LANG_ICONS = {
  javascript: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#F7DF1E"/><path d="M19.3 25.1c.6 1 1.4 1.7 2.8 1.7 1.2 0 1.9-.6 1.9-1.4 0-1-.8-1.3-2-1.9l-.7-.3c-2-.8-3.3-1.9-3.3-4.1 0-2 1.6-3.6 4-3.6 1.7 0 3 .6 3.9 2.2l-2.1 1.4c-.5-.9-1-1.2-1.8-1.2-.8 0-1.3.5-1.3 1.2 0 .8.5 1.2 1.7 1.7l.7.3c2.3 1 3.6 2 3.6 4.3 0 2.5-1.9 3.8-4.5 3.8-2.5 0-4.1-1.2-4.9-2.8l2.1-1.3zm-9.8.3c.4.8.8 1.4 1.7 1.4.9 0 1.4-.3 1.4-1.7V16h2.6v9.2c0 2.8-1.6 4-4 4-2.2 0-3.4-1.1-4-2.5l2.3-1.3z" fill="#000"/></svg>`,
  js: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#F7DF1E"/><path d="M19.3 25.1c.6 1 1.4 1.7 2.8 1.7 1.2 0 1.9-.6 1.9-1.4 0-1-.8-1.3-2-1.9l-.7-.3c-2-.8-3.3-1.9-3.3-4.1 0-2 1.6-3.6 4-3.6 1.7 0 3 .6 3.9 2.2l-2.1 1.4c-.5-.9-1-1.2-1.8-1.2-.8 0-1.3.5-1.3 1.2 0 .8.5 1.2 1.7 1.7l.7.3c2.3 1 3.6 2 3.6 4.3 0 2.5-1.9 3.8-4.5 3.8-2.5 0-4.1-1.2-4.9-2.8l2.1-1.3zm-9.8.3c.4.8.8 1.4 1.7 1.4.9 0 1.4-.3 1.4-1.7V16h2.6v9.2c0 2.8-1.6 4-4 4-2.2 0-3.4-1.1-4-2.5l2.3-1.3z" fill="#000"/></svg>`,
  typescript: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#3178C6"/><path d="M18.3 22.1v2.5c.4.2.9.4 1.5.5.6.1 1.2.2 1.8.2.6 0 1.2-.1 1.7-.2.5-.1 1-.3 1.4-.6.4-.3.7-.6.9-1.1.2-.4.3-1 .3-1.6 0-.5-.1-.9-.2-1.2-.1-.4-.3-.7-.6-1-.3-.3-.6-.5-1-.7-.4-.2-.9-.4-1.4-.6l-.8-.3c-.2-.1-.4-.2-.6-.3-.2-.1-.3-.2-.4-.3-.1-.1-.1-.3-.1-.4 0-.2 0-.3.1-.4.1-.1.2-.2.3-.3.1-.1.3-.1.5-.2.2 0 .4-.1.6-.1.4 0 .8.1 1.2.2.4.1.7.3 1 .6v-2.4c-.3-.1-.7-.2-1.1-.3-.4-.1-.9-.1-1.4-.1-.6 0-1.1.1-1.6.2-.5.2-1 .4-1.3.7-.4.3-.7.7-.9 1.1-.2.4-.3.9-.3 1.5 0 .8.2 1.4.6 1.9.4.5 1 .9 1.8 1.2l.9.3c.3.1.5.2.7.3.2.1.3.2.4.3.1.1.1.3.1.5 0 .2-.1.3-.2.5-.1.1-.2.2-.4.3-.2.1-.4.1-.6.2-.2 0-.5.1-.7.1-.5 0-1-.1-1.5-.3-.5-.2-.9-.5-1.2-.8zM13 16.2h3.4V14H6.8v2.2h3.4V27H13V16.2z" fill="#fff"/></svg>`,
  ts: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#3178C6"/><path d="M18.3 22.1v2.5c.4.2.9.4 1.5.5.6.1 1.2.2 1.8.2.6 0 1.2-.1 1.7-.2.5-.1 1-.3 1.4-.6.4-.3.7-.6.9-1.1.2-.4.3-1 .3-1.6 0-.5-.1-.9-.2-1.2-.1-.4-.3-.7-.6-1-.3-.3-.6-.5-1-.7-.4-.2-.9-.4-1.4-.6l-.8-.3c-.2-.1-.4-.2-.6-.3-.2-.1-.3-.2-.4-.3-.1-.1-.1-.3-.1-.4 0-.2 0-.3.1-.4.1-.1.2-.2.3-.3.1-.1.3-.1.5-.2.2 0 .4-.1.6-.1.4 0 .8.1 1.2.2.4.1.7.3 1 .6v-2.4c-.3-.1-.7-.2-1.1-.3-.4-.1-.9-.1-1.4-.1-.6 0-1.1.1-1.6.2-.5.2-1 .4-1.3.7-.4.3-.7.7-.9 1.1-.2.4-.3.9-.3 1.5 0 .8.2 1.4.6 1.9.4.5 1 .9 1.8 1.2l.9.3c.3.1.5.2.7.3.2.1.3.2.4.3.1.1.1.3.1.5 0 .2-.1.3-.2.5-.1.1-.2.2-.4.3-.2.1-.4.1-.6.2-.2 0-.5.1-.7.1-.5 0-1-.1-1.5-.3-.5-.2-.9-.5-1.2-.8zM13 16.2h3.4V14H6.8v2.2h3.4V27H13V16.2z" fill="#fff"/></svg>`,
  python: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="py-a" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#5A9FD4"/><stop offset="100%" stop-color="#306998"/></linearGradient><linearGradient id="py-b" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#FFD43B"/><stop offset="100%" stop-color="#FFE873"/></linearGradient></defs><path d="M15.9 4C11 4 11.3 6.1 11.3 6.1l.1 2.2h4.7v.6H8.9S6 8.6 6 13.6s2.6 4.8 2.6 4.8h1.5v-2.3s-.1-2.6 2.5-2.6h4.4s2.4 0 2.4-2.3V6.5s.4-2.5-3.5-2.5zm-2.4 1.4c.4 0 .8.4.8.8s-.4.8-.8.8-.8-.4-.8-.8.4-.8.8-.8z" fill="url(#py-a)"/><path d="M16.1 28c4.9 0 4.6-2.1 4.6-2.1l-.1-2.2h-4.7v-.6h7.2s2.9.3 2.9-4.7-2.6-4.8-2.6-4.8h-1.5v2.3s.1 2.6-2.5 2.6h-4.4s-2.4 0-2.4 2.3v3.8s-.4 2.4 3.5 2.4zm2.4-1.4c-.4 0-.8-.4-.8-.8s.4-.8.8-.8.8.4.8.8-.4.8-.8.8z" fill="url(#py-b)"/></svg>`,
  py: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="py2-a" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#5A9FD4"/><stop offset="100%" stop-color="#306998"/></linearGradient><linearGradient id="py2-b" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#FFD43B"/><stop offset="100%" stop-color="#FFE873"/></linearGradient></defs><path d="M15.9 4C11 4 11.3 6.1 11.3 6.1l.1 2.2h4.7v.6H8.9S6 8.6 6 13.6s2.6 4.8 2.6 4.8h1.5v-2.3s-.1-2.6 2.5-2.6h4.4s2.4 0 2.4-2.3V6.5s.4-2.5-3.5-2.5zm-2.4 1.4c.4 0 .8.4.8.8s-.4.8-.8.8-.8-.4-.8-.8.4-.8.8-.8z" fill="url(#py2-a)"/><path d="M16.1 28c4.9 0 4.6-2.1 4.6-2.1l-.1-2.2h-4.7v-.6h7.2s2.9.3 2.9-4.7-2.6-4.8-2.6-4.8h-1.5v2.3s.1 2.6-2.5 2.6h-4.4s-2.4 0-2.4 2.3v3.8s-.4 2.4 3.5 2.4zm2.4-1.4c-.4 0-.8-.4-.8-.8s.4-.8.8-.8.8.4.8-.8-.4.8-.8.8z" fill="url(#py2-b)"/></svg>`,
  go: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#00ACD7"/><path d="M6 17.2c0-.1.1-.2.2-.2l1.2-.3c.1 0 .2 0 .2.1l.2.5c0 .1 0 .2-.1.2l-1.2.3c-.1 0-.2 0-.2-.1L6 17.2zm13.3-5.6c-1.5-.4-2.5-.2-3.2.5L15 13.3c-.1.1 0 .2.1.3.4.1.7.3.9.5.1.1.2.1.3 0l.9-.8c.5-.5 1-.6 1.8-.4l.2.1c.7.2 1.1.7 1 1.4L20 15.5c-.1.7-.7 1.2-1.4 1.4l-.3.1c-.8.2-1.4 0-1.8-.6l-.1-.2c-.1-.1-.2-.1-.3 0l-1 .6c-.1.1-.1.2-.1.3.5 1.2 1.7 1.7 3.2 1.4l.3-.1c1.5-.4 2.5-1.5 2.7-3l.2-1.2c.2-1.5-.7-2.6-2.1-3zm-7.2 3.2L11 16.1c-.1.1-.2.2-.1.3l.2.5c0 .1.1.1.2.1h1.3c.1 0 .2-.1.2-.2l-.1-.8c0-.1-.2-.2-.3-.2h-.3zm-2.3.5L8.6 15c-.1 0-.2 0-.2.1l-.3.8c0 .1 0 .2.1.2l1.2.3c.1 0 .2 0 .2-.1l.2-.5c0-.1 0-.2-.1-.2l-.1.1zm14.9-1.2c0-.1-.1-.2-.2-.2l-1.2-.3c-.1 0-.2 0-.2.1l-.2.5c0 .1 0 .2.1.2l1.2.3c.1 0 .2 0 .2-.1l.3-.5z" fill="#fff"/></svg>`,
  rust: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#000"/><path d="M16 5l.7 1.4H23l-1-1.4H16zm-1 0H8.9L8 6.4h6.3L15 5zm1.7 3H9.5L8 10h15.9L22.4 8H16.7zm-7.5 3L7.6 14h16.8l-1.6-3H9.2zm-2 4l-1.5 3h20.5l-1.5-3H7.2zm-2 4l-1.1 3h23.7l-1-3H5.2zm-.5 4L4 21h24l-.7 2H4.7z" fill="#CE422B"/><circle cx="16" cy="16" r="5" fill="none" stroke="#CE422B" stroke-width="1.5"/></svg>`,
  bash: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#293137"/><path d="M8.5 21l4.5-5-4.5-5h3l3 3.5v3L11.5 21H8.5zm7 0v-2h7v2h-7z" fill="#4EAA25"/></svg>`,
  shell: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#293137"/><path d="M8.5 21l4.5-5-4.5-5h3l3 3.5v3L11.5 21H8.5zm7 0v-2h7v2h-7z" fill="#4EAA25"/></svg>`,
  sh: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#293137"/><path d="M8.5 21l4.5-5-4.5-5h3l3 3.5v3L11.5 21H8.5zm7 0v-2h7v2h-7z" fill="#4EAA25"/></svg>`,
  json: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#1a1a1a"/><path d="M9 11c-1.5 0-2.5 1-2.5 2.5v1c0 .8-.5 1.5-1.5 1.5 1 0 1.5.7 1.5 1.5v1C6.5 20 7.5 21 9 21m14 0c1.5 0 2.5-1 2.5-2.5v-1c0-.8.5-1.5 1.5-1.5-1 0-1.5-.7-1.5-1.5v-1C25.5 12 24.5 11 23 11" stroke="#F5A623" stroke-width="1.5" fill="none" stroke-linecap="round"/><circle cx="12" cy="16" r="1.2" fill="#F5A623"/><circle cx="16" cy="16" r="1.2" fill="#F5A623"/><circle cx="20" cy="16" r="1.2" fill="#F5A623"/></svg>`,
  css: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#264de4"/><path d="M7 5l1.7 19.1L16 26.5l7.3-2.4L25 5H7zm14.7 5.2l-.2 2.3H13v2.3h8.3l-.6 6.8-4.7 1.3-4.7-1.3-.3-3.8h2.3l.2 1.8 2.5.7 2.5-.7.3-3.2H10.7l-.6-6.2h11.8l-.2.1v-.1z" fill="#fff"/></svg>`,
  html: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#e34c26"/><path d="M7 5l1.7 19.1L16 26.5l7.3-2.4L25 5H7zm13.4 6H12v2.3h8.2l-.3 3.2H12v2.3h7.7l-.5 5.3-3.2.9-3.2-.9-.2-2.5H10l.4 4.3 5.6 1.6 5.6-1.6.8-8.8.3-3.2.3-2.9z" fill="#fff"/></svg>`,
  xml: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#f16529"/><path d="M12 10l-5 6 5 6M20 10l5 6-5 6M17 8l-2 16" stroke="#fff" stroke-width="1.8" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  vue: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#1a1a1a"/><path d="M16 26L4 8h5.5L16 19.5 22.5 8H28L16 26z" fill="#41B883"/><path d="M16 26l-6.5-10h4L16 20.5l2.5-4.5h4L16 26z" fill="#35495E"/></svg>`,
  sql: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#003B57"/><ellipse cx="16" cy="10" rx="9" ry="3.5" fill="#00AFF0" opacity=".9"/><path d="M7 10v4c0 1.9 4 3.5 9 3.5s9-1.6 9-3.5v-4c0 1.9-4 3.5-9 3.5S7 11.9 7 10z" fill="#00AFF0" opacity=".7"/><path d="M7 14v4c0 1.9 4 3.5 9 3.5s9-1.6 9-3.5v-4c0 1.9-4 3.5-9 3.5S7 15.9 7 14z" fill="#00AFF0" opacity=".5"/></svg>`,
  yaml: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#cb171e"/><path d="M8 9h3l5 7 5-7h3l-6.5 9v7h-3v-7L8 9z" fill="#fff"/></svg>`,
  yml: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#cb171e"/><path d="M8 9h3l5 7 5-7h3l-6.5 9v7h-3v-7L8 9z" fill="#fff"/></svg>`,
  toml: `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="#1a1a1a"/><path d="M7 10h18M7 16h10M7 22h14" stroke="#9B59B6" stroke-width="2" stroke-linecap="round"/><circle cx="22" cy="16" r="2.5" fill="#9B59B6"/><circle cx="24" cy="22" r="2.5" fill="#9B59B6"/></svg>`,
}

const LANG_ICON_DEFAULT = `<svg viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32" rx="3" fill="rgba(255,255,255,0.06)"/><path d="M11 11l-4 5 4 5M21 11l4 5-4 5" stroke="rgba(125,211,252,0.5)" stroke-width="1.8" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`

/** langIcon returns the SVG string for a given language, falling back to a generic code icon. */
function langIcon(lang) {
  return LANG_ICONS[lang?.toLowerCase()] ?? LANG_ICON_DEFAULT
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
  const iconHtml = `<span class="code-lang-icon">${langIcon(language)}</span>`
  const result = `<div class="code-block"><div class="code-header">${iconHtml}<span class="code-lang">${language || 'text'}</span><button class="code-copy" onclick="navigator.clipboard.writeText(decodeURIComponent(atob(this.dataset.code)));this.textContent='✓';setTimeout(()=>this.textContent='复制',2000)" data-code="${btoa(encodeURIComponent(text))}">复制</button></div><pre><code>${numbered}</code></pre></div>`
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

/** stripToolCallTags removes <tool-call> and <skill-call> markers from a string (e.g. before TTS). */
export function stripToolCallTags(s) {
  return s
    .replace(/<tool-call[^>]*><\/tool-call>\n*/g, '')
    .replace(/<skill-call[^>]*><\/skill-call>\n*/g, '')
}

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
  const GEAR_ICON = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>`
  const SKILL_ICON = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg>`
  const CHIP_RE = /<span class="tool-call-chip[^"]*"[\s\S]*?<\/span>/g
  // Merge consecutive chip-only <p> elements into a collapsible flex row.
  // Only the first chip shows by default; extra chips are hidden until toggled.
  const groupChips = (h) => h.replace(
    /(?:<p>(?:\s*<span class="tool-call-chip[^"]*"[\s\S]*?<\/span>\s*)+<\/p>\s*)+/g,
    match => {
      const chips = []
      let m
      CHIP_RE.lastIndex = 0
      while ((m = CHIP_RE.exec(match)) !== null) chips.push(m[0])
      if (chips.length <= 1) {
        return `<div class="tool-call-group">${chips.join('')}</div>`
      }
      const visible = chips[0]
      const extra = chips.slice(1).map(c =>
        c.replace('<span class="tool-call-chip', '<span class="tool-call-chip tool-call-chip-extra')
      ).join('')
      const count = chips.length - 1
      return `<div class="tool-call-group collapsed">${visible}${extra}<button class="tool-call-toggle" onclick="window.__toggleChipGroup(this)">+${count} 更多</button></div>`
    }
  )
  const html = groupChips(
    marked(processed)
      .replace(/‍/g, '')
      .replace(/<tool-call name="([^"]*)" args="([^"]*)"><\/tool-call>/g,
        (_, name, args) => `<span class="tool-call-chip tool-call-chip--tool" data-args="${args}" onclick="window.__showToolArgs(event)">${GEAR_ICON}工具: ${name}</span>`
      )
      .replace(/<skill-call name="([^"]*)" args="([^"]*)"><\/skill-call>/g,
        (_, name, args) => `<span class="tool-call-chip tool-call-chip--skill" data-args="${args}" onclick="window.__showToolArgs(event)">${SKILL_ICON}技能: ${name}</span>`
      )
  )
  if (_mdCache.size >= _MD_CACHE_MAX) _mdCache.delete(_mdCache.keys().next().value)
  _mdCache.set(text, html)
  return html
}

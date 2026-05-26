<template>
  <Transition name="lp-reveal">
  <div v-if="preview" class="link-preview" @click="open">
    <!-- Content area -->
    <div class="lp-body">
      <div class="lp-title">{{ stripHtml(preview.title) || preview.url }}</div>
      <div v-if="preview.description" class="lp-desc">{{ stripHtml(preview.description) }}</div>

      <div class="lp-footer">
        <img v-if="faviconUrl" class="lp-favicon" :src="faviconUrl" @error="hideFavicon" />
        <span class="lp-site">{{ preview.siteName || hostname }}</span>
        <svg class="lp-ext-icon" viewBox="0 0 12 12" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path d="M7 1h4v4M11 1 5.5 6.5M2 3h3M2 3v7h7V7" stroke="currentColor" stroke-width="1.2"
            stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </div>
    </div>
  </div>
  </Transition>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { FetchLinkPreview } from '../../wailsjs/go/main/App'
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

const props = defineProps({
  url: { type: String, required: true },
})

// Module-level cache so the same URL is never fetched twice across all message bubbles.
const _previewCache = new Map()

const preview = ref(null)

/** stripHtml removes HTML tags from a string, returning plain text. */
function stripHtml(str) {
  if (!str) return ''
  return str.replace(/<[^>]*>/g, '').trim()
}

const hostname = computed(() => {
  try { return new URL(props.url).hostname } catch { return props.url }
})

/** faviconUrl returns the Google S2 favicon URL for the card's domain. */
const faviconUrl = computed(() => {
  return hostname.value ? `https://www.google.com/s2/favicons?domain=${hostname.value}&sz=32` : null
})

onMounted(async () => {
  if (_previewCache.has(props.url)) {
    preview.value = _previewCache.get(props.url)
    return
  }
  try {
    const data = await FetchLinkPreview(props.url)
    if (data && (data.title || data.description)) {
      _previewCache.set(props.url, data)
      preview.value = data
    } else {
      _previewCache.set(props.url, null)
    }
  } catch {
    // Silently ignore — network errors, blocked sites, etc.
  }
})

/** open navigates to the URL in the default system browser via Wails. */
function open() {
  BrowserOpenURL(props.url)
}

/** hideFavicon hides the favicon if it fails to load. */
function hideFavicon(e) {
  e.target.style.display = 'none'
}
</script>

<style scoped>
.link-preview {
  position: relative;
  margin-top: 8px;
  border-radius: 10px;
  overflow: hidden;
  background: var(--lg-surface-elevated);
  border: 1px solid var(--lg-border-subtle);
  cursor: pointer;
  max-width: 100%;
  transition: border-color 0.15s, background 0.15s;
  backdrop-filter: var(--lg-blur-sm);
  border-left: 3px solid var(--accent);
}

.link-preview:hover {
  background: var(--accent-alpha-08);
  border-color: var(--accent-alpha-20);
}

/* ── Body ──────────────────────────────────────────────────── */
.lp-body {
  padding: 10px 12px 9px;
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}

.lp-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.35;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.lp-desc {
  font-size: 12px;
  color: var(--text-body-muted);
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* ── Footer ────────────────────────────────────────────────── */
.lp-footer {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-top: 3px;
}

.lp-favicon {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  flex-shrink: 0;
  object-fit: contain;
}

.lp-site {
  font-size: 11px;
  color: var(--accent);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
  font-weight: 500;
}

.lp-ext-icon {
  width: 10px;
  height: 10px;
  color: var(--text-tertiary);
  flex-shrink: 0;
  margin-left: auto;
}

.link-preview:hover .lp-ext-icon {
  color: var(--accent);
}

/* Mount animation: fade + slide up from 6px below */
.lp-reveal-enter-active {
  transition: opacity 0.24s ease, transform 0.24s cubic-bezier(0.16, 1, 0.3, 1);
}
.lp-reveal-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
@media (prefers-reduced-motion: reduce) {
  .lp-reveal-enter-active { transition: opacity 0.14s; }
  .lp-reveal-enter-from   { transform: none; }
}
</style>

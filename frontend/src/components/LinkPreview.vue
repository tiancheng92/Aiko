<template>
  <div v-if="preview" class="link-preview" @click="open">
    <img v-if="preview.image" class="lp-image" :src="preview.image" @error="hideImage" />
    <div class="lp-body">
      <div class="lp-title">{{ preview.title || preview.url }}</div>
      <div v-if="preview.description" class="lp-desc">{{ preview.description }}</div>
      <div class="lp-footer">
        <img v-if="faviconUrl" class="lp-favicon" :src="faviconUrl" @error="hideFavicon" />
        <span class="lp-site">{{ preview.siteName || hostname }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { FetchLinkPreview } from '../../wailsjs/go/main/App'
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

const props = defineProps({
  url: { type: String, required: true },
})

const preview = ref(null)

const hostname = computed(() => {
  try { return new URL(props.url).hostname } catch { return props.url }
})

/** faviconUrl returns the Google S2 favicon URL for the card's domain. */
const faviconUrl = computed(() => {
  return hostname.value ? `https://www.google.com/s2/favicons?domain=${hostname.value}&sz=32` : null
})

onMounted(async () => {
  try {
    const data = await FetchLinkPreview(props.url)
    // Only show card if at least a title was found.
    if (data && (data.title || data.description)) {
      preview.value = data
    }
  } catch {
    // Silently ignore — network errors, blocked sites, etc.
  }
})

/** open navigates to the URL in the default system browser via Wails. */
function open() {
  BrowserOpenURL(props.url)
}

/** hideImage removes the broken OG image so the card layout collapses cleanly. */
function hideImage(e) {
  e.target.style.display = 'none'
}

/** hideFavicon hides the favicon if it fails to load. */
function hideFavicon(e) {
  e.target.style.display = 'none'
}
</script>

<style scoped>
.link-preview {
  display: flex;
  align-items: stretch;
  margin-top: 8px;
  border-radius: 10px;
  overflow: hidden;
  background: rgba(30, 32, 40, 0.85);
  border: 1px solid rgba(255, 255, 255, 0.08);
  cursor: pointer;
  max-width: 320px;
  transition: background 0.15s;
  backdrop-filter: blur(8px);
}

.link-preview:hover {
  background: rgba(45, 48, 60, 0.95);
}

.lp-image {
  width: 100px;
  min-width: 100px;
  height: 80px;
  object-fit: cover;
  flex-shrink: 0;
}

.lp-body {
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 4px;
  min-width: 0;
  flex: 1;
}

.lp-title {
  font-size: 12px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.9);
  line-height: 1.3;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.lp-desc {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.45);
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  flex: 1;
}

.lp-footer {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-top: 2px;
}

.lp-favicon {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  flex-shrink: 0;
  object-fit: contain;
}

.lp-site {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.4);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>

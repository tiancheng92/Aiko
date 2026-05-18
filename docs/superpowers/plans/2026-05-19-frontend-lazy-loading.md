# Frontend Startup Optimization — Lazy Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce first-frame JS parse time by lazily loading four non-critical Vue components (Live2DPet, VRMPet, SettingsWindow, ChatPanel) so the pet appears on screen faster.

**Architecture:** Replace synchronous `import` statements in `App.vue` and `ChatBubble.vue` with `defineAsyncComponent(() => import(...))`. Vite automatically splits each dynamic import into a separate chunk that is only downloaded and parsed when the component first renders. Remove now-redundant `manualChunks` entries whose libraries are exclusively used by the lazy-loaded components.

**Tech Stack:** Vue 3 Composition API (`defineAsyncComponent`, `Suspense`), Vite/Rollup (`manualChunks`), Wails v2 desktop app

---

## File Map

| File | Change |
|---|---|
| `frontend/src/App.vue` | Convert `Live2DPet`, `VRMPet`, `SettingsWindow` imports to async |
| `frontend/src/components/ChatBubble.vue` | Convert `ChatPanel` import to async; add `<Suspense>` wrapper |
| `frontend/vite.config.js` | Remove `vendor-pixi`, `vendor-katex`, `vendor-marked`, `vendor-hljs` from `manualChunks` |

---

## Task 1: Lazy-load Live2DPet and VRMPet in App.vue

**Files:**
- Modify: `frontend/src/App.vue` (lines 2–4)

**Background:** `App.vue` currently synchronously imports both render backends. Only one is used per session. `renderBackend` defaults to `'live2d'`; `GetConfig()` async call may switch it to `'vrm'`. Converting both to `defineAsyncComponent` means each chunk is only parsed when its `v-if` condition becomes true.

- [ ] **Step 1: Open `frontend/src/App.vue` and find the import block**

The current lines 2–4 look like:
```js
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import Live2DPet from './components/Live2DPet.vue'
import VRMPet from './components/VRMPet.vue'
```

- [ ] **Step 2: Add `defineAsyncComponent` to the vue import and replace the two static imports**

Change lines 2–4 to:
```js
import { ref, onMounted, onUnmounted, nextTick, watch, defineAsyncComponent } from 'vue'
const Live2DPet = defineAsyncComponent(() => import('./components/Live2DPet.vue'))
const VRMPet = defineAsyncComponent(() => import('./components/VRMPet.vue'))
```

The `v-if="renderBackend === 'live2d'"` and `v-else-if="renderBackend === 'vrm'"` in the template remain unchanged.

- [ ] **Step 3: Build and verify no console errors**

```bash
cd frontend && yarn build 2>&1 | tail -20
```

Expected: build succeeds, output lists separate chunks for `Live2DPet` and `VRMPet` (or their vendor deps), no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.vue
git commit -m "perf: lazy-load Live2DPet and VRMPet to defer render backend parse"
```

---

## Task 2: Lazy-load SettingsWindow in App.vue

**Files:**
- Modify: `frontend/src/App.vue` (line 6)

**Background:** `SettingsWindow` is only shown when the user triggers `settings:open`. It's a 148KB file — lazily loading it removes its template parse from startup.

- [ ] **Step 1: Find the SettingsWindow import (currently line 6)**

```js
import SettingsWindow from './components/SettingsWindow.vue'
```

- [ ] **Step 2: Replace with async component**

```js
const SettingsWindow = defineAsyncComponent(() => import('./components/SettingsWindow.vue'))
```

Note: `defineAsyncComponent` was already added to the vue import in Task 1 — do not duplicate it.

- [ ] **Step 3: Build and verify**

```bash
cd frontend && yarn build 2>&1 | tail -20
```

Expected: build succeeds, `SettingsWindow` appears as a separate chunk (or merged with its deps chunk), no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.vue
git commit -m "perf: lazy-load SettingsWindow to defer settings template parse"
```

---

## Task 3: Lazy-load ChatPanel in ChatBubble.vue with Suspense

**Files:**
- Modify: `frontend/src/components/ChatBubble.vue` (line 3, and template around line 433)

**Background:** `ChatBubble.vue` synchronously imports `ChatPanel`, which in turn imports `marked`, `katex`, and `highlight.js` — ~93KB gzip. These are only needed when the user opens the chat bubble. Wrapping in `<Suspense>` provides a no-op loading state (the content area is invisible until the bubble opens anyway).

- [ ] **Step 1: Open `frontend/src/components/ChatBubble.vue` and find the import (line 3)**

```js
import ChatPanel from './ChatPanel.vue'
```

- [ ] **Step 2: Replace with async import**

```js
import { defineAsyncComponent } from 'vue'
const ChatPanel = defineAsyncComponent(() => import('./ChatPanel.vue'))
```

- [ ] **Step 3: Find the `<ChatPanel>` usage in the template (around line 433)**

Current:
```html
<div class="content">
  <ChatPanel ref="chatPanelRef" />
</div>
```

- [ ] **Step 4: Wrap ChatPanel in Suspense**

```html
<div class="content">
  <Suspense>
    <ChatPanel ref="chatPanelRef" />
    <template #fallback></template>
  </Suspense>
</div>
```

The empty `#fallback` slot means the content area shows nothing during the brief async load — which is fine because the chat area is already styled and the load takes <30ms from local files.

- [ ] **Step 5: Build and verify**

```bash
cd frontend && yarn build 2>&1 | tail -20
```

Expected: build succeeds, `ChatPanel` chunk is separate from the main bundle, no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/ChatBubble.vue
git commit -m "perf: lazy-load ChatPanel to defer marked/katex/hljs parse until chat opens"
```

---

## Task 4: Clean up vite.config.js manualChunks

**Files:**
- Modify: `frontend/vite.config.js`

**Background:** After Tasks 1–3, `pixi.js`, `pixi-live2d-display/cubism4`, `katex`, `marked-katex-extension`, `marked`, and all `highlight.js` language entries are only referenced by lazy-loaded components. Rollup will automatically co-locate them in those component chunks. Keeping them in `manualChunks` would force them into separate eagerly-loaded chunks, defeating the lazy-loading optimization. Only `vendor-vue` should remain since Vue is used synchronously by all components.

- [ ] **Step 1: Open `frontend/vite.config.js`**

Current `manualChunks`:
```js
manualChunks: {
  'vendor-vue':    ['vue'],
  'vendor-pixi':   ['pixi.js', 'pixi-live2d-display/cubism4'],
  'vendor-katex':  ['katex', 'marked-katex-extension'],
  'vendor-marked': ['marked'],
  'vendor-hljs':   [
    'highlight.js/lib/core',
    'highlight.js/lib/languages/javascript',
    'highlight.js/lib/languages/typescript',
    'highlight.js/lib/languages/python',
    'highlight.js/lib/languages/bash',
    'highlight.js/lib/languages/go',
    'highlight.js/lib/languages/json',
    'highlight.js/lib/languages/css',
    'highlight.js/lib/languages/xml',
  ],
},
```

- [ ] **Step 2: Replace with vendor-vue only**

```js
manualChunks: {
  'vendor-vue': ['vue'],
},
```

- [ ] **Step 3: Build and check the output chunk list**

```bash
cd frontend && yarn build 2>&1
```

Expected output will list chunks. Verify:
- `vendor-vue` chunk exists (Vue runtime, eagerly loaded)
- No `vendor-pixi`, `vendor-katex`, `vendor-marked`, `vendor-hljs` chunks (they're now inside the component chunks)
- `index.js` (main chunk) is significantly smaller than the previous ~245KB gzip
- Component chunks for Live2DPet/VRMPet/ChatPanel/SettingsWindow appear in the list

- [ ] **Step 4: Run the app and smoke-test**

```bash
make run
```

Verify:
1. Pet appears on screen (Live2D or VRM depending on config)
2. Chat bubble opens and messages render correctly (markdown, code highlighting, math)
3. Settings window opens correctly
4. No console errors in the Wails webview devtools

- [ ] **Step 5: Commit**

```bash
git add frontend/vite.config.js
git commit -m "perf: remove redundant manualChunks now handled by lazy-loaded component chunks"
```

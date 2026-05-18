# Frontend Startup Optimization — Lazy Loading Design

## Goal

Reduce first-frame JS parse/execution time by lazily loading non-critical components in the Aiko Wails v2 desktop app. Users currently wait for pixi.js, three.js, katex, hljs, and large Vue templates to parse before the pet appears on screen.

## Context

- **App**: Wails v2 desktop app; JS assets served as local embedded files (no network latency)
- **Render backend**: either Live2D (pixi.js) or VRM (three.js) — only one is used per session, determined by `GetConfig()` async call at startup
- **Default**: `renderBackend` starts as `'live2d'`; switches to `'vrm'` after `GetConfig()` returns if configured
- **Bundle today** (gzip): `index.js` 245KB, `vendor-pixi.js` 163KB, `vendor-katex.js` 75KB, `vendor-hljs.js` 18KB, `vendor-vue.js` 30KB

## Approach

Replace synchronous `import` statements in `App.vue` and `ChatBubble.vue` with `defineAsyncComponent(() => import(...))` for the four non-critical components. Vite automatically splits each dynamic import into a separate chunk that is only parsed when the component first needs to render.

Remove redundant `manualChunks` entries for libraries that are now only used by lazy-loaded components—Rollup will co-locate them in the component chunk automatically. Retain only `vendor-vue`.

## Components to Lazy-Load

| Component | File | Changed In | Trigger for first load |
|---|---|---|---|
| `Live2DPet` | `components/Live2DPet.vue` | `App.vue` | `renderBackend === 'live2d'` (default, loads during GetConfig async gap) |
| `VRMPet` | `components/VRMPet.vue` | `App.vue` | `renderBackend === 'vrm'` (only after GetConfig returns) |
| `SettingsWindow` | `components/SettingsWindow.vue` | `App.vue` | `settings:open` event |
| `ChatPanel` | `components/ChatPanel.vue` | `ChatBubble.vue` | Chat bubble first opens |

## Changes Required

### `frontend/src/App.vue`

Replace:
```js
import Live2DPet from './components/Live2DPet.vue'
import VRMPet from './components/VRMPet.vue'
import SettingsWindow from './components/SettingsWindow.vue'
```

With:
```js
import { defineAsyncComponent } from 'vue'
const Live2DPet = defineAsyncComponent(() => import('./components/Live2DPet.vue'))
const VRMPet = defineAsyncComponent(() => import('./components/VRMPet.vue'))
const SettingsWindow = defineAsyncComponent(() => import('./components/SettingsWindow.vue'))
```

No template changes needed — `v-if` conditions remain the same.

### `frontend/src/components/ChatBubble.vue`

Replace:
```js
import ChatPanel from './ChatPanel.vue'
```

With:
```js
import { defineAsyncComponent } from 'vue'
const ChatPanel = defineAsyncComponent(() => import('./ChatPanel.vue'))
```

Wrap the `<ChatPanel>` usage in a `<Suspense>` block with an empty or minimal loading fallback (the chat area is already hidden until the bubble opens, so a brief loading state is invisible to users).

### `frontend/vite.config.js`

Remove `vendor-pixi`, `vendor-katex`, `vendor-marked`, `vendor-hljs` from `manualChunks` — these libraries now belong to the component chunks that import them. Retain only `vendor-vue`.

```js
manualChunks: {
  'vendor-vue': ['vue'],
}
```

## Expected Outcome

- **Live2D users**: eliminate ~163KB three.js parse at startup (VRM chunk never loaded)
- **VRM users**: eliminate ~163KB pixi.js parse at startup (Live2D chunk loads during GetConfig async gap, typically <30ms)
- **All users**: defer ~93KB katex+hljs parse until chat opens; defer SettingsWindow template parse until settings opens
- `index.js` main chunk shrinks significantly; first-frame render unblocked

## Risk Assessment

- **Live2D flash risk**: Low. `renderBackend` defaults to `'live2d'`, so Live2DPet triggers immediately. `GetConfig()` is async — the Live2D chunk loads in parallel during this gap. Local file serving means chunk load is ~10–30ms.
- **SettingsWindow**: Zero risk — only renders on explicit user action.
- **ChatPanel**: Zero risk — only renders when chat bubble opens.
- **VRM**: Zero risk — never renders until GetConfig confirms VRM backend.

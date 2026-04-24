# Voice Auto-Send Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "语音消息立刻发送" toggle so that releasing the Option key after voice recording automatically sends the transcribed message once the final STT result arrives.

**Architecture:** A new `FINAL:` prefix in the ObjC voice pipe signals when SFSpeechRecognition has produced its isFinal result; the Go goroutine emits a `voice:final` Wails event; ChatPanel listens and calls `send()` if the `VoiceAutoSend` config flag is true. Config is persisted via the existing `SaveConfig` / `GetConfig` pattern and toggled in SettingsWindow.

**Tech Stack:** Objective-C CGO (`macos.go`), Go (`macos.go`, `app.go`, `internal/config/config.go`), Vue 3 `<script setup>` (`ChatPanel.vue`, `SettingsWindow.vue`), Wails v2 events.

---

## File Structure

| File | Change |
|---|---|
| `macos.go` | ObjC: emit `FINAL:<text>` when `result.isFinal`. Go: add `FINAL:` branch to voice pipe goroutine → emit `voice:final` |
| `internal/config/config.go` | Add `VoiceAutoSend bool` field; Load/Save `voice_auto_send` key |
| `app.go` | Preserve `VoiceAutoSend` in `SaveConfig`; add `GetVoiceAutoSend` / `SetVoiceAutoSend` methods |
| `frontend/src/components/ChatPanel.vue` | Import `GetVoiceAutoSend`; add `voiceAutoSend` ref; listen `voice:final`; auto-call `send()` |
| `frontend/src/components/SettingsWindow.vue` | Add toggle row; load/save `VoiceAutoSend` via `cfg` object (same path as other config fields) |

---

### Task 1: ObjC — emit FINAL: prefix for isFinal results

**Files:**
- Modify: `macos.go` (ObjC resultHandler, lines ~471-478)

The current resultHandler sends every partial result identically. We need to prefix the final result so Go can distinguish it.

- [ ] **Step 1: Locate the resultHandler block**

Open `macos.go`. Find this block (around line 471):

```objc
if (result) {
    NSString *text = result.bestTranscription.formattedString;
    sendVoiceText([text UTF8String]);
}
```

- [ ] **Step 2: Replace with isFinal-aware version**

Replace the block above with:

```objc
if (result) {
    NSString *text = result.bestTranscription.formattedString;
    if (result.isFinal) {
        NSString *msg = [NSString stringWithFormat:@"FINAL:%@", text];
        sendVoiceText([msg UTF8String]);
    } else {
        sendVoiceText([text UTF8String]);
    }
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add macos.go
git commit -m "feat(voice): emit FINAL: prefix on isFinal STT result"
```

---

### Task 2: Go — handle FINAL: in voice pipe goroutine → emit voice:final

**Files:**
- Modify: `macos.go` (Go voice pipe goroutine, lines ~626-630)

The goroutine currently handles `ERROR:` and falls through to `voice:transcript`. Add a `FINAL:` branch.

- [ ] **Step 1: Locate the pipe dispatch block**

In `macos.go`, find the Go voice pipe goroutine with this code (around line 626):

```go
text := string(textBuf)
if len(text) > 6 && text[:6] == "ERROR:" {
    wailsruntime.EventsEmit(globalAppCtx, "voice:error", text[6:])
} else {
    wailsruntime.EventsEmit(globalAppCtx, "voice:transcript", text)
}
```

- [ ] **Step 2: Add FINAL: branch**

Add `"strings"` to the import if not already present (check: `macos.go` already imports `"strings"` — if not, add it). Replace the dispatch block with:

```go
text := string(textBuf)
if strings.HasPrefix(text, "FINAL:") {
    wailsruntime.EventsEmit(globalAppCtx, "voice:final", text[6:])
} else if len(text) > 6 && text[:6] == "ERROR:" {
    wailsruntime.EventsEmit(globalAppCtx, "voice:error", text[6:])
} else {
    wailsruntime.EventsEmit(globalAppCtx, "voice:transcript", text)
}
```

- [ ] **Step 3: Check strings import**

```bash
grep '"strings"' macos.go
```

If no output, add `"strings"` to the import block at the top of `macos.go`.

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add macos.go
git commit -m "feat(voice): route FINAL: pipe messages to voice:final Wails event"
```

---

### Task 3: Config — add VoiceAutoSend field

**Files:**
- Modify: `internal/config/config.go`

Follow the exact same pattern as `SMSWatcherEnabled` which was added just above.

- [ ] **Step 1: Add field to Config struct**

In `internal/config/config.go`, find the `Config` struct. After the `SMSWatcherEnabled` line, add:

```go
VoiceAutoSend      bool   // 语音识别完成后是否自动发送消息
```

- [ ] **Step 2: Add Load**

In the `Load()` function, after `cfg.SMSWatcherEnabled = m["sms_watcher_enabled"] == "true"`, add:

```go
cfg.VoiceAutoSend = m["voice_auto_send"] == "true"
```

- [ ] **Step 3: Add Save**

In the `Save()` function's `pairs` map, after `"sms_watcher_enabled": strconv.FormatBool(cfg.SMSWatcherEnabled),`, add:

```go
"voice_auto_send": strconv.FormatBool(cfg.VoiceAutoSend),
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./internal/config/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add VoiceAutoSend field"
```

---

### Task 4: app.go — SaveConfig guard + GetVoiceAutoSend / SetVoiceAutoSend

**Files:**
- Modify: `app.go`

Two things: (1) prevent `SaveConfig` from clobbering `VoiceAutoSend` (same pattern as `SMSWatcherEnabled`); (2) add two exported methods for the frontend.

- [ ] **Step 1: Guard VoiceAutoSend in SaveConfig**

In `app.go`, find `SaveConfig`. It already has:

```go
cfg.SMSWatcherEnabled = a.cfg.SMSWatcherEnabled
```

Add immediately after that line:

```go
cfg.VoiceAutoSend = a.cfg.VoiceAutoSend
```

- [ ] **Step 2: Add GetVoiceAutoSend**

After the `IsSMSWatcherRunning` method (around line 911), add:

```go
// GetVoiceAutoSend returns whether voice messages are sent automatically
// after the final STT result arrives.
func (a *App) GetVoiceAutoSend() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.VoiceAutoSend
}
```

- [ ] **Step 3: Add SetVoiceAutoSend**

Immediately after `GetVoiceAutoSend`, add:

```go
// SetVoiceAutoSend sets the voice auto-send flag and persists it.
func (a *App) SetVoiceAutoSend(enabled bool) error {
	a.mu.Lock()
	a.cfg.VoiceAutoSend = enabled
	a.mu.Unlock()
	return a.configStore.Save(a.cfg)
}
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Regenerate Wails bindings**

```bash
wails generate module
```

Expected: `frontend/src/wailsjs/go/main/App.js` and `App.d.ts` now include `GetVoiceAutoSend` and `SetVoiceAutoSend`.

- [ ] **Step 6: Commit**

```bash
git add app.go frontend/src/wailsjs/
git commit -m "feat(app): add GetVoiceAutoSend/SetVoiceAutoSend; guard SaveConfig"
```

---

### Task 5: ChatPanel.vue — listen voice:final and auto-send

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: Import GetVoiceAutoSend**

Find the existing import from wailsjs (around line 1-10). It currently imports `SendMessage` and others. Add `GetVoiceAutoSend`:

```js
import { SendMessage, GetVoiceAutoSend } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: Add voiceAutoSend reactive ref**

Find where `const voiceHint = ref('')` is declared (around line 93). After it, add:

```js
const voiceAutoSend = ref(false)
```

- [ ] **Step 3: Load initial value in onMounted**

Inside `onMounted`, find where the existing `EventsOn('voice:start', ...)` handler is set up. Before that block, add:

```js
try { voiceAutoSend.value = await GetVoiceAutoSend() } catch {}
```

- [ ] **Step 4: Listen for config:voice:auto-send:changed**

Inside `onMounted`, after the `voice:error` handler block, add:

```js
EventsOn('config:voice:auto-send:changed', (val) => {
  voiceAutoSend.value = val
})
```

- [ ] **Step 5: Add voice:final handler**

Inside `onMounted`, after the `voice:end` handler block, add:

```js
EventsOn('voice:final', (text) => {
  input.value = text
  voiceHint.value = ''
  if (voiceAutoSend.value && text.trim()) {
    send()
  }
})
```

- [ ] **Step 6: Verify dev build compiles**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(frontend): auto-send on voice:final when voiceAutoSend enabled"
```

---

### Task 6: SettingsWindow.vue — add toggle UI

**Files:**
- Modify: `frontend/src/components/SettingsWindow.vue`

- [ ] **Step 1: Import SetVoiceAutoSend**

Find the existing import line (around line 5):

```js
import { GetConfig, SaveConfig, ... } from '../../wailsjs/go/main/App'
```

Add `GetVoiceAutoSend, SetVoiceAutoSend` to the import.

- [ ] **Step 2: cfg already carries VoiceAutoSend — verify**

The settings form uses `cfg.value` which is loaded via `GetConfig()`. Since `GetConfig` returns the full `Config` struct (which now includes `VoiceAutoSend`), and `SaveConfig` is called with the full `cfg.value` spread, `VoiceAutoSend` will be persisted automatically through the existing `save()` function — **no extra save logic needed**.

Verify that `cfg.value` is initialized with a default:

Find the `const cfg = ref({...})` initialization in SettingsWindow.vue and add `VoiceAutoSend: false` to the default object.

- [ ] **Step 3: Add toggleVoiceAutoSend handler**

Find `toggleSMSWatcher` function (around line 544). After it, add:

```js
/** toggleVoiceAutoSend updates voice auto-send setting immediately and notifies ChatPanel. */
async function toggleVoiceAutoSend() {
  try {
    await SetVoiceAutoSend(cfg.value.VoiceAutoSend)
    EventsEmit('config:voice:auto-send:changed', cfg.value.VoiceAutoSend)
  } catch (e) {
    console.warn('toggleVoiceAutoSend failed:', e)
  }
}
```

- [ ] **Step 4: Add toggle row to the UI**

Find the SMS watcher section in the template. It looks like a tab pane with a `.sms-toggle-row`. In the same settings tab (or immediately after the SMS section), add:

```html
<!-- Voice auto-send -->
<div class="settings-row">
  <div class="settings-row-label">
    <span class="settings-row-title">语音消息立刻发送</span>
    <span class="settings-row-desc">释放 Option 键后，等待转录完成并自动发送消息</span>
  </div>
  <label class="toggle-switch">
    <input
      type="checkbox"
      v-model="cfg.value.VoiceAutoSend"
      @change="toggleVoiceAutoSend"
    />
    <span class="toggle-slider"></span>
  </label>
</div>
```

Note: look at the existing toggle-switch markup in the SMS section and copy the exact class names used there for consistency.

- [ ] **Step 5: Verify the existing toggle-switch CSS class**

```bash
grep -n "toggle-switch\|toggle-slider" frontend/src/components/SettingsWindow.vue | head -10
```

Confirm the class names match what was added in Step 4. Adjust if needed.

- [ ] **Step 6: Verify dev build**

```bash
cd frontend && yarn build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/SettingsWindow.vue
git commit -m "feat(settings): add voice auto-send toggle"
```

---

### Task 7: Manual end-to-end verification

No automated test harness exists for voice (requires microphone). Verify manually.

- [ ] **Step 1: Start dev mode**

```bash
wails dev
```

- [ ] **Step 2: Test toggle persists**

Open Settings → find "语音消息立刻发送" toggle → enable it → close Settings → reopen Settings → confirm toggle is still on.

- [ ] **Step 3: Test with voiceAutoSend OFF (default behavior unchanged)**

Ensure toggle is OFF. Long-press Option → speak → release. Confirm transcribed text appears in input box but is NOT automatically sent.

- [ ] **Step 4: Test with voiceAutoSend ON**

Enable toggle. Long-press Option → speak a short phrase → release. Confirm:
1. Recording animation stops immediately on release.
2. Input box fills with transcribed text.
3. Message is sent automatically (appears in chat) without pressing Enter.

- [ ] **Step 5: Test empty transcript guard**

Enable toggle. Long-press Option → say nothing → release. Confirm no empty message is sent.

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "chore: voice auto-send feature complete"
```

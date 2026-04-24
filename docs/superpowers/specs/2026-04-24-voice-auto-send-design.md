# Voice Auto-Send Implementation Design

## Goal

Add a "语音消息立刻发送" toggle to Settings. When enabled, releasing the Option key after voice recording automatically sends the transcribed message — but only after the final (isFinal) STT result arrives.

## Architecture

The feature adds one new event (`voice:final`) to the existing voice pipeline, and one new config field (`VoiceAutoSend`). No existing behavior is changed.

### Data flow

```
Option released
  → Go hotkey pipe: emit voice:end       → frontend: stops recording UI
  → ObjC: stopVoiceRecognition()
      → [gRecogTask finish]
          → resultHandler (isFinal=YES)  → sendVoiceText("FINAL:<text>")
              → Go voice pipe goroutine  → emit voice:final("<text>")
                  → ChatPanel.vue        → input.value = text
                                         → if voiceAutoSend → send()
```

### Why voice:end and voice:final are separate

`voice:end` fires immediately on Option release and stops the recording animation. `voice:final` arrives asynchronously after the SFSpeechRecognition framework finalizes its result. Keeping them separate means the UI is always responsive and the auto-send only fires when the transcript is actually ready.

---

## Files and Changes

### `macos.go` — ObjC resultHandler

In the CGO block, the `resultHandler` currently calls `sendVoiceText([text UTF8String])` for every partial result. Change: when `result.isFinal == YES`, prepend `FINAL:` to the message:

```objc
if (result.isFinal) {
    NSString *msg = [NSString stringWithFormat:@"FINAL:%@", text];
    sendVoiceText([msg UTF8String]);
} else {
    sendVoiceText([text UTF8String]);
}
```

### `macos.go` — Go voice pipe goroutine

Currently the goroutine checks for `ERROR:` prefix. Add a `FINAL:` check before the existing else branch:

```go
if strings.HasPrefix(text, "FINAL:") {
    wailsruntime.EventsEmit(globalAppCtx, "voice:final", text[6:])
} else if len(text) > 6 && text[:6] == "ERROR:" {
    wailsruntime.EventsEmit(globalAppCtx, "voice:error", text[6:])
} else {
    wailsruntime.EventsEmit(globalAppCtx, "voice:transcript", text)
}
```

### `internal/config/config.go`

Add to `Config` struct:
```go
VoiceAutoSend bool // 语音识别完成后自动发送
```

Add to `Load()`:
```go
cfg.VoiceAutoSend = m["voice_auto_send"] == "true"
```

Add to `Save()` pairs map:
```go
"voice_auto_send": strconv.FormatBool(cfg.VoiceAutoSend),
```

### `app.go`

`SaveConfig()` must preserve `VoiceAutoSend` (same pattern as `SMSWatcherEnabled`):
```go
cfg.VoiceAutoSend = a.cfg.VoiceAutoSend
```

Add two exported methods for the frontend:
```go
// GetVoiceAutoSend returns whether voice auto-send is enabled.
func (a *App) GetVoiceAutoSend() bool

// SetVoiceAutoSend sets the voice auto-send flag and persists it.
func (a *App) SetVoiceAutoSend(enabled bool) error
```

`SetVoiceAutoSend` updates `a.cfg.VoiceAutoSend` under `a.mu.Lock()`, then calls `a.configStore.Save(a.cfg)`.

### `frontend/src/components/ChatPanel.vue`

1. Import `GetVoiceAutoSend` from wailsjs bindings.
2. Add reactive state: `const voiceAutoSend = ref(false)`
3. In `onMounted`, load initial value: `voiceAutoSend.value = await GetVoiceAutoSend()`
4. Listen for config change event so the setting takes effect without reload:
   ```js
   EventsOn('config:voice:auto-send:changed', (val) => { voiceAutoSend.value = val })
   ```
5. Add `voice:final` handler:
   ```js
   EventsOn('voice:final', (text) => {
     input.value = text
     voiceHint.value = ''
     if (voiceAutoSend.value && text.trim()) {
       send()
     }
   })
   ```

### `frontend/src/components/SettingsWindow.vue`

In the appropriate settings tab (通用 or a dedicated 语音 section), add a toggle row:

```
语音消息立刻发送   [toggle]
开启后，释放 Option 键并等待转录完成后自动发送消息
```

- On mount: read initial value via `GetVoiceAutoSend()`
- On toggle change: call `SetVoiceAutoSend(newVal)`, then emit `config:voice:auto-send:changed` so ChatPanel picks up the change immediately

---

## Error / Edge Cases

- **STT fails after voice:end** — `voice:final` never arrives. No auto-send. User sees the last partial result in the input box and can send manually. No change needed.
- **Empty transcript** — `voice:final` arrives with empty string. Guard `text.trim()` prevents sending an empty message.
- **voiceAutoSend disabled** — `voice:final` still fires and updates `input.value` with the final (more accurate) text, but does not call `send()`. This is a free improvement over the current behavior where only partial results fill the input.
- **User manually edits input while waiting for voice:final** — `voice:final` overwrites it. Acceptable UX: the recording animation is active so the user knows voice is in progress.

---

## Out of Scope

- No debouncing or timeout fallback — if `isFinal` never fires (framework bug), the user sends manually.
- No per-language STT behavior changes.
- No changes to the double-click Option toggle behavior.

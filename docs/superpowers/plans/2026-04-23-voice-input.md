# Voice Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 长按 Option 键（≥1s）全局触发语音输入，使用 macOS 原生 SFSpeechRecognizer 实时转文字并显示在聊天对话框中，录音期间显示 Siri 风格屏幕边框彩虹光 + 居中圆形波形特效。

**Architecture:** 在现有 `macos.go` 的 CGO Objective-C 层扩展 Option 键监控（时长区分双击/长按），新增 AVAudioEngine + SFSpeechRecognizer STT 管道，通过 CGO exported callback 传递实时识别文字给 Go，Go 通过 Wails events 推送前端；前端 App.vue 负责视觉特效，ChatPanel.vue 负责实时文字展示。

**Tech Stack:** Go CGO + Objective-C, AVAudioEngine, SFSpeechRecognizer, Wails v2 Events, Vue 3 Composition API, CSS animations

---

## File Map

| 文件 | 操作 | 职责 |
|------|------|------|
| `macos.go` | 修改 | 扩展 Option 长按检测；添加 STT Objective-C 实现；添加 CGO callback；扩展 pipe 读取 switch |
| `build/darwin/Info.plist` | 修改 | 新增麦克风 + 语音识别权限描述字段 |
| `frontend/src/App.vue` | 修改 | 新增 `voiceActive` 状态、Siri 边框光、居中波形、`voice:start`/`voice:end`/`voice:error` 事件监听 |
| `frontend/src/components/ChatPanel.vue` | 修改 | 新增 `voice:transcript` 事件监听、实时文字更新逻辑、录音状态条渲染 |

---

## Task 1：`build/darwin/Info.plist` — 新增权限描述

**Files:**
- Modify: `build/darwin/Info.plist`

- [ ] **Step 1: 在 `NSAccessibilityUsageDescription` 条目之后插入两个新权限键**

打开 `build/darwin/Info.plist`，在 `</dict>` 结束标签之前，`NSHumanReadableCopyright` 键之后插入：

```xml
        <key>NSMicrophoneUsageDescription</key>
        <string>Aiko 需要麦克风权限以接收语音输入。</string>
        <key>NSSpeechRecognitionUsageDescription</key>
        <string>Aiko 需要语音识别权限以将您的语音转换为文字。</string>
```

- [ ] **Step 2: 验证 plist 格式正确**

```bash
cd /Users/xutiancheng/code/self/Aiko
plutil -lint build/darwin/Info.plist
```

Expected: `build/darwin/Info.plist: OK`

- [ ] **Step 3: Commit**

```bash
git add build/darwin/Info.plist
git commit -m "feat: add microphone and speech recognition usage descriptions to Info.plist"
```

---

## Task 2：`macos.go` — CGO callback 基础设施

**Files:**
- Modify: `macos.go`

这个 task 只添加 CGO callback 函数和相关全局变量，不涉及录音逻辑，以便独立验证 CGO 编译正确性。

- [ ] **Step 1: 在 `macos.go` 的 CGO 头部（`import "C"` 之前的注释块内）添加必要 framework 链接和 import**

找到现有的 `#cgo LDFLAGS:` 行：
```
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework ApplicationServices
```

替换为：
```
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework ApplicationServices -framework AVFoundation -framework Speech
```

并在 `#import` 区块末尾添加：
```objc
#import <AVFoundation/AVFoundation.h>
#import <Speech/Speech.h>
```

- [ ] **Step 2: 在 CGO 注释块内、`enableClickThrough` 函数之前，添加全局 STT 变量声明**

```objc
// --- Voice Recognition globals ---
static SFSpeechRecognizer        *gSpeechRecognizer  = nil;
static SFSpeechAudioBufferRecognitionRequest *gRecogRequest = nil;
static SFSpeechRecognitionTask   *gRecogTask         = nil;
static AVAudioEngine             *gAudioEngine       = nil;
```

- [ ] **Step 3: 在 CGO 注释块内添加 forward declaration，供 ObjC 代码调用 Go 回调**

紧接全局变量之后添加：
```objc
// Forward declaration — implemented as CGO export in Go.
extern void voiceTranscriptCallback(const char *text);
```

- [ ] **Step 4: 验证 CGO 编译通过**

```bash
cd /Users/xutiancheng/code/self/Aiko
go build ./...
```

Expected: 无错误输出。

- [ ] **Step 5: Commit**

```bash
git add macos.go
git commit -m "feat: add STT globals and CGO framework links to macos.go"
```

---

## Task 3：`macos.go` — Objective-C STT 实现

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: 在 CGO 注释块内添加 `startVoiceRecognition` 函数**

在 `enableClickThrough` 静态函数之前插入：

```objc
// startVoiceRecognition requests permissions and starts streaming STT.
// Results are delivered via voiceTranscriptCallback().
static void startVoiceRecognition() {
    dispatch_async(dispatch_get_main_queue(), ^{
        // Check microphone permission
        if (@available(macOS 14.0, *)) {
            AVAudioApplication *audioApp = [AVAudioApplication sharedInstance];
            AVAudioApplicationRecordPermission perm = [audioApp recordPermission];
            if (perm == AVAudioApplicationRecordPermissionUndetermined) {
                [audioApp requestRecordPermissionWithCompletionHandler:^(BOOL granted) {
                    if (granted) {
                        startVoiceRecognition();
                    } else {
                        voiceTranscriptCallback("ERROR:mic_denied");
                    }
                }];
                return;
            } else if (perm == AVAudioApplicationRecordPermissionDenied) {
                voiceTranscriptCallback("ERROR:mic_denied");
                return;
            }
        } else {
            AVAuthorizationStatus micStatus = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
            if (micStatus == AVAuthorizationStatusNotDetermined) {
                [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
                    if (granted) {
                        startVoiceRecognition();
                    } else {
                        voiceTranscriptCallback("ERROR:mic_denied");
                    }
                }];
                return;
            } else if (micStatus == AVAuthorizationStatusDenied || micStatus == AVAuthorizationStatusRestricted) {
                voiceTranscriptCallback("ERROR:mic_denied");
                return;
            }
        }

        // Check speech recognition permission
        SFSpeechRecognizerAuthorizationStatus speechStatus = [SFSpeechRecognizer authorizationStatus];
        if (speechStatus == SFSpeechRecognizerAuthorizationStatusNotDetermined) {
            [SFSpeechRecognizer requestAuthorization:^(SFSpeechRecognizerAuthorizationStatus status) {
                if (status == SFSpeechRecognizerAuthorizationStatusAuthorized) {
                    startVoiceRecognition();
                } else {
                    voiceTranscriptCallback("ERROR:speech_denied");
                }
            }];
            return;
        } else if (speechStatus != SFSpeechRecognizerAuthorizationStatusAuthorized) {
            voiceTranscriptCallback("ERROR:speech_denied");
            return;
        }

        // Initialize recognizer (prefer zh-CN, fallback to device locale)
        gSpeechRecognizer = [[SFSpeechRecognizer alloc] initWithLocale:[NSLocale localeWithLocaleIdentifier:@"zh-CN"]];
        if (!gSpeechRecognizer || !gSpeechRecognizer.available) {
            gSpeechRecognizer = [SFSpeechRecognizer new];
        }
        gSpeechRecognizer.defaultTaskHint = SFSpeechRecognitionTaskHintDictation;

        gAudioEngine = [AVAudioEngine new];
        gRecogRequest = [SFSpeechAudioBufferRecognitionRequest new];
        gRecogRequest.shouldReportPartialResults = YES;

        AVAudioInputNode *inputNode = gAudioEngine.inputNode;
        AVAudioFormat *fmt = [inputNode outputFormatForBus:0];

        [inputNode installTapOnBus:0 bufferSize:1024 format:fmt block:^(AVAudioPCMBuffer *buf, AVAudioTime *when) {
            [gRecogRequest appendAudioPCMBuffer:buf];
        }];

        NSError *startErr = nil;
        [gAudioEngine startAndReturnError:&startErr];
        if (startErr) {
            NSString *msg = [NSString stringWithFormat:@"ERROR:audio_engine:%@", startErr.localizedDescription];
            voiceTranscriptCallback([msg UTF8String]);
            return;
        }

        gRecogTask = [gSpeechRecognizer recognitionTaskWithRequest:gRecogRequest
            resultHandler:^(SFSpeechRecognitionResult *result, NSError *err) {
                if (err) {
                    // Ignore cancellation errors (code 301) — they fire on normal stop
                    if (err.code != 301) {
                        NSString *msg = [NSString stringWithFormat:@"ERROR:recognition:%@", err.localizedDescription];
                        voiceTranscriptCallback([msg UTF8String]);
                    }
                    return;
                }
                if (result) {
                    NSString *text = result.bestTranscription.formattedString;
                    voiceTranscriptCallback([text UTF8String]);
                }
            }];
    });
}
```

- [ ] **Step 2: 添加 `stopVoiceRecognition` 函数**

紧接 `startVoiceRecognition` 之后插入：

```objc
// stopVoiceRecognition ends the STT task and tears down the audio engine.
static void stopVoiceRecognition() {
    dispatch_async(dispatch_get_main_queue(), ^{
        [gRecogTask finish];
        gRecogTask = nil;

        if (gAudioEngine.running) {
            [gAudioEngine.inputNode removeTapOnBus:0];
            [gAudioEngine stop];
        }
        [gRecogRequest endAudio];
        gRecogRequest = nil;
        gAudioEngine = nil;
        gSpeechRecognizer = nil;
    });
}
```

- [ ] **Step 3: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko
go build ./...
```

Expected: 无错误。

- [ ] **Step 4: Commit**

```bash
git add macos.go
git commit -m "feat: add startVoiceRecognition and stopVoiceRecognition ObjC functions"
```

---

## Task 4：`macos.go` — Option 长按状态机 + Go 侧集成

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: 在 Go 部分（`import "C"` 之后）添加 CGO exported callback**

在现有 `import` 块（`"log/slog"`, `"syscall"`, `wailsruntime`）中加入 `"strings"`，然后在 `enableClickThrough()` Go 函数之前添加：

```go
// voiceTranscriptCallback is called from Objective-C on the main thread when
// a partial or final STT result is available. It must be a CGO export.
//
//export voiceTranscriptCallback
func voiceTranscriptCallback(text *C.char) {
	if globalAppCtx == nil {
		return
	}
	t := C.GoString(text)
	if strings.HasPrefix(t, "ERROR:") {
		wailsruntime.EventsEmit(globalAppCtx, "voice:error", t[6:])
		return
	}
	wailsruntime.EventsEmit(globalAppCtx, "voice:transcript", t)
}
```

- [ ] **Step 2: 在 Objective-C 的 `registerGlobalHotkey` 函数中扩展 Option 长按检测**

找到现有 handler block 内容，当前逻辑是：
```objc
    __block NSTimeInterval lastOptUp = 0;
    __block BOOL optWasDown = NO;
    __block BOOL justTriggered = NO;
    const NSTimeInterval kDoubleTapInterval = 0.5;
```

在这几行之后、handler block 定义之前，添加长按相关变量：
```objc
    __block BOOL gLongPressTriggered = NO;
    __block dispatch_block_t gLongPressBlock = nil;
```

然后找到 handler block 内 `optDown && !optWasDown` 分支（双击检测逻辑），在其中加入长按定时器。原来的分支为：

```objc
        } else if (optDown && !optWasDown) {
            if (lastOptUp > 0) {
                NSTimeInterval now = [NSDate timeIntervalSinceReferenceDate];
                if (now - lastOptUp <= kDoubleTapInterval) {
                    if (gHotkeyPipeFd >= 0) {
                        char b = 1;
                        write(gHotkeyPipeFd, &b, 1);
                    }
                    justTriggered = YES;
                }
                lastOptUp = 0;
            }
        }
```

替换为：

```objc
        } else if (optDown && !optWasDown) {
            // Start long-press timer (1 second)
            dispatch_block_t blk = dispatch_block_create(0, ^{
                if (optWasDown && !gLongPressTriggered) {
                    gLongPressTriggered = YES;
                    lastOptUp = 0; // cancel double-tap window
                    if (gHotkeyPipeFd >= 0) {
                        char b = 2;
                        write(gHotkeyPipeFd, &b, 1);
                    }
                }
            });
            gLongPressBlock = blk;
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.0 * NSEC_PER_SEC)),
                           dispatch_get_main_queue(), blk);

            // Double-tap detection (only if long-press not yet triggered)
            if (lastOptUp > 0) {
                NSTimeInterval now = [NSDate timeIntervalSinceReferenceDate];
                if (now - lastOptUp <= kDoubleTapInterval) {
                    // Cancel the long-press timer — this is a double-tap, not a hold
                    if (gLongPressBlock) {
                        dispatch_block_cancel(gLongPressBlock);
                        gLongPressBlock = nil;
                    }
                    if (gHotkeyPipeFd >= 0) {
                        char b = 1;
                        write(gHotkeyPipeFd, &b, 1);
                    }
                    justTriggered = YES;
                }
                lastOptUp = 0;
            }
        }
```

然后找到 `optUp` 分支：
```objc
        if (optUp) {
            if (justTriggered) {
                // Release of the triggering press — skip, don't start a new window.
                justTriggered = NO;
            } else if (lastOptUp == 0) {
                lastOptUp = [NSDate timeIntervalSinceReferenceDate];
            }
        }
```

替换为：
```objc
        if (optUp) {
            // Cancel pending long-press timer (released before 1s)
            if (gLongPressBlock) {
                dispatch_block_cancel(gLongPressBlock);
                gLongPressBlock = nil;
            }
            if (gLongPressTriggered) {
                // Long-press release → stop recording
                gLongPressTriggered = NO;
                if (gHotkeyPipeFd >= 0) {
                    char b = 3;
                    write(gHotkeyPipeFd, &b, 1);
                }
            } else if (justTriggered) {
                justTriggered = NO;
            } else if (lastOptUp == 0) {
                lastOptUp = [NSDate timeIntervalSinceReferenceDate];
            }
        }
```

- [ ] **Step 3: 在 Go 的 `registerGlobalHotkey` 函数中扩展 pipe 读取 switch**

找到现有的 goroutine 读取循环：
```go
		if globalAppCtx != nil {
			C.activateApp()
			wailsruntime.EventsEmit(globalAppCtx, "bubble:toggle")
		}
```

替换为：
```go
		if globalAppCtx == nil {
			continue
		}
		C.activateApp()
		switch buf[0] {
		case 1:
			// 双击 Option — 切换气泡（现有行为）
			wailsruntime.EventsEmit(globalAppCtx, "bubble:toggle")
		case 2:
			// 长按 Option ≥1s — 开始录音
			wailsruntime.EventsEmit(globalAppCtx, "voice:start")
			C.startVoiceRecognition()
		case 3:
			// Option 释放 — 停止录音
			C.stopVoiceRecognition()
			wailsruntime.EventsEmit(globalAppCtx, "voice:end")
		}
```

- [ ] **Step 4: 确认 `strings` 已加入 import**

检查 `import` 块是否已包含 `"strings"`，若无则添加。

- [ ] **Step 5: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko
go build ./...
```

Expected: 无错误。

- [ ] **Step 6: Commit**

```bash
git add macos.go
git commit -m "feat: add Option long-press detection and STT pipe integration"
```

---

## Task 5：`frontend/src/App.vue` — Siri 边框光 + 圆形波形

**Files:**
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: 在 `<script setup>` 中添加 `voiceActive` 状态和事件监听**

在现有 `let offToggle, offToken, offDone, offError, offSettings` 声明之后添加：
```js
const voiceActive = ref(false)
let offVoiceStart, offVoiceEnd, offVoiceError
```

在 `onMounted` 内、`offSettings = EventsOn(...)` 之后添加：
```js
  offVoiceStart = EventsOn('voice:start', () => {
    // 若气泡未打开则自动打开
    if (!bubbleOpen.value) {
      bubbleOpen.value = true
      nextTick(() => {
        chatBubbleRef.value?.focusInput()
        chatBubbleRef.value?.scrollToBottom()
      })
    }
    voiceActive.value = true
  })

  offVoiceEnd = EventsOn('voice:end', () => {
    voiceActive.value = false
  })

  offVoiceError = EventsOn('voice:error', () => {
    voiceActive.value = false
  })
```

在 `onUnmounted` 中添加：
```js
  offVoiceStart?.()
  offVoiceEnd?.()
  offVoiceError?.()
```

- [ ] **Step 2: 在 `<template>` 末尾、`</template>` 之前添加视觉特效层**

```html
  <!-- Voice recording visual effects: Siri border + wave rings -->
  <div class="siri-border" :class="{ active: voiceActive }" />
  <div v-if="voiceActive" class="voice-wave">
    <div
      v-for="i in 4"
      :key="i"
      class="wave-ring"
      :style="{ animationDelay: `${(i - 1) * 0.3}s` }"
    />
  </div>
```

- [ ] **Step 3: 在 App.vue 末尾添加 `<style>` 块**

```html
<style scoped>
/* ── Siri-style screen border glow ─────────────────────────── */
.siri-border {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 9998;
  border-radius: 12px;
  opacity: 0;
  transition: opacity 0.3s ease;
  /* Four-sided gradient border via box-shadow */
  box-shadow:
    0 0 0 3px transparent,
    inset 0 0 0 3px transparent;
}

.siri-border.active {
  opacity: 1;
  animation: siri-border-flow 3s linear infinite;
}

@keyframes siri-border-flow {
  0%   { box-shadow: 0 0 20px 4px #3b82f6, inset 0 0 0 3px #3b82f6; }
  25%  { box-shadow: 0 0 20px 4px #8b5cf6, inset 0 0 0 3px #8b5cf6; }
  50%  { box-shadow: 0 0 20px 4px #ec4899, inset 0 0 0 3px #ec4899; }
  75%  { box-shadow: 0 0 20px 4px #f97316, inset 0 0 0 3px #f97316; }
  100% { box-shadow: 0 0 20px 4px #3b82f6, inset 0 0 0 3px #3b82f6; }
}

/* ── Centered circular wave rings ──────────────────────────── */
.voice-wave {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  z-index: 9997;
}

.wave-ring {
  position: absolute;
  width: 80px;
  height: 80px;
  border-radius: 50%;
  border: 2px solid #8b5cf6;
  animation: wave-pulse 1.6s ease-out infinite;
}

@keyframes wave-pulse {
  0%   { transform: scale(1);   opacity: 0.6; }
  100% { transform: scale(3);   opacity: 0; }
}
</style>
```

- [ ] **Step 4: 在开发模式下验证样式可加载（不需要权限）**

```bash
cd /Users/xutiancheng/code/self/Aiko
go build ./...
```

Expected: 无编译错误（前端错误会在 `wails dev` 时浮出）。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/App.vue
git commit -m "feat: add Siri border glow and voice wave ring UI effects to App.vue"
```

---

## Task 6：`frontend/src/components/ChatPanel.vue` — 实时文字 + 状态条

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

- [ ] **Step 1: 在 `<script setup>` import 区添加 `EventsOn` 和 `EventsEmit`**

检查文件顶部是否已有：
```js
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
```

若无则添加。

- [ ] **Step 2: 在现有 ref 声明之后添加录音状态 ref**

在 `const textareaEl = ref(null)` 之后添加：
```js
const isRecording = ref(false)
const voiceHint = ref('')  // 实时识别文字（状态条显示用）
```

- [ ] **Step 3: 在 `onMounted` 内（现有 EventsOn 监听之后）添加 voice 事件监听**

```js
  EventsOn('voice:start', () => {
    isRecording.value = true
    voiceHint.value = ''
    input.value = ''
    nextTick(() => textareaEl.value?.focus())
  })

  EventsOn('voice:transcript', (text) => {
    input.value = text
    voiceHint.value = text
  })

  EventsOn('voice:end', () => {
    isRecording.value = false
    voiceHint.value = ''
    // input.value 保留最终识别文字，用户确认后手动发送
  })

  EventsOn('voice:error', (errMsg) => {
    isRecording.value = false
    voiceHint.value = ''
    input.value = ''
    EventsEmit('notification:show', {
      title: '🎙️ 语音识别失败',
      message: errMsg === 'mic_denied'
        ? '请在系统偏好设置中允许 Aiko 使用麦克风。'
        : errMsg === 'speech_denied'
          ? '请在系统偏好设置中允许 Aiko 使用语音识别。'
          : `语音识别出错：${errMsg}`,
    })
  })
```

- [ ] **Step 4: 在消息列表末尾添加录音状态条**

找到现有消息列表的渲染区域（模板中 `v-for` 渲染消息的 div 之后），在其后、`<div class="input-row">` 之前插入：

```html
      <!-- Voice recording status bar -->
      <div v-if="isRecording" class="voice-hint-bar">
        <span class="voice-hint-icon">🎙️</span>
        <span class="voice-hint-text">
          {{ voiceHint ? `"${voiceHint}"` : '正在聆听...' }}
        </span>
        <span class="voice-hint-dots">
          <span />
          <span />
          <span />
        </span>
      </div>
```

- [ ] **Step 5: 在 ChatPanel.vue 的 `<style>` 块末尾追加状态条样式**

```css
/* ── Voice hint status bar ─────────────────────────────────── */
.voice-hint-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px 12px;
  padding: 10px 14px;
  border-radius: 10px;
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.15), rgba(139, 92, 246, 0.15));
  border: 1px solid rgba(139, 92, 246, 0.3);
  font-size: 13px;
  color: rgba(200, 210, 255, 0.9);
}

.voice-hint-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.voice-hint-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  background: #8b5cf6;
  animation: dot-bounce 1.2s ease-in-out infinite;
}

.voice-hint-dots span:nth-child(2) { animation-delay: 0.2s; }
.voice-hint-dots span:nth-child(3) { animation-delay: 0.4s; }

@keyframes dot-bounce {
  0%, 80%, 100% { transform: translateY(0);    opacity: 0.4; }
  40%           { transform: translateY(-4px); opacity: 1; }
}
```

- [ ] **Step 6: 验证编译**

```bash
cd /Users/xutiancheng/code/self/Aiko
go build ./...
```

Expected: 无错误。

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/ChatPanel.vue
git commit -m "feat: add real-time voice transcript display and recording status bar in ChatPanel"
```

---

## Task 7：集成测试

手动验证完整功能。此阶段无自动化测试（CGO + 音频权限难以 mock），通过行为观察验证。

- [ ] **Step 1: 构建并运行开发模式**

```bash
cd /Users/xutiancheng/code/self/Aiko
wails dev
```

Expected: 应用启动，无崩溃。

- [ ] **Step 2: 验证双击 Option 仍正常工作**

快速双击 Option 键（两次间隔 < 0.5s）。

Expected: 聊天气泡正常切换显示/隐藏，无录音特效触发。

- [ ] **Step 3: 验证长按 Option 触发录音**

按住 Option 键保持 > 1s。

Expected:
- 若是首次：系统弹出麦克风权限对话框，点击允许。
- 若是首次：系统弹出语音识别权限对话框，点击允许。
- 屏幕边框出现蓝紫粉橙渐变彩虹光循环动画。
- 屏幕中央出现 4 个向外扩散的同心圆波形。
- 聊天气泡自动打开（若原来是关闭的）。
- 消息列表末尾出现 `🎙️ 正在聆听...` 状态条。

- [ ] **Step 4: 验证实时语音转文字**

保持 Option 按住，对麦克风说话（中文或英文）。

Expected:
- ChatPanel textarea 实时更新为识别出的文字。
- 状态条文字同步更新为当前识别内容。

- [ ] **Step 5: 验证释放 Option 结束录音**

说完后松开 Option 键。

Expected:
- 屏幕边框彩虹光消失。
- 波形消失。
- 状态条消失。
- textarea 内保留最终识别文字，用户可编辑后按 Enter 发送。

- [ ] **Step 6: 验证权限拒绝错误处理**

（可选，若已授权则跳过）在系统设置中撤销麦克风权限，再次长按 Option。

Expected: 显示 `🎙️ 语音识别失败` 通知气泡，提示用户开启权限。

- [ ] **Step 7: 最终 commit**

```bash
git add -A
git commit -m "feat: voice input integration — long-press Option triggers STT with Siri border effect"
```

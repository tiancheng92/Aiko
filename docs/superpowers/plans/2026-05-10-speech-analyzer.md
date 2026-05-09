# SpeechAnalyzer 迁移（macOS 26+） Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 macOS 26+ 上用 `SpeechAnalyzer / DictationTranscriber` 替代 `SFSpeechRecognizer`，macOS < 26 自动 fallback，Go 侧和前端事件系统零改动。

**Architecture:** `macos.go` 的 `startVoiceRecognition()` 和 `stopVoiceRecognition()` 加 `@available(macOS 26.0, *)` 运行时分支；新路径用相同的 pipe 格式（`FINAL:` 前缀 / 普通文本 / `ERROR:xxx`）写入 `gVoicePipeFd`；Go goroutine `readVoicePipe()` 无需感知走了哪条路径。

**Tech Stack:** Objective-C（`macos.go` CGO 内嵌），`SpeechAnalyzer` / `DictationTranscriber`（macOS 26+ Speech framework），`AVAudioEngine`

> ⚠️ **本计划需要 macOS 26 真机或 beta 模拟器**才能完整验证。编译期可在任何 macOS 版本上通过（`@available` 保证编译兼容），但 `SpeechAnalyzer` 路径运行时只在 macOS 26+ 生效。

---

### Task 1: 新增 SpeechAnalyzer 全局变量声明

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: 在现有语音识别全局变量块之后追加新变量**

在 `macos.go` 的以下注释块之后：

```objc
// --- Voice Recognition globals ---
static SFSpeechRecognizer        *gSpeechRecognizer  = nil;
static SFSpeechAudioBufferRecognitionRequest *gRecogRequest = nil;
static SFSpeechRecognitionTask   *gRecogTask         = nil;
static AVAudioEngine             *gAudioEngine        = nil;
```

追加：

```objc
// --- SpeechAnalyzer globals (macOS 26+) ---
// Declared as 'id' to avoid compile errors on older SDK targets.
// Actual types are used only inside @available(macOS 26.0, *) blocks.
static id gSpeechAnalyzer26       = nil;
static id gDictationTranscriber26 = nil;
static AVAudioEngine *gAudioEngine26 = nil;
```

- [ ] **Step 2: 确认 Go 编译通过**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./... 2>&1
```

期望：无输出（编译成功）

- [ ] **Step 3: Commit**

```bash
git add macos.go
git commit -m "feat(voice): add SpeechAnalyzer global variable declarations for macOS 26+"
```

---

### Task 2: 实现 startVoiceRecognition_SpeechAnalyzer()

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: 在 `startVoiceRecognition()` 函数之前插入新函数**

在 `// startVoiceRecognition requests permissions and starts streaming STT.` 注释行之前，插入以下完整函数：

```objc
// startVoiceRecognition_SpeechAnalyzer starts STT using the macOS 26+ SpeechAnalyzer API.
// Results are written to gVoicePipeFd in the same format as the SFSpeechRecognizer path:
//   plain text  → partial transcript
//   "FINAL:..."  → final transcript
//   "ERROR:..."  → error description
// Must only be called inside an @available(macOS 26.0, *) block.
static void startVoiceRecognition_SpeechAnalyzer(void) API_AVAILABLE(macos(26.0)) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @try {
            // 1. Mic permission — same check as the SFSpeechRecognizer path.
            AVAuthorizationStatus micStatus = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
            if (micStatus == AVAuthorizationStatusNotDetermined) {
                [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
                    if (granted) {
                        startVoiceRecognition_SpeechAnalyzer();
                    } else {
                        sendVoiceText("ERROR:mic_denied");
                    }
                }];
                return;
            } else if (micStatus == AVAuthorizationStatusDenied || micStatus == AVAuthorizationStatusRestricted) {
                sendVoiceText("ERROR:mic_denied");
                return;
            }

            // 2. Create SpeechAnalyzer with a DictationTranscriber module.
            SpeechAnalyzer *analyzer = [[SpeechAnalyzer alloc] init];
            DictationTranscriber *transcriber = [[DictationTranscriber alloc] init];
            [analyzer addModule:transcriber];

            gSpeechAnalyzer26       = analyzer;
            gDictationTranscriber26 = transcriber;

            // 3. Start AVAudioEngine and feed PCM buffers to the analyzer.
            gAudioEngine26 = [AVAudioEngine new];
            AVAudioInputNode *inputNode = gAudioEngine26.inputNode;
            AVAudioFormat *fmt = [inputNode outputFormatForBus:0];

            [inputNode installTapOnBus:0 bufferSize:1024 format:fmt block:^(AVAudioPCMBuffer *buf, AVAudioTime *when) {
                @try { [analyzer appendAudioPCMBuffer:buf]; } @catch (...) {}
            }];

            NSError *startErr = nil;
            [gAudioEngine26 startAndReturnError:&startErr];
            if (startErr) {
                NSString *msg = [NSString stringWithFormat:@"ERROR:audio_engine:%@", startErr.localizedDescription];
                sendVoiceText([msg UTF8String]);
                return;
            }

            // 4. Set result handler — same pipe format as the SFSpeechRecognizer path.
            [transcriber setResultHandler:^(DictationTranscriberResult *result, NSError *err) {
                @try {
                    if (err) {
                        NSString *msg = [NSString stringWithFormat:@"ERROR:recognition:%@", err.localizedDescription];
                        sendVoiceText([msg UTF8String]);
                        return;
                    }
                    if (!result) return;
                    if (result.isFinal) {
                        NSString *msg = [NSString stringWithFormat:@"FINAL:%@", result.text];
                        sendVoiceText([msg UTF8String]);
                    } else {
                        sendVoiceText([result.text UTF8String]);
                    }
                } @catch (NSException *ex) {
                    NSString *msg = [NSString stringWithFormat:@"ERROR:result_handler:%@: %@", ex.name, ex.reason];
                    sendVoiceText([msg UTF8String]);
                } @catch (...) {}
            }];

            // 5. Start analysis.
            [analyzer startAnalysis];

        } @catch (NSException *ex) {
            NSString *msg = [NSString stringWithFormat:@"ERROR:exception:%@: %@", ex.name, ex.reason];
            sendVoiceText([msg UTF8String]);
        }
    });
}
```

- [ ] **Step 2: 确认编译通过**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./... 2>&1
```

期望：无输出

- [ ] **Step 3: Commit**

```bash
git add macos.go
git commit -m "feat(voice): implement startVoiceRecognition_SpeechAnalyzer for macOS 26+"
```

---

### Task 3: startVoiceRecognition() 加运行时分支

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: 改造 `startVoiceRecognition()` 入口**

找到现有的 `startVoiceRecognition()` 函数体内，`dispatch_async(dispatch_get_main_queue(), ^{` 的第一行 `@try {` 之前，将整个函数替换如下结构——即在外层加 `@available` 分支，旧逻辑挪入 `else` 分支：

```objc
// startVoiceRecognition requests permissions and starts streaming STT.
// On macOS 26+, uses SpeechAnalyzer for better accuracy and local processing.
// On older systems, falls back to SFSpeechRecognizer.
static void startVoiceRecognition() {
    if (@available(macOS 26.0, *)) {
        startVoiceRecognition_SpeechAnalyzer();
    } else {
        dispatch_async(dispatch_get_main_queue(), ^{
            @try {
            // Check microphone permission (AVCaptureDevice works on all supported macOS versions)
            AVAuthorizationStatus micStatus = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
            if (micStatus == AVAuthorizationStatusNotDetermined) {
                [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
                    if (granted) {
                        startVoiceRecognition();
                    } else {
                        sendVoiceText("ERROR:mic_denied");
                    }
                }];
                return;
            } else if (micStatus == AVAuthorizationStatusDenied || micStatus == AVAuthorizationStatusRestricted) {
                sendVoiceText("ERROR:mic_denied");
                return;
            }

            // Check speech recognition permission.
            SFSpeechRecognizerAuthorizationStatus speechStatus = [SFSpeechRecognizer authorizationStatus];
            if (speechStatus == SFSpeechRecognizerAuthorizationStatusDenied ||
                speechStatus == SFSpeechRecognizerAuthorizationStatusRestricted) {
                sendVoiceText("ERROR:speech_denied");
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
                @try { [gRecogRequest appendAudioPCMBuffer:buf]; } @catch (...) {}
            }];

            NSError *startErr = nil;
            [gAudioEngine startAndReturnError:&startErr];
            if (startErr) {
                NSString *msg = [NSString stringWithFormat:@"ERROR:audio_engine:%@", startErr.localizedDescription];
                sendVoiceText([msg UTF8String]);
                return;
            }

            gRecogTask = [gSpeechRecognizer recognitionTaskWithRequest:gRecogRequest
                resultHandler:^(SFSpeechRecognitionResult *result, NSError *err) {
                    @try {
                        if (err) {
                            if (err.code != 301) {
                                NSString *msg = [NSString stringWithFormat:@"ERROR:recognition:%@", err.localizedDescription];
                                sendVoiceText([msg UTF8String]);
                            }
                            return;
                        }
                        if (result) {
                            NSString *text = result.bestTranscription.formattedString;
                            if (result.isFinal) {
                                NSString *msg = [NSString stringWithFormat:@"FINAL:%@", text];
                                sendVoiceText([msg UTF8String]);
                            } else {
                                sendVoiceText([text UTF8String]);
                            }
                        }
                    } @catch (NSException *ex) {
                        NSString *msg = [NSString stringWithFormat:@"ERROR:result_handler:%@: %@", ex.name, ex.reason];
                        sendVoiceText([msg UTF8String]);
                    } @catch (...) {}
                }];
            } @catch (NSException *ex) {
                NSString *msg = [NSString stringWithFormat:@"ERROR:exception:%@: %@", ex.name, ex.reason];
                sendVoiceText([msg UTF8String]);
            }
        });
    }
}
```

- [ ] **Step 2: 确认编译通过**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./... 2>&1
```

期望：无输出

- [ ] **Step 3: Commit**

```bash
git add macos.go
git commit -m "feat(voice): add @available runtime branch in startVoiceRecognition"
```

---

### Task 4: stopVoiceRecognition() 加运行时分支

**Files:**
- Modify: `macos.go`

- [ ] **Step 1: 改造 `stopVoiceRecognition()`**

找到现有的 `stopVoiceRecognition()` 函数，在 `dispatch_async(dispatch_get_main_queue(), ^{` 块的第一行 `@try {` 之前，将整个函数改为：

```objc
// stopVoiceRecognition ends the STT task and tears down the audio engine.
// Branches on the same @available check used in startVoiceRecognition.
static void stopVoiceRecognition() {
    if (@available(macOS 26.0, *)) {
        dispatch_async(dispatch_get_main_queue(), ^{
            @try {
                if (gAudioEngine26 && [gAudioEngine26 isRunning]) {
                    [[gAudioEngine26 inputNode] removeTapOnBus:0];
                    [gAudioEngine26 stop];
                }
                if (gSpeechAnalyzer26) {
                    [gSpeechAnalyzer26 stopAnalysis];
                }
            } @catch (NSException *ex) {
                NSLog(@"[Aiko] stopVoiceRecognition_SA exception: %@: %@", ex.name, ex.reason);
            } @catch (...) {}
            gSpeechAnalyzer26       = nil;
            gDictationTranscriber26 = nil;
            gAudioEngine26          = nil;
            // Notify Go that the engine is fully stopped; Go will emit voice:end.
            if (gHotkeyPipeFd >= 0) {
                char b = 4;
                write(gHotkeyPipeFd, &b, 1);
            }
        });
    } else {
        dispatch_async(dispatch_get_main_queue(), ^{
            @try {
                // 1. Stop the audio tap first to prevent new buffers from being appended.
                if (gAudioEngine && gAudioEngine.running) {
                    [gAudioEngine.inputNode removeTapOnBus:0];
                    [gAudioEngine stop];
                }
                // 2. Signal end of audio stream to the recognizer.
                [gRecogRequest endAudio];
                // 3. Ask the task to finalize with what it has received.
                [gRecogTask finish];
            } @catch (NSException *ex) {
                NSLog(@"[Aiko] stopVoiceRecognition exception: %@: %@", ex.name, ex.reason);
            } @catch (...) {}
            gRecogTask        = nil;
            gRecogRequest     = nil;
            gAudioEngine      = nil;
            gSpeechRecognizer = nil;
            // Notify Go that the engine is fully stopped; Go will emit voice:end.
            if (gHotkeyPipeFd >= 0) {
                char b = 4;
                write(gHotkeyPipeFd, &b, 1);
            }
        });
    }
}
```

- [ ] **Step 2: 确认编译通过**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./... 2>&1
```

期望：无输出

- [ ] **Step 3: 跑全量 Go 测试**

```bash
go test ./... 2>&1 | grep -E "FAIL|ok"
```

期望：所有包 `ok`，无 `FAIL`

- [ ] **Step 4: Commit**

```bash
git add macos.go
git commit -m "feat(voice): add @available runtime branch in stopVoiceRecognition"
```

---

### Task 5: macOS 26 真机验证

> ⚠️ 以下步骤需要运行 macOS 26 的 Mac 才能执行。如无设备，跳过此 Task，功能在 macOS < 26 上会自动走 SFSpeechRecognizer 路径，不影响现有用户。

- [ ] **Step 1: 在 macOS 26 机器上构建**

```bash
cd /Users/xutiancheng/code/self/Aiko && make run
```

期望：应用正常启动，无崩溃

- [ ] **Step 2: 验证 SpeechAnalyzer 路径生效**

长按 Option 键 ≥ 1 秒触发录音：
- 录音彩虹光边框出现（说明 Go 收到了 voice:start）
- 说话时输入框出现实时文字（`voice:transcript` 事件触发）
- 松开 Option 键后输入框保留最终文字（`voice:final` 触发）
- 录音指示灯消失（`voice:end` 触发）

- [ ] **Step 3: 验证错误处理**

拒绝麦克风权限后触发录音，确认输入框显示「麦克风权限已拒绝」错误提示（Go 侧将 `ERROR:mic_denied` 转为 `voice:error` 事件）

- [ ] **Step 4: 验证 macOS < 26 fallback（在旧版 Mac 上）**

同一构建在 macOS 15 或更低版本上运行，长按 Option 键，确认语音识别仍然正常工作（走 SFSpeechRecognizer 路径）

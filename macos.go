//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=10.15
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework ApplicationServices -framework AVFoundation -framework Speech

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <AVFoundation/AVFoundation.h>
#import <Speech/Speech.h>
#include <unistd.h>

static id gGlobalMonitor    = nil;
static id gLocalMonitor     = nil;
static id gHotkeyMonitor    = nil;
static NSWindow  *gWindow  = nil;
static WKWebView *gWebView = nil;

// --- Voice Recognition globals ---
static SFSpeechRecognizer        *gSpeechRecognizer  = nil;
static SFSpeechAudioBufferRecognitionRequest *gRecogRequest = nil;
static SFSpeechRecognitionTask   *gRecogTask         = nil;
static AVAudioEngine             *gAudioEngine       = nil;

// gHotkeyPipeFd is the write end of a pipe; Go reads from the read end.
static int gHotkeyPipeFd = -1;

// setHotkeyPipeFd stores the write-end fd so the monitor handler can signal Go.
static void setHotkeyPipeFd(int fd) { gHotkeyPipeFd = fd; }

// gVoicePipeFd is the write end of a pipe used to send voice transcript strings to Go.
static int gVoicePipeFd = -1;

// setVoicePipeFd stores the write-end fd so STT callbacks can send text to Go.
static void setVoicePipeFd(int fd) { gVoicePipeFd = fd; }

// sendVoiceText writes a length-prefixed text message to the voice pipe.
// Format: 4-byte little-endian uint32 length, followed by UTF-8 text bytes.
static void sendVoiceText(const char *text) {
    if (gVoicePipeFd < 0 || !text) return;
    uint32_t len = (uint32_t)strlen(text);
    write(gVoicePipeFd, &len, sizeof(uint32_t));
    if (len > 0) write(gVoicePipeFd, text, len);
}

// activateApp brings the application to the foreground on the main thread.
static void activateApp() {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp activateIgnoringOtherApps:YES];
    });
}

// aikoLogException writes exception info to /tmp/aiko_crash.log for diagnosis.
static void aikoLogException(NSString *context, NSException *ex) {
    NSString *msg = [NSString stringWithFormat:@"[Aiko] EXCEPTION in %@: %@: %@\nStack: %@\n",
        context, ex.name, ex.reason, [ex.callStackSymbols componentsJoinedByString:@"\n"]];
    NSLog(@"%@", msg);
    NSString *path = @"/tmp/aiko_crash.log";
    NSString *existing = [NSString stringWithContentsOfFile:path encoding:NSUTF8StringEncoding error:nil] ?: @"";
    [[existing stringByAppendingString:msg] writeToFile:path atomically:YES encoding:NSUTF8StringEncoding error:nil];
    sendVoiceText([[NSString stringWithFormat:@"ERROR:exception:%@: %@", ex.name, ex.reason] UTF8String]);
}

// aikoUncaughtExceptionHandler is the last-resort handler before AppKit calls abort().
static void aikoUncaughtExceptionHandler(NSException *ex) {
    aikoLogException(@"uncaught", ex);
}

// Static globals shared with the CGEventTap C callback (blocks cannot be used
// directly in C function pointers).
static volatile BOOL gTapOptDown   = NO;  // mirrors optWasDown for the tap callback
static volatile BOOL gTapComboSeen = NO;  // set by tap when a key is pressed while opt is held
static dispatch_block_t gTapLongPressBlock = nil; // reference so the tap can cancel it
static CFMachPortRef gEventTapPort = NULL; // kept for re-enable on timeout

// aikoKeyTap is the CGEventTap callback. It runs on the main run-loop and sees
// ALL key events (including those consumed by the system, e.g. opt+space for
// input-method switching or Spotlight) before NSEvent global monitors do.
static CGEventRef aikoKeyTap(CGEventTapProxy proxy, CGEventType type,
                              CGEventRef event, void *info) {
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (gEventTapPort) CGEventTapEnable(gEventTapPort, true);
        return event;
    }
    if (type == kCGEventKeyDown && gTapOptDown && !gTapComboSeen) {
        gTapComboSeen = YES;
        if (gTapLongPressBlock) {
            dispatch_block_cancel(gTapLongPressBlock);
            gTapLongPressBlock = nil;
        }
    }
    return event;
}


// Requires Accessibility permission; prompts the user if not yet granted.
// On match, writes a single byte to gHotkeyPipeFd — no CGO call-back needed.
static void registerGlobalHotkey() {
    NSSetUncaughtExceptionHandler(aikoUncaughtExceptionHandler);

    // Check accessibility permission. Only pass kAXTrustedCheckOptionPrompt=YES
    // when not yet trusted — passing YES unconditionally triggers the system
    // prompt on every launch even if the user already granted access.
    BOOL trusted = AXIsProcessTrustedWithOptions(NULL);
    if (!trusted) {
        NSDictionary *opts = @{(__bridge id)kAXTrustedCheckOptionPrompt: @YES};
        trusted = AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts);
    }
    if (!trusted) {
        NSLog(@"[Aiko] Accessibility not granted yet; global hotkey inactive until relaunch.");
    }

    __block NSTimeInterval lastOptUp = 0;
    __block BOOL optWasDown = NO;
    __block BOOL justTriggered = NO;
    const NSTimeInterval kDoubleTapInterval = 0.5;
    __block BOOL gLongPressTriggered = NO;
    __block dispatch_block_t gLongPressBlock = nil;
    __block BOOL optComboDetected = NO; // set when a non-opt key is pressed while opt is held

    // cancelLongPress cancels the pending long-press timer and, if recording already
    // started, sends the stop signal. Call this whenever a combo is detected.
    void (^cancelLongPress)(void) = ^{
        if (gLongPressBlock) {
            dispatch_block_cancel(gLongPressBlock);
            gLongPressBlock = nil;
            gTapLongPressBlock = nil;
        }
        if (gLongPressTriggered) {
            gLongPressTriggered = NO;
            if (gHotkeyPipeFd >= 0) {
                char b = 3;
                write(gHotkeyPipeFd, &b, 1);
            }
        }
        lastOptUp = 0;
        justTriggered = NO;
    };

    // Install a CGEventTap to catch keyDown events at the HID level — this fires
    // even for system-consumed keys (opt+space, opt+tab, etc.) that NSEvent
    // global monitors never see. The tap callback sets gTapComboSeen which the
    // flagsChanged handler reads on the main queue.
    CGEventMask tapMask = CGEventMaskBit(kCGEventKeyDown);
    CFMachPortRef tap = CGEventTapCreate(kCGSessionEventTap,
                                         kCGHeadInsertEventTap,
                                         kCGEventTapOptionDefault,
                                         tapMask,
                                         aikoKeyTap,
                                         NULL);
    if (tap) {
        gEventTapPort = tap;
        CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0);
        CFRunLoopAddSource(CFRunLoopGetMain(), src, kCFRunLoopCommonModes);
        CGEventTapEnable(tap, true);
        CFRelease(src);
    } else {
        NSLog(@"[Aiko] CGEventTap creation failed (Accessibility not granted?)");
    }

    void (^handler)(NSEvent *) = ^(NSEvent *evt) {
        @try {
        NSUInteger standardMask = NSEventModifierFlagOption
                                | NSEventModifierFlagCommand
                                | NSEventModifierFlagShift
                                | NSEventModifierFlagControl
                                | NSEventModifierFlagCapsLock
                                | NSEventModifierFlagFunction;
        NSUInteger std = evt.modifierFlags & standardMask;
        BOOL optDown = (std == NSEventModifierFlagOption);
        BOOL optUp   = !optDown && optWasDown && !(std & ~NSEventModifierFlagOption);

        if (optUp) {
            if (gTapComboSeen) {
                // This opt release is part of a combo — ignore entirely.
                gTapComboSeen = NO;
                optComboDetected = NO;
            } else if (gLongPressTriggered) {
                // Long-press release → stop recording
                gLongPressTriggered = NO;
                if (gHotkeyPipeFd >= 0) {
                    char b = 3;
                    write(gHotkeyPipeFd, &b, 1);
                }
            } else {
                // Cancel pending long-press timer (released before 1s)
                if (gLongPressBlock) {
                    dispatch_block_cancel(gLongPressBlock);
                    gLongPressBlock = nil;
                }
                if (justTriggered) {
                    justTriggered = NO;
                } else if (lastOptUp == 0) {
                    lastOptUp = [NSDate timeIntervalSinceReferenceDate];
                }
            }
        } else if (optDown && !optWasDown) {
            // Fresh Option press — reset combo flags and start long-press timer.
            optComboDetected = NO;
            gTapComboSeen = NO;
            gTapOptDown = YES;
            dispatch_block_t blk = dispatch_block_create(0, ^{
                @try {
                    if (!gLongPressTriggered) {
                        gLongPressTriggered = YES;
                        lastOptUp = 0; // cancel double-tap window
                        if (gHotkeyPipeFd >= 0) {
                            char b = 2;
                            write(gHotkeyPipeFd, &b, 1);
                        }
                    }
                    gLongPressBlock = nil;
                } @catch (NSException *ex) {
                    aikoLogException(@"longPressTimer", ex);
                } @catch (...) {}
            });
            gLongPressBlock = blk;
            gTapLongPressBlock = blk;
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
        } else if (!optDown && !optUp) {
            // Another modifier key involved (e.g. opt+cmd) — cancel and reset.
            cancelLongPress();
            optComboDetected = YES;
            gTapComboSeen = YES;
        }
        optWasDown = optDown;
        gTapOptDown = optDown;
        } @catch (NSException *ex) {
            aikoLogException(@"hotkeyHandler", ex);
        } @catch (...) {}
    };

    // Global monitor: fires when another app is active.
    gHotkeyMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:NSEventMaskFlagsChanged
        handler:handler];
    // Local monitor: fires when Aiko itself is the active app.
    [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskFlagsChanged
        handler:^NSEvent *(NSEvent *evt) { handler(evt); return evt; }];
}

// 🔧 调试开关：设为 1 禁用点击穿透，方便调试
static int gDebugDisableHitTest = 0;

// findWebView recursively searches a view hierarchy for a WKWebView.
static WKWebView *findWebView(NSView *view) {
    if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
    for (NSView *sub in view.subviews) {
        WKWebView *found = findWebView(sub);
        if (found) return found;
    }
    return nil;
}

// hitTestPoint evaluates JS to determine whether (cssX, cssY) lies over an
// interactive element. Toggles mouse-event passthrough based on the result.
static void hitTestPoint(CGFloat cssX, CGFloat cssY) {
    if (!gWebView || !gWindow) return;

    // 🔧 调试模式：禁用点击穿透
    if (gDebugDisableHitTest) {
        dispatch_async(dispatch_get_main_queue(), ^{
            [gWindow setIgnoresMouseEvents:NO]; // 总是接收鼠标事件
        });
        return;
    }

    NSString *js = [NSString stringWithFormat:
        @"(function(x,y){"
         "var e=document.elementFromPoint(x,y);"
         "return !!(e&&e.closest('.live2d-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble,.execution-progress'));"
         "}(%g,%g))",
        cssX, cssY];
    [gWebView evaluateJavaScript:js completionHandler:^(id result, NSError *err) {
        BOOL interactive = !err && [result isEqual:@YES];
        dispatch_async(dispatch_get_main_queue(), ^{
            [gWindow setIgnoresMouseEvents:!interactive];
        });
    }];
}

// handleScreenPoint converts a macOS screen point (Y-up) to CSS coordinates
// (Y-down from window top-left) and calls hitTestPoint.
static void handleScreenPoint(NSPoint screen) {
    if (!gWindow || ![gWindow isVisible]) return;
    NSRect frame = gWindow.frame;
    CGFloat cssX = screen.x - frame.origin.x;
    CGFloat cssY = frame.size.height - (screen.y - frame.origin.y);
    hitTestPoint(cssX, cssY);
}

// getMouseScreenX returns the current mouse cursor X position in macOS screen coordinates (Y-up).
static CGFloat getMouseScreenX() { return [NSEvent mouseLocation].x; }
// getMouseScreenY returns the current mouse cursor Y position in macOS screen coordinates (Y-up).
static CGFloat getMouseScreenY() { return [NSEvent mouseLocation].y; }

// getWindowOriginX returns the window origin X in screen coordinates.
static CGFloat getWindowOriginX() { return gWindow ? gWindow.frame.origin.x : 0; }
// getWindowOriginY returns the window origin Y in screen coordinates.
static CGFloat getWindowOriginY() { return gWindow ? gWindow.frame.origin.y : 0; }
// getWindowHeight returns the window height.
static CGFloat getWindowHeight() { return gWindow ? gWindow.frame.size.height : 0; }
// getCurrentScreenOriginX returns the X origin of the screen containing gWindow, in macOS screen coords.
static CGFloat getCurrentScreenOriginX() {
    if (!gWindow) return 0;
    return gWindow.screen ? gWindow.screen.frame.origin.x : 0;
}
// getCurrentScreenOriginY returns the Y origin of the screen containing gWindow, in macOS screen coords.
static CGFloat getCurrentScreenOriginY() {
    if (!gWindow) return 0;
    return gWindow.screen ? gWindow.screen.frame.origin.y : 0;
}

// getNumScreens returns the number of connected screens.
static int getNumScreens() {
    return (int)[[NSScreen screens] count];
}

// getScreenOriginX returns the X origin of the nth screen (macOS Y-up coords).
static CGFloat getScreenOriginX(int n) {
    NSArray<NSScreen *> *screens = [NSScreen screens];
    if (n < 0 || n >= (int)[screens count]) return 0;
    return [[screens objectAtIndex:n] frame].origin.x;
}

// getScreenOriginY returns the Y origin of the nth screen (macOS Y-up coords).
static CGFloat getScreenOriginY(int n) {
    NSArray<NSScreen *> *screens = [NSScreen screens];
    if (n < 0 || n >= (int)[screens count]) return 0;
    return [[screens objectAtIndex:n] frame].origin.y;
}

// getScreenWidth returns the width of the nth screen.
static CGFloat getScreenWidth(int n) {
    NSArray<NSScreen *> *screens = [NSScreen screens];
    if (n < 0 || n >= (int)[screens count]) return 0;
    return [[screens objectAtIndex:n] frame].size.width;
}

// getScreenHeight returns the height of the nth screen.
static CGFloat getScreenHeight(int n) {
    NSArray<NSScreen *> *screens = [NSScreen screens];
    if (n < 0 || n >= (int)[screens count]) return 0;
    return [[screens objectAtIndex:n] frame].size.height;
}

typedef struct CScreenFrame {
    CGFloat originX;
    CGFloat originY;
    CGFloat width;
    CGFloat height;
    int     valid; // 1 if index was in bounds, 0 otherwise
} CScreenFrame;

// getScreenFrame returns the frame of the nth NSScreen atomically.
static CScreenFrame getScreenFrame(int n) {
    NSArray<NSScreen *> *screens = [NSScreen screens];
    CScreenFrame f = {0, 0, 0, 0, 0};
    if (n < 0 || n >= (int)[screens count]) return f;
    NSRect frame = [[screens objectAtIndex:n] frame];
    f.originX = frame.origin.x;
    f.originY = frame.origin.y;
    f.width   = frame.size.width;
    f.height  = frame.size.height;
    f.valid   = 1;
    return f;
}

// moveWindowToScreen moves gWindow to cover the nth NSScreen exactly.
// Uses setFrame:display: to bypass Wails' relative-position coordinate conversion,
// which is anchored to the current screen and cannot reliably move to another screen.
static void moveWindowToScreen(int n) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSArray<NSScreen *> *screens = [NSScreen screens];
        if (n < 0 || n >= (int)[screens count] || !gWindow) return;
        NSRect frame = [[screens objectAtIndex:n] frame];
        [gWindow setFrame:frame display:YES animate:NO];
    });
}

// hasWindow returns 1 if gWindow is initialized.
static int hasWindow() { return gWindow != nil ? 1 : 0; }

// startVoiceRecognition requests permissions and starts streaming STT.
// Results are written to the voice pipe via sendVoiceText().
// All ObjC exceptions are caught and forwarded as ERROR: messages.
static void startVoiceRecognition() {
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
        // We only block on Denied/Restricted. If NotDetermined, the system will
        // auto-prompt when the recognition task starts. Calling requestAuthorization:
        // explicitly causes a C-level abort() when the dev bundle lacks the plist key.
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

        // resultHandler runs on a Speech framework background thread — must catch all exceptions.
        gRecogTask = [gSpeechRecognizer recognitionTaskWithRequest:gRecogRequest
            resultHandler:^(SFSpeechRecognitionResult *result, NSError *err) {
                @try {
                    if (err) {
                        // Ignore cancellation errors (code 301) — they fire on normal stop
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

// stopVoiceRecognition ends the STT task and tears down the audio engine.
// Correct order: stop tap → stop engine → endAudio → finish task → nil globals.
// After the engine is fully stopped, byte 4 is written to gHotkeyPipeFd so
// Go emits voice:end only after AVAudioEngine is no longer running — this
// prevents macOS from showing the "Aiko is recording" indicator after stop.
static void stopVoiceRecognition() {
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
        gRecogTask = nil;
        gRecogRequest = nil;
        gAudioEngine = nil;
        gSpeechRecognizer = nil;
        // Notify Go that the engine is fully stopped; Go will emit voice:end.
        if (gHotkeyPipeFd >= 0) {
            char b = 4;
            write(gHotkeyPipeFd, &b, 1);
        }
    });
}

// enableClickThrough sets the window to ignore mouse events by default,
// doHideNativeScrollbars must be called on the main thread.
// It disables the native macOS overlay scrollbar that WKWebView renders on hover.
// Tries enclosingScrollView first; falls back to the private _scrollView KVC key
// for the common Wails layout where WKWebView is the direct window content view
// (no outer NSScrollView wrapper), in which case enclosingScrollView returns nil.
static void doHideNativeScrollbars() {
    if (!gWebView) return;
    NSScrollView *sv = (NSScrollView *)[gWebView enclosingScrollView];
    if (!sv) {
        @try { sv = (NSScrollView *)[gWebView valueForKey:@"_scrollView"]; }
        @catch (...) {}
    }
    if (!sv) return;
    [sv setHasVerticalScroller:NO];
    [sv setHasHorizontalScroller:NO];
}

// hideNativeScrollbarsC dispatches doHideNativeScrollbars to the main queue.
// Safe to call from any thread (e.g. from Go's domReady callback).
static void hideNativeScrollbarsC() {
    dispatch_async(dispatch_get_main_queue(), ^{ doHideNativeScrollbars(); });
}

// then installs global and local NSEvent monitors so that the window
// temporarily accepts events only when the cursor is over interactive elements.
//
// The initial setIgnoresMouseEvents:YES is deferred 2 seconds to avoid a macOS 26
// regression where _NSTrackingAreaAKManager SIGABRTs if mouse-event ignoring is
// applied to the window before WKWebView's tracking areas have been initialized
// during the first display-cycle commit.
static void enableClickThrough() {
    dispatch_async(dispatch_get_main_queue(), ^{
        for (NSWindow *win in [NSApp windows]) {
            gWindow  = win;
            gWebView = findWebView(win.contentView);
            break;
        }
        if (!gWindow || !gWebView) return;

        doHideNativeScrollbars();

        // Remove system shadow and ensure the window is transparent so no border rendering occurs.
        [gWindow setHasShadow:NO];
        [gWindow setOpaque:NO];
        [gWindow setBackgroundColor:[NSColor clearColor]];

        // Monitor mouse-moved and drag events.
        NSEventMask mask = NSEventMaskMouseMoved | NSEventMaskLeftMouseDragged;
        gGlobalMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:mask
            handler:^(NSEvent *evt) { handleScreenPoint([NSEvent mouseLocation]); }];

        // Local monitor fires when our window is receiving events (setIgnoresMouseEvents:NO).
        gLocalMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask
            handler:^NSEvent *(NSEvent *evt) {
                handleScreenPoint([NSEvent mouseLocation]);
                return evt;
            }];

        // Defer setIgnoresMouseEvents:YES until after WKWebView's first display cycle.
        // Applying it immediately races with _NSTrackingAreaAKManager initialization
        // on macOS 26 (Tahoe) and causes a SIGABRT in the display-cycle flush.
        // The mouse monitors above already drive hitTestPoint which will re-apply
        // the correct setIgnoresMouseEvents state on the first mouse-move event.
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(2.0 * NSEC_PER_SEC)),
                       dispatch_get_main_queue(), ^{
            if (gWindow) [gWindow setIgnoresMouseEvents:YES];
        });
    });
}

// requestPermissionsEarly pre-requests microphone permission at startup while the
// app is still in the foreground, so macOS shows a proper alert dialog rather than
// a silent notification banner.
//
// Speech recognition is intentionally NOT requested here. On macOS 26 (Tahoe),
// calling [SFSpeechRecognizer requestAuthorization:] during domReady triggers
// system UI that interacts with WKWebView's _NSTrackingAreaAKManager during its
// first display-cycle flush, causing a SIGABRT. The lazy check in
// startVoiceRecognition() is sufficient — the system auto-prompts on first use.
static void requestPermissionsEarly() {
    dispatch_async(dispatch_get_main_queue(), ^{
        AVAuthorizationStatus micStatus = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
        if (micStatus == AVAuthorizationStatusNotDetermined) {
            [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
                NSLog(@"[Aiko] microphone permission: %@", granted ? @"granted" : @"denied");
            }];
        }
    });
}
*/
import "C"
import (
	"encoding/binary"
	"log/slog"
	"strings"
	"syscall"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// enableClickThrough installs per-pixel click-through for the main window.
func enableClickThrough() {
	C.enableClickThrough()
}

// requestPermissionsEarly pre-requests microphone and speech recognition at startup
// so macOS shows a proper alert dialog while the app is still in the foreground.
func requestPermissionsEarly() {
	C.requestPermissionsEarly()
}

// hideNativeScrollbars disables the native macOS overlay scrollbar inside
// WKWebView. It is safe to call from any goroutine; it dispatches to the main
// queue internally. Call it again from domReady to cover the case where the
// WKWebView scroll view is not yet initialized during startup.
func hideNativeScrollbars() {
	C.hideNativeScrollbarsC()
}

// registerGlobalHotkey creates a pipe, passes the write-end to the ObjC monitor,
// and starts a goroutine that reads from the read-end and emits bubble:toggle.
// This avoids any C→Go callback, which causes SIGSEGV via CGO re-entry on m=0.
func registerGlobalHotkey() {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		slog.Error("registerGlobalHotkey: pipe failed", "err", err)
		return
	}
	readFd, writeFd := fds[0], fds[1]

	C.setHotkeyPipeFd(C.int(writeFd))
	C.registerGlobalHotkey()

	go func() {
		buf := make([]byte, 1)
		for {
			n, err := syscall.Read(readFd, buf)
			if err != nil || n == 0 {
				return
			}
			if globalAppCtx == nil {
				continue
			}
			C.activateApp()
			switch buf[0] {
			case 1:
				// 双击 Option — 切换气泡（现有行为）
				wailsruntime.EventsEmit(globalAppCtx, "bubble:toggle")
			case 2:
				// 长按 Option ≥1s — 开始录音，同时启动 voice pipe 监听
				wailsruntime.EventsEmit(globalAppCtx, "voice:start")
				C.startVoiceRecognition()
			case 3:
				// Option 释放 — 请求停止录音；voice:end 在引擎真正停止后由 case 4 发出
				C.stopVoiceRecognition()
			case 4:
				// AVAudioEngine 已完全停止 — 现在才通知前端结束录音
				wailsruntime.EventsEmit(globalAppCtx, "voice:end")
			}
		}
	}()

	// Voice transcript pipe: ObjC sends length-prefixed UTF-8 strings.
	var vfds [2]int
	if err := syscall.Pipe(vfds[:]); err != nil {
		slog.Error("registerGlobalHotkey: voice pipe failed", "err", err)
		return
	}
	vReadFd, vWriteFd := vfds[0], vfds[1]
	C.setVoicePipeFd(C.int(vWriteFd))

	go func() {
		lenBuf := make([]byte, 4)
		for {
			// Read 4-byte little-endian length prefix
			if _, err := readFull(vReadFd, lenBuf); err != nil {
				return
			}
			length := binary.LittleEndian.Uint32(lenBuf)
			if length == 0 {
				continue
			}
			textBuf := make([]byte, length)
			if _, err := readFull(vReadFd, textBuf); err != nil {
				return
			}
			if globalAppCtx == nil {
				continue
			}
			text := string(textBuf)
			if strings.HasPrefix(text, "FINAL:") {
				wailsruntime.EventsEmit(globalAppCtx, "voice:final", text[6:])
			} else if len(text) > 6 && text[:6] == "ERROR:" {
				wailsruntime.EventsEmit(globalAppCtx, "voice:error", text[6:])
			} else {
				wailsruntime.EventsEmit(globalAppCtx, "voice:transcript", text)
			}
		}
	}()
}

// readFull reads exactly len(buf) bytes from fd, handling partial reads.
func readFull(fd int, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := syscall.Read(fd, buf[total:])
		if err != nil || n == 0 {
			return total, err
		}
		total += n
	}
	return total, nil
}

// getCurrentScreenOriginX returns the X origin of the screen that contains the main window.
func getCurrentScreenOriginX() float64 { return float64(C.getCurrentScreenOriginX()) }

// getCurrentScreenOriginY returns the Y origin of the screen that contains the main window.
func getCurrentScreenOriginY() float64 { return float64(C.getCurrentScreenOriginY()) }

// getNumScreens returns the number of connected screens.
func getNumScreens() int { return int(C.getNumScreens()) }

// getScreenOriginX returns the X origin of the nth NSScreen in macOS screen coords.
func getScreenOriginX(n int) float64 { return float64(C.getScreenOriginX(C.int(n))) }

// getScreenOriginY returns the Y origin of the nth NSScreen in macOS screen coords.
func getScreenOriginY(n int) float64 { return float64(C.getScreenOriginY(C.int(n))) }

// getScreenWidth returns the width of the nth NSScreen.
func getScreenWidth(n int) float64 { return float64(C.getScreenWidth(C.int(n))) }

// getScreenHeight returns the height of the nth NSScreen.
func getScreenHeight(n int) float64 { return float64(C.getScreenHeight(C.int(n))) }

// ScreenFrame holds the macOS frame of a single screen.
type ScreenFrame struct {
	OriginX float64
	OriginY float64
	Width   float64
	Height  float64
	Valid   bool
}

// getScreenFrame returns the NSScreen frame for the nth screen atomically.
func getScreenFrame(n int) ScreenFrame {
	f := C.getScreenFrame(C.int(n))
	return ScreenFrame{
		OriginX: float64(f.originX),
		OriginY: float64(f.originY),
		Width:   float64(f.width),
		Height:  float64(f.height),
		Valid:   f.valid == 1,
	}
}

// moveWindowToScreen moves the main window to cover the nth NSScreen exactly.
// This bypasses Wails' WindowSetPosition which is relative to the current screen.
func moveWindowToScreen(n int) { C.moveWindowToScreen(C.int(n)) }

// getMouseX returns the current mouse cursor X in macOS screen coordinates.
func getMouseX() float64 { return float64(C.getMouseScreenX()) }

// getMouseY returns the current mouse cursor Y in macOS screen coordinates.
func getMouseY() float64 { return float64(C.getMouseScreenY()) }

// GetMousePosition returns the current mouse cursor position in CSS coordinates
// (origin at window top-left, Y-down), matching position:fixed layout in the WebView.
func GetMousePosition() (x, y float64) {
	sx := float64(C.getMouseScreenX())
	sy := float64(C.getMouseScreenY())
	if C.hasWindow() == 0 {
		return sx, sy
	}
	// Convert macOS screen coords (Y-up, origin at screen bottom-left) to
	// CSS coords (Y-down, origin at window top-left).
	ox := float64(C.getWindowOriginX())
	oy := float64(C.getWindowOriginY())
	h := float64(C.getWindowHeight())
	x = sx - ox
	y = h - (sy - oy)
	return
}

//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
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

// Forward declaration — implemented as CGO export in Go.
// Must match the signature CGO generates (char*, not const char*).
extern void voiceTranscriptCallback(char *text);

// gHotkeyPipeFd is the write end of a pipe; Go reads from the read end.
static int gHotkeyPipeFd = -1;

// setHotkeyPipeFd stores the write-end fd so the monitor handler can signal Go.
static void setHotkeyPipeFd(int fd) { gHotkeyPipeFd = fd; }

// activateApp brings the application to the foreground on the main thread.
static void activateApp() {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp activateIgnoringOtherApps:YES];
    });
}

// registerGlobalHotkey installs a global NSEvent monitor for double-tap Option.
// Requires Accessibility permission; prompts the user if not yet granted.
// On match, writes a single byte to gHotkeyPipeFd — no CGO call-back needed.
static void registerGlobalHotkey() {
    // NSEventMaskFlagsChanged global monitors require Accessibility permission.
    NSDictionary *opts = @{(__bridge id)kAXTrustedCheckOptionPrompt: @YES};
    BOOL trusted = AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts);
    if (!trusted) {
        NSLog(@"[Aiko] Accessibility not granted yet; global hotkey inactive until relaunch.");
    }

    __block NSTimeInterval lastOptUp = 0;
    __block BOOL optWasDown = NO;
    __block BOOL justTriggered = NO;
    const NSTimeInterval kDoubleTapInterval = 0.5;
    __block BOOL gLongPressTriggered = NO;
    __block dispatch_block_t gLongPressBlock = nil;

    void (^handler)(NSEvent *) = ^(NSEvent *evt) {
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
        } else if (!optDown && !optUp) {
            // Another modifier involved — reset.
            lastOptUp = 0;
            justTriggered = NO;
        }
        optWasDown = optDown;
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
         "return !!(e&&e.closest('.live2d-pet,.chat-bubble,.settings-win,.ctx-menu,.notif-bubble'));"
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

// enableClickThrough sets the window to ignore mouse events by default,
// then installs global and local NSEvent monitors so that the window
// temporarily accepts events only when the cursor is over interactive elements.
static void enableClickThrough() {
    dispatch_async(dispatch_get_main_queue(), ^{
        for (NSWindow *win in [NSApp windows]) {
            gWindow  = win;
            gWebView = findWebView(win.contentView);
            [win setIgnoresMouseEvents:YES];
            break;
        }
        if (!gWindow || !gWebView) return;

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
    });
}
*/
import "C"
import (
	"log/slog"
	"strings"
	"syscall"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

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

// enableClickThrough installs per-pixel click-through for the main window.
func enableClickThrough() {
	C.enableClickThrough()
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
				// 长按 Option ≥1s — 开始录音
				wailsruntime.EventsEmit(globalAppCtx, "voice:start")
				C.startVoiceRecognition()
			case 3:
				// Option 释放 — 停止录音
				C.stopVoiceRecognition()
				wailsruntime.EventsEmit(globalAppCtx, "voice:end")
			}
		}
	}()
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

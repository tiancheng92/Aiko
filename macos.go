//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework ApplicationServices

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <unistd.h>

static id gGlobalMonitor    = nil;
static id gLocalMonitor     = nil;
static id gHotkeyMonitor    = nil;
static NSWindow  *gWindow  = nil;
static WKWebView *gWebView = nil;

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
            if (justTriggered) {
                // Release of the triggering press — skip, don't start a new window.
                justTriggered = NO;
            } else if (lastOptUp == 0) {
                lastOptUp = [NSDate timeIntervalSinceReferenceDate];
            }
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

// hasWindow returns 1 if gWindow is initialized.
static int hasWindow() { return gWindow != nil ? 1 : 0; }

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
	"syscall"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

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
			if globalAppCtx != nil {
				C.activateApp()
				wailsruntime.EventsEmit(globalAppCtx, "bubble:toggle")
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

//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

static id gGlobalMonitor = nil;
static id gLocalMonitor  = nil;
static NSWindow  *gWindow  = nil;
static WKWebView *gWebView = nil;

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
// interactive element (.floating-ball or .chat-bubble). Toggles mouse-event
// passthrough based on the result.
static void hitTestPoint(CGFloat cssX, CGFloat cssY) {
    if (!gWebView || !gWindow) return;
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

// goHotkeyFired is implemented in Go (//export goHotkeyFired).
extern void goHotkeyFired(void);

// enableGlobalHotkey 注册 Cmd+Shift+P 的全局 NSEvent 监听器，直接调用 Go 导出函数。
static void enableGlobalHotkey() {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSEventMask keyMask = NSEventMaskKeyDown;
        [NSEvent addGlobalMonitorForEventsMatchingMask:keyMask
            handler:^(NSEvent *evt) {
                // keyCode 35 = P, flags & (Cmd|Shift)
                NSEventModifierFlags flags = evt.modifierFlags &
                    (NSEventModifierFlagCommand | NSEventModifierFlagShift);
                if (evt.keyCode == 35 &&
                    flags == (NSEventModifierFlagCommand | NSEventModifierFlagShift)) {
                    dispatch_async(dispatch_get_main_queue(), ^{
                        goHotkeyFired();
                    });
                }
            }];
    });
}
*/
import "C"

// enableClickThrough installs per-pixel click-through for the main window.
func enableClickThrough() {
	C.enableClickThrough()
}

// hotkeyHandler is set by RegisterHotkeyCallback and called from C on Cmd+Shift+P.
var hotkeyHandler func()

//export goHotkeyFired
func goHotkeyFired() {
	if hotkeyHandler != nil {
		hotkeyHandler()
	}
}

// RegisterHotkeyCallback registers fn to be called when Cmd+Shift+P is pressed globally.
func RegisterHotkeyCallback(fn func()) {
	hotkeyHandler = fn
	C.enableGlobalHotkey()
}
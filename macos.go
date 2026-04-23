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

// enableClickThrough installs per-pixel click-through for the main window.
func enableClickThrough() {
	C.enableClickThrough()
}

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

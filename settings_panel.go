//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=10.15

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

static NSPanel   *gSettingsPanel   = nil;
static WKWebView *gSettingsWebView = nil;

// openSettingsPanelC creates (or shows) the settings NSPanel.
// url is the full URL to load (e.g. http://localhost:PORT/?panel=settings).
static void openSettingsPanelC(const char *url) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSURL *nsURL = [NSURL URLWithString:[NSString stringWithUTF8String:url]];
        if (!gSettingsPanel) {
            NSRect frame = NSMakeRect(0, 0, 900, 700);
            gSettingsPanel = [[NSPanel alloc]
                initWithContentRect:frame
                styleMask:NSWindowStyleMaskTitled |
                          NSWindowStyleMaskClosable |
                          NSWindowStyleMaskResizable |
                          NSWindowStyleMaskMiniaturizable
                backing:NSBackingStoreBuffered
                defer:NO];
            [gSettingsPanel setTitle:@"Aiko 设置"];
            [gSettingsPanel setLevel:NSNormalWindowLevel];
            [gSettingsPanel setMinSize:NSMakeSize(780, 520)];
            [gSettingsPanel center];

            WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
            gSettingsWebView = [[WKWebView alloc] initWithFrame:frame configuration:cfg];
            [gSettingsWebView setAutoresizingMask:NSViewWidthSizable | NSViewHeightSizable];
            [gSettingsPanel setContentView:gSettingsWebView];
        }
        NSURLRequest *req = [NSURLRequest requestWithURL:nsURL];
        [gSettingsWebView loadRequest:req];
        [gSettingsPanel makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    });
}

// closeSettingsPanelC hides the settings panel without destroying it.
static void closeSettingsPanelC() {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gSettingsPanel) [gSettingsPanel orderOut:nil];
    });
}

// isSettingsPanelVisibleC returns 1 if the panel is open and visible.
static int isSettingsPanelVisibleC() {
    return (gSettingsPanel && [gSettingsPanel isVisible]) ? 1 : 0;
}
*/
import "C"
import "unsafe"

// openSettingsPanel creates (first call) or shows the settings NSPanel
// and loads the Wails frontend URL with the panel query param appended.
func openSettingsPanel(serverURL string) {
	u := serverURL + "?panel=settings"
	cs := C.CString(u)
	defer C.free(unsafe.Pointer(cs))
	C.openSettingsPanelC(cs)
}

// closeSettingsPanel hides the settings NSPanel.
func closeSettingsPanel() {
	C.closeSettingsPanelC()
}

// isSettingsPanelVisible reports whether the panel is currently visible.
func isSettingsPanelVisible() bool {
	return C.isSettingsPanelVisibleC() == 1
}

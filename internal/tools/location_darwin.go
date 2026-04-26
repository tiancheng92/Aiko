//go:build darwin

// internal/tools/location_darwin.go
package tools

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=11.0
#cgo LDFLAGS: -framework CoreLocation -framework Foundation

#import <CoreLocation/CoreLocation.h>
#import <Foundation/Foundation.h>
#include <string.h>

// ---- Location result -------------------------------------------------------

typedef struct {
    double lat;
    double lon;
    double accuracy;
    int    status;   // 0=success 1=denied 2=error/timeout
    char   errMsg[256];
} ToolsLocationResult;

@interface ToolsCLLocationHelper : NSObject <CLLocationManagerDelegate>
@property (strong) CLLocationManager *mgr;
@property (strong) dispatch_semaphore_t sem;
@end

static ToolsLocationResult gToolsLocResult;

@implementation ToolsCLLocationHelper

- (ToolsLocationResult)fetchWithTimeout:(double)secs {
    memset(&gToolsLocResult, 0, sizeof(gToolsLocResult));
    self.sem = dispatch_semaphore_create(0);
    dispatch_async(dispatch_get_main_queue(), ^{
        self.mgr = [[CLLocationManager alloc] init];
        self.mgr.delegate = self;
        self.mgr.desiredAccuracy = kCLLocationAccuracyBest;
        // requestWhenInUseAuthorization triggers locationManagerDidChangeAuthorization:
        // which will call startUpdatingLocation once permission is confirmed.
        [self.mgr requestWhenInUseAuthorization];
    });
    long timedOut = dispatch_semaphore_wait(
        self.sem,
        dispatch_time(DISPATCH_TIME_NOW, (int64_t)(secs * NSEC_PER_SEC))
    );
    dispatch_async(dispatch_get_main_queue(), ^{ [self.mgr stopUpdatingLocation]; });
    if (timedOut) {
        gToolsLocResult.status = 2;
        strncpy(gToolsLocResult.errMsg, "location request timed out", 255);
    }
    return gToolsLocResult;
}

- (void)locationManager:(CLLocationManager *)manager
     didUpdateLocations:(NSArray<CLLocation *> *)locations {
    CLLocation *loc = locations.lastObject;
    gToolsLocResult.lat      = loc.coordinate.latitude;
    gToolsLocResult.lon      = loc.coordinate.longitude;
    gToolsLocResult.accuracy = loc.horizontalAccuracy;
    gToolsLocResult.status   = 0;
    dispatch_semaphore_signal(self.sem);
}

- (void)locationManager:(CLLocationManager *)manager didFailWithError:(NSError *)error {
    gToolsLocResult.status = 2;
    const char *msg = error.localizedDescription.UTF8String;
    strncpy(gToolsLocResult.errMsg, msg ? msg : "unknown error", 255);
    dispatch_semaphore_signal(self.sem);
}

// locationManagerDidChangeAuthorization: replaces the deprecated
// didChangeAuthorizationStatus: (deprecated macOS 14 / iOS 14).
- (void)locationManagerDidChangeAuthorization:(CLLocationManager *)manager {
    CLAuthorizationStatus status = manager.authorizationStatus;
    if (status == kCLAuthorizationStatusDenied ||
        status == kCLAuthorizationStatusRestricted) {
        gToolsLocResult.status = 1;
        strncpy(gToolsLocResult.errMsg, "location permission denied", 255);
        dispatch_semaphore_signal(self.sem);
    } else if (status == kCLAuthorizationStatusAuthorized) {
        // Authorization just granted — (re)start location updates.
        [self.mgr startUpdatingLocation];
    }
}

@end

static ToolsLocationResult getToolsCoreLocation() {
    ToolsCLLocationHelper *h = [[ToolsCLLocationHelper alloc] init];
    return [h fetchWithTimeout:10.0];
}

*/
import "C"
import (
	"fmt"
	"strings"
)

// coreLocation fetches the device's GPS location via CoreLocation on macOS.
func coreLocation() (lat, lon, accuracy float64, err error) {
	r := C.getToolsCoreLocation()
	switch r.status {
	case 0:
		return float64(r.lat), float64(r.lon), float64(r.accuracy), nil
	case 1:
		return 0, 0, 0, fmt.Errorf("位置权限被拒绝，请在系统设置 → 隐私与安全性 → 位置服务 中授权")
	default:
		return 0, 0, 0, fmt.Errorf("%s", clErrorMessage(C.GoString(&r.errMsg[0])))
	}
}

// clErrorMessage converts raw CoreLocation error strings to user-friendly Chinese messages.
func clErrorMessage(raw string) string {
	switch {
	case strings.Contains(raw, "kCLErrorDomain error 0"):
		return "无法获取位置（设备无 GPS 或位置信号不足）"
	case strings.Contains(raw, "kCLErrorDomain error 1"):
		return "位置权限被拒绝"
	case strings.Contains(raw, "kCLErrorDomain error 2"):
		return "网络不可用，无法获取位置"
	case strings.Contains(raw, "timed out"):
		return "定位超时"
	default:
		return raw
	}
}

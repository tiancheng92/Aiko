//go:build darwin

// internal/tools/location_darwin.go
package tools

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreLocation -framework Foundation

#import <CoreLocation/CoreLocation.h>
#import <Foundation/Foundation.h>
#include <string.h>

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
        [self.mgr requestWhenInUseAuthorization];
        [self.mgr startUpdatingLocation];
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

- (void)locationManager:(CLLocationManager *)manager
    didChangeAuthorizationStatus:(CLAuthorizationStatus)status {
    if (status == kCLAuthorizationStatusDenied ||
        status == kCLAuthorizationStatusRestricted) {
        gToolsLocResult.status = 1;
        strncpy(gToolsLocResult.errMsg, "location permission denied", 255);
        dispatch_semaphore_signal(self.sem);
    }
}

@end

static ToolsLocationResult getToolsCoreLocation() {
    ToolsCLLocationHelper *h = [[ToolsCLLocationHelper alloc] init];
    return [h fetchWithTimeout:10.0];
}
*/
import "C"
import "fmt"

// coreLocation fetches the device's GPS location via CoreLocation on macOS.
// Returns lat, lon, accuracy (metres), and error.
func coreLocation() (lat, lon, accuracy float64, err error) {
	r := C.getToolsCoreLocation()
	switch r.status {
	case 0:
		return float64(r.lat), float64(r.lon), float64(r.accuracy), nil
	case 1:
		return 0, 0, 0, fmt.Errorf("位置权限被拒绝，请在系统设置 → 隐私与安全性 → 位置服务 中授权")
	default:
		return 0, 0, 0, fmt.Errorf("%s", C.GoString(&r.errMsg[0]))
	}
}

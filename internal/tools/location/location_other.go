//go:build !darwin

// internal/tools/location/location_other.go
package location

import "fmt"

// coreLocation is not available on non-darwin platforms.
func coreLocation() (lat, lon, accuracy float64, err error) {
	return 0, 0, 0, fmt.Errorf("CoreLocation only available on macOS")
}

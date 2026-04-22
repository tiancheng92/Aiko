//go:build !darwin

// internal/tools/location_other.go
package tools

import "fmt"

// coreLocation is not available on non-darwin platforms.
func coreLocation() (lat, lon, accuracy float64, err error) {
	return 0, 0, 0, fmt.Errorf("CoreLocation only available on macOS")
}

// reverseGeocode is not available on non-darwin platforms.
func reverseGeocode(lat, lon float64) string { return "" }

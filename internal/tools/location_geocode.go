// internal/tools/location_geocode.go
package tools

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	json "github.com/bytedance/sonic"
)

const geocodeTimeout = 8 * time.Second

// nominatimResponse maps the relevant fields from the Nominatim reverse-geocode API.
type nominatimResponse struct {
	DisplayName string `json:"display_name"`
	Error       string `json:"error"`
}

// reverseGeocode converts GPS coordinates to a human-readable address via the
// OpenStreetMap Nominatim API. Returns an empty string if geocoding fails (non-fatal).
func reverseGeocode(lat, lon float64) string {
	reqURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/reverse?format=json&lat=%f&lon=%f&zoom=16&addressdetails=0",
		lat, lon,
	)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return ""
	}
	// Nominatim requires a descriptive User-Agent per usage policy.
	req.Header.Set("User-Agent", "Aiko-DesktopPet/1.0 (https://github.com/tiancheng92/Aiko)")
	req.Header.Set("Accept-Language", url.QueryEscape("zh-CN,zh;q=0.9,en;q=0.8"))

	client := &http.Client{Timeout: geocodeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}

	var r nominatimResponse
	if err := json.Unmarshal(body, &r); err != nil || r.Error != "" {
		return ""
	}
	return r.DisplayName
}

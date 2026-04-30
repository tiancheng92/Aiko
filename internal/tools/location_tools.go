// internal/tools/location_tools.go
package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const locationTimeout = 10 * time.Second

// ipAPIResponse maps the JSON response from ip-api.com.
type ipAPIResponse struct {
	Status      string  `json:"status"`
	Message     string  `json:"message"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Zip         string  `json:"zip"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Timezone    string  `json:"timezone"`
	ISP         string  `json:"isp"`
	Query       string  `json:"query"`
}

// GetLocationTool returns geographic location via CoreLocation (macOS) or IP geolocation fallback.
type GetLocationTool struct{}

// Name returns the tool identifier.
func (t *GetLocationTool) Name() string { return "get_location" }

// Permission marks the tool as protected (requires user opt-in).
func (t *GetLocationTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema.
func (t *GetLocationTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: "获取当前地理位置。在 macOS 上优先使用系统 CoreLocation 获取精确 GPS 坐标，失败时回退到公网 IP 定位。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// InvokableRun tries CoreLocation first, falls back to ip-api.com on error.
func (t *GetLocationTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	lat, lon, accuracy, err := coreLocation()
	if err == nil {
		result := fmt.Sprintf("来源: CoreLocation (GPS)\n坐标: %.6f, %.6f\n精度: %.0f 米", lat, lon, accuracy)
		if addr := reverseGeocode(lat, lon); addr != "" {
			result += "\n地址: " + addr
		}
		return result, nil
	}

	// GPS not available — fall back to IP geolocation silently.
	ipResult, ipErr := ipLocation()
	if ipErr != nil {
		return "", fmt.Errorf("定位失败: %w", ipErr)
	}
	return "来源: IP 定位\n" + ipResult, nil
}

// FetchLocation returns a compact location string for context injection.
// It tries CoreLocation (GPS) first; falls back to IP geolocation on failure.
func FetchLocation() string {
	// Try GPS via CoreLocation first.
	lat, lon, _, err := coreLocation()
	if err == nil {
		addr := reverseGeocode(lat, lon)
		if addr != "" {
			return addr
		}
		return fmt.Sprintf("%.4f, %.4f", lat, lon)
	}

	// Fall back to IP geolocation.
	result, err := ipLocation()
	if err != nil {
		return ""
	}
	var country, region, city, timezone string
	for _, line := range strings.Split(result, "\n") {
		switch {
		case strings.HasPrefix(line, "国家:"):
			country = strings.TrimPrefix(line, "国家: ")
		case strings.HasPrefix(line, "地区:"):
			region = strings.TrimPrefix(line, "地区: ")
		case strings.HasPrefix(line, "城市:"):
			city = strings.TrimPrefix(line, "城市: ")
		case strings.HasPrefix(line, "时区:"):
			timezone = strings.TrimPrefix(line, "时区: ")
		}
	}
	if city == "" && country == "" {
		return ""
	}
	return fmt.Sprintf("%s, %s, %s (时区: %s)", city, region, country, timezone)
}

// ipLocation fetches approximate location via ip-api.com.
func ipLocation() (string, error) {
	client := &http.Client{Timeout: locationTimeout}
	resp, err := client.Get("http://ip-api.com/json/?fields=status,message,country,countryCode,regionName,city,zip,lat,lon,timezone,isp,query")
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var r ipAPIResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if r.Status != "success" {
		return "", fmt.Errorf("ip-api: %s", r.Message)
	}

	return fmt.Sprintf(
		"IP: %s\n国家: %s (%s)\n地区: %s\n城市: %s\n邮编: %s\n坐标: %.4f, %.4f\n时区: %s\nISP: %s",
		r.Query, r.Country, r.CountryCode, r.RegionName, r.City, r.Zip,
		r.Lat, r.Lon, r.Timezone, r.ISP,
	), nil
}

// internal/tools/location_tools.go
package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

// GetLocationTool returns approximate geographic location based on the current public IP.
type GetLocationTool struct{}

// Name returns the tool identifier.
func (t *GetLocationTool) Name() string { return "get_location" }

// Permission marks the tool as protected (requires user opt-in).
func (t *GetLocationTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema.
func (t *GetLocationTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.Name(),
		Desc:        "根据当前公网 IP 获取近似地理位置，返回国家、城市、经纬度、时区等信息。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// InvokableRun calls ip-api.com and returns location info as a formatted string.
func (t *GetLocationTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	client := &http.Client{Timeout: locationTimeout}
	resp, err := client.Get("http://ip-api.com/json/?fields=status,message,country,countryCode,regionName,city,zip,lat,lon,timezone,isp,query")
	if err != nil {
		return "", fmt.Errorf("location request failed: %w", err)
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
		return "", fmt.Errorf("location lookup failed: %s", r.Message)
	}

	return fmt.Sprintf(
		"IP: %s\n国家: %s (%s)\n地区: %s\n城市: %s\n邮编: %s\n坐标: %.4f, %.4f\n时区: %s\nISP: %s",
		r.Query, r.Country, r.CountryCode, r.RegionName, r.City, r.Zip,
		r.Lat, r.Lon, r.Timezone, r.ISP,
	), nil
}

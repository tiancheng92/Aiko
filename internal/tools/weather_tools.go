// internal/tools/weather_tools.go
package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const weatherTimeout = 10 * time.Second

// wttrResponse maps the relevant fields from wttr.in JSON format=j1 response.
type wttrResponse struct {
	CurrentCondition []struct {
		TempC          string `json:"temp_C"`
		TempF          string `json:"temp_F"`
		FeelsLikeC     string `json:"FeelsLikeC"`
		Humidity       string `json:"humidity"`
		WindspeedKmph  string `json:"windspeedKmph"`
		WinddirDegree  string `json:"winddirDegree"`
		WeatherDesc    []struct{ Value string `json:"value"` } `json:"weatherDesc"`
		Visibility     string `json:"visibility"`
		Pressure       string `json:"pressure"`
		UVIndex        string `json:"uvIndex"`
	} `json:"current_condition"`
	NearestArea []struct {
		AreaName    []struct{ Value string `json:"value"` } `json:"areaName"`
		Country     []struct{ Value string `json:"value"` } `json:"country"`
		Region      []struct{ Value string `json:"value"` } `json:"region"`
	} `json:"nearest_area"`
}

// GetWeatherTool returns current weather for a given location.
type GetWeatherTool struct{}

// Name returns the tool identifier.
func (t *GetWeatherTool) Name() string { return "get_weather" }

// Permission marks the tool as protected.
func (t *GetWeatherTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema.
func (t *GetWeatherTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: "查询指定城市或地点的当前天气，返回温度、湿度、风速、天气描述等信息。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"location": {
				Type:     schema.String,
				Desc:     "查询地点，支持城市名（中英文均可）、经纬度（如 \"31.2,121.4\"）。留空则根据当前 IP 定位。",
				Required: false,
			},
		}),
	}, nil
}

// InvokableRun fetches weather from wttr.in and returns a formatted summary.
func (t *GetWeatherTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	var params struct {
		Location string `json:"location"`
	}
	_ = json.Unmarshal([]byte(input), &params)

	loc := params.Location
	if loc == "" {
		loc = "" // wttr.in auto-detects from IP when location is empty
	}

	apiURL := fmt.Sprintf("https://wttr.in/%s?format=j1", url.PathEscape(loc))
	client := &http.Client{Timeout: weatherTimeout}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("weather service returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var r wttrResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(r.CurrentCondition) == 0 {
		return "未能获取天气信息", nil
	}

	cur := r.CurrentCondition[0]
	desc := ""
	if len(cur.WeatherDesc) > 0 {
		desc = cur.WeatherDesc[0].Value
	}

	place := loc
	if len(r.NearestArea) > 0 {
		a := r.NearestArea[0]
		parts := []string{}
		if len(a.AreaName) > 0 {
			parts = append(parts, a.AreaName[0].Value)
		}
		if len(a.Region) > 0 {
			parts = append(parts, a.Region[0].Value)
		}
		if len(a.Country) > 0 {
			parts = append(parts, a.Country[0].Value)
		}
		if len(parts) > 0 {
			joined := parts[0]
			for _, p := range parts[1:] {
				if p != parts[0] {
					joined += ", " + p
				}
			}
			place = joined
		}
	}

	return fmt.Sprintf(
		"地点: %s\n天气: %s\n温度: %s°C (%s°F)，体感 %s°C\n湿度: %s%%\n风速: %s km/h\n能见度: %s km\n气压: %s hPa\nUV指数: %s",
		place, desc, cur.TempC, cur.TempF, cur.FeelsLikeC,
		cur.Humidity, cur.WindspeedKmph, cur.Visibility, cur.Pressure, cur.UVIndex,
	), nil
}

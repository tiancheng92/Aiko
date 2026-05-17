// internal/tools/dev/dev_convert.go
package dev

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/tools/base"
)

// ── 7. convert_timestamp ──────────────────────────────────────────────────────

// ConvertTimestampTool converts between Unix timestamps and human-readable datetime strings.
type ConvertTimestampTool struct{}

func (t *ConvertTimestampTool) Name() string                    { return "convert_timestamp" }
func (t *ConvertTimestampTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for convert_timestamp.
func (t *ConvertTimestampTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "Unix 时间戳与可读时间字符串互转，unix→datetime 时返回多时区对照。",
		map[string]*schema.ParameterInfo{
			"value":     {Type: schema.String, Desc: "Unix 时间戳（整数）或日期时间字符串（RFC3339 / 常见格式）", Required: true},
			"direction": {Type: schema.String, Desc: "unix_to_datetime 或 datetime_to_unix", Required: true},
			"timezone":  {Type: schema.String, Desc: "IANA 时区名，如 Asia/Shanghai，默认 local"},
		}), nil
}

// InvokableRun converts timestamps.
func (t *ConvertTimestampTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	value, _ := args["value"].(string)
	direction, _ := args["direction"].(string)
	tzName, _ := args["timezone"].(string)

	loc := time.Local
	if tzName != "" && tzName != "local" {
		var err error
		loc, err = time.LoadLocation(tzName)
		if err != nil {
			return "", fmt.Errorf("unknown timezone %q: %w", tzName, err)
		}
	}

	switch direction {
	case "unix_to_datetime":
		ts, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid Unix timestamp %q: %w", value, err)
		}
		tm := time.Unix(ts, 0)
		utc := tm.UTC()
		local := tm.In(time.Local)
		requested := tm.In(loc)
		lines := []string{
			fmt.Sprintf("UTC:   %s", utc.Format(time.RFC3339)),
			fmt.Sprintf("Local: %s", local.Format(time.RFC3339)),
		}
		if loc != time.Local {
			lines = append(lines, fmt.Sprintf("%s: %s", tzName, requested.Format(time.RFC3339)))
		}
		return strings.Join(lines, "\n"), nil

	case "datetime_to_unix":
		layouts := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		var parsed time.Time
		var parseErr error
		for _, layout := range layouts {
			parsed, parseErr = time.ParseInLocation(layout, strings.TrimSpace(value), loc)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			return "", fmt.Errorf("cannot parse datetime %q; supported formats: RFC3339, YYYY-MM-DD HH:MM:SS, YYYY-MM-DD", value)
		}
		return fmt.Sprintf("Unix 时间戳: %d", parsed.Unix()), nil

	default:
		return "", fmt.Errorf("direction must be unix_to_datetime or datetime_to_unix")
	}
}

// ── 9. number_base_convert ────────────────────────────────────────────────────

// NumberBaseConvertTool converts an integer between number bases 2/8/10/16.
type NumberBaseConvertTool struct{}

func (t *NumberBaseConvertTool) Name() string                    { return "number_base_convert" }
func (t *NumberBaseConvertTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for number_base_convert.
func (t *NumberBaseConvertTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "整数进制转换（二 / 八 / 十 / 十六进制），结果带进制前缀。",
		map[string]*schema.ParameterInfo{
			"value": {Type: schema.String, Desc: "源进制表示的数字字符串（如 \"FF\"、\"255\"、\"11111111\"）", Required: true},
			"from":  {Type: schema.String, Desc: "源进制: 2 / 8 / 10 / 16", Required: true},
			"to":    {Type: schema.String, Desc: "目标进制: 2 / 8 / 10 / 16", Required: true},
		}), nil
}

// InvokableRun converts the number between the specified bases.
func (t *NumberBaseConvertTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	value, _ := args["value"].(string)
	fromStr, _ := args["from"].(string)
	toStr, _ := args["to"].(string)

	fromBase, err := strconv.Atoi(fromStr)
	if err != nil || (fromBase != 2 && fromBase != 8 && fromBase != 10 && fromBase != 16) {
		return "", fmt.Errorf("from must be 2, 8, 10, or 16")
	}
	toBase, err := strconv.Atoi(toStr)
	if err != nil || (toBase != 2 && toBase != 8 && toBase != 10 && toBase != 16) {
		return "", fmt.Errorf("to must be 2, 8, 10, or 16")
	}

	clean := strings.TrimSpace(value)
	clean = strings.TrimPrefix(clean, "0x")
	clean = strings.TrimPrefix(clean, "0X")
	clean = strings.TrimPrefix(clean, "0b")
	clean = strings.TrimPrefix(clean, "0B")
	clean = strings.TrimPrefix(clean, "0o")
	clean = strings.TrimPrefix(clean, "0O")

	n, err := strconv.ParseInt(clean, fromBase, 64)
	if err != nil {
		return "", fmt.Errorf("cannot parse %q as base-%d: %w", value, fromBase, err)
	}

	var result string
	switch toBase {
	case 2:
		result = "0b" + strconv.FormatInt(n, 2)
	case 8:
		result = "0o" + strconv.FormatInt(n, 8)
	case 10:
		result = strconv.FormatInt(n, 10)
	case 16:
		result = "0x" + strings.ToUpper(strconv.FormatInt(n, 16))
	}
	return fmt.Sprintf("%s (base %d) → %s (base %d)", value, fromBase, result, toBase), nil
}

// ── 10. convert_units ─────────────────────────────────────────────────────────

// ConvertUnitsTool converts physical quantities between units across seven categories.
type ConvertUnitsTool struct{}

func (t *ConvertUnitsTool) Name() string                    { return "convert_units" }
func (t *ConvertUnitsTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for convert_units.
func (t *ConvertUnitsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(),
		"物理量单位换算（长度/重量/温度/面积/体积/速度/数据）。单位标识符: mm/cm/m/km/inch/ft/yard/mile | mg/g/kg/ton/lb/oz | C/F/K | mm2/cm2/m2/km2/acre/hectare | ml/l/gallon/fl_oz/cup | m_s/km_h/mph/knot | bit/byte/KB/MB/GB/TB",
		map[string]*schema.ParameterInfo{
			"value":     {Type: schema.Number, Desc: "待换算的数值", Required: true},
			"from_unit": {Type: schema.String, Desc: "源单位标识符", Required: true},
			"to_unit":   {Type: schema.String, Desc: "目标单位标识符", Required: true},
		}), nil
}

var (
	reRGB = regexp.MustCompile(`(?i)^rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)$`)
	reHSL = regexp.MustCompile(`(?i)^hsl\(\s*([\d.]+)\s*,\s*([\d.]+)%\s*,\s*([\d.]+)%\s*\)$`)
)

// unitToMeters maps length units to their equivalent in meters.
var unitToMeters = map[string]float64{
	"mm": 0.001, "cm": 0.01, "m": 1, "km": 1000,
	"inch": 0.0254, "ft": 0.3048, "yard": 0.9144, "mile": 1609.344,
}

// unitToGrams maps weight units to their equivalent in grams.
var unitToGrams = map[string]float64{
	"mg": 0.001, "g": 1, "kg": 1000, "ton": 1e6,
	"lb": 453.592, "oz": 28.3495,
}

// unitToM2 maps area units to their equivalent in square meters.
var unitToM2 = map[string]float64{
	"mm2": 1e-6, "cm2": 1e-4, "m2": 1, "km2": 1e6,
	"acre": 4046.856, "hectare": 10000,
}

// unitToML maps volume units to their equivalent in millilitres.
var unitToML = map[string]float64{
	"ml": 1, "l": 1000, "gallon": 3785.41, "fl_oz": 29.5735, "cup": 236.588,
}

// unitToMS maps speed units to their equivalent in metres per second.
var unitToMS = map[string]float64{
	"m_s": 1, "km_h": 1.0 / 3.6, "mph": 0.44704, "knot": 0.514444,
}

// unitToBits maps data units to their equivalent in bits.
var unitToBits = map[string]float64{
	"bit": 1, "byte": 8,
	"KB": 8 * 1024, "MB": 8 * 1024 * 1024,
	"GB": 8 * 1024 * 1024 * 1024, "TB": 8 * 1024 * 1024 * 1024 * 1024,
}

// InvokableRun converts the value between the specified units.
func (t *ConvertUnitsTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	value, ok := args["value"].(float64)
	if !ok {
		return "", fmt.Errorf("value must be a number")
	}
	from, _ := args["from_unit"].(string)
	to, _ := args["to_unit"].(string)

	result, err := convertUnits(value, from, to)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%g %s = %g %s", value, from, result, to), nil
}

// convertUnits performs the actual unit conversion.
func convertUnits(value float64, from, to string) (float64, error) {
	if isTemp(from) || isTemp(to) {
		return convertTemp(value, from, to)
	}

	tables := []map[string]float64{unitToMeters, unitToGrams, unitToM2, unitToML, unitToMS, unitToBits}
	for _, table := range tables {
		fv, fok := table[from]
		tv, tok := table[to]
		if fok && tok {
			return value * fv / tv, nil
		}
	}
	return 0, fmt.Errorf("cannot convert %q to %q — check unit identifiers", from, to)
}

func isTemp(u string) bool { return u == "C" || u == "F" || u == "K" }

// convertTemp converts between Celsius, Fahrenheit, and Kelvin.
func convertTemp(value float64, from, to string) (float64, error) {
	var celsius float64
	switch from {
	case "C":
		celsius = value
	case "F":
		celsius = (value - 32) * 5 / 9
	case "K":
		celsius = value - 273.15
	default:
		return 0, fmt.Errorf("unknown temperature unit %q", from)
	}
	switch to {
	case "C":
		return celsius, nil
	case "F":
		return celsius*9/5 + 32, nil
	case "K":
		return celsius + 273.15, nil
	default:
		return 0, fmt.Errorf("unknown temperature unit %q", to)
	}
}

// ── 11. get_exchange_rate ─────────────────────────────────────────────────────

// GetExchangeRateTool fetches real-time exchange rates and converts an amount.
type GetExchangeRateTool struct{}

func (t *GetExchangeRateTool) Name() string                    { return "get_exchange_rate" }
func (t *GetExchangeRateTool) Permission() base.PermissionLevel { return base.PermProtected }

// Info returns the eino tool schema for get_exchange_rate.
func (t *GetExchangeRateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "查询实时外汇汇率并换算金额。",
		map[string]*schema.ParameterInfo{
			"base":    {Type: schema.String, Desc: "基础货币代码，如 USD、CNY", Required: true},
			"targets": {Type: schema.String, Desc: "目标货币代码列表，格式为 JSON 数组字符串，如 \"[\\\"USD\\\",\\\"EUR\\\"]\"；留空则返回主要货币"},
			"amount":  {Type: schema.Number, Desc: "换算金额，默认 1"},
		}), nil
}

var defaultCurrencies = []string{"USD", "EUR", "CNY", "JPY", "GBP", "HKD", "KRW"}

// InvokableRun fetches rates and returns a conversion table.
func (t *GetExchangeRateTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	base_, _ := args["base"].(string)
	base_ = strings.ToUpper(strings.TrimSpace(base_))
	amount := 1.0
	if v, ok := args["amount"].(float64); ok && v > 0 {
		amount = v
	}

	var targets []string
	if v, ok := args["targets"].(string); ok && v != "" {
		_ = json.UnmarshalString(v, &targets) // best-effort: fall back to comma-split if JSON parse fails
		if len(targets) == 0 {
			for _, c := range strings.Split(v, ",") {
				c = strings.TrimSpace(strings.ToUpper(c))
				if c != "" {
					targets = append(targets, c)
				}
			}
		}
	}
	if len(targets) == 0 {
		targets = defaultCurrencies
	}

	apiURL := fmt.Sprintf("https://open.er-api.com/v6/latest/%s", url.PathEscape(base_))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("exchange rate request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Result != "success" {
		return fmt.Sprintf("汇率服务返回错误，请检查货币代码 %q 是否正确", base_), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("基础货币: %s (金额: %g)", base_, amount))
	lines = append(lines, "---")
	for _, cur := range targets {
		rate, ok := result.Rates[strings.ToUpper(cur)]
		if !ok {
			lines = append(lines, fmt.Sprintf("%s: 不支持", cur))
			continue
		}
		converted := amount * rate
		lines = append(lines, fmt.Sprintf("%s: 1 %s = %.6f %s → %g %s = %.2f %s",
			cur, base_, rate, cur, amount, base_, converted, cur))
	}
	return strings.Join(lines, "\n"), nil
}

// ── 12. convert_color ─────────────────────────────────────────────────────────

// ConvertColorTool converts color values between HEX, RGB, and HSL formats.
type ConvertColorTool struct{}

func (t *ConvertColorTool) Name() string                    { return "convert_color" }
func (t *ConvertColorTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for convert_color.
func (t *ConvertColorTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "色值格式互转（HEX / RGB / HSL）。输入支持 #RRGGBB、#RGB、rgb(r,g,b)、hsl(h,s%,l%)。",
		map[string]*schema.ParameterInfo{
			"input":  {Type: schema.String, Desc: "颜色值，支持 #RRGGBB / #RGB / rgb(r,g,b) / hsl(h,s%,l%)", Required: true},
			"output": {Type: schema.String, Desc: "输出格式: hex / rgb / hsl / all（默认）"},
		}), nil
}

// InvokableRun converts color between formats.
func (t *ConvertColorTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	colorInput, _ := args["input"].(string)
	output, _ := args["output"].(string)
	if output == "" {
		output = "all"
	}

	r, g, b, err := parseColor(strings.TrimSpace(colorInput))
	if err != nil {
		return "", err
	}

	hexStr := fmt.Sprintf("#%02X%02X%02X", r, g, b)
	rgbStr := fmt.Sprintf("rgb(%d, %d, %d)", r, g, b)
	h, s, l := rgbToHSL(r, g, b)
	hslStr := fmt.Sprintf("hsl(%.1f, %.1f%%, %.1f%%)", h, s*100, l*100)

	switch output {
	case "hex":
		return hexStr, nil
	case "rgb":
		return rgbStr, nil
	case "hsl":
		return hslStr, nil
	default:
		return fmt.Sprintf("HEX: %s\nRGB: %s\nHSL: %s", hexStr, rgbStr, hslStr), nil
	}
}

// parseColor parses any supported color format into RGB components.
func parseColor(s string) (r, g, b uint8, err error) {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "#") {
		hexStr := s[1:]
		if len(hexStr) == 3 {
			hexStr = string([]byte{hexStr[0], hexStr[0], hexStr[1], hexStr[1], hexStr[2], hexStr[2]})
		}
		if len(hexStr) != 6 {
			return 0, 0, 0, fmt.Errorf("invalid hex color %q", s)
		}
		var rgb uint64
		rgb, err = strconv.ParseUint(hexStr, 16, 32)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid hex color %q: %w", s, err)
		}
		return uint8(rgb >> 16), uint8((rgb >> 8) & 0xFF), uint8(rgb & 0xFF), nil
	}

	if m := reRGB.FindStringSubmatch(s); m != nil {
		rv, _ := strconv.Atoi(m[1])
		gv, _ := strconv.Atoi(m[2])
		bv, _ := strconv.Atoi(m[3])
		return uint8(rv), uint8(gv), uint8(bv), nil
	}

	if m := reHSL.FindStringSubmatch(s); m != nil {
		h, _ := strconv.ParseFloat(m[1], 64)
		sv, _ := strconv.ParseFloat(m[2], 64)
		lv, _ := strconv.ParseFloat(m[3], 64)
		r8, g8, b8 := hslToRGB(h, sv/100, lv/100)
		return r8, g8, b8, nil
	}

	return 0, 0, 0, fmt.Errorf("unsupported color format %q; use #RRGGBB, #RGB, rgb(r,g,b), or hsl(h,s%%,l%%)", s)
}

// rgbToHSL converts RGB (0-255) to HSL (h 0-360, s 0-1, l 0-1).
func rgbToHSL(r, g, b uint8) (h, s, l float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	l = (max + min) / 2
	if max == min {
		return 0, 0, l
	}
	d := max - min
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	switch max {
	case rf:
		h = (gf-bf)/d + map[bool]float64{true: 0, false: 6}[gf >= bf]
	case gf:
		h = (bf-rf)/d + 2
	case bf:
		h = (rf-gf)/d + 4
	}
	h *= 60
	return h, s, l
}

// hslToRGB converts HSL (h 0-360, s 0-1, l 0-1) to RGB (0-255).
func hslToRGB(h, s, l float64) (uint8, uint8, uint8) {
	if s == 0 {
		v := uint8(l * 255)
		return v, v, v
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := h / 360
	rf := hueToRGB(p, q, hk+1.0/3)
	gf := hueToRGB(p, q, hk)
	bf := hueToRGB(p, q, hk-1.0/3)
	return uint8(math.Round(rf * 255)), uint8(math.Round(gf * 255)), uint8(math.Round(bf * 255))
}

// hueToRGB is a helper for HSL→RGB conversion.
func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 1.0/2:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	}
	return p
}

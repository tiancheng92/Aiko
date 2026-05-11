// internal/tools/dev_tools.go
package tools

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// ── 1. format_json ────────────────────────────────────────────────────────────

// FormatJSONTool formats, minifies, or validates a JSON string.
type FormatJSONTool struct{}

func (t *FormatJSONTool) Name() string               { return "format_json" }
func (t *FormatJSONTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for format_json.
func (t *FormatJSONTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "JSON 格式化（缩进美化）、压缩（单行）或语法校验。",
		map[string]*schema.ParameterInfo{
			"json_string": {Type: schema.String, Desc: "待处理的 JSON 字符串", Required: true},
			"action":      {Type: schema.String, Desc: "操作类型: pretty（默认）/ minify / validate"},
			"indent":      {Type: schema.Integer, Desc: "pretty 模式每级缩进空格数，默认 2"},
		}), nil
}

// InvokableRun formats, minifies, or validates JSON.
func (t *FormatJSONTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	jsonStr, _ := args["json_string"].(string)
	action, _ := args["action"].(string)
	if action == "" {
		action = "pretty"
	}
	indent := 2
	if v, ok := args["indent"].(float64); ok && v > 0 {
		indent = int(v)
	}

	var raw any
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		if action == "validate" {
			return fmt.Sprintf("invalid JSON — error: %s", err.Error()), nil
		}
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	switch action {
	case "validate":
		return "valid JSON ✓", nil
	case "minify":
		b, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		return string(b), nil
	default: // pretty
		b, err := json.MarshalIndent(raw, "", strings.Repeat(" ", indent))
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

// ── 2. json_to_struct ─────────────────────────────────────────────────────────

// JSONToStructTool converts a JSON object to a struct definition in the target language.
type JSONToStructTool struct{}

func (t *JSONToStructTool) Name() string               { return "json_to_struct" }
func (t *JSONToStructTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for json_to_struct.
func (t *JSONToStructTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "将 JSON 对象转换为 Go / TypeScript / Python / Rust 结构体定义。",
		map[string]*schema.ParameterInfo{
			"json_string": {Type: schema.String, Desc: "有效的 JSON 对象或数组", Required: true},
			"language":    {Type: schema.String, Desc: "目标语言: go / typescript / python / rust", Required: true},
			"type_name":   {Type: schema.String, Desc: "根类型名，默认 Root"},
		}), nil
}

// InvokableRun generates struct code from a JSON string.
func (t *JSONToStructTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	jsonStr, _ := args["json_string"].(string)
	lang, _ := args["language"].(string)
	typeName, _ := args["type_name"].(string)
	if typeName == "" {
		typeName = "Root"
	}

	var raw any
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	// Unwrap array: use first element as representative object.
	if arr, ok := raw.([]any); ok {
		if len(arr) == 0 {
			return "// empty array — cannot infer element type", nil
		}
		raw = arr[0]
	}

	obj, ok := raw.(map[string]any)
	if !ok {
		return "", fmt.Errorf("JSON root must be an object or array of objects")
	}

	switch strings.ToLower(lang) {
	case "go":
		return jsonToGo(typeName, obj), nil
	case "typescript":
		return jsonToTypeScript(typeName, obj), nil
	case "python":
		return jsonToPython(typeName, obj), nil
	case "rust":
		return jsonToRust(typeName, obj), nil
	default:
		return "", fmt.Errorf("unsupported language %q; use go/typescript/python/rust", lang)
	}
}

// jsonInferGoType infers a Go type string from a JSON value.
func jsonInferGoType(v any) string {
	switch val := v.(type) {
	case nil:
		return "any"
	case bool:
		return "bool"
	case float64:
		if val == math.Trunc(val) {
			return "int"
		}
		return "float64"
	case string:
		return "string"
	case []any:
		if len(val) == 0 {
			return "[]any"
		}
		return "[]" + jsonInferGoType(val[0])
	case map[string]any:
		return "" // caller handles nested struct
	}
	return "any"
}

// toPascalCase converts a JSON key to PascalCase.
func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var b strings.Builder
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		runes := []rune(p)
		b.WriteRune(unicode.ToUpper(runes[0]))
		b.WriteString(string(runes[1:]))
	}
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

// jsonToGo generates Go struct definitions (nested inline).
func jsonToGo(name string, obj map[string]any) string {
	var nested []string
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("type %s struct {\n", name))

	keys := sortedKeys(obj)
	for _, k := range keys {
		v := obj[k]
		field := toPascalCase(k)
		var typStr string
		if sub, ok := v.(map[string]any); ok {
			subName := toPascalCase(k)
			nested = append(nested, jsonToGo(subName, sub))
			typStr = subName
		} else {
			typStr = jsonInferGoType(v)
			if typStr == "" {
				typStr = "any"
			}
		}
		buf.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`\n", field, typStr, k))
	}
	buf.WriteString("}")

	if len(nested) > 0 {
		return strings.Join(nested, "\n\n") + "\n\n" + buf.String()
	}
	return buf.String()
}

// jsonToTypeScript generates a TypeScript interface definition.
func jsonToTypeScript(name string, obj map[string]any) string {
	var nested []string
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("interface %s {\n", name))

	keys := sortedKeys(obj)
	for _, k := range keys {
		v := obj[k]
		var typStr string
		optional := ""
		if v == nil {
			typStr = "unknown | null"
			optional = "?"
		} else if sub, ok := v.(map[string]any); ok {
			subName := toPascalCase(k)
			nested = append(nested, jsonToTypeScript(subName, sub))
			typStr = subName
		} else {
			typStr = jsonInferTSType(v)
		}
		buf.WriteString(fmt.Sprintf("  %s%s: %s;\n", k, optional, typStr))
	}
	buf.WriteString("}")

	if len(nested) > 0 {
		return strings.Join(nested, "\n\n") + "\n\n" + buf.String()
	}
	return buf.String()
}

// jsonInferTSType infers a TypeScript type string from a JSON value.
func jsonInferTSType(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		if len(val) == 0 {
			return "unknown[]"
		}
		return jsonInferTSType(val[0]) + "[]"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// jsonToPython generates a Python @dataclass definition.
func jsonToPython(name string, obj map[string]any) string {
	var nested []string
	var buf strings.Builder
	buf.WriteString("from dataclasses import dataclass\nfrom typing import Optional, List\n\n")
	buf.WriteString("@dataclass\n")
	buf.WriteString(fmt.Sprintf("class %s:\n", name))

	keys := sortedKeys(obj)
	for _, k := range keys {
		v := obj[k]
		var typStr string
		if v == nil {
			typStr = "Optional[object]"
		} else if sub, ok := v.(map[string]any); ok {
			subName := toPascalCase(k)
			nested = append(nested, jsonToPythonClass(subName, sub))
			typStr = subName
		} else {
			typStr = jsonInferPyType(v)
		}
		buf.WriteString(fmt.Sprintf("    %s: %s\n", k, typStr))
	}

	if len(nested) > 0 {
		return strings.Join(nested, "\n\n") + "\n\n" + buf.String()
	}
	return buf.String()
}

// jsonToPythonClass generates a Python @dataclass without the import header (for nested classes).
func jsonToPythonClass(name string, obj map[string]any) string {
	var buf strings.Builder
	buf.WriteString("@dataclass\n")
	buf.WriteString(fmt.Sprintf("class %s:\n", name))
	keys := sortedKeys(obj)
	for _, k := range keys {
		v := obj[k]
		typStr := jsonInferPyType(v)
		buf.WriteString(fmt.Sprintf("    %s: %s\n", k, typStr))
	}
	return buf.String()
}

// jsonInferPyType infers a Python type annotation from a JSON value.
func jsonInferPyType(v any) string {
	switch val := v.(type) {
	case nil:
		return "Optional[object]"
	case bool:
		return "bool"
	case float64:
		if val == math.Trunc(val) {
			return "int"
		}
		return "float"
	case string:
		return "str"
	case []any:
		if len(val) == 0 {
			return "List[object]"
		}
		return "List[" + jsonInferPyType(val[0]) + "]"
	case map[string]any:
		return "object"
	}
	return "object"
}

// jsonToRust generates a Rust struct definition with serde derives.
func jsonToRust(name string, obj map[string]any) string {
	var nested []string
	var buf strings.Builder
	buf.WriteString("use serde::{Deserialize, Serialize};\n\n")
	buf.WriteString(fmt.Sprintf("#[derive(Debug, Serialize, Deserialize)]\npub struct %s {\n", name))

	keys := sortedKeys(obj)
	for _, k := range keys {
		v := obj[k]
		snakeKey := toSnakeCase(k)
		var typStr string
		if sub, ok := v.(map[string]any); ok {
			subName := toPascalCase(k)
			nested = append(nested, jsonToRustStruct(subName, sub))
			typStr = subName
		} else {
			typStr = jsonInferRustType(v)
		}
		rename := ""
		if snakeKey != k {
			rename = fmt.Sprintf("    #[serde(rename = \"%s\")]\n", k)
		}
		buf.WriteString(fmt.Sprintf("%s    pub %s: %s,\n", rename, snakeKey, typStr))
	}
	buf.WriteString("}")

	if len(nested) > 0 {
		return strings.Join(nested, "\n\n") + "\n\n" + buf.String()
	}
	return buf.String()
}

// jsonToRustStruct generates a Rust struct without the serde use statement (for nested types).
func jsonToRustStruct(name string, obj map[string]any) string {
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("#[derive(Debug, Serialize, Deserialize)]\npub struct %s {\n", name))
	keys := sortedKeys(obj)
	for _, k := range keys {
		v := obj[k]
		snakeKey := toSnakeCase(k)
		typStr := jsonInferRustType(v)
		rename := ""
		if snakeKey != k {
			rename = fmt.Sprintf("    #[serde(rename = \"%s\")]\n", k)
		}
		buf.WriteString(fmt.Sprintf("%s    pub %s: %s,\n", rename, snakeKey, typStr))
	}
	buf.WriteString("}")
	return buf.String()
}

// jsonInferRustType infers a Rust type from a JSON value.
func jsonInferRustType(v any) string {
	switch val := v.(type) {
	case nil:
		return "Option<serde_json::Value>"
	case bool:
		return "bool"
	case float64:
		if val == math.Trunc(val) {
			return "i64"
		}
		return "f64"
	case string:
		return "String"
	case []any:
		if len(val) == 0 {
			return "Vec<serde_json::Value>"
		}
		return "Vec<" + jsonInferRustType(val[0]) + ">"
	case map[string]any:
		return "serde_json::Value"
	}
	return "serde_json::Value"
}

// toSnakeCase converts a camelCase or PascalCase string to snake_case.
func toSnakeCase(s string) string {
	var buf strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			buf.WriteRune('_')
		}
		buf.WriteRune(unicode.ToLower(r))
	}
	return buf.String()
}

// sortedKeys returns the keys of a map in sorted order for deterministic output.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ── 3. yaml_json_convert ──────────────────────────────────────────────────────

// YAMLJSONConvertTool converts between YAML and JSON.
type YAMLJSONConvertTool struct{}

func (t *YAMLJSONConvertTool) Name() string               { return "yaml_json_convert" }
func (t *YAMLJSONConvertTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for yaml_json_convert.
func (t *YAMLJSONConvertTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "YAML ↔ JSON 互转。",
		map[string]*schema.ParameterInfo{
			"input":     {Type: schema.String, Desc: "YAML 或 JSON 内容", Required: true},
			"direction": {Type: schema.String, Desc: "yaml_to_json 或 json_to_yaml", Required: true},
			"pretty":    {Type: schema.Boolean, Desc: "JSON 输出是否美化（默认 true）"},
		}), nil
}

// InvokableRun converts between YAML and JSON.
func (t *YAMLJSONConvertTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	src, _ := args["input"].(string)
	direction, _ := args["direction"].(string)
	pretty := true
	if v, ok := args["pretty"].(bool); ok {
		pretty = v
	}

	switch direction {
	case "yaml_to_json":
		var node any
		if err := yaml.Unmarshal([]byte(src), &node); err != nil {
			return "", fmt.Errorf("YAML parse error: %w", err)
		}
		var b []byte
		var err error
		if pretty {
			b, err = json.MarshalIndent(node, "", "  ")
		} else {
			b, err = json.Marshal(node)
		}
		if err != nil {
			return "", fmt.Errorf("JSON marshal error: %w", err)
		}
		return string(b), nil

	case "json_to_yaml":
		var node any
		if err := json.Unmarshal([]byte(src), &node); err != nil {
			return "", fmt.Errorf("JSON parse error: %w", err)
		}
		b, err := yaml.Marshal(node)
		if err != nil {
			return "", fmt.Errorf("YAML marshal error: %w", err)
		}
		return string(b), nil

	default:
		return "", fmt.Errorf("direction must be yaml_to_json or json_to_yaml")
	}
}

// ── 4. encode_decode ──────────────────────────────────────────────────────────

// EncodeDecodeTool encodes or decodes text in base64/URL/HTML formats.
type EncodeDecodeTool struct{}

func (t *EncodeDecodeTool) Name() string               { return "encode_decode" }
func (t *EncodeDecodeTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for encode_decode.
func (t *EncodeDecodeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "Base64 / URL / HTML 编解码。",
		map[string]*schema.ParameterInfo{
			"text":   {Type: schema.String, Desc: "待处理的文本", Required: true},
			"format": {Type: schema.String, Desc: "base64 / base64url / url / html", Required: true},
			"action": {Type: schema.String, Desc: "encode 或 decode", Required: true},
		}), nil
}

// InvokableRun encodes or decodes the input text.
func (t *EncodeDecodeTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	text, _ := args["text"].(string)
	format, _ := args["format"].(string)
	action, _ := args["action"].(string)

	switch format {
	case "base64":
		if action == "encode" {
			return base64.StdEncoding.EncodeToString([]byte(text)), nil
		}
		b, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return "", fmt.Errorf("base64 decode error: %w", err)
		}
		return string(b), nil

	case "base64url":
		if action == "encode" {
			return base64.URLEncoding.EncodeToString([]byte(text)), nil
		}
		b, err := base64.URLEncoding.DecodeString(text)
		if err != nil {
			return "", fmt.Errorf("base64url decode error: %w", err)
		}
		return string(b), nil

	case "url":
		if action == "encode" {
			return url.QueryEscape(text), nil
		}
		decoded, err := url.QueryUnescape(text)
		if err != nil {
			return "", fmt.Errorf("url decode error: %w", err)
		}
		return decoded, nil

	case "html":
		if action == "encode" {
			return html.EscapeString(text), nil
		}
		return html.UnescapeString(text), nil

	default:
		return "", fmt.Errorf("unsupported format %q; use base64/base64url/url/html", format)
	}
}

// ── 5. hash_text ──────────────────────────────────────────────────────────────

// HashTextTool computes a cryptographic hash of the input text.
type HashTextTool struct{}

func (t *HashTextTool) Name() string               { return "hash_text" }
func (t *HashTextTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for hash_text.
func (t *HashTextTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "计算文本的 MD5 / SHA1 / SHA256 / SHA512 哈希值。",
		map[string]*schema.ParameterInfo{
			"text":      {Type: schema.String, Desc: "待哈希的文本", Required: true},
			"algorithm": {Type: schema.String, Desc: "md5 / sha1 / sha256 / sha512", Required: true},
			"encoding":  {Type: schema.String, Desc: "输出编码: hex（默认）/ base64"},
		}), nil
}

// InvokableRun hashes text using the specified algorithm.
func (t *HashTextTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	text, _ := args["text"].(string)
	algorithm, _ := args["algorithm"].(string)
	encoding, _ := args["encoding"].(string)
	if encoding == "" {
		encoding = "hex"
	}

	var h []byte
	switch algorithm {
	case "md5":
		s := md5.Sum([]byte(text))
		h = s[:]
	case "sha1":
		s := sha1.Sum([]byte(text))
		h = s[:]
	case "sha256":
		s := sha256.Sum256([]byte(text))
		h = s[:]
	case "sha512":
		s := sha512.Sum512([]byte(text))
		h = s[:]
	default:
		return "", fmt.Errorf("unsupported algorithm %q; use md5/sha1/sha256/sha512", algorithm)
	}

	if encoding == "base64" {
		return base64.StdEncoding.EncodeToString(h), nil
	}
	return hex.EncodeToString(h), nil
}

// ── 6. generate_uuid ──────────────────────────────────────────────────────────

// GenerateUUIDTool generates one or more UUID v4 values.
type GenerateUUIDTool struct{}

func (t *GenerateUUIDTool) Name() string               { return "generate_uuid" }
func (t *GenerateUUIDTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for generate_uuid.
func (t *GenerateUUIDTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "批量生成 UUID v4。",
		map[string]*schema.ParameterInfo{
			"count":  {Type: schema.Integer, Desc: "生成数量 1-100，默认 1"},
			"format": {Type: schema.String, Desc: "standard（默认）/ no_dash / upper"},
		}), nil
}

// InvokableRun generates UUIDs in the requested format.
func (t *GenerateUUIDTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	count := 1
	if v, ok := args["count"].(float64); ok && v >= 1 {
		count = int(v)
	}
	if count > 100 {
		count = 100
	}
	format, _ := args["format"].(string)
	if format == "" {
		format = "standard"
	}

	results := make([]string, count)
	for i := range results {
		id := uuid.New().String()
		switch format {
		case "no_dash":
			id = strings.ReplaceAll(id, "-", "")
		case "upper":
			id = strings.ToUpper(id)
		}
		results[i] = id
	}
	return strings.Join(results, "\n"), nil
}

// ── 7. convert_timestamp ──────────────────────────────────────────────────────

// ConvertTimestampTool converts between Unix timestamps and human-readable datetime strings.
type ConvertTimestampTool struct{}

func (t *ConvertTimestampTool) Name() string               { return "convert_timestamp" }
func (t *ConvertTimestampTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for convert_timestamp.
func (t *ConvertTimestampTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "Unix 时间戳与可读时间字符串互转，unix→datetime 时返回多时区对照。",
		map[string]*schema.ParameterInfo{
			"value":     {Type: schema.String, Desc: "Unix 时间戳（整数）或日期时间字符串（RFC3339 / 常见格式）", Required: true},
			"direction": {Type: schema.String, Desc: "unix_to_datetime 或 datetime_to_unix", Required: true},
			"timezone":  {Type: schema.String, Desc: "IANA 时区名，如 Asia/Shanghai，默认 local"},
		}), nil
}

// InvokableRun converts timestamps.
func (t *ConvertTimestampTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
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

// ── 8. regex_test ─────────────────────────────────────────────────────────────

// RegexTestTool tests a RE2 regular expression against a text string.
type RegexTestTool struct{}

func (t *RegexTestTool) Name() string               { return "regex_test" }
func (t *RegexTestTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for regex_test.
func (t *RegexTestTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "测试正则表达式（Go RE2 语法），返回匹配结果、所有匹配子串和捕获组。",
		map[string]*schema.ParameterInfo{
			"pattern": {Type: schema.String, Desc: "RE2 正则表达式模式", Required: true},
			"text":    {Type: schema.String, Desc: "待匹配的文本", Required: true},
		}), nil
}

// InvokableRun tests the regex against the text.
func (t *RegexTestTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	pattern, _ := args["pattern"].(string)
	text, _ := args["text"].(string)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Sprintf("无效的正则表达式: %s", err.Error()), nil
	}

	matched := re.MatchString(text)
	var lines []string
	if matched {
		lines = append(lines, "匹配: ✓")
	} else {
		lines = append(lines, "匹配: ✗")
		return strings.Join(lines, "\n"), nil
	}

	all := re.FindAllString(text, -1)
	lines = append(lines, fmt.Sprintf("匹配数量: %d", len(all)))
	for i, m := range all {
		lines = append(lines, fmt.Sprintf("  [%d] %q", i, m))
	}

	names := re.SubexpNames()
	if len(names) > 1 {
		subs := re.FindAllStringSubmatch(text, -1)
		lines = append(lines, "捕获组:")
		for _, sub := range subs {
			for i, name := range names {
				if i == 0 || i >= len(sub) {
					continue
				}
				label := name
				if label == "" {
					label = fmt.Sprintf("$%d", i)
				}
				lines = append(lines, fmt.Sprintf("  %s: %q", label, sub[i]))
			}
		}
	}

	return strings.Join(lines, "\n"), nil
}

// ── 9. number_base_convert ────────────────────────────────────────────────────

// NumberBaseConvertTool converts an integer between number bases 2/8/10/16.
type NumberBaseConvertTool struct{}

func (t *NumberBaseConvertTool) Name() string               { return "number_base_convert" }
func (t *NumberBaseConvertTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for number_base_convert.
func (t *NumberBaseConvertTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "整数进制转换（二 / 八 / 十 / 十六进制），结果带进制前缀。",
		map[string]*schema.ParameterInfo{
			"value": {Type: schema.String, Desc: "源进制表示的数字字符串（如 \"FF\"、\"255\"、\"11111111\"）", Required: true},
			"from":  {Type: schema.String, Desc: "源进制: 2 / 8 / 10 / 16", Required: true},
			"to":    {Type: schema.String, Desc: "目标进制: 2 / 8 / 10 / 16", Required: true},
		}), nil
}

// InvokableRun converts the number between the specified bases.
func (t *NumberBaseConvertTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
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

func (t *ConvertUnitsTool) Name() string               { return "convert_units" }
func (t *ConvertUnitsTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for convert_units.
func (t *ConvertUnitsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"物理量单位换算（长度/重量/温度/面积/体积/速度/数据）。单位标识符: mm/cm/m/km/inch/ft/yard/mile | mg/g/kg/ton/lb/oz | C/F/K | mm2/cm2/m2/km2/acre/hectare | ml/l/gallon/fl_oz/cup | m_s/km_h/mph/knot | bit/byte/KB/MB/GB/TB",
		map[string]*schema.ParameterInfo{
			"value":     {Type: schema.Number, Desc: "待换算的数值", Required: true},
			"from_unit": {Type: schema.String, Desc: "源单位标识符", Required: true},
			"to_unit":   {Type: schema.String, Desc: "目标单位标识符", Required: true},
		}), nil
}

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
	args := parseArgs(input)
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

func (t *GetExchangeRateTool) Name() string               { return "get_exchange_rate" }
func (t *GetExchangeRateTool) Permission() PermissionLevel { return PermProtected }

// Info returns the eino tool schema for get_exchange_rate.
func (t *GetExchangeRateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "查询实时外汇汇率并换算金额（数据源: open.er-api.com，免费无需 API Key）。",
		map[string]*schema.ParameterInfo{
			"base":    {Type: schema.String, Desc: "基础货币代码，如 USD、CNY", Required: true},
			"targets": {Type: schema.String, Desc: "目标货币代码列表（JSON 数组字符串），空则返回主要货币"},
			"amount":  {Type: schema.Number, Desc: "换算金额，默认 1"},
		}), nil
}

var defaultCurrencies = []string{"USD", "EUR", "CNY", "JPY", "GBP", "HKD", "KRW"}

// InvokableRun fetches rates and returns a conversion table.
func (t *GetExchangeRateTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	base, _ := args["base"].(string)
	base = strings.ToUpper(strings.TrimSpace(base))
	amount := 1.0
	if v, ok := args["amount"].(float64); ok && v > 0 {
		amount = v
	}

	var targets []string
	if v, ok := args["targets"].(string); ok && v != "" {
		_ = json.Unmarshal([]byte(v), &targets)
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

	apiURL := fmt.Sprintf("https://open.er-api.com/v6/latest/%s", url.PathEscape(base))
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
		return fmt.Sprintf("汇率服务返回错误，请检查货币代码 %q 是否正确", base), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("基础货币: %s (金额: %g)", base, amount))
	lines = append(lines, "---")
	for _, cur := range targets {
		rate, ok := result.Rates[strings.ToUpper(cur)]
		if !ok {
			lines = append(lines, fmt.Sprintf("%s: 不支持", cur))
			continue
		}
		converted := amount * rate
		lines = append(lines, fmt.Sprintf("%s: 1 %s = %.6f %s → %g %s = %.2f %s",
			cur, base, rate, cur, amount, base, converted, cur))
	}
	return strings.Join(lines, "\n"), nil
}

// ── 12. convert_color ─────────────────────────────────────────────────────────

// ConvertColorTool converts color values between HEX, RGB, and HSL formats.
type ConvertColorTool struct{}

func (t *ConvertColorTool) Name() string               { return "convert_color" }
func (t *ConvertColorTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for convert_color.
func (t *ConvertColorTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(), "色值格式互转（HEX / RGB / HSL）。输入支持 #RRGGBB、#RGB、rgb(r,g,b)、hsl(h,s%,l%)。",
		map[string]*schema.ParameterInfo{
			"input":  {Type: schema.String, Desc: "颜色值，支持 #RRGGBB / #RGB / rgb(r,g,b) / hsl(h,s%,l%)", Required: true},
			"output": {Type: schema.String, Desc: "输出格式: hex / rgb / hsl / all（默认）"},
		}), nil
}

// InvokableRun converts color between formats.
func (t *ConvertColorTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
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

	reRGB := regexp.MustCompile(`(?i)^rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)$`)
	if m := reRGB.FindStringSubmatch(s); m != nil {
		rv, _ := strconv.Atoi(m[1])
		gv, _ := strconv.Atoi(m[2])
		bv, _ := strconv.Atoi(m[3])
		return uint8(rv), uint8(gv), uint8(bv), nil
	}

	reHSL := regexp.MustCompile(`(?i)^hsl\(\s*([\d.]+)\s*,\s*([\d.]+)%\s*,\s*([\d.]+)%\s*\)$`)
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

// Ensure bytes import is used — removed in Task 3 cleanup.
var _ = bytes.NewBuffer

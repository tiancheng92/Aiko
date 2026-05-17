// internal/tools/dev/dev_json.go
package dev

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"

	"aiko/internal/tools/base"
)

// ── 1. format_json ────────────────────────────────────────────────────────────

// FormatJSONTool formats, minifies, or validates a JSON string.
type FormatJSONTool struct{}

func (t *FormatJSONTool) Name() string                    { return "format_json" }
func (t *FormatJSONTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for format_json.
func (t *FormatJSONTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "JSON 格式化（缩进美化）、压缩（单行）或语法校验。",
		map[string]*schema.ParameterInfo{
			"json_string": {Type: schema.String, Desc: "待处理的 JSON 字符串", Required: true},
			"action":      {Type: schema.String, Desc: "操作类型: pretty（默认）/ minify / validate"},
			"indent":      {Type: schema.Integer, Desc: "pretty 模式每级缩进空格数，默认 2"},
		}), nil
}

// InvokableRun formats, minifies, or validates JSON.
func (t *FormatJSONTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
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
	if err := json.UnmarshalString(jsonStr, &raw); err != nil {
		if action == "validate" {
			return fmt.Sprintf("invalid JSON — error: %s", err.Error()), nil
		}
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	switch action {
	case "validate":
		return "有效的 JSON ✓", nil
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

func (t *JSONToStructTool) Name() string                    { return "json_to_struct" }
func (t *JSONToStructTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for json_to_struct.
func (t *JSONToStructTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "将 JSON 对象转换为 Go / TypeScript / Python / Rust 结构体定义。",
		map[string]*schema.ParameterInfo{
			"json_string": {Type: schema.String, Desc: "有效的 JSON 对象或数组", Required: true},
			"language":    {Type: schema.String, Desc: "目标语言: go / typescript / python / rust", Required: true},
			"type_name":   {Type: schema.String, Desc: "根类型名，默认 Root"},
		}), nil
}

// InvokableRun generates struct code from a JSON string.
func (t *JSONToStructTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	jsonStr, _ := args["json_string"].(string)
	lang, _ := args["language"].(string)
	typeName, _ := args["type_name"].(string)
	if typeName == "" {
		typeName = "Root"
	}

	var raw any
	if err := json.UnmarshalString(jsonStr, &raw); err != nil {
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

func (t *YAMLJSONConvertTool) Name() string                    { return "yaml_json_convert" }
func (t *YAMLJSONConvertTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for yaml_json_convert.
func (t *YAMLJSONConvertTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "YAML ↔ JSON 互转。",
		map[string]*schema.ParameterInfo{
			"input":     {Type: schema.String, Desc: "YAML 或 JSON 内容", Required: true},
			"direction": {Type: schema.String, Desc: "yaml_to_json 或 json_to_yaml", Required: true},
			"pretty":    {Type: schema.Boolean, Desc: "JSON 输出是否美化（默认 true）"},
		}), nil
}

// InvokableRun converts between YAML and JSON.
func (t *YAMLJSONConvertTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
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
		if err := json.UnmarshalString(src, &node); err != nil {
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

// internal/tools/dev/dev_misc.go
package dev

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/google/uuid"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/tools/base"
)

// ── 6. generate_uuid ──────────────────────────────────────────────────────────

// GenerateUUIDTool generates one or more UUID v4 values.
type GenerateUUIDTool struct{}

func (t *GenerateUUIDTool) Name() string                    { return "generate_uuid" }
func (t *GenerateUUIDTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for generate_uuid.
func (t *GenerateUUIDTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "批量生成 UUID v4。",
		map[string]*schema.ParameterInfo{
			"count":  {Type: schema.Integer, Desc: "生成数量 1-100，默认 1"},
			"format": {Type: schema.String, Desc: "standard（默认）/ no_dash / upper"},
		}), nil
}

// InvokableRun generates UUIDs in the requested format.
func (t *GenerateUUIDTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
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

// ── 8. regex_test ─────────────────────────────────────────────────────────────

// RegexTestTool tests a RE2 regular expression against a text string.
type RegexTestTool struct{}

func (t *RegexTestTool) Name() string                    { return "regex_test" }
func (t *RegexTestTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for regex_test.
func (t *RegexTestTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "测试正则表达式（Go RE2 语法），返回匹配结果、所有匹配子串和捕获组。",
		map[string]*schema.ParameterInfo{
			"pattern": {Type: schema.String, Desc: "RE2 正则表达式模式", Required: true},
			"text":    {Type: schema.String, Desc: "待匹配的文本", Required: true},
		}), nil
}

// InvokableRun tests the regex against the text.
func (t *RegexTestTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
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

// internal/tools/growth_tools.go
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/memory"
)

// SaveMemoryTool saves a single concrete fact or preference to long-term memory.
type SaveMemoryTool struct {
	LongMem *memory.LongStore
}

// Name returns the tool's stable identifier.
func (t *SaveMemoryTool) Name() string { return "save_memory" }

// Permission returns the required permission level.
func (t *SaveMemoryTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for save_memory.
func (t *SaveMemoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"保存单条具体事实、偏好或结论到长期记忆（一两句话）。不用摘要整段对话——对话历史由系统自动处理。",
		map[string]*schema.ParameterInfo{
			"content": {
				Type:     schema.String,
				Desc:     "要长期记住的具体事实、偏好或结论（一两句话）",
				Required: true,
			},
		},
	), nil
}

// InvokableRun stores the given content into the long-term memory store.
func (t *SaveMemoryTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	if t.LongMem == nil {
		return "长期记忆未启用（需配置 Embedding 模型）", nil
	}
	args := parseArgs(input)
	content, _ := args["content"].(string)
	if content == "" {
		return "请提供要保存的内容", nil
	}
	if err := t.LongMem.Store(ctx, content); err != nil {
		return "", fmt.Errorf("save memory: %w", err)
	}
	return fmt.Sprintf("已保存到长期记忆：%s", content), nil
}

// SearchMemoryTool queries long-term memory for segments relevant to a given topic.
type SearchMemoryTool struct {
	LongMem *memory.LongStore
}

// Name returns the tool's stable identifier.
func (t *SearchMemoryTool) Name() string { return "search_memory" }

// Permission returns the required permission level.
func (t *SearchMemoryTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for search_memory.
func (t *SearchMemoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"在长期记忆中语义搜索，返回与查询最相关的历史片段。适合回答「我之前说过什么」、「我们讨论过 X 吗」等问题。",
		map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "搜索关键词或问题描述",
				Required: true,
			},
			"limit": {
				Type: schema.Integer,
				Desc: "返回条数（默认 5，最大 20）",
			},
		},
	), nil
}

// InvokableRun searches long-term memory and returns the top matching segments.
func (t *SearchMemoryTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	if t.LongMem == nil {
		return "长期记忆未启用（需配置 Embedding 模型）", nil
	}
	args := parseArgs(input)
	query, _ := args["query"].(string)
	if query == "" {
		return "请提供搜索关键词", nil
	}
	limit := 5
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = min(int(v), 20)
	}
	results, err := t.LongMem.Search(ctx, query, limit)
	if err != nil {
		return "", fmt.Errorf("search memory: %w", err)
	}
	if len(results) == 0 {
		return "未找到相关记忆片段", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "找到 %d 条相关记忆片段：\n\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&sb, "【%d】%s\n\n", i+1, r)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// UpdateUserProfileTool updates a key-value entry in ~/.aiko/USER.md.
type UpdateUserProfileTool struct {
	DataDir string
}

// Name returns the tool's stable identifier.
func (t *UpdateUserProfileTool) Name() string { return "update_user_profile" }

// Permission returns the required permission level.
func (t *UpdateUserProfileTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for update_user_profile.
func (t *UpdateUserProfileTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"更新用户画像中的某个条目（习惯、偏好、背景信息）。已存在的 key 会被覆盖，否则追加。",
		map[string]*schema.ParameterInfo{
			"key": {
				Type:     schema.String,
				Desc:     "画像条目的键名，如 preferred_language、coding_style",
				Required: true,
			},
			"value": {
				Type:     schema.String,
				Desc:     "条目的值",
				Required: true,
			},
		},
	), nil
}

// InvokableRun reads ~/.aiko/USER.md, updates or appends the key-value line, and writes back atomically.
func (t *UpdateUserProfileTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	key, _ := args["key"].(string)
	value, _ := args["value"].(string)
	if key == "" {
		return "请提供 key", nil
	}

	profilePath := userProfilePath(t.DataDir)
	updated, err := upsertProfileLine(profilePath, key, value)
	if err != nil {
		return "", fmt.Errorf("update user profile: %w", err)
	}
	if updated {
		return fmt.Sprintf("已更新用户画像：%s = %s", key, value), nil
	}
	return fmt.Sprintf("已追加用户画像：%s = %s", key, value), nil
}

// SaveSkillTool writes a reusable skill file to ~/.aiko/auto-skills/<name>/SKILL.md.
type SaveSkillTool struct {
	DataDir string
}

// Name returns the tool's stable identifier.
func (t *SaveSkillTool) Name() string { return "save_skill" }

// Permission returns the required permission level.
func (t *SaveSkillTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for save_skill.
func (t *SaveSkillTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"将当前解决的问题模式保存为可复用的技能文件。已存在的同名技能会被更新（自我改进）。",
		map[string]*schema.ParameterInfo{
			"name": {
				Type:     schema.String,
				Desc:     "技能的唯一标识名（英文小写，用连字符分隔，如 fix-go-import-cycle）",
				Required: true,
			},
			"description": {
				Type:     schema.String,
				Desc:     "技能的一句话描述",
				Required: true,
			},
			"content": {
				Type:     schema.String,
				Desc:     "技能的详细内容（Markdown 格式，说明何时使用及具体步骤）",
				Required: true,
			},
		},
	), nil
}

// InvokableRun creates or overwrites ~/.aiko/auto-skills/<name>/SKILL.md.
func (t *SaveSkillTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	content, _ := args["content"].(string)
	if name == "" {
		return "请提供技能名称", nil
	}

	skillPath, err := writeSkillFile(t.DataDir, name, description, content)
	if err != nil {
		return "", fmt.Errorf("save skill: %w", err)
	}
	return fmt.Sprintf("已保存技能文件：%s", skillPath), nil
}

// userProfilePath returns the path to USER.md in the given data directory.
func userProfilePath(dataDir string) string {
	return filepath.Join(dataDir, "USER.md")
}

// upsertProfileLine reads the profile file, replaces the line starting with
// "- <key>:" if found, otherwise appends it. Returns true if the key existed.
// Writes atomically via a temp file + rename.
func upsertProfileLine(path, key, value string) (updated bool, err error) {
	existing, readErr := os.ReadFile(path)
	var lines []string
	if readErr == nil {
		lines = strings.Split(string(existing), "\n")
	}

	prefix := fmt.Sprintf("- %s:", key)
	newLine := fmt.Sprintf("- %s: %s", key, value)
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = newLine
			found = true
			break
		}
	}
	if !found {
		// Remove any trailing empty line before appending.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, newLine, "")
	}

	data := []byte(strings.Join(lines, "\n"))
	if err := atomicWrite(path, data); err != nil {
		return false, err
	}
	return found, nil
}

// writeSkillFile creates ~/.aiko/auto-skills/<name>/SKILL.md with frontmatter.
// Returns the path of the written file.
func writeSkillFile(dataDir, name, description, content string) (string, error) {
	dir := filepath.Join(dataDir, "auto-skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir auto-skills: %w", err)
	}
	skillPath := filepath.Join(dir, "SKILL.md")
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, description, content)
	if err := atomicWrite(skillPath, []byte(body)); err != nil {
		return "", err
	}
	return skillPath, nil
}

// atomicWrite writes data to path via a temp file + rename for atomicity.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// internal/tools/growth_tools.go
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		"保存单条具体事实、偏好或结论到长期记忆（一两句话）。保存前先用 search_memory 确认尚未存储类似内容，避免重复。对话历史由系统自动处理，无需摘要。",
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

// ListSkillsTool lists all auto-saved skills stored under ~/.aiko/auto-skills/.
type ListSkillsTool struct {
	DataDir string
}

// Name returns the tool's stable identifier.
func (t *ListSkillsTool) Name() string { return "list_skills" }

// Permission returns the required permission level.
func (t *ListSkillsTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for list_skills.
func (t *ListSkillsTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"列出所有已保存的自动技能（名称+描述）。在调用 save_skill 前，先调用此工具确认是否已存在同名或类似技能，避免重复；改名前也应先调用以获取旧技能名称。",
		map[string]*schema.ParameterInfo{},
	), nil
}

// InvokableRun scans ~/.aiko/auto-skills/ and returns a formatted list of skill names and descriptions.
func (t *ListSkillsTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	baseDir := filepath.Join(t.DataDir, "auto-skills")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "暂无已保存的技能。", nil
		}
		return "", fmt.Errorf("list skills: %w", err)
	}

	type skillEntry struct{ name, desc string }
	var skills []skillEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(baseDir, e.Name(), "SKILL.md")
		data, readErr := os.ReadFile(skillPath)
		if readErr != nil {
			continue
		}
		desc := extractFrontmatterField(string(data), "description")
		skills = append(skills, skillEntry{name: e.Name(), desc: desc})
	}
	if len(skills) == 0 {
		return "暂无已保存的技能。", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "已保存 %d 个技能：\n\n", len(skills))
	for _, s := range skills {
		if s.desc != "" {
			fmt.Fprintf(&sb, "• %s — %s\n", s.name, s.desc)
		} else {
			fmt.Fprintf(&sb, "• %s\n", s.name)
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// extractFrontmatterField reads a YAML frontmatter value from Markdown content.
// It matches lines of the form "key: value" within the leading "---" block.
func extractFrontmatterField(content, key string) string {
	lines := strings.SplitN(content, "\n", 50)
	inFrontmatter := false
	prefix := key + ":"
	for _, line := range lines {
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break
		}
		if inFrontmatter && strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

// SaveSkillTool writes a reusable skill file to ~/.aiko/auto-skills/<name>/SKILL.md.
type SaveSkillTool struct {
	DataDir string
	// OnSaved is called asynchronously after a skill is successfully written.
	// Used by app.go to trigger skill middleware hot-reload.
	OnSaved func()
}

// Name returns the tool's stable identifier.
func (t *SaveSkillTool) Name() string { return "save_skill" }

// Permission returns the required permission level.
func (t *SaveSkillTool) Permission() PermissionLevel { return PermPublic }

// Info returns the eino tool schema for save_skill.
func (t *SaveSkillTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return infoFromSchema(t.Name(),
		"将当前解决的问题模式保存为可复用的技能文件。已存在的同名技能会被更新（自我改进）。改名时通过 old_name 传入旧技能名，旧目录会被自动删除。",
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
			"old_name": {
				Type: schema.String,
				Desc: "改名时填写旧技能名称；写入新技能后会自动删除旧技能目录。留空则不删除任何旧技能。",
			},
		},
	), nil
}

// InvokableRun creates or overwrites ~/.aiko/auto-skills/<name>/SKILL.md,
// then calls OnSaved asynchronously so the caller can hot-reload the skill middleware.
// If old_name is provided and differs from name, the old skill directory is removed.
func (t *SaveSkillTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	content, _ := args["content"].(string)
	oldName, _ := args["old_name"].(string)
	if name == "" {
		return "请提供技能名称", nil
	}

	skillPath, err := writeSkillFile(t.DataDir, name, description, content)
	if err != nil {
		return "", fmt.Errorf("save skill: %w", err)
	}

	var msg strings.Builder
	msg.WriteString("已保存技能文件：")
	msg.WriteString(skillPath)

	if oldName != "" && oldName != name {
		oldDir := filepath.Join(t.DataDir, "auto-skills", oldName)
		if rmErr := os.RemoveAll(oldDir); rmErr != nil {
			fmt.Fprintf(&msg, "；旧技能目录删除失败：%v", rmErr)
		} else {
			fmt.Fprintf(&msg, "；已删除旧技能 %q", oldName)
		}
	}

	if t.OnSaved != nil {
		go t.OnSaved()
	}
	return msg.String(), nil
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
	newLine := fmt.Sprintf("- %s: %s  (updated: %s)", key, value, time.Now().Format("2006-01-02"))
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
	// Back up the previous version before overwriting so Agent-driven updates are recoverable.
	if data, err := os.ReadFile(skillPath); err == nil {
		_ = os.WriteFile(skillPath+".bak", data, 0o644)
	}
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

package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
)

// NewMiddleware builds a skill.Middleware from all directories in skillsDirs.
// Directories that do not exist are silently skipped. Returns nil, nil when
// skillsDirs is empty or no SKILL.md files are found in any directory.
//
// When agentHub is non-nil, skills with context:fork or context:fork_with_context
// in their frontmatter can execute in an isolated sub-agent. modelHub allows
// individual skills to specify a different model via the "model" frontmatter field.
func NewMiddleware(ctx context.Context, skillsDirs []string, agentHub skill.AgentHub, modelHub skill.ModelHub) (adk.ChatModelAgentMiddleware, error) {
	var backends []skill.Backend
	for _, dir := range skillsDirs {
		b := backendForDir(expandHome(dir))
		if b != nil {
			backends = append(backends, b)
		}
	}
	if len(backends) == 0 {
		return nil, nil
	}

	backend := skill.Backend(&multiBackend{backends: backends})
	if len(backends) == 1 {
		backend = backends[0]
	}

	return skill.NewMiddleware(ctx, &skill.Config{
		Backend:  backend,
		AgentHub: agentHub,
		ModelHub: modelHub,
		CustomSystemPrompt: func(ctx context.Context, toolName string) string {
			return buildSkillSystemPrompt(toolName)
		},
		CustomToolDescription: func(ctx context.Context, skills []skill.FrontMatter) string {
			return buildSkillToolDescription(skills)
		},
	})
}

// buildSkillSystemPrompt returns the Chinese system prompt injected for the
// skill tool. It instructs the LLM to load and follow skills when they match
// the user's intent.
func buildSkillSystemPrompt(toolName string) string {
	return `调用 ` + toolName + ` 工具可以加载一个 Skill（技能）。Skill 是一段可复用的 Markdown 指令，
告诉你在特定场景下应该如何工作。

规则：
- 当用户的任务明显匹配某个 Skill 的描述时，必须调用 ` + toolName + ` 加载该 Skill。
- 加载后，严格按照 Skill 中的步骤执行。
- 如果 Skill 的内容不适用或带来限制，退出该 Skill 后自行处理。
- 不要加载与当前任务无关的 Skill。`
}

// buildSkillToolDescription returns a description that lists all available
// skills by name and summary, helping the LLM select the right skill.
func buildSkillToolDescription(skills []skill.FrontMatter) string {
	if len(skills) == 0 {
		return "加载一个 Skill 以获取特定场景的操作指引。当前没有可用 Skill。"
	}
	var b strings.Builder
	b.WriteString("加载一个 Skill 以获取特定场景的操作指引。可用 Skill 列表：\n")
	for i := range skills {
		b.WriteString("- ")
		b.WriteString(skills[i].Name)
		if skills[i].Description != "" {
			b.WriteString(" — ")
			b.WriteString(skills[i].Description)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// backendForDir creates a fault-tolerant skill.Backend for a single directory.
// Returns nil if dir does not exist or is not a directory.
// Individual SKILL.md files with parse errors are skipped with a warning.
func backendForDir(dir string) skill.Backend {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}
	return &tolerantFilesystemBackend{baseDir: dir}
}

// tolerantFilesystemBackend implements skill.Backend, scanning baseDir for immediate
// subdirectories that contain a SKILL.md. Broken SKILL.md files are skipped with a
// warning instead of failing the entire list operation.
type tolerantFilesystemBackend struct {
	baseDir string
}

// List returns all valid skills found under baseDir, skipping any with parse errors.
func (b *tolerantFilesystemBackend) List(_ context.Context) ([]skill.FrontMatter, error) {
	skills, err := b.loadAll()
	if err != nil {
		return nil, err
	}
	matters := make([]skill.FrontMatter, 0, len(skills))
	for i := range skills {
		matters = append(matters, skills[i].FrontMatter)
	}
	return matters, nil
}

// Get retrieves a skill by name.
func (b *tolerantFilesystemBackend) Get(_ context.Context, name string) (skill.Skill, error) {
	skills, err := b.loadAll()
	if err != nil {
		return skill.Skill{}, err
	}
	for i := range skills {
		if skills[i].Name == name {
			return skills[i], nil
		}
	}
	return skill.Skill{}, fmt.Errorf("skill %q not found", name)
}

// loadAll scans baseDir's immediate subdirectories for SKILL.md files.
func (b *tolerantFilesystemBackend) loadAll() ([]skill.Skill, error) {
	entries, err := os.ReadDir(b.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill dir %s: %w", b.baseDir, err)
	}
	var skills []skill.Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(b.baseDir, entry.Name(), "SKILL.md")
		s, loadErr := loadSkillFile(skillPath)
		if loadErr != nil {
			log.Warn().Str("path", skillPath).Err(loadErr).Msg("skill: skipping malformed SKILL.md")
			continue
		}
		skills = append(skills, s)
	}
	return skills, nil
}

// loadSkillFile parses a single SKILL.md file into a skill.Skill.
func loadSkillFile(path string) (skill.Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return skill.Skill{}, fmt.Errorf("failed to read file: %w", err)
	}
	fm, content, err := parseFrontmatter(strings.TrimSpace(string(data)))
	if err != nil {
		return skill.Skill{}, err
	}
	var matter skill.FrontMatter
	if err = yaml.Unmarshal([]byte(fm), &matter); err != nil {
		return skill.Skill{}, fmt.Errorf("failed to unmarshal frontmatter: %w", err)
	}
	return skill.Skill{
		FrontMatter:   matter,
		Content:       strings.TrimSpace(content),
		BaseDirectory: filepath.Dir(path),
	}, nil
}

// parseFrontmatter splits a SKILL.md body into its YAML frontmatter and markdown content.
func parseFrontmatter(data string) (frontmatter, content string, err error) {
	const delim = "---"
	if !strings.HasPrefix(data, delim) {
		return "", "", fmt.Errorf("missing frontmatter delimiter")
	}
	rest := data[len(delim):]
	fm, after, found := strings.Cut(rest, "\n"+delim)
	if !found {
		return "", "", fmt.Errorf("frontmatter closing delimiter not found")
	}
	frontmatter = strings.TrimSpace(fm)
	content = strings.TrimPrefix(after, "\n")
	return frontmatter, content, nil
}

// multiBackend merges multiple skill.Backends into one, deduplicating by name.
type multiBackend struct {
	backends []skill.Backend
}

// List returns the union of all skills across backends, deduplicating by name.
func (m *multiBackend) List(ctx context.Context) ([]skill.FrontMatter, error) {
	seen := map[string]struct{}{}
	var all []skill.FrontMatter
	for _, b := range m.backends {
		items, err := b.List(ctx)
		if err != nil {
			return nil, err
		}
		for i := range items {
			if _, dup := seen[items[i].Name]; dup {
				log.Warn().Str("name", items[i].Name).Msg("skill: duplicate skill name, skipping")
				continue
			}
			seen[items[i].Name] = struct{}{}
			all = append(all, items[i])
		}
	}
	return all, nil
}

// Get retrieves a skill by name from the first backend that contains it.
func (m *multiBackend) Get(ctx context.Context, name string) (skill.Skill, error) {
	for _, b := range m.backends {
		items, err := b.List(ctx)
		if err != nil {
			return skill.Skill{}, err
		}
		for i := range items {
			if items[i].Name == name {
				return b.Get(ctx, name)
			}
		}
	}
	return skill.Skill{}, fmt.Errorf("skill %q not found", name)
}

// expandHome replaces a leading "~" with the current user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

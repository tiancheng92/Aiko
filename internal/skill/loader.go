package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

// Definition describes a skill loaded from skill.yaml.
type Definition struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Model        string   `yaml:"model"`
	Tools        []string `yaml:"tools"`
}

// skillTool wraps a skill Definition as an eino InvokableTool.
type skillTool struct {
	def Definition
}

// Info returns the ToolInfo for this skill tool.
func (t *skillTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.def.Name,
		Desc: t.def.Description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"input": {
				Desc:     "The task or question to send to this skill",
				Required: true,
				Type:     schema.String,
			},
		}),
	}, nil
}

// InvokableRun executes the skill tool (stub implementation).
func (t *skillTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return fmt.Sprintf("skill %s: not yet implemented", t.def.Name), nil
}

// LoadAll scans skillsDir and returns one InvokableTool per valid skill.yaml.
// Returns nil, nil if skillsDir is empty or does not exist.
func LoadAll(skillsDir string) ([]tool.BaseTool, error) {
	if skillsDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tools []tool.BaseTool
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		yamlPath := filepath.Join(skillsDir, e.Name(), "skill.yaml")
		b, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		var def Definition
		if err := yaml.Unmarshal(b, &def); err != nil {
			return nil, fmt.Errorf("parse %s: %w", yamlPath, err)
		}
		tools = append(tools, &skillTool{def: def})
	}
	return tools, nil
}

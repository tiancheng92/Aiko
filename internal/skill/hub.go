package skill

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
)

// agentHub implements skill.AgentHub by creating lightweight deep agents
// for fork/fork_with_context skill execution.
type agentHub struct {
	chatModel    model.ToolCallingChatModel
	systemPrompt string
}

// NewAgentHub creates a skill.AgentHub that spawns minimal ReAct agents
// (without tools) for skill fork execution. chatModel is used for all
// sub-agent LLM calls; systemPrompt is the instruction injected into
// each forked agent.
//
// Returns nil if chatModel is nil (fork mode will be unavailable).
func NewAgentHub(chatModel model.ToolCallingChatModel, systemPrompt string) skill.AgentHub {
	if chatModel == nil {
		return nil
	}
	return &agentHub{
		chatModel:    chatModel,
		systemPrompt: systemPrompt,
	}
}

// Get returns an agent for the given name. When name is empty, returns a
// default agent. The agent is stateless — each fork execution creates a new
// deep agent instance with no tools, relying purely on the skill instructions
// and the chat model's reasoning.
func (h *agentHub) Get(ctx context.Context, name string, opts *skill.AgentHubOptions) (adk.Agent, error) {
	cm := h.chatModel
	if opts != nil && opts.Model != nil {
		// Skill specified a model override via the "model" frontmatter field.
		if tcm, ok := opts.Model.(model.ToolCallingChatModel); ok {
			cm = tcm
		}
	}

	instruction := h.systemPrompt
	if name != "" {
		instruction += fmt.Sprintf("\n\n当前激活的 Skill: %s。请严格按照 Skill 指令执行。", name)
	}

	agent, err := deep.New(ctx, &deep.Config{
		Name:                   "skill-fork",
		Description:            "Forked skill execution agent",
		Instruction:            instruction,
		ChatModel:              cm,
		MaxIteration:           20,
		WithoutGeneralSubAgent: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create skill fork agent: %w", err)
	}
	return agent, nil
}

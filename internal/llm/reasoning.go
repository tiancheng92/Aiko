package llm

import (
	einoopenai "github.com/cloudwego/eino-ext/libs/acl/openai"
	einoopenrouter "github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino/components/model"

	"aiko/internal/config"
)

// ReasoningOption translates a UI thinking level ("default"|"off"|"low"|"medium"|"high")
// into a provider-specific eino model.Option. Returns nil when no option should be passed
// (level is "default", or level is "off" for OpenAI which has no disable mechanism).
func ReasoningOption(level string, provider config.Provider) *model.Option {
	var opt model.Option
	switch provider {
	case config.ProviderOpenRouter:
		switch level {
		case "off":
			opt = einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{Effort: einoopenrouter.EffortOfNone})
		case "low":
			opt = einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{Effort: einoopenrouter.EffortOfLow})
		case "medium":
			opt = einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{Effort: einoopenrouter.EffortOfMedium})
		case "high":
			opt = einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{Effort: einoopenrouter.EffortOfHigh})
		default:
			return nil
		}
	default: // openai-compatible
		switch level {
		case "low":
			opt = einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelLow)
		case "medium":
			opt = einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelMedium)
		case "high":
			opt = einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelHigh)
		default:
			// "default" or "off" for OpenAI: no disable mechanism, don't pass any option.
			return nil
		}
	}
	return &opt
}

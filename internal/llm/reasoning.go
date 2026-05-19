package llm

import (
	einoopenai "github.com/cloudwego/eino-ext/libs/acl/openai"
	einoopenrouter "github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino/components/model"

	"aiko/internal/config"
)

// ReasoningOption translates a UI thinking level ("default"|"off"|"low"|"medium"|"high")
// into a provider-specific eino model.Option. Returns (option, true) when an option should
// be passed, or (zero, false) when no option is needed (level is "default", or level is
// "off" for OpenAI which has no disable mechanism).
func ReasoningOption(level string, provider config.Provider) (model.Option, bool) {
	switch provider {
	case config.ProviderOpenRouter:
		switch level {
		case "off":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfNone,
			}), true
		case "low":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfLow,
			}), true
		case "medium":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfMedium,
			}), true
		case "high":
			return einoopenrouter.WithReasoning(&einoopenrouter.Reasoning{
				Effort: einoopenrouter.EffortOfHigh,
			}), true
		}
	default: // openai-compatible
		switch level {
		case "low":
			return einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelLow), true
		case "medium":
			return einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelMedium), true
		case "high":
			return einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevelHigh), true
		}
	}
	// "default" or "off" for OpenAI: don't pass any option.
	return model.Option{}, false
}

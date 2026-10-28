//go:build unit

package handler

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Unknown sibling objects must not select another protocol's pricing parser.
// The endpoint determines which explicit effort/tier the hold may consume,
// just as it does for the request eventually forwarded and settled.
func TestTokenInflightEstimateUsesEndpointProtocolDespiteForeignSiblings(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, model, body, effort, tier string
	}{
		{
			name: "chat foreign output config", protocol: service.ContentModerationProtocolOpenAIChat, model: "gpt-5.5", effort: "high",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high","output_config":{"effort":"low"}}`,
		},
		{
			name: "chat foreign generation config", protocol: service.ContentModerationProtocolOpenAIChat, model: "gpt-5.5", effort: "high",
			body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high","generationConfig":{}}`,
		},
		{
			name: "responses foreign output config", protocol: service.ContentModerationProtocolOpenAIResponses, model: "gpt-5.5", effort: "high", tier: "priority",
			body: `{"model":"gpt-5.5","input":"hello","reasoning":{"effort":"high"},"service_tier":"priority","output_config":{"effort":"low"}}`,
		},
		{
			name: "responses foreign snake generation config", protocol: service.ContentModerationProtocolOpenAIResponses, model: "gpt-5.5", effort: "high",
			body: `{"model":"gpt-5.5","input":"hello","reasoning":{"effort":"high"},"generation_config":{"thinking_config":{"thinking_level":"low"}}}`,
		},
		{
			name: "gemini foreign output config", protocol: service.ContentModerationProtocolGemini, model: "gemini-3-pro", effort: "high",
			body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"HIGH"}},"output_config":{"effort":"low"}}`,
		},
		{
			name: "gemini foreign reasoning without native level", protocol: service.ContentModerationProtocolGemini, model: "gemini-3-pro",
			body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"reasoning_effort":"high"}`,
		},
		{
			name: "gemini foreign service tier", protocol: service.ContentModerationProtocolGemini, model: "gemini-3-pro", effort: "low",
			body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"LOW"}},"service_tier":"priority"}`,
		},
		{
			name: "messages foreign reasoning", protocol: service.ContentModerationProtocolAnthropicMessages, model: "claude-opus-4-7", effort: "high",
			body: `{"model":"claude-opus-4-7","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"high"},"reasoning":{"effort":"low"},"generationConfig":{"thinkingConfig":{"thinkingLevel":"low"}}}`,
		},
		{
			name: "messages foreign reasoning without native effort", protocol: service.ContentModerationProtocolAnthropicMessages, model: "claude-opus-4-7",
			body: `{"model":"claude-opus-4-7","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenInflightEstimate(tc.protocol, tc.model, []byte(tc.body))
			require.Equal(t, tc.effort, got.ReasoningEffort, "%s effort must come only from its endpoint's canonical fields", tc.protocol)
			require.Equal(t, tc.tier, got.ServiceTier, "%s tier must come only from its endpoint's canonical fields", tc.protocol)
		})
	}
}

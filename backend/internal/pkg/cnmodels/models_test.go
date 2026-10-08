//go:build unit || !integration

package cnmodels

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialProviderCatalog(t *testing.T) {
	for _, tc := range []struct {
		platform string
		current  []string
		removed  []string
	}{
		{"deepseek", []string{"deepseek-flash", "deepseek-v4.1-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-flash", "deepseek-v4-flash-0731", "deepseek-v4-pro", "deepseek-v4-pro-0813"}, []string{"deepseek-r1", "deepseek-coder"}},
		{"kimi", []string{"kimi-k3", "kimi-k2.7-code-highspeed", "kimi-k2.6", "kimi-for-coding", "kimi-for-coding-highspeed"}, []string{"kimi-k2", "kimi-k2.5", "kimi-latest", "moonshot-v1-8k"}},
		{"zhipu", []string{"glm-5.3", "glm-5.3-flashx", "glm-4.6v", "glm-4.7-flash"}, []string{"chatglm_turbo", "cogview-3", "cogvideo"}},
		{"minimax", []string{"MiniMax-M3.1-Flash-Preview", "MiniMax-M3", "MiniMax-M2.7-highspeed", "MiniMax-M2.1"}, []string{"minimax-m3", "abab6.5-chat", "abab5.5-chat"}},
		{"stepfun", []string{"step-5-preview", "step-3.7-flash", "step-3.5-flash-2603", "step-3.5-flash", "step-router-v1"}, []string{"step-image", "step-tts"}},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			models := DefaultModelIDs(tc.platform)
			require.NotEmpty(t, models)
			seen := map[string]bool{}
			for _, model := range models {
				require.Equal(t, strings.TrimSpace(model), model)
				require.NotEmpty(t, model)
				require.False(t, seen[model], "duplicate model %s", model)
				require.NotContains(t, model, "claude")
				seen[model] = true
			}
			for _, model := range tc.current {
				require.Contains(t, models, model)
			}
			for _, model := range tc.removed {
				require.NotContains(t, models, model)
			}
			models[0] = "changed-by-caller"
			require.NotContains(t, DefaultModelIDs(tc.platform), "changed-by-caller")
		})
	}
	require.Nil(t, DefaultModelIDs("unknown"))
}

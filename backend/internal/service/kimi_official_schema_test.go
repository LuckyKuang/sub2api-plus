//go:build unit || !integration

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Oracle: kimi-code@7ccac2b45b8a3481c76f771bb99c043b562de2c5,
// packages/oauth/src/managed-kimi-code.ts. Capability synchronization must not
// discard an official entry merely because reasoning is an object.
func TestKimiOfficialObjectReasoningCatalog(t *testing.T) {
	body := []byte(`{"data":[{"id":"kimi-for-coding","display_name":"Kimi Coding","limit":{"context":262144,"output":32768},"modalities":{"input":["text","image"],"output":["text"]},"reasoning":{"type":"only","effort":{"valid":["low","high","max"],"default":"max"}},"tools":{"dynamic_load":true}},{"id":"plain","limit":{"context":131072,"output":8192},"modalities":{"input":["text"]},"reasoning":{"type":"none"}},{"id":"other-provider","context_window":64000,"reasoning":true,"supported_reasoning_levels":["medium","high"]}]}`)
	ids, metadata, err := extractUpstreamModelCatalog(body, false)
	require.NoError(t, err)
	require.Equal(t, []string{"kimi-for-coding", "other-provider", "plain"}, ids)
	coding, ok := metadata["kimi-for-coding"]
	require.True(t, ok, "ID success must also preserve upstream capability facts")
	require.Equal(t, int64(262144), coding.ContextWindow)
	require.Equal(t, int64(32768), coding.MaxOutputTokens)
	require.Equal(t, []string{"text", "image"}, coding.InputModalities)
	require.NotNil(t, coding.Reasoning)
	require.True(t, *coding.Reasoning)
	require.Equal(t, []string{"low", "high", "max"}, coding.SupportedReasoningLevels)
	require.Equal(t, "max", coding.DefaultReasoningLevel)
	require.NotNil(t, metadata["plain"].Reasoning)
	require.False(t, *metadata["plain"].Reasoning)
	require.Empty(t, metadata["plain"].SupportedReasoningLevels)
	require.True(t, *metadata["other-provider"].Reasoning)
	require.Equal(t, []string{"medium", "high"}, metadata["other-provider"].SupportedReasoningLevels)
}

func TestKimiUnknownReasoningDoesNotDiscardSiblingCapabilities(t *testing.T) {
	ids, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"future","limit":{"context":262144},"modalities":{"input":["text","image"]},"reasoning":{"type":"future"}}]}`), false)
	require.NoError(t, err)
	require.Equal(t, []string{"future"}, ids)
	require.Nil(t, metadata["future"].Reasoning, "unknown is not an assertion of no thinking")
	require.Equal(t, int64(262144), metadata["future"].ContextWindow)
	require.Equal(t, []string{"text", "image"}, metadata["future"].InputModalities)
}

// Oracle: same snapshot, packages/oauth/src/managed-usage.ts. used_ratio is a
// fraction; absent/malformed quota is unknown, not a zero-percent observation.
func TestKimiOfficialManagedQuotaSchema(t *testing.T) {
	body := []byte(`{"usages":{"limit_5h":{"used_ratio":0.25,"reset_time":"2026-10-26T03:00:00Z"},"limit_7d":{"used_ratio":0.75,"reset_time":"2026-10-30T00:00:00Z"},"limit_month_total":{"used_ratio":0.4,"reset_time":"2026-11-01T00:00:00Z"}}}`)
	require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 25, ResetAt: "2026-10-26T03:00:00Z"}, {Window: "weekly", UsedPercent: 75, ResetAt: "2026-10-30T00:00:00Z"}, {Window: "monthly", UsedPercent: 40, ResetAt: "2026-11-01T00:00:00Z"}}, parseKimiUsageTiers(body))
	for _, body := range []string{`{"usages":{}}`, `{"usages":{"limit_5h":{}}}`, `{"usages":{"limit_5h":{"used_ratio":"bad"}}}`, `{"usages":{"limit_5h":{"used_ratio":-1}}}`} {
		require.Empty(t, parseKimiUsageTiers([]byte(body)))
	}
}

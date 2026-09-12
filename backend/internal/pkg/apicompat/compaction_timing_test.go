package apicompat

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCompactionOutputIsNotATokenDelta(t *testing.T) {
	for _, kind := range []string{"compaction", "compaction_summary"} {
		obs := ObserveResponsesOutput([]byte(`{"type":"response.output_item.done","item":{"type":"` + kind + `","encrypted_content":"opaque"}}`))
		require.True(t, obs.MeaningfulOutput)
		require.False(t, obs.TokenLikeDelta)
		require.Equal(t, StreamOutputCompaction, obs.Kind)
	}
	require.False(t, ObserveResponsesOutput([]byte(`{"type":"response.output_item.added","item":{"type":"compaction"}}`)).MeaningfulOutput)
}

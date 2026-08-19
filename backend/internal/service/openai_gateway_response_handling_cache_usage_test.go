package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAICacheReadTokensFromUsagePresencePriority(t *testing.T) {
	t.Parallel()

	require.Zero(t, openAICacheReadTokensFromUsage(gjson.Parse(
		`{"input_tokens_details":{"cached_tokens":0},"cache_read_input_tokens":19}`,
	)))
	require.Zero(t, openAICacheReadTokensFromUsage(gjson.Parse(
		`{"cache_read_input_tokens":0,"cache_read_tokens":19}`,
	)))
	require.Equal(t, 4, openAICacheReadTokensFromUsage(gjson.Parse(
		`{"cache_read_input_tokens":4,"cache_read_tokens":19}`,
	)))
}

func TestOpenAICacheCreationTokensFromUsagePresencePriority(t *testing.T) {
	t.Parallel()

	require.Zero(t, openAICacheCreationTokensFromUsage(gjson.Parse(
		`{"input_tokens_details":{"cache_write_tokens":0},"cache_write_tokens":19}`,
	)))
	require.Zero(t, openAICacheCreationTokensFromUsage(gjson.Parse(
		`{"cache_write_tokens":0,"cache_creation_input_tokens":19}`,
	)))
	require.Equal(t, 4, openAICacheCreationTokensFromUsage(gjson.Parse(
		`{"cache_write_tokens":4,"cache_creation_input_tokens":19}`,
	)))
}

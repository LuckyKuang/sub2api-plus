//go:build unit

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestCodexVersionConstants_Consistency(t *testing.T) {
	require.Equal(t, codexCLIVersion, DefaultOpenAICodexVersion,
		"the compiled-in Codex identity version must have one source of truth")
	require.Equal(t, codexCLIVersion, openAICodexProbeVersion,
		"codexCLIVersion and openAICodexProbeVersion must stay in sync")

	originator, userAgent, ok := openai.PairCodexClientIdentity(DefaultOpenAICodexUserAgent)
	require.True(t, ok, "the built-in User-Agent must be a supported Codex identity")
	require.Equal(t, openai.CodexCLIOriginator, originator)
	require.Equal(t, DefaultOpenAICodexUserAgent, userAgent)
	require.Equal(t, DefaultOpenAICodexVersion, openai.CodexUserAgentVersion(userAgent))
}

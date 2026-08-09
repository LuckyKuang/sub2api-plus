//go:build unit

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestCodexVersionConstants_Consistency(t *testing.T) {
	const expectedDefaultUserAgent = "codex-tui/0.147.0 (Ubuntu 24.04; x86_64) xterm-256color"

	require.Equal(t, codexCLIVersion, DefaultOpenAICodexVersion,
		"the compiled-in Codex identity version must have one source of truth")
	require.Equal(t, codexCLIVersion, openAICodexProbeVersion,
		"codexCLIVersion and openAICodexProbeVersion must stay in sync")
	require.Equal(t, expectedDefaultUserAgent, DefaultOpenAICodexUserAgent)

	originator, userAgent, ok := openai.PairCodexClientIdentity(DefaultOpenAICodexUserAgent)
	require.True(t, ok, "the built-in User-Agent must be a supported Codex identity")
	require.Equal(t, openai.CodexDefaultOriginator, originator)
	require.Equal(t, DefaultOpenAICodexUserAgent, userAgent)
	require.Equal(t, DefaultOpenAICodexVersion, openai.CodexUserAgentVersion(userAgent))
}

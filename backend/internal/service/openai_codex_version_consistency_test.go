//go:build unit

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestCodexVersionConstants_Consistency(t *testing.T) {
	const expectedDefaultUserAgent = "codex_cli_rs/0.167.0 (Ubuntu 24.04; x86_64) xterm-256color"

	require.Equal(t, codexCLIVersion, DefaultOpenAICodexVersion,
		"the compiled-in Codex identity version must have one source of truth")
	require.Equal(t, codexCLIVersion, openAICodexProbeVersion,
		"codexCLIVersion and openAICodexProbeVersion must stay in sync")
	// User requirement: each reviewed default identity upgrade also upgrades
	// the executable and its independently reviewed release artifact manifest.
	// Explicit administrator identity pins do not select an executable.
	require.Equal(t, "0.167.0", codexcli.TargetVersion())
	require.Equal(t, codexCLIVersion, codexcli.TargetVersion(),
		"the reviewed runtime must advance with the compiled outbound identity")
	require.Equal(t, expectedDefaultUserAgent, DefaultOpenAICodexUserAgent)

	originator, userAgent, ok := openai.PairCodexClientIdentity(DefaultOpenAICodexUserAgent)
	require.True(t, ok, "the built-in User-Agent must be a supported Codex identity")
	require.Equal(t, openai.CodexDefaultOriginator, originator)
	require.Equal(t, DefaultOpenAICodexUserAgent, userAgent)
	require.Equal(t, DefaultOpenAICodexVersion, openai.CodexUserAgentVersion(userAgent))
}

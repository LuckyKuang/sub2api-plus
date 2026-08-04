//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexVersionConstants_Consistency(t *testing.T) {
	require.Equal(t, "0.146.0", codexDefaultVersion,
		"the compiled-in Codex identity version must track the current default")
	require.Equal(t, "codex-tui", codexDefaultOriginator,
		"the compiled-in Codex identity originator must track the current default")
	require.Equal(t, codexCLIVersion, openAICodexProbeVersion,
		"codexCLIVersion and openAICodexProbeVersion must stay in sync")

	require.Equal(t,
		codexDefaultOriginator+"/"+codexCLIVersion+" (Ubuntu 22.4.0; x86_64) xterm-256color ("+codexDefaultOriginator+"; "+codexCLIVersion+")",
		DefaultOpenAICodexUserAgent,
		"the built-in User-Agent must be the complete codex-tui identity")
}

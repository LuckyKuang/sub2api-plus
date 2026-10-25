//go:build unit || !integration

package openai

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// AGENTS.md Codex Identity: synchronization may change only version segments;
// an accepted configured family, OS, architecture and terminal remain intact.
func TestCodexConfiguredFamiliesSurviveUpstreamSync(t *testing.T) {
	cases := []struct{ ua, originator string }{
		{"codex-tui/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color (codex-tui; 0.158.0)", "codex-tui"},
		{"codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs"},
	}
	for _, test := range cases {
		t.Run(test.originator, func(t *testing.T) {
			originator, ua, ok := PairCodexClientIdentity(test.ua)
			require.True(t, ok)
			require.Equal(t, test.originator, originator)
			require.Equal(t, test.ua, ua)
		})
	}
}

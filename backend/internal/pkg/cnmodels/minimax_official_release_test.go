//go:build unit || !integration

package cnmodels

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// MiniMax Code da22f11c55d4748155d7162c1030414228a00565,
// packages/config/src/minimax-model-catalog.ts: current first-run model ID.
func TestMiniMaxOfficialFlashReleaseCandidate(t *testing.T) {
	require.Contains(t, DefaultModelIDs("minimax"), "MiniMax-M3.1-Flash")
	require.NotContains(t, DefaultModelIDs("minimax"), "MiniMax-M3.1-Flash-Preview")
}

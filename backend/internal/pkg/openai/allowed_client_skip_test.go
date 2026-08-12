package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchClientEntry_ReturnsExplicitCompatibilityEntry(t *testing.T) {
	entries := []AllowedClientEntry{
		{Originator: "opencode", UAContains: []string{"opencode/"}},
		{Originator: "Claude Code", UAContains: []string{"Claude Code/"}},
	}

	e, ok := MatchClientEntry("opencode/1.0", "opencode", entries)
	require.True(t, ok)
	require.Equal(t, "opencode", e.Originator)

	e2, ok2 := MatchClientEntry("Claude Code/1.0 (x) (Claude Code; 1)", "Claude Code", entries)
	require.True(t, ok2)
	require.Equal(t, "Claude Code", e2.Originator)

	_, ok3 := MatchClientEntry("curl/8", "evil", entries)
	require.False(t, ok3)
	_, mismatched := MatchClientEntry("opencode/1.0", "Claude Code", entries)
	require.False(t, mismatched, "configured profiles require coherent UA/originator identity")

	// 薄封装保持兼容
	require.True(t, MatchClientEntries("opencode/1.0", "opencode", entries))
	require.False(t, MatchClientEntries("curl/8", "evil", entries))
}

//go:build unit || !integration

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The in-band `codex.rate_limits` event is the only quota source the official
// WebSocket client has: the 101 handshake carries none. Plus must turn it into
// the same snapshot an HTTP response header produces.
func TestParseCodexRateLimitEventSnapshot(t *testing.T) {
	snapshot := parseCodexRateLimitEventSnapshot([]byte(`{
		"type":"codex.rate_limits",
		"plan_type":"plus",
		"rate_limits":{
			"primary":{"used_percent":31.5,"window_minutes":10080,"reset_at":1790000000},
			"secondary":{"used_percent":82.25,"window_minutes":300,"reset_at":1790003600}
		},
		"credits":{"has_credits":true,"unlimited":false,"balance":"12.75"}
	}`))
	require.NotNil(t, snapshot)
	require.NotNil(t, snapshot.PrimaryUsedPercent)
	require.InDelta(t, 31.5, *snapshot.PrimaryUsedPercent, 1e-9)
	require.NotNil(t, snapshot.PrimaryWindowMinutes)
	require.Equal(t, 10080, *snapshot.PrimaryWindowMinutes)
	require.NotNil(t, snapshot.PrimaryResetAtUnix)
	require.Equal(t, int64(1790000000), *snapshot.PrimaryResetAtUnix)
	require.NotNil(t, snapshot.SecondaryUsedPercent)
	require.InDelta(t, 82.25, *snapshot.SecondaryUsedPercent, 1e-9)
	require.NotNil(t, snapshot.SecondaryWindowMinutes)
	require.Equal(t, 300, *snapshot.SecondaryWindowMinutes)
	require.NotNil(t, snapshot.SecondaryResetAtUnix)
	require.Equal(t, int64(1790003600), *snapshot.SecondaryResetAtUnix)
	require.NotNil(t, snapshot.CreditsHasCredits)
	require.True(t, *snapshot.CreditsHasCredits)
	require.NotNil(t, snapshot.CreditsUnlimited)
	require.False(t, *snapshot.CreditsUnlimited)
	require.Equal(t, "12.75", snapshot.CreditsBalance)
}

// Windows without a parseable used_percent are dropped, matching the official
// `used_percent.and_then(...)`.
func TestParseCodexRateLimitEventSnapshot_WindowGuards(t *testing.T) {
	snapshot := parseCodexRateLimitEventSnapshot([]byte(`{
		"type":"codex.rate_limits",
		"rate_limits":{
			"primary":{"used_percent":50},
			"secondary":{"window_minutes":300}
		}
	}`))
	require.NotNil(t, snapshot)
	require.NotNil(t, snapshot.PrimaryUsedPercent)
	require.InDelta(t, 50, *snapshot.PrimaryUsedPercent, 1e-9)
	require.Nil(t, snapshot.PrimaryWindowMinutes, "a missing window must stay nil")
	require.Nil(t, snapshot.PrimaryResetAtUnix, "a missing reset must stay nil")
	require.Nil(t, snapshot.SecondaryUsedPercent, "a window without used_percent is dropped")
}

func TestParseCodexRateLimitEventSnapshot_CreditsRequireBothFlags(t *testing.T) {
	snapshot := parseCodexRateLimitEventSnapshot([]byte(`{
		"type":"codex.rate_limits",
		"rate_limits":{"primary":{"used_percent":10}},
		"credits":{"has_credits":true}
	}`))
	require.NotNil(t, snapshot)
	require.Nil(t, snapshot.CreditsHasCredits, "official parse_credits_snapshot requires both flags")
	require.Empty(t, snapshot.CreditsBalance)
}

func TestParseCodexRateLimitEventSnapshot_OutOfRangeResetDropped(t *testing.T) {
	for _, resetAt := range []string{"0", "-1", "9223372036854775807", "253402300800"} {
		snapshot := parseCodexRateLimitEventSnapshot([]byte(`{
			"type":"codex.rate_limits",
			"rate_limits":{"primary":{"used_percent":100,"reset_at":` + resetAt + `}}
		}`))
		require.NotNil(t, snapshot)
		require.Nil(t, snapshot.PrimaryResetAtUnix, "%s must not produce a reset time", resetAt)
	}
}

func TestParseCodexRateLimitEventSnapshot_NonDefaultFamilyAndNoise(t *testing.T) {
	// Non-default metered families are handled by the local group-quota
	// synthesis, not by the account-level 5h/7d view.
	require.Nil(t, parseCodexRateLimitEventSnapshot([]byte(`{
		"type":"codex.rate_limits",
		"metered_limit_name":"codex_other",
		"rate_limits":{"primary":{"used_percent":100}}
	}`)))

	require.Nil(t, parseCodexRateLimitEventSnapshot([]byte(`not json`)))
	require.Nil(t, parseCodexRateLimitEventSnapshot([]byte(`{"type":"response.completed"}`)))
	require.Nil(t, parseCodexRateLimitEventSnapshot([]byte(`{"type":"codex.rate_limits"}`)),
		"an event without rate_limits or credits carries no snapshot")
}

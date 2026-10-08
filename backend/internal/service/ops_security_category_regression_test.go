//go:build unit

package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditSecurityCategoryFiltersMatchLocalDenialsOnly(t *testing.T) {
	for _, code := range []string{"content_policy_violation", "session_blocked_by_content_policy", "prompt_guard_blocked"} {
		require.Equal(t, "security_audit", MapUserErrorCategory("request", code))
		require.Equal(t, "upstream", MapUserErrorCategory("upstream", code))
	}
	phases, types := CategoryToFilter("security_audit")
	require.Equal(t, []string{"request"}, phases)
	require.ElementsMatch(t, []string{"content_policy_violation", "session_blocked_by_content_policy", "prompt_guard_blocked"}, types)
}

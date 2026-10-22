//go:build unit || !integration

package minimax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatrixUsesOfficialAccountClientWithoutChangingOwnerSource(t *testing.T) {
	identity := DefaultIdentity()
	identity.Source = "account"
	identity.AccountID = 51
	for _, target := range []string{"https://agent.minimaxi.com/v1/api/user/info", "https://agent.minimax.io/v1/api/user/info", "https://agent.minimaxi.com/matrix/api/v1/user/get_user_info", "https://agent.minimax.io/matrix/api/v1/commerce/get_membership_info"} {
		got := ControlIdentityForURL(identity, target)
		require.Equal(t, "MiniMaxCode", got.UserAgent)
		require.Equal(t, "MiniMaxCode", got.Originator)
		require.Equal(t, "account", got.Source)
		require.EqualValues(t, 51, got.AccountID)
		require.Equal(t, map[string]string{"User-Agent": "MiniMaxCode"}, got.Headers)
	}
	for _, target := range []string{"https://platform.minimax.io/v1/api/openplatform/coding_plan/remains", "https://account.minimax.io/oauth2/token", "https://agent.minimax.io/v1/messages", "https://agent.minimax.io.evil.invalid/matrix/api/v1/user/get_user_info"} {
		require.Equal(t, "MiniMaxAgent", ControlIdentityForURL(identity, target).UserAgent)
	}
	require.Equal(t, "MiniMaxAgent", identity.UserAgent)
	require.Equal(t, "Anthropic/JS 0.91.1", APIKeyIdentity().UserAgent)
}

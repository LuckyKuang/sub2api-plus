//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdminResetGrokAccountClearsItsModelOwnersAndPreservesTeamLimits(t *testing.T) {
	for _, transient := range []bool{false, true} {
		t.Run(map[bool]string{false: "free usage", true: "transient capacity"}[transient], func(t *testing.T) {
			team := "reset-grok-team-" + time.Now().Format("150405.000000000")
			current := &Account{ID: time.Now().UnixNano(), Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive,
				Credentials: map[string]any{"team_id": team, "access_token": "current-owner"}}
			oldOwner, err := snapshotCNControlAccount(current)
			require.NoError(t, err)
			oldOwner.Credentials["access_token"] = "previous-owner"
			other := &Account{ID: current.ID + 1, Platform: PlatformGrok, Type: AccountTypeOAuth,
				Credentials: map[string]any{"team_id": team, "access_token": "independent-owner"}}
			mark := markGrokModelQuotaBlock
			if transient {
				mark = markGrokModelTransientBlock
			}
			for _, owner := range []*Account{current, oldOwner, other} {
				mark(owner, "grok-4.5", time.Now().Add(time.Minute))
				require.True(t, isGrokModelQuotaBlocked(owner, "grok-4.5", time.Now()))
			}
			markGrokTeamModelRateLimit(current, "grok-4.5", time.Now().Add(time.Minute))
			repo := &accountRepoStubForClearAccountError{account: current}
			gateway := &OpenAIGatewayService{}
			admin := &adminServiceImpl{accountRepo: repo, runtimeBlocker: gateway}
			updated, err := admin.ClearAccountError(context.Background(), current.ID)
			require.NoError(t, err)
			require.NotNil(t, updated)
			require.Len(t, filterGrokModelQuotaBlockedAccounts([]Account{*updated}, "grok-4.5", time.Now()), 1, "manual reset must restore this account's local model admission")
			require.False(t, isGrokModelQuotaBlocked(oldOwner, "grok-4.5", time.Now()), "reset must retire historical owners of the same account too")
			require.True(t, isGrokModelQuotaBlocked(other, "grok-4.5", time.Now()), "reset cannot clear another account's independent model observation")
			require.True(t, isGrokTeamModelRateLimited(updated, "grok-4.5", time.Now()), "an account reset cannot erase a real shared team quota window")
			require.True(t, isGrokTeamModelRateLimited(other, "grok-4.5", time.Now()))
		})
	}
}

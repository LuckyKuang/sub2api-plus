//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokModelRuntimeOutcomeCannotBlockOwnerReplacedAfterCommit(t *testing.T) {
	for _, operation := range []string{"model-free-usage", "transient-capacity"} {
		for _, change := range []string{"unchanged", "relinked", "proxy-edited"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
				account.ID = time.Now().UnixNano()
				owner, err := snapshotCNControlAccount(account)
				require.NoError(t, err)
				repo := &grokOutcomeOwnerRepo{current: account}
				repo.afterApplied = func() {
					replacement, copyErr := snapshotCNControlAccount(account)
					require.NoError(t, copyErr)
					switch change {
					case "relinked":
						replacement.Credentials["api_key"] = "post-commit-replacement-key"
					case "proxy-edited":
						replacement.Proxy.Password = "post-commit-replacement-route"
					}
					repo.current = replacement
				}
				svc := &OpenAIGatewayService{accountRepo: repo}
				status := http.StatusBadRequest
				body := []byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"You've used all the included free usage for model grok-4.5"}}`)
				if operation == "transient-capacity" {
					status = http.StatusTooManyRequests
					body = []byte(`{"error":{"message":"The model is currently at capacity due to high demand"}}`)
				}
				published := svc.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), owner, status, nil, body)
				require.True(t, published)
				require.Equal(t, 1, repo.atomicWrites)
				require.Zero(t, repo.legacyWrites)
				filtered := filterGrokModelQuotaBlockedAccounts([]Account{*repo.current}, "grok-4.5", time.Now())
				require.Equal(t, change == "unchanged", len(filtered) == 0, "a post-commit replacement must not inherit the captured owner's delayed model block")
				require.Empty(t, filterGrokModelQuotaBlockedAccounts([]Account{*owner}, "grok-4.5", time.Now()), "the actual failing owner must retain its model cooldown")
				require.Len(t, filterGrokModelQuotaBlockedAccounts([]Account{*repo.current}, "grok-4.3", time.Now()), 1, "healthy sibling models must remain schedulable")
			})
		}
	}
}

func TestGrokTeamBlockKeepsCapturedTeamAfterAccountMoves(t *testing.T) {
	team := "captured-team-" + time.Now().Format("150405.000000000")
	account := &Account{ID: time.Now().UnixNano(), Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"team_id": team, "access_token": "original-team-login"}}
	owner, err := snapshotCNControlAccount(account)
	require.NoError(t, err)
	account.Credentials["team_id"] = team + "-replacement"
	account.Credentials["access_token"] = "replacement-team-login"
	markGrokTeamModelRateLimit(owner, "grok-4.5", time.Now().Add(time.Minute))
	sibling := &Account{ID: account.ID + 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"team_id": team, "access_token": "independent-sibling-login"}}
	require.False(t, isGrokTeamModelRateLimited(account, "grok-4.5", time.Now()), "the replacement team cannot inherit the old team's observation")
	require.True(t, isGrokTeamModelRateLimited(sibling, "grok-4.5", time.Now()), "the actual old team's siblings must still share its quota cooldown")
	require.False(t, isGrokTeamModelRateLimited(sibling, "grok-4.3", time.Now()))
}

func TestGrokModelDelayedOldOwnerCannotReplaceNewOwnerCooldown(t *testing.T) {
	for _, transient := range []bool{false, true} {
		t.Run(map[bool]string{false: "free usage", true: "transient capacity"}[transient], func(t *testing.T) {
			oldOwner := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
			oldOwner.ID = time.Now().UnixNano()
			newOwner, err := snapshotCNControlAccount(oldOwner)
			require.NoError(t, err)
			newOwner.Credentials["api_key"] = "replacement-model-owner"
			now := time.Now()
			mark := markGrokModelQuotaBlock
			oldTTL, newTTL, observedAfter := time.Hour, 2*time.Hour, 90*time.Minute
			if transient {
				mark = markGrokModelTransientBlock
				oldTTL, newTTL, observedAfter = time.Minute, 4*time.Minute, 2*time.Minute
			}
			mark(newOwner, "grok-4.5", now.Add(newTTL))
			mark(oldOwner, "grok-4.5", now.Add(oldTTL))
			require.True(t, isGrokModelQuotaBlocked(newOwner, "grok-4.5", now.Add(observedAfter)), "late publication cannot replace the new owner's longer cooldown")
			require.False(t, isGrokModelQuotaBlocked(oldOwner, "grok-4.5", now.Add(observedAfter)), "expiry of the old owner's block must be independent")
			require.True(t, isGrokModelQuotaBlocked(newOwner, "grok-4.5", now.Add(observedAfter)), "pruning the old owner's expired block cannot clear the new owner's block")
		})
	}
}

func TestGrokModelCooldownSelectionUsesActualMappedModel(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
	account.ID = time.Now().UnixNano()
	account.Credentials["model_mapping"] = map[string]any{"alias": "grok-4.5", "grok-4.5": "grok-4.3"}
	markGrokModelTransientBlock(account, "grok-4.5", time.Now().Add(time.Minute))
	t.Run("mapped-to-failing-model", func(t *testing.T) {
		require.Empty(t, filterGrokModelQuotaBlockedAccounts([]Account{*account}, "alias", time.Now()), "an alias of the failed upstream model must share its cooldown")
	})
	t.Run("mapped-to-healthy-sibling", func(t *testing.T) {
		require.Len(t, filterGrokModelQuotaBlockedAccounts([]Account{*account}, "grok-4.5", time.Now()), 1, "a request mapped to a healthy upstream model cannot inherit an unrelated model's failure")
	})
}

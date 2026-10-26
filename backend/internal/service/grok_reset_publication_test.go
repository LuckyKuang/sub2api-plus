//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokManualResetRejectsPreResetOutcomePublication(t *testing.T) {
	for _, operation := range []string{"account-cooldown", "model-free-usage", "model-capacity"} {
		for _, reset := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/unchanged", true: "/reset-after-commit"}[reset], func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
				account.ID = time.Now().UnixNano()
				owner, err := snapshotCNControlAccount(account)
				require.NoError(t, err)
				repo := &grokOutcomeOwnerRepo{current: account}
				gateway := &OpenAIGatewayService{accountRepo: repo}
				if reset {
					repo.afterApplied = func() {
						account.TempUnschedulableUntil, account.TempUnschedulableReason = nil, ""
						account.RateLimitedAt, account.RateLimitResetAt = nil, nil
						gateway.ClearAccountSchedulingBlock(account.ID)
					}
				}
				status, body := http.StatusUnauthorized, []byte(`{"error":{"message":"isolated auth failure"}}`)
				if operation == "model-free-usage" {
					status, body = http.StatusBadRequest, []byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"You've used all the included free usage for model grok-4.5"}}`)
				}
				if operation == "model-capacity" {
					status, body = http.StatusTooManyRequests, []byte(`{"error":{"message":"The model is currently at capacity due to high demand"}}`)
				}
				gateway.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), owner, status, nil, body)
				require.Equal(t, 1, repo.atomicWrites, "the original observation committed before the reset")
				if operation == "account-cooldown" {
					require.Equal(t, !reset, gateway.isOpenAIAccountRuntimeBlocked(owner), "a completed reset cannot be undone by the earlier observation's late local effect")
				} else {
					require.Equal(t, !reset, isGrokModelQuotaBlocked(owner, "grok-4.5", time.Now()), "a pre-reset model failure cannot reinstall local cooldown after reset")
				}
			})
		}
	}
}

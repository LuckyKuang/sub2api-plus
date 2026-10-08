//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Contract: docs/providers/DOMESTIC_OAUTH.md#refresh-deadlines.
// Model a deadline whose cancellation notification has not been scheduled yet.
// The expired deadline is independent evidence; Err/Done are not its authority.
type unsignaledPastRefreshDeadline struct{ context.Context }

func (unsignaledPastRefreshDeadline) Deadline() (time.Time, bool) {
	return time.Unix(1, 0), true
}

// Refresh deadlines prohibit late credential writes even under timer contention.
func TestOAuthRefreshExpiredUnsignaledDeadlineCannotPersistCredentials(t *testing.T) {
	account := &Account{ID: 91, Platform: PlatformKimi, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "current-token"}}
	repo := &refreshAPIAccountRepo{account: account}
	executor := &refreshAPIExecutorStub{needsRefresh: true,
		credentials: map[string]any{"access_token": "late-token"}}
	api := NewOAuthRefreshAPI(repo, nil)
	ctx := unsignaledPastRefreshDeadline{Context: context.Background()}
	require.NoError(t, ctx.Err(), "fixture represents delayed cancellation notification")

	result, err := api.RefreshIfNeeded(ctx, account, executor, time.Hour)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, result)
	require.Zero(t, repo.updateCredentialsCalls, "expired refresh results must not cross persistence")
	require.Equal(t, "current-token", account.Credentials["access_token"])
}

// A cycle/caller timeout is not evidence that an account needs cooldown.
func TestTokenRefreshExpiredParentDeadlineStopsWithoutAccountMutation(t *testing.T) {
	repo := &poolHealthAccountRepo{}
	refresher := &poolHealthRefresher{}
	svc := newPoolHealthService(repo, refresher, config.TokenRefreshConfig{MaxRetries: 1})
	account := grokPoolAccount(92)
	ctx := unsignaledPastRefreshDeadline{Context: context.Background()}

	err := svc.refreshWithRetry(ctx, &account, refresher, nil, time.Hour)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, refresher.calls.Load(), "an expired parent cannot authorize an upstream refresh")
	_, updated, permanent, temporary := repo.snapshot()
	require.Empty(t, updated)
	require.Zero(t, permanent)
	require.Zero(t, temporary, "caller/cycle expiry cannot penalize an account")
}

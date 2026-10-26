//go:build unit || !integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCNOAuthRefreshCASUsesCompleteGrantAndAtomicOutbox(t *testing.T) {
	for _, changed := range []int64{0, 1} {
		t.Run(string(rune('0'+changed)), func(t *testing.T) {
			exec := &recordingSQLExecutor{result: rowsAffectedResult(changed)}
			repo := newAccountRepositoryWithSQL(nil, exec, nil)
			cache := &oauthRefreshSchedulerCacheRecorder{}
			repo.schedulerCache = cache
			proxy := int64(7)
			applied, err := repo.UpdateOAuthRefreshCredentialsIfUnchanged(context.Background(), &service.Account{ID: 42, Platform: service.PlatformKimi, Credentials: map[string]any{"access_token": "attempted", "refresh_token": "old"}, ProxyID: &proxy, Type: service.AccountTypeOAuth, Proxy: &service.Proxy{ID: proxy, Status: service.StatusActive}, Status: service.StatusActive}, map[string]any{"access_token": "new", "refresh_token": "rotated"})
			require.NoError(t, err)
			require.Equal(t, changed == 1, applied)
			require.Len(t, exec.execQueries, 1)
			query := normalizeSQLWhitespace(exec.execQueries[0])
			require.Contains(t, query, "WITH updated AS")
			require.Contains(t, query, "a.credentials = $5::jsonb")
			require.Contains(t, query, "a.proxy_id IS NOT DISTINCT FROM $6")
			require.Contains(t, query, "a.status = $7")
			require.Contains(t, query, "INSERT INTO scheduler_outbox")
			require.Equal(t, service.PlatformKimi, exec.execArgs[0][2])
			require.Equal(t, service.AccountTypeOAuth, exec.execArgs[0][3])
			require.Equal(t, &proxy, exec.execArgs[0][5])
			require.Equal(t, service.StatusActive, exec.execArgs[0][6])
			require.Empty(t, cache.setAccounts, "refresh persistence must not publish a snapshot read before concurrent reauthorization")
			if changed == 1 {
				require.Equal(t, []int64{42}, cache.deleteIDs)
			} else {
				require.Empty(t, cache.deleteIDs, "a stale refresh cannot invalidate the current owner's scheduler state")
			}
		})
	}
}

func TestOAuthRefreshCASEvictsSchedulerSnapshotWithBoundedDetachedContext(t *testing.T) {
	// The exact-owner CAS and atomic outbox must not be followed by a mutable
	// read/SetAccount publication, even if the request is canceled after commit.
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		t.Run(platform, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			exec := &recordingSQLExecutor{result: rowsAffectedResult(1), afterExec: cancel}
			cache := &oauthRefreshSchedulerCacheRecorder{}
			repo := newAccountRepositoryWithSQL(nil, exec, nil)
			repo.schedulerCache = cache

			applied, err := repo.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, &service.Account{ID: 42, Platform: platform, Credentials: map[string]any{"access_token": "observed"}, ProxyID: nil, Type: service.AccountTypeOAuth, Status: service.StatusActive}, map[string]any{"access_token": "rotated"})
			require.NoError(t, err)
			require.True(t, applied)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, []int64{42}, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
			require.NoError(t, cache.deleteCtxErr, "committed refresh eviction must survive caller cancellation")
			require.True(t, cache.deleteHasDeadline, "detached eviction must be bounded")
			require.Positive(t, time.Until(cache.deleteDeadline))
			require.LessOrEqual(t, time.Until(cache.deleteDeadline), 2*time.Second)
		})
	}
}

func TestOAuthRefreshCASFailureDoesNotMutateSchedulerCache(t *testing.T) {
	dbErr := errors.New("refresh persistence failed")
	rowsErr := errors.New("refresh result unavailable")
	for _, tc := range []struct {
		name    string
		exec    *recordingSQLExecutor
		wantErr error
	}{
		{name: "cas_miss", exec: &recordingSQLExecutor{result: rowsAffectedResult(0)}},
		{name: "sql_error", exec: &recordingSQLExecutor{err: dbErr}, wantErr: dbErr},
		{name: "rows_affected_error", exec: &recordingSQLExecutor{result: oauthRefreshRowsAffectedErrorResult{err: rowsErr}}, wantErr: rowsErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &oauthRefreshSchedulerCacheRecorder{}
			repo := newAccountRepositoryWithSQL(nil, tc.exec, nil)
			repo.schedulerCache = cache
			applied, err := repo.UpdateOAuthRefreshCredentialsIfUnchanged(context.Background(), &service.Account{ID: 42, Platform: service.PlatformOpenAI, Credentials: map[string]any{"access_token": "observed"}, ProxyID: nil, Type: service.AccountTypeOAuth, Status: service.StatusActive}, map[string]any{"access_token": "obsolete"})
			require.ErrorIs(t, err, tc.wantErr)
			require.False(t, applied)
			require.Empty(t, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
		})
	}
}

type oauthRefreshSchedulerCacheRecorder struct {
	service.SchedulerCache
	setAccounts       []*service.Account
	deleteIDs         []int64
	deleteCtxErr      error
	deleteDeadline    time.Time
	deleteHasDeadline bool
}

func (s *oauthRefreshSchedulerCacheRecorder) SetAccount(_ context.Context, account *service.Account) error {
	s.setAccounts = append(s.setAccounts, account)
	return nil
}

func (s *oauthRefreshSchedulerCacheRecorder) DeleteAccount(ctx context.Context, accountID int64) error {
	s.deleteIDs = append(s.deleteIDs, accountID)
	s.deleteCtxErr = ctx.Err()
	s.deleteDeadline, s.deleteHasDeadline = ctx.Deadline()
	return nil
}

type oauthRefreshRowsAffectedErrorResult struct {
	rowsAffectedResult
	err error
}

func (r oauthRefreshRowsAffectedErrorResult) RowsAffected() (int64, error) { return 0, r.err }

//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// The account credential contract requires an atomic conditional quarantine:
// reauthorization/rotation must not be disabled by a failed older attempt, and
// scheduler notification must commit with the state transition.
func TestCredentialFailureQuarantineFencesExactOwnerAndCredentials(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	proxy := int64(17)
	changed, err := repo.SetCredentialErrorIfUnchanged(context.Background(), &service.Account{ID: 42, Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "old", "_token_version": 7}, ProxyID: &proxy, Proxy: &service.Proxy{ID: proxy, Status: service.StatusActive}, Status: service.StatusActive}, "grant_revoked")
	require.NoError(t, err)
	require.False(t, changed)
	require.Len(t, exec.execQueries, 1)
	query := normalizeSQLWhitespace(exec.execQueries[0])
	for _, condition := range []string{"a.deleted_at IS NULL", "a.status = $4", "a.platform = $5", "a.type = $6", "a.credentials = $7::jsonb", "a.proxy_id IS NOT DISTINCT FROM $8", "INSERT INTO scheduler_outbox", "FROM updated"} {
		require.Contains(t, query, condition)
	}
	require.Equal(t, service.PlatformKimi, exec.execArgs[0][4])
	require.Equal(t, service.AccountTypeOAuth, exec.execArgs[0][5])
	require.JSONEq(t, `{"access_token":"old","_token_version":7}`, exec.execArgs[0][6].(string))
	require.Equal(t, &proxy, exec.execArgs[0][7])
}

func TestCredential401CooldownFencesExactOwnerAndPreservesLongerWindow(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	proxy := int64(17)
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	changed, err := repo.SetCredentialTempUnschedulableIfUnchanged(context.Background(), &service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "sent", "refresh_token": "old", "_token_version": 7}, ProxyID: &proxy, Proxy: &service.Proxy{ID: proxy, Status: service.StatusActive}, Status: service.StatusActive}, until, "oauth_401")
	require.NoError(t, err)
	require.False(t, changed)
	require.Len(t, exec.execQueries, 1)
	query := normalizeSQLWhitespace(exec.execQueries[0])
	for _, condition := range []string{"a.deleted_at IS NULL", "a.status = $4", "a.platform = $5", "a.type = $6", "a.credentials = $7::jsonb", "a.proxy_id IS NOT DISTINCT FROM $8", "a.temp_unschedulable_until < $1", "INSERT INTO scheduler_outbox", "FROM updated"} {
		require.Contains(t, query, condition)
	}
	require.Equal(t, until, exec.execArgs[0][0])
	require.Equal(t, service.StatusActive, exec.execArgs[0][3])
	require.JSONEq(t, `{"access_token":"sent","refresh_token":"old","_token_version":7}`, exec.execArgs[0][6].(string))
	require.Equal(t, &proxy, exec.execArgs[0][7])
}

func TestCredentialConditionalMutationsEvictSchedulerWithBoundedDetachedContext(t *testing.T) {
	for _, mutation := range credentialConditionalMutationCases() {
		t.Run(mutation.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cache := &oauthRefreshSchedulerCacheRecorder{}
			exec := &recordingSQLExecutor{result: rowsAffectedResult(1), afterExec: cancel}
			repo := newAccountRepositoryWithSQL(nil, exec, cache)

			applied, err := mutation.apply(ctx, repo)

			require.NoError(t, err)
			require.True(t, applied)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Len(t, exec.execQueries, 1, "no follow-up credential write is allowed after the atomic transition")
			require.Equal(t, []int64{42}, cache.deleteIDs)
			require.Empty(t, cache.setAccounts, "an older credential operation must never publish a scheduler snapshot")
			require.NoError(t, cache.deleteCtxErr, "committed mutations finish eviction after caller cancellation")
			require.True(t, cache.deleteHasDeadline)
			require.Positive(t, time.Until(cache.deleteDeadline))
			require.LessOrEqual(t, time.Until(cache.deleteDeadline), 2*time.Second)
		})
	}
}

func TestCredentialConditionalMutationFailureDoesNotNotifyScheduler(t *testing.T) {
	dbErr := errors.New("atomic outbox insert failed")
	rowsErr := errors.New("mutation result unavailable")
	for _, mutation := range credentialConditionalMutationCases() {
		for _, outcome := range []struct {
			name string
			exec *recordingSQLExecutor
			err  error
		}{
			{name: "cas-miss", exec: &recordingSQLExecutor{result: rowsAffectedResult(0)}},
			{name: "atomic-write-error", exec: &recordingSQLExecutor{err: dbErr}, err: dbErr},
			{name: "unknown-result", exec: &recordingSQLExecutor{result: oauthRefreshRowsAffectedErrorResult{err: rowsErr}}, err: rowsErr},
		} {
			t.Run(mutation.name+"/"+outcome.name, func(t *testing.T) {
				cache := &oauthRefreshSchedulerCacheRecorder{}
				repo := newAccountRepositoryWithSQL(nil, outcome.exec, cache)

				applied, err := mutation.apply(context.Background(), repo)

				require.ErrorIs(t, err, outcome.err)
				require.False(t, applied)
				require.Empty(t, cache.deleteIDs)
				require.Empty(t, cache.setAccounts)
			})
		}
	}
}

func credentialConditionalMutationCases() []struct {
	name  string
	apply func(context.Context, *accountRepository) (bool, error)
} {
	credentials := map[string]any{"access_token": "attempted", "refresh_token": "attempted-refresh"}
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	snapshot := service.GrokCredentialMutationSnapshot{
		CredentialsJSON: `{"access_token":"attempted","refresh_token":"attempted-refresh"}`,
		Owner:           &service.Account{ID: 42, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: credentials},
	}
	return []struct {
		name  string
		apply func(context.Context, *accountRepository) (bool, error)
	}{
		{name: "generic-error", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.SetCredentialErrorIfUnchanged(ctx, &service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: credentials, ProxyID: nil, Status: service.StatusActive}, "revoked")
		}},
		{name: "generic-cooldown", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.SetCredentialTempUnschedulableIfUnchanged(ctx, &service.Account{ID: 42, Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, Credentials: credentials, ProxyID: nil, Status: service.StatusActive}, until, "rejected")
		}},
		{name: "grok-request-error", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.SetGrokCredentialErrorIfMatch(ctx, 42, snapshot, "revoked")
		}},
		{name: "grok-request-cooldown", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.SetGrokCredentialTempUnschedulableIfMatch(ctx, 42, snapshot, until, "rejected")
		}},
		{name: "grok-missing-refresh", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.SetGrokOAuthErrorIfCredentialsUnchanged(ctx, &service.Account{ID: 42, Credentials: map[string]any{"access_token": "attempted"}, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive}, "missing refresh token")
		}},
		{name: "grok-refresh-success", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.UpdateGrokOAuthCredentialsIfUnchanged(ctx, &service.Account{ID: 42, Credentials: credentials, ProxyID: nil, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive}, map[string]any{"access_token": "rotated", "refresh_token": "rotated-refresh"})
		}},
		{name: "grok-refresh-error", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.SetGrokOAuthRefreshErrorIfCredentialsUnchanged(ctx, &service.Account{ID: 42, Credentials: credentials, ProxyID: nil, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive}, "revoked")
		}},
		{name: "grok-refresh-cooldown", apply: func(ctx context.Context, repo *accountRepository) (bool, error) {
			return repo.SetGrokOAuthRefreshTempUnschedulableIfCredentialsUnchanged(ctx, &service.Account{ID: 42, Credentials: credentials, ProxyID: nil, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive}, until, "rejected")
		}},
	}
}

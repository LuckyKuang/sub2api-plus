//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type integrationCNOAuthCredentialWriter interface {
	UpdateCNOAuthCredentialsIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error)
}

func cnOAuthCredentialPort(t *testing.T, repo *accountRepository) integrationCNOAuthCredentialWriter {
	t.Helper()
	port, ok := any(repo).(integrationCNOAuthCredentialWriter)
	require.True(t, ok, "native OAuth refresh requires atomic credential-owner persistence")
	return port
}

func TestCNOAuthRefreshCASPublishesOnlyCommittedReplacementCredentials(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	applied, err := cnOAuthCredentialPort(t, repo).UpdateCNOAuthCredentialsIfUnchanged(context.Background(), expected,
		map[string]any{"oauth_provider": "kimi", "access_token": "rotated-token", "refresh_token": "rotated-refresh", "_token_version": 8})
	require.NoError(t, err)
	require.True(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, "rotated-token", got.GetCredential("access_token"))
	require.Equal(t, "rotated-refresh", got.GetCredential("refresh_token"))
	require.Equal(t, expected.Extra, got.Extra)
	require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
	require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
}

func TestCNOAuthRefreshCASConcurrentReauthorizationPreservesNewLogin(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	port := cnOAuthCredentialPort(t, repo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	relink, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = relink.Rollback() }()
	_, err = relink.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-login","refresh_token":"replacement-refresh"}'::jsonb WHERE id=$1`, expected.ID)
	require.NoError(t, err)
	type outcome struct {
		applied bool
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		applied, err := port.UpdateCNOAuthCredentialsIfUnchanged(ctx, expected,
			map[string]any{"oauth_provider": "kimi", "access_token": "late-rotated-old-login", "refresh_token": "late-rotated-old-refresh"})
		done <- outcome{applied, err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
			AND wait_event_type='Lock' AND query LIKE '%/* cn_oauth_refresh_cas */%'
		)`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "old refresh must reach PostgreSQL behind the newer authorization")
	require.NoError(t, relink.Commit())
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.False(t, result.applied)
	case <-ctx.Done():
		t.Fatal("late native OAuth refresh did not finish")
	}
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, "replacement-login", got.GetCredential("access_token"))
	require.Equal(t, "replacement-refresh", got.GetCredential("refresh_token"))
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

func TestCNOAuthRefreshCASOutboxFailureRollsBackCredentials(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	applied, err := cnOAuthCredentialPort(t, repo).UpdateCNOAuthCredentialsIfUnchanged(context.Background(), expected,
		map[string]any{"oauth_provider": "kimi", "access_token": "rotated-token", "refresh_token": "rotated-refresh"})
	require.Error(t, err)
	require.False(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, expected.Credentials, got.Credentials)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

func TestCNOAuthLoginCASRecoveryCommitsWithCredentialsAndOutbox(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET status='error',error_message='expired login',rate_limited_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '1 hour',overload_until=NOW()+INTERVAL '1 hour',temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='expired login',extra=extra || '{"model_rate_limits":{"m":{"rate_limit_reset_at":"2030-01-01T00:00:00Z"}},"antigravity_quota_scopes":{"old":true}}'::jsonb WHERE id=$1`, expected.ID)
	require.NoError(t, err)
	expected, err = repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	applied, err := applyCNProxyOwnerResult(t, context.Background(), repo, expected, "login")
	require.NoError(t, err)
	require.True(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, "new-authorized-login", got.GetCredential("access_token"))
	require.Equal(t, service.StatusActive, got.Status)
	require.Empty(t, got.ErrorMessage)
	require.Nil(t, got.RateLimitedAt)
	require.Nil(t, got.RateLimitResetAt)
	require.Nil(t, got.OverloadUntil)
	require.Nil(t, got.TempUnschedulableUntil)
	require.Empty(t, got.TempUnschedulableReason)
	require.NotContains(t, got.Extra, "model_rate_limits")
	require.NotContains(t, got.Extra, "antigravity_quota_scopes")
	require.Equal(t, "base", got.Extra["retained"])
	require.Equal(t, expected.Schedulable, got.Schedulable, "reauthorization preserves manual scheduling selection")
	require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
	require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
}

func TestCNOAuthLoginCASStorageFaultLeavesOriginalLoginAndRecoveryState(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET status='error',error_message='original failure',temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='original cooldown' WHERE id=$1`, expected.ID)
	require.NoError(t, err)
	expected, err = repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	applied, err := applyCNProxyOwnerResult(t, context.Background(), repo, expected, "login")
	require.Error(t, err)
	require.False(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, expected.Credentials, got.Credentials)
	require.Equal(t, expected.Status, got.Status)
	require.Equal(t, expected.ErrorMessage, got.ErrorMessage)
	require.Equal(t, expected.TempUnschedulableUntil, got.TempUnschedulableUntil)
	require.Equal(t, expected.TempUnschedulableReason, got.TempUnschedulableReason)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

func TestCNOAuthLoginCASPreservesNewerRuntimeFailures(t *testing.T) {
	for _, mutation := range []string{
		`status='error',error_message='new owner failure'`,
		`rate_limited_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '2 hours'`,
		`overload_until=NOW()+INTERVAL '2 hours'`,
		`temp_unschedulable_until=NOW()+INTERVAL '2 hours',temp_unschedulable_reason='new cooldown'`,
		`extra=extra || '{"model_rate_limits":{"new-model":{"until":"2030-01-01"}}}'::jsonb`,
		`schedulable=FALSE`,
	} {
		t.Run(mutation, func(t *testing.T) {
			repo, expected, cache := newCNControlSnapshotFixture(t)
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET "+mutation+" WHERE id=$1", expected.ID)
			require.NoError(t, err)
			before, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			applied, err := applyCNProxyOwnerResult(t, context.Background(), repo, expected, "login")
			require.NoError(t, err)
			require.False(t, applied)
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, before.Credentials, got.Credentials)
			require.Equal(t, before.Status, got.Status)
			require.Equal(t, before.ErrorMessage, got.ErrorMessage)
			require.Equal(t, before.RateLimitResetAt, got.RateLimitResetAt)
			require.Equal(t, before.OverloadUntil, got.OverloadUntil)
			require.Equal(t, before.TempUnschedulableUntil, got.TempUnschedulableUntil)
			require.Equal(t, before.Extra, got.Extra)
			require.Equal(t, before.Schedulable, got.Schedulable)
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestStepCloudLoginCASStorageFailureKeepsOldSession(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='stepfun',credentials='{"oauth_provider":"stepfun","api_key":"old-model-key","step_account_refresh_token":"old-passport-session","step_account_access_token":"old-passport-access"}'::jsonb,status='error',error_message='old login failure',temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='old login cooldown' WHERE id=$1`, expected.ID)
	require.NoError(t, err)
	expected, err = repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	writer, ok := any(repo).(integrationCNLoginWriter)
	require.True(t, ok)
	// Step cloud polling can return a model key without a Passport session.
	// Failure of the actual PostgreSQL outbox write must not persist that key,
	// discard the old session, or clear the original account's recovery state.
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	applied, err := writer.CompleteCNOAuthLoginIfUnchanged(context.Background(), expected,
		map[string]any{"oauth_provider": "stepfun", "api_key": "new-cloud-model-key"})
	require.Error(t, err)
	require.False(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, expected.Credentials, got.Credentials)
	require.Equal(t, expected.Status, got.Status)
	require.Equal(t, expected.ErrorMessage, got.ErrorMessage)
	require.Equal(t, expected.TempUnschedulableUntil, got.TempUnschedulableUntil)
	require.Equal(t, expected.TempUnschedulableReason, got.TempUnschedulableReason)
	require.Equal(t, expected.Extra, got.Extra)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

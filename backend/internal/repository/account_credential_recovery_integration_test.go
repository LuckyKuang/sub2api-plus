//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// The outbound identity contract makes the transmitted grant and proxy the
// only authority for recovery. A later login, route repair or error must win.
func credentialRecoveryFixture(t *testing.T) (*accountRepository, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	repo, owner := committedCredentialProxyFixture(t)
	_, err := integrationDB.ExecContext(t.Context(), `UPDATE accounts SET platform='antigravity', status='error', error_message='unauthenticated old grant', schedulable=FALSE, temp_unschedulable_until=NOW()+INTERVAL '1 hour', temp_unschedulable_reason='independent cooldown' WHERE id=$1`, owner.ID)
	require.NoError(t, err)
	owner, err = repo.GetByID(t.Context(), owner.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(t.Context(), "DELETE FROM scheduler_outbox WHERE account_id=$1", owner.ID)
	require.NoError(t, err)
	cache := &schedulerCacheRecorder{}
	repo.schedulerCache = cache
	return repo, owner, cache
}

func requireCredentialRecoveryNoEffects(t *testing.T, owner *service.Account, cache *schedulerCacheRecorder, before string) {
	t.Helper()
	var after string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1", owner.ID).Scan(&after))
	require.Equal(t, before, after, "rejected recovery cannot modify any account field")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", owner.ID).Scan(&count))
	require.Zero(t, count)
	require.Empty(t, cache.deleteIDs)
	require.Empty(t, cache.setAccounts)
}

func TestCredentialRecoveryRequiresUnchangedSavedOwnerAndError(t *testing.T) {
	changes := []struct{ name, query string }{
		{"credentials", `UPDATE accounts SET credentials='{"access_token":"new-login"}'::jsonb WHERE id=$1`},
		{"platform", "UPDATE accounts SET platform='gemini' WHERE id=$1"},
		{"type", "UPDATE accounts SET type='apikey' WHERE id=$1"},
		{"status", "UPDATE accounts SET status='disabled' WHERE id=$1"},
		{"error", "UPDATE accounts SET error_message='unauthenticated newer failure' WHERE id=$1"},
		{"binding", "UPDATE accounts SET proxy_id=NULL WHERE id=$1"},
		{"deleted", "UPDATE accounts SET deleted_at=NOW() WHERE id=$1"},
		{"proxy-host", "UPDATE proxies SET host='new.invalid' WHERE id=$1"},
		{"proxy-password", "UPDATE proxies SET password='replaced' WHERE id=$1"},
		{"proxy-status", "UPDATE proxies SET status='inactive' WHERE id=$1"},
		{"proxy-expiry", "UPDATE proxies SET expires_at=NOW()+INTERVAL '1 day' WHERE id=$1"},
		{"proxy-deleted", "UPDATE proxies SET deleted_at=NOW() WHERE id=$1"},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			repo, owner, cache := credentialRecoveryFixture(t)
			id := owner.ID
			if strings.HasPrefix(change.name, "proxy-") {
				id = *owner.ProxyID
			}
			_, err := integrationDB.ExecContext(t.Context(), change.query, id)
			require.NoError(t, err)
			var before string
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1", owner.ID).Scan(&before))
			applied, err := repo.ClearCredentialErrorIfUnchanged(t.Context(), owner)
			require.NoError(t, err)
			require.False(t, applied)
			requireCredentialRecoveryNoEffects(t, owner, cache, before)
		})
	}
}

func TestCredentialRecoveryCommitsErrorAndOutboxOnly(t *testing.T) {
	for _, proxyBound := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "proxy"}[proxyBound], func(t *testing.T) {
			repo, owner, cache := credentialRecoveryFixture(t)
			if !proxyBound {
				_, err := integrationDB.ExecContext(t.Context(), "UPDATE accounts SET proxy_id=NULL WHERE id=$1", owner.ID)
				require.NoError(t, err)
				owner, err = repo.GetByID(t.Context(), owner.ID)
				require.NoError(t, err)
			}
			applied, err := repo.ClearCredentialErrorIfUnchanged(t.Context(), owner)
			require.NoError(t, err)
			require.True(t, applied)
			current, err := repo.GetByID(t.Context(), owner.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusActive, current.Status)
			require.Empty(t, current.ErrorMessage)
			require.False(t, current.Schedulable, "quota recovery does not re-enable scheduling")
			require.Equal(t, owner.TempUnschedulableUntil, current.TempUnschedulableUntil)
			require.Equal(t, "independent cooldown", current.TempUnschedulableReason)
			require.Equal(t, owner.Credentials, current.Credentials)
			require.Equal(t, owner.Extra, current.Extra)
			var count int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", owner.ID).Scan(&count))
			require.Equal(t, 1, count)
			require.Equal(t, []int64{owner.ID}, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
			applied, err = repo.ClearCredentialErrorIfUnchanged(t.Context(), owner)
			require.NoError(t, err)
			require.False(t, applied, "recovery is idempotent")
		})
	}
}

func TestCredentialRecoveryRollsBackWithOutboxFailure(t *testing.T) {
	repo, owner, cache := credentialRecoveryFixture(t)
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	var before string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1", owner.ID).Scan(&before))
	applied, err := repo.ClearCredentialErrorIfUnchanged(t.Context(), owner)
	require.Error(t, err)
	require.False(t, applied)
	requireCredentialRecoveryNoEffects(t, owner, cache, before)
}

func TestCredentialRecoveryWaitsForProxyRepair(t *testing.T) {
	repo, owner, cache := credentialRecoveryFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	repair, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = repair.Rollback() }()
	_, err = repair.ExecContext(ctx, "UPDATE proxies SET host='repaired.invalid' WHERE id=$1", *owner.ProxyID)
	require.NoError(t, err)
	label := "credential_recovery_proxy_repair"
	repo.sql = &credentialProxyMarkedSQLExecutor{sqlExecutor: integrationDB, label: label}
	var before string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1", owner.ID).Scan(&before))
	completed := make(chan credentialProxyMutationResult, 1)
	go func() {
		applied, err := repo.ClearCredentialErrorIfUnchanged(ctx, owner)
		completed <- credentialProxyMutationResult{applied: applied, err: err}
	}()
	awaitCredentialProxyRowLock(t, ctx, label, completed)
	require.NoError(t, repair.Commit())
	select {
	case result := <-completed:
		require.NoError(t, result.err)
		require.False(t, result.applied, "old quota success cannot recover a repaired route")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	requireCredentialRecoveryNoEffects(t, owner, cache, before)
}

// If the account row is locked first, recovery must either hold the proxy lock
// until its update commits, or reject a proxy repair that committed first.
func TestCredentialRecoverySerializesRepairWhileAccountLocked(t *testing.T) {
	repo, owner, cache := credentialRecoveryFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	accountLock, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = accountLock.Rollback() }()
	_, err = accountLock.ExecContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", owner.ID)
	require.NoError(t, err)
	var before string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1", owner.ID).Scan(&before))
	label := "credential_recovery_account_first"
	repo.sql = &credentialProxyMarkedSQLExecutor{sqlExecutor: integrationDB, label: label}
	completed := make(chan credentialProxyMutationResult, 1)
	go func() {
		applied, err := repo.ClearCredentialErrorIfUnchanged(ctx, owner)
		completed <- credentialProxyMutationResult{applied: applied, err: err}
	}()
	awaitCredentialProxyRowLock(t, ctx, label, completed)
	repair, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = repair.Rollback() }()
	_, err = repair.ExecContext(ctx, "SET LOCAL lock_timeout='200ms'")
	require.NoError(t, err)
	_, repairErr := repair.ExecContext(ctx, "UPDATE proxies SET host='repaired.invalid' WHERE id=$1", *owner.ProxyID)
	serialized := repairErr != nil
	if serialized {
		var lockErr *pq.Error
		require.ErrorAs(t, repairErr, &lockErr)
		require.Equal(t, pq.ErrorCode("55P03"), lockErr.Code)
		require.NoError(t, repair.Rollback())
	} else {
		require.NoError(t, repair.Commit())
	}
	require.NoError(t, accountLock.Commit())
	select {
	case result := <-completed:
		require.NoError(t, result.err)
		require.Equal(t, serialized, result.applied, "recovery cannot commit after a proxy replacement")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if serialized {
		require.Equal(t, []int64{owner.ID}, cache.deleteIDs)
		current, err := repo.GetByID(ctx, owner.ID)
		require.NoError(t, err)
		require.Equal(t, service.StatusActive, current.Status)
		require.Empty(t, current.ErrorMessage)
	} else {
		requireCredentialRecoveryNoEffects(t, owner, cache, before)
	}
}

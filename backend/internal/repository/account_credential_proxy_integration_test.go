//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func committedCredentialProxyFixture(t *testing.T) (*accountRepository, *service.Account) {
	t.Helper()
	client := testEntClient(t)
	proxy := mustCreateProxy(t, client, &service.Proxy{Name: "credential-proxy-proof", Protocol: "http", Host: "old.invalid", Port: 8080, Username: "old-user", Password: "old-password", Status: service.StatusActive})
	account := mustCreateAccount(t, client, &service.Account{Name: "credential-proxy-owner", Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, ProxyID: &proxy.ID, Credentials: map[string]any{"access_token": "sent"}, Extra: map[string]any{"keep": "current"}})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_ = client.Account.DeleteOneID(account.ID).Exec(context.Background())
		_ = client.Proxy.DeleteOneID(proxy.ID).Exec(context.Background())
	})
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	owner, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, owner.Proxy)
	return repo, owner
}

func requireCredentialProxyMutationHasNoEffects(t *testing.T, repo *accountRepository, owner *service.Account, cache *schedulerCacheRecorder) {
	t.Helper()
	current, err := repo.GetByID(context.Background(), owner.ID)
	require.NoError(t, err)
	require.Equal(t, owner.Credentials, current.Credentials)
	require.Equal(t, service.StatusActive, current.Status)
	require.True(t, current.Schedulable)
	require.Nil(t, current.TempUnschedulableUntil)
	require.Nil(t, current.RateLimitResetAt)
	require.Equal(t, map[string]any{"keep": "current"}, current.Extra)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", owner.ID).Scan(&count))
	require.Zero(t, count)
	require.Empty(t, cache.setAccounts)
	require.Empty(t, cache.deleteIDs)
}

func TestCredentialProxyCASRejectsSameRowReplacement(t *testing.T) {
	changes := []struct{ name, sql string }{
		{"protocol", "protocol='socks5'"}, {"host", "host='new.invalid'"}, {"port", "port=9090"},
		{"username", "username='new-user'"}, {"password", "password='new-password'"},
		{"status", "status='inactive'"}, {"expiry", "expires_at=NOW()+INTERVAL '1 day'"}, {"deleted", "deleted_at=NOW()"},
	}
	for _, operation := range credentialProxyMutationCases() {
		for _, change := range changes {
			t.Run(operation.name+"/"+change.name, func(t *testing.T) {
				repo, owner := committedCredentialProxyFixture(t)
				cache := &schedulerCacheRecorder{}
				repo.schedulerCache = cache
				result, err := integrationDB.ExecContext(context.Background(), "UPDATE proxies SET "+change.sql+" WHERE id=$1", *owner.ProxyID)
				require.NoError(t, err)
				changed, err := result.RowsAffected()
				require.NoError(t, err)
				require.Equal(t, int64(1), changed)

				applied, err := operation.apply(context.Background(), repo, owner)

				require.NoError(t, err)
				require.False(t, applied, "the previous route cannot authorize a mutation of its repaired account")
				requireCredentialProxyMutationHasNoEffects(t, repo, owner, cache)
			})
		}
	}
}

func TestCredentialProxyCASRejectsMissingHydrationAndChangedAccountStatus(t *testing.T) {
	for _, operation := range credentialProxyMutationCases() {
		t.Run(operation.name+"/missing-hydration", func(t *testing.T) {
			repo, owner := committedCredentialProxyFixture(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			owner.Proxy = nil
			applied, err := operation.apply(context.Background(), repo, owner)
			require.Error(t, err)
			require.False(t, applied)
			requireCredentialProxyMutationHasNoEffects(t, repo, owner, cache)
		})
		t.Run(operation.name+"/disabled-account", func(t *testing.T) {
			repo, owner := committedCredentialProxyFixture(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET status='disabled' WHERE id=$1", owner.ID)
			require.NoError(t, err)
			applied, err := operation.apply(context.Background(), repo, owner)
			require.NoError(t, err)
			require.False(t, applied)
			current, err := repo.GetByID(context.Background(), owner.ID)
			require.NoError(t, err)
			require.Equal(t, "disabled", current.Status)
			require.Equal(t, owner.Credentials, current.Credentials)
			require.Equal(t, map[string]any{"keep": "current"}, current.Extra)
			require.Nil(t, current.TempUnschedulableUntil)
			require.Empty(t, cache.setAccounts)
			require.Empty(t, cache.deleteIDs)
		})
	}
}

type credentialProxyMarkedSQLExecutor struct {
	sqlExecutor
	label string
}

func (e *credentialProxyMarkedSQLExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return e.sqlExecutor.ExecContext(ctx, "/* "+e.label+" */ "+query, args...)
}

type credentialProxyMutationResult struct {
	applied bool
	err     error
}

func awaitCredentialProxyRowLock(t *testing.T, ctx context.Context, label string, completed <-chan credentialProxyMutationResult) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-completed:
			t.Fatalf("old mutation escaped the proxy lock before repair committed: applied=%v err=%v", result.applied, result.err)
		case <-ctx.Done():
			t.Fatalf("proxy lock was not observed: %v", ctx.Err())
		case <-ticker.C:
			var waiting int
			err := integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE $1", "%/* "+label+" */%").Scan(&waiting)
			require.NoError(t, err)
			if waiting > 0 {
				return
			}
		}
	}
}

func TestCredentialProxyCASSerializesAgainstInFlightRepair(t *testing.T) {
	for _, operation := range credentialProxyMutationCases() {
		t.Run(operation.name, func(t *testing.T) {
			repo, owner := committedCredentialProxyFixture(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			repair, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = repair.Rollback() }()
			_, err = repair.ExecContext(ctx, "UPDATE proxies SET host='repaired.invalid', password='repaired-password' WHERE id=$1", *owner.ProxyID)
			require.NoError(t, err)
			label := "credential_proxy_repair_" + strings.ReplaceAll(operation.name, "-", "_")
			repo.sql = &credentialProxyMarkedSQLExecutor{sqlExecutor: integrationDB, label: label}
			completed := make(chan credentialProxyMutationResult, 1)
			go func() {
				applied, err := operation.apply(ctx, repo, owner)
				completed <- credentialProxyMutationResult{applied: applied, err: err}
			}()

			awaitCredentialProxyRowLock(t, ctx, label, completed)
			require.NoError(t, repair.Commit())
			select {
			case result := <-completed:
				require.NoError(t, result.err)
				require.False(t, result.applied, "locking must recheck the committed replacement route")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			requireCredentialProxyMutationHasNoEffects(t, repo, owner, cache)
		})
	}
}

// Holding the account first exposes a different statement-snapshot race: the
// mutation may have observed the old proxy before waiting for the account row.
// A plan that locks the proxy first must serialize the repair; a plan that
// reaches it after the wait must reject the committed replacement.
func TestCredentialProxyCASSerializesRepairWhileAccountIsLocked(t *testing.T) {
	for _, operation := range credentialProxyMutationCases() {
		t.Run(operation.name, func(t *testing.T) {
			repo, owner := committedCredentialProxyFixture(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			accountLock, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = accountLock.Rollback() }()
			_, err = accountLock.ExecContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", owner.ID)
			require.NoError(t, err)
			label := "credential_account_first_" + strings.ReplaceAll(operation.name, "-", "_")
			repo.sql = &credentialProxyMarkedSQLExecutor{sqlExecutor: integrationDB, label: label}
			completed := make(chan credentialProxyMutationResult, 1)
			go func() {
				applied, err := operation.apply(ctx, repo, owner)
				completed <- credentialProxyMutationResult{applied: applied, err: err}
			}()
			awaitCredentialProxyRowLock(t, ctx, label, completed)

			repair, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = repair.Rollback() }()
			_, err = repair.ExecContext(ctx, "SET LOCAL lock_timeout = '200ms'")
			require.NoError(t, err)
			_, repairErr := repair.ExecContext(ctx, "UPDATE proxies SET host='repaired.invalid', password='repaired-password' WHERE id=$1", *owner.ProxyID)
			serialized := repairErr != nil
			if serialized {
				var lockErr *pq.Error
				require.ErrorAs(t, repairErr, &lockErr)
				require.Equal(t, pq.ErrorCode("55P03"), lockErr.Code, "only an actual proxy row lock can serialize this repair")
				require.NoError(t, repair.Rollback())
			} else {
				require.NoError(t, repair.Commit())
			}
			require.NoError(t, accountLock.Commit())
			select {
			case result := <-completed:
				require.NoError(t, result.err)
				require.Equal(t, serialized, result.applied, "the old observation may commit only before the proxy repair")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if serialized {
				_, err = integrationDB.ExecContext(ctx, "UPDATE proxies SET host='repaired.invalid', password='repaired-password' WHERE id=$1", *owner.ProxyID)
				require.NoError(t, err)
				require.Equal(t, []int64{owner.ID}, cache.deleteIDs)
			} else {
				requireCredentialProxyMutationHasNoEffects(t, repo, owner, cache)
			}
		})
	}
}

func TestGrokMissingProxyCASCannotQuarantineRestoredRow(t *testing.T) {
	repo, owner := committedCredentialProxyFixture(t)
	cache := &schedulerCacheRecorder{}
	repo.schedulerCache = cache
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := integrationDB.ExecContext(ctx, "UPDATE proxies SET deleted_at=NOW() WHERE id=$1", *owner.ProxyID)
	require.NoError(t, err)
	repair, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = repair.Rollback() }()
	_, err = repair.ExecContext(ctx, "UPDATE proxies SET deleted_at=NULL WHERE id=$1", *owner.ProxyID)
	require.NoError(t, err)
	label := "credential_missing_proxy_restore"
	repo.sql = &credentialProxyMarkedSQLExecutor{sqlExecutor: integrationDB, label: label}
	completed := make(chan credentialProxyMutationResult, 1)
	missingOwner := *owner
	missingOwner.Proxy = nil
	snapshot := service.GrokCredentialMutationSnapshot{Owner: &missingOwner, CredentialsJSON: `{"access_token":"sent"}`, ProxyID: owner.ProxyID, ProxyMissing: true}
	go func() {
		applied, err := repo.SetGrokCredentialErrorIfMatch(ctx, owner.ID, snapshot, string(service.GrokCredentialReasonProxyInvalid))
		completed <- credentialProxyMutationResult{applied: applied, err: err}
	}()
	awaitCredentialProxyRowLock(t, ctx, label, completed)
	require.NoError(t, repair.Commit())
	select {
	case result := <-completed:
		require.NoError(t, result.err)
		require.False(t, result.applied)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	requireCredentialProxyMutationHasNoEffects(t, repo, owner, cache)
}

func TestCredentialProxyCASAcceptsUnchangedHydratedRoute(t *testing.T) {
	for _, operation := range credentialProxyMutationCases() {
		t.Run(operation.name, func(t *testing.T) {
			repo, owner := committedCredentialProxyFixture(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache

			applied, err := operation.apply(context.Background(), repo, owner)

			require.NoError(t, err)
			require.True(t, applied)
			current, err := repo.GetByID(context.Background(), owner.ID)
			require.NoError(t, err)
			switch operation.name {
			case "generic-error", "grok-request-error", "grok-missing-refresh", "grok-refresh-error":
				require.Equal(t, service.StatusError, current.Status)
				require.False(t, current.Schedulable)
			case "generic-refresh", "grok-refresh-success":
				require.Equal(t, "rotated", current.GetCredential("access_token"))
			case "generic-cooldown", "grok-request-cooldown", "grok-refresh-cooldown", "quota-state":
				require.NotNil(t, current.TempUnschedulableUntil)
				require.True(t, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC).Equal(*current.TempUnschedulableUntil))
			}
			if operation.name == "quota-state" {
				require.Equal(t, true, current.Extra["old_observation"])
			}
			var count int
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", owner.ID).Scan(&count))
			require.Equal(t, 1, count)
			require.Equal(t, []int64{owner.ID}, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
		})
	}
}

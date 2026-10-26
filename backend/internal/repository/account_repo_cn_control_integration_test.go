//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func newCNControlSnapshotFixture(t *testing.T) (*accountRepository, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: fmt.Sprintf("cn-control-cas-%d", time.Now().UnixNano()), Platform: service.PlatformKimi, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"oauth_provider": "kimi", "access_token": "observed-token", "refresh_token": "observed-refresh", "_token_version": 7, "nested": map[string]any{"region": "cn"}},
		Extra:       map[string]any{"retained": "base", "kimi_weekly_used_percent": 91.0},
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", account.ID)
	})
	cache := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, cache)
	expected, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.True(t, expected.IsDomesticOAuth(), "the CAS fixture must represent a valid native Kimi OAuth credential owner")
	return repo, expected, cache
}

func cnControlSnapshotPort(t *testing.T, repo *accountRepository) service.CNControlSnapshotRepository {
	t.Helper()
	port, ok := any(repo).(service.CNControlSnapshotRepository)
	require.True(t, ok, "native control snapshots require an atomic credential-owner persistence port")
	return port
}

func cnControlOutboxCount(t *testing.T, accountID int64) int {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(),
		"SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", accountID).Scan(&count))
	return count
}

func TestCNControlSnapshotCASMergesCurrentExtraAndInvalidatesCommittedSnapshot(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	port := cnControlSnapshotPort(t, repo)
	// An unrelated management write after the probe began must survive the
	// provider result. Extra is merged against the current row, not the input.
	_, err := integrationDB.ExecContext(context.Background(),
		`UPDATE accounts SET extra=extra || '{"concurrent_management":"keep"}'::jsonb WHERE id=$1`, expected.ID)
	require.NoError(t, err)
	applied, err := port.UpdateCNControlExtraIfUnchanged(context.Background(), expected,
		map[string]any{"kimi_5h_used_percent": 25.0, "kimi_weekly_used_percent": nil, "kimi_weekly_reset_at": nil})
	require.NoError(t, err)
	require.True(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, "base", got.Extra["retained"])
	require.Equal(t, "keep", got.Extra["concurrent_management"])
	require.Equal(t, 25.0, got.Extra["kimi_5h_used_percent"])
	require.Contains(t, got.Extra, "kimi_weekly_used_percent")
	require.Nil(t, got.Extra["kimi_weekly_used_percent"])
	require.Equal(t, expected.Credentials, got.Credentials)
	require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
	require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
}

func TestCNControlSnapshotCASRejectsChangedOwnerWithoutSideEffects(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"login replaced", `credentials=credentials || '{"access_token":"new-login"}'::jsonb`},
		{"refresh generation changed", `credentials=credentials || '{"_token_version":8}'::jsonb`},
		{"unrelated credential changed", `credentials=credentials || '{"nested":{"region":"global"}}'::jsonb`},
		{"proxy changed", `proxy_id=$2`},
		{"platform changed", `platform='deepseek'`},
		{"account type changed", `type='apikey'`},
		{"account deleted", `deleted_at=NOW()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, expected, cache := newCNControlSnapshotFixture(t)
			port := cnControlSnapshotPort(t, repo)
			args := []any{expected.ID}
			if tc.name == "proxy changed" {
				proxy := mustCreateProxy(t, integrationEntClient, &service.Proxy{Name: fmt.Sprintf("cn-control-proxy-%d", expected.ID)})
				t.Cleanup(func() {
					_, _ = integrationDB.ExecContext(context.Background(), "UPDATE accounts SET proxy_id=NULL WHERE id=$1", expected.ID)
					_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM proxies WHERE id=$1", proxy.ID)
				})
				args = append(args, proxy.ID)
			}
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET "+tc.mutation+" WHERE id=$1", args...)
			require.NoError(t, err)
			var before string
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT extra::text FROM accounts WHERE id=$1", expected.ID).Scan(&before))
			applied, err := port.UpdateCNControlExtraIfUnchanged(context.Background(), expected, map[string]any{"kimi_5h_used_percent": 25.0})
			require.NoError(t, err)
			require.False(t, applied)
			var after string
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT extra::text FROM accounts WHERE id=$1", expected.ID).Scan(&after))
			require.JSONEq(t, before, after, "a previous login's response cannot update the replacement account")
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
			require.Empty(t, cache.deleteIDs)
		})
	}
}

func TestCNControlSnapshotCASConcurrentRelinkWinsBeforeDelayedProbeWrite(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	port := cnControlSnapshotPort(t, repo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	relink, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = relink.Rollback() }()
	_, err = relink.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"access_token":"new-login"}'::jsonb WHERE id=$1`, expected.ID)
	require.NoError(t, err)
	type outcome struct {
		applied bool
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		applied, err := port.UpdateCNControlExtraIfUnchanged(ctx, expected, map[string]any{"kimi_5h_used_percent": 25.0})
		done <- outcome{applied, err}
	}()
	// The stale observation has reached PostgreSQL while the replacement login
	// owns the row lock. It must recheck the owner after that lock is released.
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
			AND wait_event_type='Lock' AND query LIKE '%/* cn_control_snapshot_cas */%'
		)`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "delayed provider result must reach the blocked compare-and-set")
	require.NoError(t, relink.Commit())
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.False(t, result.applied, "PostgreSQL must reject the old owner after the concurrent relink commits")
	case <-ctx.Done():
		t.Fatal("delayed provider observation did not finish")
	}
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, "new-login", got.GetCredential("access_token"))
	require.NotContains(t, got.Extra, "kimi_5h_used_percent")
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

func TestCNControlSnapshotCASOutboxFailureRollsBackObservation(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	applied, err := cnControlSnapshotPort(t, repo).UpdateCNControlExtraIfUnchanged(context.Background(), expected,
		map[string]any{"kimi_5h_used_percent": 25.0})
	require.Error(t, err)
	require.False(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, expected.Extra, got.Extra)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

func TestCNControlSnapshotCASCallerTransactionRollbackDoesNotPublishSnapshot(t *testing.T) {
	repo, expected, cache := newCNControlSnapshotFixture(t)
	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	ctx := dbent.NewTxContext(context.Background(), tx)
	applied, err := cnControlSnapshotPort(t, repo).UpdateCNControlExtraIfUnchanged(ctx, expected,
		map[string]any{"kimi_5h_used_percent": 25.0})
	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, cache.setAccounts, "caller-owned uncommitted writes cannot reach the scheduler cache")
	require.NoError(t, tx.Rollback())
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, expected.Extra, got.Extra)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
}

// Force a replacement login at the exact cache boundary. The former
// read-then-SetAccount path would publish the old row after this commit.
type replacingCASCache struct {
	*schedulerCacheRecorder
	beforePublish func()
	once          sync.Once
}

func (c *replacingCASCache) SetAccount(ctx context.Context, account *service.Account) error {
	c.once.Do(c.beforePublish)
	return c.schedulerCacheRecorder.SetAccount(ctx, account)
}

func (c *replacingCASCache) DeleteAccount(ctx context.Context, id int64) error {
	c.once.Do(c.beforePublish)
	return c.schedulerCacheRecorder.DeleteAccount(ctx, id)
}

func TestControlCASCacheBoundaryCannotRepublishSupersededLogin(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(fmt.Sprintf("managed=%t", managed), func(t *testing.T) {
			repo, expected, recorder := newCNControlSnapshotFixture(t)
			recorder.accounts = map[int64]*service.Account{expected.ID: expected}
			replacementCommitted := false
			cache := &replacingCASCache{schedulerCacheRecorder: recorder, beforePublish: func() {
				_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-login"}'::jsonb WHERE id=$1`, expected.ID)
				require.NoError(t, err)
				replacementCommitted = true
			}}
			repo.schedulerCache = cache
			write := func(ctx context.Context, writer service.AccountRepository) error {
				conditional, ok := writer.(service.CNControlSnapshotRepository)
				require.True(t, ok)
				applied, err := conditional.UpdateCNControlExtraIfUnchanged(ctx, expected, map[string]any{"kimi_5h_used_percent": 25.0})
				require.NoError(t, err)
				require.True(t, applied)
				return err
			}
			if managed {
				require.NoError(t, repo.RunOpenAIOAuthPolicyTransaction(context.Background(), write))
			} else {
				require.NoError(t, write(context.Background(), repo))
			}
			require.True(t, replacementCommitted, "the race must cross the actual cache boundary")
			require.Empty(t, recorder.setAccounts, "the old committed row must never overwrite a newer login in shared cache")
			require.Equal(t, []int64{expected.ID}, recorder.deleteIDs)
			require.NotContains(t, recorder.accounts, expected.ID)
			scheduler := service.NewSchedulerSnapshotService(cache, nil, repo, nil, nil)
			current, err := scheduler.GetAccount(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, "replacement-login", current.GetCredential("access_token"), "a cache miss must reload the actual new owner")
			require.Equal(t, 25.0, current.Extra["kimi_5h_used_percent"])
			require.Equal(t, 1, cnControlOutboxCount(t, expected.ID), "the committed observation keeps durable invalidation for retry")
		})
	}
}

func requireEvictedSchedulerAccount(t *testing.T, repo *accountRepository, cache *schedulerCacheRecorder, id int64) *service.Account {
	t.Helper()
	require.Empty(t, cache.setAccounts, "a mutation must not publish a potentially superseded snapshot")
	require.Contains(t, cache.deleteIDs, id, "committed owner state must invalidate its shared snapshot")
	require.NotContains(t, cache.accounts, id)
	scheduler := service.NewSchedulerSnapshotService(cache, nil, repo, nil, nil)
	account, err := scheduler.GetAccount(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, account)
	return account
}

func TestCNControlWalletSnapshotCannotClearNewerReactiveBalanceMarker(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("newer-cooldown=%t", changed), func(t *testing.T) {
			repo, initial, cache := newCNControlSnapshotFixture(t)
			_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='deepseek',type='apikey',credentials='{"api_key":"wallet-owner","account_mode":"payg"}'::jsonb,extra=extra || '{"deepseek_balance_low":true}'::jsonb WHERE id=$1`, initial.ID)
			require.NoError(t, err)
			expected, err := repo.GetByID(context.Background(), initial.ID)
			require.NoError(t, err)
			if changed {
				_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='newer upstream balance rejection' WHERE id=$1`, initial.ID)
				require.NoError(t, err)
			}
			applied, err := repo.UpdateCNControlExtraIfUnchanged(context.Background(), expected, map[string]any{"deepseek_balance": 50.0, "deepseek_balance_low": false})
			require.NoError(t, err)
			require.Equal(t, !changed, applied)
			stored, err := repo.GetByID(context.Background(), initial.ID)
			require.NoError(t, err)
			require.Equal(t, expected.Credentials, stored.Credentials)
			require.Equal(t, changed, stored.Extra["deepseek_balance_low"])
			require.Empty(t, cache.setAccounts)
			if changed {
				require.Equal(t, "newer upstream balance rejection", stored.TempUnschedulableReason)
				require.NotContains(t, stored.Extra, "deepseek_balance")
				require.Zero(t, cnControlOutboxCount(t, initial.ID))
				require.Empty(t, cache.deleteIDs)
			} else {
				require.Equal(t, 50.0, stored.Extra["deepseek_balance"])
				require.Equal(t, 1, cnControlOutboxCount(t, initial.ID))
				require.Equal(t, []int64{initial.ID}, cache.deleteIDs)
			}
		})
	}
}

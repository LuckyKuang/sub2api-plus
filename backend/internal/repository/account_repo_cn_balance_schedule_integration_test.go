//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// The wallet result is not authority to pause a replacement login or erase a
// newer cooldown from another subsystem. PostgreSQL must recheck that boundary
// after waiting for concurrent row writers, and invalidate only committed rows.
func newCNBalanceScheduleFixture(t *testing.T, paused bool) (*accountRepository, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	repo, account, cache := newCNControlSnapshotFixture(t)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='deepseek',credentials='{"oauth_provider":"deepseek","access_token":"wallet-owner"}'::jsonb,status='active',schedulable=TRUE WHERE id=$1`, account.ID)
	require.NoError(t, err)
	if paused {
		_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='cn_balance_low: previous wallet' WHERE id=$1`, account.ID)
		require.NoError(t, err)
	}
	expected, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.True(t, expected.IsDomesticOAuth())
	return repo, expected, cache
}

func TestCNBalanceScheduleCASKeepsOwnerAndPublishesCurrentSnapshot(t *testing.T) {
	for _, clear := range []bool{false, true} {
		t.Run(map[bool]string{false: "low wallet pauses", true: "healthy wallet recovers"}[clear], func(t *testing.T) {
			repo, expected, cache := newCNBalanceScheduleFixture(t, clear)
			until := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
			next := &until
			reason := "cn_balance_low: observed wallet"
			if clear {
				next = nil
				reason = ""
			}
			applied, err := repo.UpdateCNBalanceScheduleIfUnchanged(context.Background(), expected, next, reason)
			require.NoError(t, err)
			require.True(t, applied)
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, reason, got.TempUnschedulableReason)
			require.Equal(t, next, got.TempUnschedulableUntil)
			require.Equal(t, expected.Credentials, got.Credentials)
			require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
			require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
		})
	}
}

func TestCNBalanceScheduleCASRejectsConcurrentOwnerOrSchedulerChange(t *testing.T) {
	for _, clear := range []bool{false, true} {
		for _, tc := range []struct{ name, mutation string }{
			{"reauthorization", `credentials=credentials || '{"access_token":"replacement-owner"}'::jsonb`},
			{"other cooldown", `temp_unschedulable_until=NOW()+INTERVAL '2 hours',temp_unschedulable_reason='transport failure'`},
			{"manual pause", `schedulable=FALSE`},
		} {
			t.Run(map[bool]string{false: "pause/", true: "recover/"}[clear]+tc.name, func(t *testing.T) {
				repo, expected, cache := newCNBalanceScheduleFixture(t, clear)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				tx, err := integrationDB.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				_, err = tx.ExecContext(ctx, "UPDATE accounts SET "+tc.mutation+" WHERE id=$1", expected.ID)
				require.NoError(t, err)
				until := time.Now().Add(time.Hour)
				next := &until
				reason := "cn_balance_low: late result"
				if clear {
					next = nil
					reason = ""
				}
				type outcome struct {
					applied bool
					err     error
				}
				done := make(chan outcome, 1)
				go func() {
					applied, err := repo.UpdateCNBalanceScheduleIfUnchanged(ctx, expected, next, reason)
					done <- outcome{applied, err}
				}()
				require.Eventually(t, func() bool {
					var waiting bool
					err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%/* cn_balance_schedule_cas */%')`).Scan(&waiting)
					return err == nil && waiting
				}, 3*time.Second, 10*time.Millisecond)
				require.NoError(t, tx.Commit())
				select {
				case result := <-done:
					require.NoError(t, result.err)
					require.False(t, result.applied)
				case <-ctx.Done():
					t.Fatal("conditional wallet scheduling did not finish")
				}
				got, err := repo.GetByID(context.Background(), expected.ID)
				require.NoError(t, err)
				if tc.name == "reauthorization" {
					require.Equal(t, "replacement-owner", got.GetCredential("access_token"))
					require.Equal(t, expected.TempUnschedulableUntil, got.TempUnschedulableUntil)
					require.Equal(t, expected.TempUnschedulableReason, got.TempUnschedulableReason)
				}
				if tc.name == "other cooldown" {
					require.Equal(t, "transport failure", got.TempUnschedulableReason)
				}
				if tc.name == "manual pause" {
					require.False(t, got.Schedulable)
					require.Equal(t, expected.TempUnschedulableUntil, got.TempUnschedulableUntil)
				}
				require.Zero(t, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts)
			})
		}
	}
}

func TestCNBalanceScheduleCASOutboxFailureRollsBackCooldown(t *testing.T) {
	repo, expected, cache := newCNBalanceScheduleFixture(t, false)
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	until := time.Now().Add(time.Hour)
	applied, err := repo.UpdateCNBalanceScheduleIfUnchanged(context.Background(), expected, &until, "cn_balance_low: late result")
	require.Error(t, err)
	require.False(t, applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Nil(t, got.TempUnschedulableUntil)
	require.Empty(t, got.TempUnschedulableReason)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

func TestCNBalanceScheduleCASCallerRollbackDoesNotPublishCooldown(t *testing.T) {
	repo, expected, cache := newCNBalanceScheduleFixture(t, false)
	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	until := time.Now().Add(time.Hour)
	applied, err := repo.UpdateCNBalanceScheduleIfUnchanged(dbent.NewTxContext(context.Background(), tx), expected, &until, "cn_balance_low: late result")
	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, cache.setAccounts)
	require.NoError(t, tx.Rollback())
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Nil(t, got.TempUnschedulableUntil)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
}

//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func (s *AccountRepoSuite) TestCredentialStateMutationFencesSnapshotsAndCooldownTogether() {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformKimi, service.PlatformGrok, service.PlatformMiniMax, service.PlatformDeepseek, service.PlatformStepFun, service.PlatformZhipu} {
		account := mustCreateAccount(s.T(), s.client, &service.Account{Name: "state-" + platform, Platform: platform, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "sent", "_token_version": 1}})
		observed, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().NoError(s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"access_token": "relinked", "_token_version": 2}))
		_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
		s.Require().NoError(err)
		cache := &schedulerCacheRecorder{}
		s.repo.schedulerCache = cache
		until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
		mutation := service.CredentialStateMutation{Extra: map[string]any{"review_snapshot": "paid"}, RateLimitUntil: &until, ExtendRateLimitOnly: true, TempUnschedulableUntil: &until, TempUnschedulableReason: "quota"}
		applied, err := s.repo.ApplyCredentialStateMutation(s.ctx, observed, mutation)
		s.Require().NoError(err)
		s.Require().False(applied)
		s.Require().Empty(cache.deleteIDs)
		s.Require().Empty(cache.setAccounts)
		fresh, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Nil(fresh.RateLimitResetAt)
		s.Require().Nil(fresh.TempUnschedulableUntil)
		s.Require().NotContains(fresh.Extra, "review_snapshot")
		var count int
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Zero(count)
		applied, err = s.repo.ApplyCredentialStateMutation(s.ctx, fresh, mutation)
		s.Require().NoError(err)
		s.Require().True(applied)
		fresh, err = s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Equal("paid", fresh.Extra["review_snapshot"])
		s.Require().True(until.Equal(*fresh.RateLimitResetAt))
		s.Require().True(until.Equal(*fresh.TempUnschedulableUntil))
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Equal(1, count)
		staleReset := until.Add(-time.Minute)
		clear := service.CredentialStateMutation{ClearRateLimit: true, ObservedLimitedAt: fresh.RateLimitedAt, ObservedResetAt: &staleReset}
		applied, err = s.repo.ApplyCredentialStateMutation(s.ctx, fresh, clear)
		s.Require().NoError(err)
		s.Require().False(applied)
		clear.ObservedResetAt = fresh.RateLimitResetAt
		applied, err = s.repo.ApplyCredentialStateMutation(s.ctx, fresh, clear)
		s.Require().NoError(err)
		s.Require().True(applied)
		cleared, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Nil(cleared.RateLimitResetAt)
		// Refresh cleanup is a CAS on both grant and exact observed cooldown.
		// A same-deadline replacement with another reason is also a newer state.
		clearTemp := service.CredentialStateMutation{ClearTempUnschedulable: true, ObservedTempUntil: &until, ObservedTempReason: "quota"}
		applied, err = s.repo.ApplyCredentialStateMutation(s.ctx, observed, clearTemp)
		s.Require().NoError(err)
		s.Require().False(applied, "old grant cannot clear a current cooldown")
		clearTemp.ObservedTempUntil = &staleReset
		applied, err = s.repo.ApplyCredentialStateMutation(s.ctx, cleared, clearTemp)
		s.Require().NoError(err)
		s.Require().False(applied, "wrong observed deadline")
		clearTemp.ObservedTempUntil, clearTemp.ObservedTempReason = &until, "another-reason"
		applied, err = s.repo.ApplyCredentialStateMutation(s.ctx, cleared, clearTemp)
		s.Require().NoError(err)
		s.Require().False(applied, "same deadline with another reason cannot clear")
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Equal(2, count, "rejected cleanup must not publish scheduler events")
		clearTemp.ObservedTempReason = "quota"
		applied, err = s.repo.ApplyCredentialStateMutation(s.ctx, cleared, clearTemp)
		s.Require().NoError(err)
		s.Require().True(applied)
		final, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Nil(final.TempUnschedulableUntil)
		s.Require().Empty(final.TempUnschedulableReason)
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Equal(3, count)
		s.Require().Equal([]int64{account.ID, account.ID, account.ID}, cache.deleteIDs, "successful CAS evicts for authoritative reload")
		s.Require().Empty(cache.setAccounts, "cleanup must never publish a possibly superseded credential snapshot")
	}
}

func TestCredentialStateCleanupRollsBackWhenOutboxInsertFails(t *testing.T) {
	client := testEntClient(t)
	ctx := context.Background()
	account := mustCreateAccount(t, client, &service.Account{Name: "credential-cleanup-outbox-rollback", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive,
		Credentials: map[string]any{"access_token": "refreshed", "refresh_token": "rotated"}, Extra: map[string]any{"review_snapshot": "keep"}})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_ = client.Account.DeleteOneID(account.ID).Exec(context.Background())
	})
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET temp_unschedulable_until=$1, temp_unschedulable_reason=$2 WHERE id=$3", until, "observed-401", account.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
	require.NoError(t, err)
	cache := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(client, &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}, cache)
	owner, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	applied, err := repo.ApplyCredentialStateMutation(ctx, owner, service.CredentialStateMutation{
		ClearTempUnschedulable: true, ObservedTempUntil: owner.TempUnschedulableUntil, ObservedTempReason: owner.TempUnschedulableReason,
		Extra: map[string]any{"review_snapshot": "must-roll-back"},
	})
	require.Error(t, err)
	require.False(t, applied)
	fresh, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, until.Equal(*fresh.TempUnschedulableUntil))
	require.Equal(t, "observed-401", fresh.TempUnschedulableReason)
	require.Equal(t, "keep", fresh.Extra["review_snapshot"])
	require.Equal(t, owner.Credentials, fresh.Credentials)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&count))
	require.Zero(t, count)
	require.Empty(t, cache.deleteIDs, "failed atomic cleanup must not notify the scheduler")
	require.Empty(t, cache.setAccounts)
}

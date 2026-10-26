//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialStateObservedTempGuardPreservesNewerCooldown(t *testing.T) {
	for _, scenario := range []string{"previously-absent", "newer-deadline", "same-deadline-new-reason"} {
		t.Run(scenario, func(t *testing.T) {
			repo, owner := committedCredentialProxyFixture(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			oldUntil := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
			if scenario != "previously-absent" {
				_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET temp_unschedulable_until=$1,temp_unschedulable_reason='observed' WHERE id=$2", oldUntil, owner.ID)
				require.NoError(t, err)
				owner, err = repo.GetByID(ctx, owner.ID)
				require.NoError(t, err)
			}
			newUntil := oldUntil.Add(time.Hour)
			if scenario == "same-deadline-new-reason" {
				newUntil = oldUntil
			}
			newer, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = newer.Rollback() }()
			_, err = newer.ExecContext(ctx, "UPDATE accounts SET temp_unschedulable_until=$1,temp_unschedulable_reason='newer-failure' WHERE id=$2", newUntil, owner.ID)
			require.NoError(t, err)
			label := "credential_observed_temp_" + scenario
			repo.sql = &credentialProxyMarkedSQLExecutor{sqlExecutor: integrationDB, label: label}
			completed := make(chan credentialProxyMutationResult, 1)
			lateUntil := newUntil.Add(time.Hour)
			go func() {
				applied, err := repo.ApplyCredentialStateMutation(ctx, owner, service.CredentialStateMutation{
					RequireTempUnschedulableUnchanged: true, ObservedTempUntil: owner.TempUnschedulableUntil, ObservedTempReason: owner.TempUnschedulableReason,
					Extra: map[string]any{"late_balance": true}, TempUnschedulableUntil: &lateUntil, TempUnschedulableReason: "old-probe",
				})
				completed <- credentialProxyMutationResult{applied: applied, err: err}
			}()
			awaitCredentialProxyRowLock(t, ctx, label, completed)
			require.NoError(t, newer.Commit())
			select {
			case result := <-completed:
				require.NoError(t, result.err)
				require.False(t, result.applied, "a probe that waited on the account row must reject a newer cooldown")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			current, err := repo.GetByID(ctx, owner.ID)
			require.NoError(t, err)
			require.True(t, newUntil.Equal(*current.TempUnschedulableUntil))
			require.Equal(t, "newer-failure", current.TempUnschedulableReason)
			require.Equal(t, map[string]any{"keep": "current"}, current.Extra)
			require.Equal(t, owner.Credentials, current.Credentials)
			var events int
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", owner.ID).Scan(&events))
			require.Zero(t, events)
			require.Empty(t, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestCredentialStateObservedTempGuardAcceptsUnchangedCooldown(t *testing.T) {
	for _, observed := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[observed], func(t *testing.T) {
			repo, owner := committedCredentialProxyFixture(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			ctx := context.Background()
			if observed {
				_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='observed' WHERE id=$1", owner.ID)
				require.NoError(t, err)
				owner, err = repo.GetByID(ctx, owner.ID)
				require.NoError(t, err)
			}
			until := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Microsecond)
			applied, err := repo.ApplyCredentialStateMutation(ctx, owner, service.CredentialStateMutation{
				RequireTempUnschedulableUnchanged: true, ObservedTempUntil: owner.TempUnschedulableUntil, ObservedTempReason: owner.TempUnschedulableReason,
				TempUnschedulableUntil: &until, TempUnschedulableReason: "current-probe",
			})
			require.NoError(t, err)
			require.True(t, applied)
			current, err := repo.GetByID(ctx, owner.ID)
			require.NoError(t, err)
			require.True(t, until.Equal(*current.TempUnschedulableUntil))
			require.Equal(t, "current-probe", current.TempUnschedulableReason)
			require.Equal(t, []int64{owner.ID}, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
		})
	}
}

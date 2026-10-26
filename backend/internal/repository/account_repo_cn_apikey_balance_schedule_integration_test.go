//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// API-key balance controls carry the same revocable owner authority as native
// wallets. Their pause/recovery must atomically retain the key and scheduler
// generation, including commits made by another instance before this write.
func TestCNAPIKeyBalanceScheduleCASRetainsOwnerAndScheduler(t *testing.T) {
	for _, platform := range []string{service.PlatformDeepseek, service.PlatformKimi} {
		for _, healthy := range []bool{false, true} {
			for _, mutation := range []string{"same-owner", "api-key", "other-cooldown", "manual-pause"} {
				t.Run(platform+map[bool]string{false: "/low/", true: "/healthy/"}[healthy]+mutation, func(t *testing.T) {
					repo, initial, cache := newCNControlSnapshotFixture(t)
					credentials, err := json.Marshal(map[string]any{"api_key": "captured-wallet-key", "account_mode": "payg"})
					require.NoError(t, err)
					_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform=$2,type='apikey',credentials=$3::jsonb,status='active',schedulable=TRUE WHERE id=$1`, initial.ID, platform, string(credentials))
					require.NoError(t, err)
					if healthy {
						_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='cn_balance_low: previous wallet' WHERE id=$1`, initial.ID)
						require.NoError(t, err)
					}
					captured, err := repo.GetByID(context.Background(), initial.ID)
					require.NoError(t, err)
					switch mutation {
					case "api-key":
						_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || '{"api_key":"replacement-wallet-key"}'::jsonb WHERE id=$1`, initial.ID)
					case "other-cooldown":
						_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '2 hours',temp_unschedulable_reason='transport-failure' WHERE id=$1`, initial.ID)
					case "manual-pause":
						_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET schedulable=FALSE WHERE id=$1`, initial.ID)
					}
					require.NoError(t, err)
					before, err := repo.GetByID(context.Background(), initial.ID)
					require.NoError(t, err)
					until := time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)
					next, reason := &until, "cn_balance_low: current observation"
					if healthy {
						next, reason = nil, ""
					}
					applied, err := repo.UpdateCNBalanceScheduleIfUnchanged(context.Background(), captured, next, reason)
					require.NoError(t, err)
					after, err := repo.GetByID(context.Background(), initial.ID)
					require.NoError(t, err)
					if mutation == "same-owner" {
						require.True(t, applied)
						require.Equal(t, next, after.TempUnschedulableUntil)
						require.Equal(t, reason, after.TempUnschedulableReason)
						require.Equal(t, 1, cnControlOutboxCount(t, initial.ID))
						require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
						require.Equal(t, []int64{initial.ID}, cache.deleteIDs)
					} else {
						require.False(t, applied)
						require.Equal(t, before.Credentials, after.Credentials)
						require.Equal(t, before.Schedulable, after.Schedulable)
						require.Equal(t, before.TempUnschedulableUntil, after.TempUnschedulableUntil)
						require.Equal(t, before.TempUnschedulableReason, after.TempUnschedulableReason)
						require.Zero(t, cnControlOutboxCount(t, initial.ID))
						require.Empty(t, cache.setAccounts)
					}
				})
			}
		}
	}
}

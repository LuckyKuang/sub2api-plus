//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func TestCredentialModelRateLimitRecoveryPreservesNewerScopeAndOwner(t *testing.T) {
	const scope = "claude-fable-5"
	ctx := context.Background()
	client := testEntClient(t)
	for _, scenario := range []string{"unchanged", "credits required", "admin threshold", "new deadline", "new observation", "new credential", "outbox failure"} {
		t.Run(scenario, func(t *testing.T) {
			observed := map[string]any{
				"reason":              "anthropic_7d_oi_window_exhausted",
				"rate_limited_at":     time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
				"rate_limit_reset_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			}
			other := map[string]any{"reason": "other-window", "rate_limit_reset_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)}
			account := mustCreateAccount(t, client, &service.Account{
				Name: "model-recovery-" + uuid.NewString(), Platform: service.PlatformAnthropic,
				Type: service.AccountTypeOAuth, Status: service.StatusActive,
				Credentials: map[string]any{"access_token": "observed-grant"},
				Extra:       map[string]any{"keep": "untouched", "model_rate_limits": map[string]any{scope: observed, "claude-opus-5": other}},
			})
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
				require.NoError(t, err)
				require.NoError(t, client.Account.DeleteOneID(account.ID).Exec(context.Background()))
			})
			repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
			owner, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			observed = owner.Extra["model_rate_limits"].(map[string]any)[scope].(map[string]any)
			replacement := make(map[string]any, len(observed))
			for key, value := range observed {
				replacement[key] = value
			}
			switch scenario {
			case "credits required":
				replacement["reason"] = "anthropic_fable_credits_required"
			case "admin threshold":
				replacement["reason"] = "admin_threshold"
			case "new deadline":
				replacement["rate_limit_reset_at"] = time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
			case "new observation":
				replacement["rate_limited_at"] = time.Now().UTC().Format(time.RFC3339)
			case "new credential":
				_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials = '{"access_token":"replacement-grant"}'::jsonb WHERE id = $1`, account.ID)
				require.NoError(t, err)
			}
			payload, err := json.Marshal(replacement)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET extra = jsonb_set(extra, ARRAY['model_rate_limits', $1::text], $2::jsonb) WHERE id = $3`, scope, string(payload), account.ID)
			require.NoError(t, err)
			before, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			if scenario == "outbox failure" {
				repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
			}
			applied, err := repo.ApplyCredentialStateMutation(ctx, owner, service.CredentialStateMutation{
				ClearModelRateLimitScope: scope, ObservedModelRateLimit: observed,
				Extra: map[string]any{"recovery_sample": 0},
			})
			if scenario == "outbox failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			fresh, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			var events int
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1", account.ID).Scan(&events))
			if scenario == "unchanged" {
				require.True(t, applied)
				limits := fresh.Extra["model_rate_limits"].(map[string]any)
				require.NotContains(t, limits, scope)
				require.Equal(t, other, limits["claude-opus-5"])
				require.Equal(t, "untouched", fresh.Extra["keep"])
				require.Equal(t, float64(0), fresh.Extra["recovery_sample"])
				require.Equal(t, 1, events)
				require.Equal(t, []int64{account.ID}, cache.deleteIDs)
			} else {
				require.False(t, applied)
				require.Equal(t, before.Extra, fresh.Extra, "late recovery cannot replace newer limit state")
				require.Equal(t, before.Credentials, fresh.Credentials)
				require.Zero(t, events)
				require.Empty(t, cache.deleteIDs)
			}
			require.Empty(t, cache.setAccounts, "recovery only evicts; it never republishes a stale owner")
		})
	}
}

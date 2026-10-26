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

func TestOAuthCredentialOwnerCASMergesCurrentMetadataAndKeepsRecoveryBoundary(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(map[bool]string{false: "refresh", true: "reauthorize"}[recover], func(t *testing.T) {
			repo, _, expected, _, cache, _ := newAdminOAuthFixture(t, service.PlatformGrok, "success")
			_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET extra=extra || '{"concurrent_preference":"keep"}'::jsonb WHERE id=$1`, expected.ID)
			require.NoError(t, err)
			credentials := map[string]any{"access_token": "issued-owner", "refresh_token": "issued-rotation", "custom_config": "retained"}
			applied, err := repo.UpdateOAuthCredentialsIfUnchanged(context.Background(), expected, service.AccountTypeOAuth, credentials, map[string]any{"new_preference": "saved"}, recover)
			require.NoError(t, err)
			require.True(t, applied)
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, credentials, got.Credentials)
			require.Equal(t, "keep", got.Extra["concurrent_preference"])
			require.Equal(t, "base", got.Extra["retained"])
			require.Equal(t, "saved", got.Extra["new_preference"])
			require.Equal(t, expected.Schedulable, got.Schedulable)
			require.Equal(t, expected.ProxyID, got.ProxyID)
			if recover {
				require.Equal(t, service.StatusActive, got.Status)
				require.Empty(t, got.ErrorMessage)
				require.Nil(t, got.TempUnschedulableUntil)
				require.Nil(t, got.RateLimitResetAt)
				require.NotContains(t, got.Extra, "model_rate_limits")
			} else {
				require.Equal(t, expected.Status, got.Status)
				require.Equal(t, expected.ErrorMessage, got.ErrorMessage)
				require.Equal(t, expected.TempUnschedulableUntil, got.TempUnschedulableUntil)
				require.Equal(t, expected.RateLimitResetAt, got.RateLimitResetAt)
				require.Equal(t, expected.Extra["model_rate_limits"], got.Extra["model_rate_limits"])
			}
			require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
			require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
			require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
		})
	}
}

func TestOAuthCredentialOwnerCASPreservesNewerModelAndReauthorizationState(t *testing.T) {
	for _, mutation := range []string{
		`extra=extra || '{"model_rate_limits":{"other":{"reason":"newer model failure"}}}'::jsonb`,
		`extra=extra || '{"antigravity_quota_scopes":{"newer":true}}'::jsonb`,
		`extra=extra || '{"grok_needs_reauth_reason":"newer billing failure"}'::jsonb`,
		`extra=extra || '{"grok_needs_reauth_at":"2030-01-01T00:00:00Z"}'::jsonb`,
	} {
		t.Run(mutation, func(t *testing.T) {
			repo, _, expected, _, cache, _ := newAdminOAuthFixture(t, service.PlatformGrok, "success")
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET "+mutation+" WHERE id=$1", expected.ID)
			require.NoError(t, err)
			before, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			applied, err := repo.UpdateOAuthCredentialsIfUnchanged(context.Background(), expected, service.AccountTypeOAuth, map[string]any{"access_token": "late-authorization"}, nil, true)
			require.NoError(t, err)
			require.False(t, applied)
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			assertAdminOAuthStoredOwner(t, before, got)
			require.Empty(t, cache.setAccounts)
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
		})
	}
}

func TestOAuthCredentialOwnerCASWaitsForConcurrentAccountOrProxyWriter(t *testing.T) {
	for _, target := range []string{"account", "proxy"} {
		t.Run(target, func(t *testing.T) {
			repo, _, expected, _, cache, _ := newAdminOAuthFixture(t, service.PlatformGrok, "success")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			if target == "account" {
				_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-owner"}'::jsonb WHERE id=$1`, expected.ID)
			} else {
				_, err = tx.ExecContext(ctx, "UPDATE proxies SET password='replacement-proxy-secret' WHERE id=$1", *expected.ProxyID)
			}
			require.NoError(t, err)
			type result struct {
				applied bool
				err     error
			}
			done := make(chan result, 1)
			go func() {
				applied, err := repo.UpdateOAuthCredentialsIfUnchanged(ctx, expected, service.AccountTypeOAuth, map[string]any{"access_token": "late-authorization"}, nil, true)
				done <- result{applied, err}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%/* oauth_credential_owner_cas */%')`).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			require.NoError(t, tx.Commit())
			select {
			case outcome := <-done:
				require.NoError(t, outcome.err)
				require.False(t, outcome.applied)
			case <-ctx.Done():
				t.Fatal("conditional OAuth credential write did not finish")
			}
			require.Empty(t, cache.setAccounts)
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
		})
	}
}

func TestOAuthCredentialOwnerCASOutboxFaultAndCallerRollbackRetainPriorOwner(t *testing.T) {
	for _, scenario := range []string{"outbox failure", "caller rollback"} {
		t.Run(scenario, func(t *testing.T) {
			repo, _, expected, _, cache, _ := newAdminOAuthFixture(t, service.PlatformGrok, "success")
			ctx := context.Background()
			var tx *dbent.Tx
			var err error
			if scenario == "outbox failure" {
				repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
			} else {
				tx, err = integrationEntClient.Tx(ctx)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				ctx = dbent.NewTxContext(ctx, tx)
			}
			applied, err := repo.UpdateOAuthCredentialsIfUnchanged(ctx, expected, service.AccountTypeOAuth, map[string]any{"access_token": "late-authorization"}, map[string]any{"late_metadata": true}, true)
			if tx == nil {
				require.Error(t, err)
				require.False(t, applied)
			} else {
				require.NoError(t, err)
				require.True(t, applied)
				require.Empty(t, cache.setAccounts)
				require.NoError(t, tx.Rollback())
			}
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			assertAdminOAuthStoredOwner(t, expected, got)
			require.Empty(t, cache.setAccounts)
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
		})
	}
}

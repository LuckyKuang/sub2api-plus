//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func newGrokRequestOutcomeFixture(t *testing.T, accountType string) (*accountRepository, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	credentials := map[string]any{"api_key": "observed-grok-api-key"}
	if accountType == service.AccountTypeOAuth {
		credentials = map[string]any{"access_token": "observed-grok-token", "refresh_token": "observed-grok-refresh", "team_id": "observed-team"}
	}
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: fmt.Sprintf("grok-request-outcome-%d", time.Now().UnixNano()), Platform: service.PlatformGrok, Type: accountType,
		Credentials: credentials, Extra: map[string]any{"retained": "base"}, Status: service.StatusActive, Schedulable: true,
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", account.ID)
	})
	cache := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, cache)
	expected, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	return repo, expected, cache
}

func grokRequestOutcomePort(t *testing.T, repo *accountRepository) service.GrokRequestOutcomeRepository {
	t.Helper()
	port, ok := any(repo).(service.GrokRequestOutcomeRepository)
	require.True(t, ok, "request errors need an atomic credential-and-route owner persistence port")
	return port
}

func grokRequestOutcomeMutation() service.GrokRequestOutcome {
	until := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	reset := until.Add(time.Hour)
	return service.GrokRequestOutcome{
		Extra: map[string]any{"grok_last_error_status": 401.0}, TempUnschedulableUntil: &until,
		TempUnschedulableReason: "grok invalid credentials", RateLimitResetAt: &reset,
	}
}

func TestGrokRequestOutcomeCASPublishesAtomicOwnerState(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, accountType)
			_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET extra=extra || '{"concurrent_management":"keep"}'::jsonb WHERE id=$1`, expected.ID)
			require.NoError(t, err)
			mutation := grokRequestOutcomeMutation()
			applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(context.Background(), expected, mutation)
			require.NoError(t, err)
			require.True(t, applied.Applied)
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, expected.Credentials, got.Credentials)
			require.Equal(t, "base", got.Extra["retained"])
			require.Equal(t, "keep", got.Extra["concurrent_management"])
			require.Equal(t, 401.0, got.Extra["grok_last_error_status"])
			require.Equal(t, mutation.TempUnschedulableUntil, got.TempUnschedulableUntil)
			require.Equal(t, mutation.TempUnschedulableReason, got.TempUnschedulableReason)
			require.Equal(t, mutation.RateLimitResetAt, got.RateLimitResetAt)
			require.NotNil(t, got.RateLimitedAt)
			require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
			require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
		})
	}
}

func TestGrokRequestOutcomeCASRejectsOwnerOrSchedulerChanges(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"credential replacement", `credentials=credentials || '{"access_token":"replacement-owner"}'::jsonb`},
		{"platform replacement", `platform='openai'`},
		{"account type replacement", `type='apikey'`},
		{"manual pause", `schedulable=FALSE`},
		{"disabled account", `status='inactive'`},
		{"other cooldown", `temp_unschedulable_until=NOW()+INTERVAL '2 hours',temp_unschedulable_reason='newer failure'`},
		{"newer rate limit", `rate_limited_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '3 hours'`},
		{"newer overload", `overload_until=NOW()+INTERVAL '2 hours'`},
		{"account expiry", `expires_at=NOW()`},
		{"expiry policy", `auto_pause_on_expired=NOT auto_pause_on_expired`},
		{"deleted account", `deleted_at=NOW()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET "+tc.mutation+" WHERE id=$1", expected.ID)
			require.NoError(t, err)
			var replacementExtra string
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT extra::text FROM accounts WHERE id=$1", expected.ID).Scan(&replacementExtra))
			applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(context.Background(), expected, grokRequestOutcomeMutation())
			require.NoError(t, err)
			require.False(t, applied.Applied, "late errors cannot change a replacement owner or newer scheduling decision")
			var extra string
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT extra::text FROM accounts WHERE id=$1", expected.ID).Scan(&extra))
			require.JSONEq(t, replacementExtra, extra, "the stale result must leave the replacement row's exact stored Extra untouched")
			var storedExtra map[string]any
			require.NoError(t, json.Unmarshal([]byte(extra), &storedExtra))
			require.Equal(t, "base", storedExtra["retained"])
			require.NotContains(t, storedExtra, "grok_last_error_status")
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestGrokRequestOutcomeCASRejectsSameProxyRouteEdits(t *testing.T) {
	for _, mutation := range []string{"host='replacement.example'", "port=9090", "username='replacement-user'", "password='replacement-password'", "protocol='socks5'", "status='inactive'", "expires_at=NOW()", "deleted_at=NOW()", "binding removed"} {
		t.Run(mutation, func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			expected = bindCNProxyOwner(t, repo, expected)
			query, id := "UPDATE proxies SET "+mutation+" WHERE id=$1", *expected.ProxyID
			if mutation == "binding removed" {
				query, id = "UPDATE accounts SET proxy_id=NULL WHERE id=$1", expected.ID
			}
			_, err := integrationDB.ExecContext(context.Background(), query, id)
			require.NoError(t, err)
			applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(context.Background(), expected, grokRequestOutcomeMutation())
			require.NoError(t, err)
			require.False(t, applied.Applied, "the same proxy ID does not authorize the edited route")
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, expected.Extra, got.Extra)
			require.Nil(t, got.TempUnschedulableUntil)
			require.Nil(t, got.RateLimitResetAt)
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestGrokRequestOutcomeCASWaitsForConcurrentOwnerOrProxyEdit(t *testing.T) {
	for _, target := range []string{"account", "proxy"} {
		t.Run(target, func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			expected = bindCNProxyOwner(t, repo, expected)
			port := grokRequestOutcomePort(t, repo)
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
			mutation := grokRequestOutcomeMutation()
			go func() {
				applied, err := port.ApplyGrokRequestOutcomeIfUnchanged(ctx, expected, mutation)
				done <- result{applied.Applied, err}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%/* grok_request_outcome_cas */%')`).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond, "persistence must wait for the uncommitted owner writer")
			require.NoError(t, tx.Commit())
			select {
			case result := <-done:
				require.NoError(t, result.err)
				require.False(t, result.applied)
			case <-ctx.Done():
				t.Fatal("conditional request outcome did not finish")
			}
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestGrokRequestOutcomeCASOutboxFailureRollsBackAllState(t *testing.T) {
	repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
	repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
	applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(context.Background(), expected, grokRequestOutcomeMutation())
	require.Error(t, err)
	require.False(t, applied.Applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, expected.Extra, got.Extra)
	require.Nil(t, got.TempUnschedulableUntil)
	require.Nil(t, got.RateLimitedAt)
	require.Nil(t, got.RateLimitResetAt)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
	require.Empty(t, cache.setAccounts)
}

func TestGrokRequestOutcomeCASCallerRollbackDoesNotPublishState(t *testing.T) {
	repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(dbent.NewTxContext(context.Background(), tx), expected, grokRequestOutcomeMutation())
	require.NoError(t, err)
	require.True(t, applied.Applied)
	require.Empty(t, cache.setAccounts)
	require.NoError(t, tx.Rollback())
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, expected.Extra, got.Extra)
	require.Nil(t, got.TempUnschedulableUntil)
	require.Nil(t, got.RateLimitResetAt)
	require.Zero(t, cnControlOutboxCount(t, expected.ID))
}

func TestGrokRequestOutcomeCASDoesNotShortenCurrentCooldownOrRateLimit(t *testing.T) {
	repo, expected, _ := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
	later := time.Now().UTC().Truncate(time.Microsecond).Add(4 * time.Hour)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET temp_unschedulable_until=$2,temp_unschedulable_reason='existing long cooldown',rate_limited_at=NOW(),rate_limit_reset_at=$2 WHERE id=$1`, expected.ID, later)
	require.NoError(t, err)
	expected, err = repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(context.Background(), expected, grokRequestOutcomeMutation())
	require.NoError(t, err)
	require.True(t, applied.Applied)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, &later, got.TempUnschedulableUntil)
	require.Equal(t, &later, got.RateLimitResetAt)
}

func TestGrokRequestOutcomeCASAllowsOwnerClaimWithoutPersistentError(t *testing.T) {
	repo, expected, _ := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
	applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(context.Background(), expected, service.GrokRequestOutcome{})
	require.NoError(t, err)
	require.True(t, applied.Applied, "model-scoped runtime effects still need a conditional owner claim")
	_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-owner"}'::jsonb WHERE id=$1`, expected.ID)
	require.NoError(t, err)
	applied, err = grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(context.Background(), expected, service.GrokRequestOutcome{})
	require.NoError(t, err)
	require.False(t, applied.Applied)
}

func TestGrokRequestOutcomeCASRateLimitRecoveryKeepsObservedOwner(t *testing.T) {
	for _, scenario := range []string{"matching owner", "newer rate limit", "replacement credential", "same proxy edited", "outbox failure", "caller rollback", "contradictory reset"} {
		t.Run(scenario, func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			if scenario == "same proxy edited" {
				expected = bindCNProxyOwner(t, repo, expected)
			}
			reset := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET rate_limited_at=NOW(),rate_limit_reset_at=$2 WHERE id=$1", expected.ID, reset)
			require.NoError(t, err)
			expected, err = repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			wantReset := &reset
			switch scenario {
			case "newer rate limit":
				newReset := reset.Add(time.Hour)
				wantReset = &newReset
				_, err = integrationDB.ExecContext(context.Background(), "UPDATE accounts SET rate_limit_reset_at=$2 WHERE id=$1", expected.ID, newReset)
			case "replacement credential":
				_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-owner"}'::jsonb WHERE id=$1`, expected.ID)
			case "same proxy edited":
				_, err = integrationDB.ExecContext(context.Background(), "UPDATE proxies SET password='replacement-proxy-secret' WHERE id=$1", *expected.ProxyID)
			case "outbox failure":
				repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
			}
			require.NoError(t, err)
			ctx := context.Background()
			var callerTx *dbent.Tx
			if scenario == "caller rollback" {
				callerTx, err = integrationEntClient.Tx(ctx)
				require.NoError(t, err)
				defer func() { _ = callerTx.Rollback() }()
				ctx = dbent.NewTxContext(ctx, callerTx)
			}
			mutation := service.GrokRequestOutcome{ClearRateLimit: true, Extra: map[string]any{"grok_recovery_status": 200.0}}
			if scenario == "contradictory reset" {
				mutation.RateLimitResetAt = &reset
			}
			applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(ctx, expected, mutation)
			if scenario == "outbox failure" || scenario == "contradictory reset" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, scenario == "matching owner" || scenario == "caller rollback", applied.Applied)
			if callerTx != nil {
				require.Empty(t, cache.setAccounts, "uncommitted recovery must not become shared cache state")
				require.NoError(t, callerTx.Rollback())
			}
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, "base", got.Extra["retained"])
			if scenario == "matching owner" {
				require.Nil(t, got.RateLimitedAt)
				require.Nil(t, got.RateLimitResetAt)
				require.Equal(t, 200.0, got.Extra["grok_recovery_status"])
				require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
				require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
			} else {
				require.Equal(t, expected.RateLimitedAt, got.RateLimitedAt)
				require.Equal(t, wantReset, got.RateLimitResetAt)
				require.NotContains(t, got.Extra, "grok_recovery_status")
				require.Zero(t, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts)
			}
		})
	}
}

func TestGrokRequestOutcomeCASModelCooldownMergesConcurrentSibling(t *testing.T) {
	for _, scenario := range []string{"matching owner", "replacement credential", "outbox failure", "caller rollback", "unsafe extra overwrite"} {
		t.Run(scenario, func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET extra=extra || '{"model_rate_limits":{"grok-sibling":{"rate_limit_reset_at":"2030-01-01T00:00:00Z","reason":"concurrent sibling"}}}'::jsonb WHERE id=$1`, expected.ID)
			require.NoError(t, err)
			if scenario == "replacement credential" {
				_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-owner"}'::jsonb WHERE id=$1`, expected.ID)
				require.NoError(t, err)
			}
			if scenario == "outbox failure" {
				repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
			}
			ctx := context.Background()
			var callerTx *dbent.Tx
			if scenario == "caller rollback" {
				callerTx, err = integrationEntClient.Tx(ctx)
				require.NoError(t, err)
				defer func() { _ = callerTx.Rollback() }()
				ctx = dbent.NewTxContext(ctx, callerTx)
			}
			target := map[string]any{"rate_limit_reset_at": "2030-02-01T00:00:00Z", "reason": "configured model rule"}
			mutation := service.GrokRequestOutcome{ModelRateLimits: map[string]any{"grok-target": target}}
			if scenario == "unsafe extra overwrite" {
				mutation.Extra = map[string]any{"model_rate_limits": mutation.ModelRateLimits}
			}
			applied, err := grokRequestOutcomePort(t, repo).ApplyGrokRequestOutcomeIfUnchanged(ctx, expected, mutation)
			if scenario == "outbox failure" || scenario == "unsafe extra overwrite" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, scenario == "matching owner" || scenario == "caller rollback", applied.Applied)
			if callerTx != nil {
				require.Empty(t, cache.setAccounts)
				require.NoError(t, callerTx.Rollback())
			}
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, "base", got.Extra["retained"])
			limits, ok := got.Extra["model_rate_limits"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, map[string]any{"rate_limit_reset_at": "2030-01-01T00:00:00Z", "reason": "concurrent sibling"}, limits["grok-sibling"])
			if scenario == "matching owner" {
				require.Equal(t, target, limits["grok-target"])
				require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
				require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
			} else {
				require.NotContains(t, limits, "grok-target")
				require.Zero(t, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts)
			}
			require.Nil(t, got.TempUnschedulableUntil, "a model rule must not pause the entire account")
			require.Nil(t, got.RateLimitResetAt)
		})
	}
}

//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Requirement: after a proxy/credential lifecycle mutation, delayed upstream
// quota and capability responses must have zero effects on the successor row.
func codexObservationFixture(t *testing.T) (*accountRepository, *service.Account) {
	t.Helper()
	repo, owner := committedCredentialProxyFixture(t)
	_, err := integrationDB.Exec(`UPDATE accounts SET platform='openai',credentials='{"access_token":"sent","chatgpt_account_id":"principal-A"}'::jsonb WHERE id=$1`, owner.ID)
	require.NoError(t, err)
	owner, err = repo.GetByID(context.Background(), owner.ID)
	require.NoError(t, err)
	return repo, owner
}
func TestCodexObservationCASRejectsCredentialAndProxyLifecycleChanges(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"reauthorization", `UPDATE accounts SET credentials='{"access_token":"replacement","chatgpt_account_id":"principal-B"}'::jsonb WHERE id=$1`},
		{"remove proxy", `UPDATE accounts SET proxy_id=NULL WHERE id=$1`},
		{"proxy hostname", `UPDATE proxies SET host='replacement.invalid' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
		{"proxy protocol", `UPDATE proxies SET protocol='socks5' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
		{"proxy port", `UPDATE proxies SET port=9999 WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
		{"proxy auth username", `UPDATE proxies SET username='replacement' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
		{"proxy auth password", `UPDATE proxies SET password='replacement' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
		{"proxy disabled", `UPDATE proxies SET status='inactive' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
		{"proxy soft deleted", `UPDATE proxies SET deleted_at=NOW() WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
		{"account disabled", `UPDATE accounts SET status='inactive' WHERE id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, owner := codexObservationFixture(t)
			_, err := integrationDB.Exec(tc.query, owner.ID)
			require.NoError(t, err)
			accepted, err := repo.UpdateOpenAICodexUsageIfUnchanged(context.Background(), owner.ID, owner, map[string]any{"codex_usage_updated_at": time.Now().UTC().Format(time.RFC3339Nano), "codex_7d_used_percent": 99.0, "compact_supported": true})
			require.NoError(t, err)
			require.False(t, accepted)
			current, err := repo.GetByID(context.Background(), owner.ID)
			require.NoError(t, err)
			require.Equal(t, owner.Extra, current.Extra, "rejected evidence must leave all existing configuration unchanged")
			var events int
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, owner.ID).Scan(&events))
			require.Zero(t, events)
		})
	}
}
func TestCodexObservationCASAcceptsCurrentOwnerPreservesOrderingAndNeutralOutbox(t *testing.T) {
	repo, owner := codexObservationFixture(t)
	now := time.Now().UTC()
	accepted, err := repo.UpdateOpenAICodexUsageIfUnchanged(context.Background(), owner.ID, owner, map[string]any{"codex_usage_updated_at": now.Format(time.RFC3339Nano), "codex_7d_used_percent": 7.0})
	require.NoError(t, err)
	require.True(t, accepted)
	accepted, err = repo.UpdateOpenAICodexUsageIfUnchanged(context.Background(), owner.ID, owner, map[string]any{"codex_usage_updated_at": now.Add(-time.Second).Format(time.RFC3339Nano), "codex_7d_used_percent": 99.0})
	require.NoError(t, err)
	require.False(t, accepted)
	current, err := repo.GetByID(context.Background(), owner.ID)
	require.NoError(t, err)
	require.Equal(t, 7.0, current.Extra["codex_7d_used_percent"])
	require.Equal(t, "current", current.Extra["keep"])
	var events int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, owner.ID).Scan(&events))
	require.Zero(t, events)
}
func TestCodexObservationCASRechecksProxyAfterConcurrentEdit(t *testing.T) {
	repo, owner := codexObservationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `UPDATE proxies SET host='replacement.invalid' WHERE id=$1`, *owner.ProxyID)
	require.NoError(t, err)
	type outcome struct {
		accepted bool
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		accepted, err := repo.UpdateOpenAICodexUsageIfUnchanged(ctx, owner.ID, owner, map[string]any{"codex_7d_used_percent": 99.0})
		done <- outcome{accepted, err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM proxies%')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	result := <-done
	require.NoError(t, result.err)
	require.False(t, result.accepted)
	var value sql.NullString
	require.NoError(t, integrationDB.QueryRow(`SELECT extra->>'codex_7d_used_percent' FROM accounts WHERE id=$1`, owner.ID).Scan(&value))
	require.False(t, value.Valid)
}

func TestCodexWeeklyResetRejectsDelayedGrantAndQueuedSuccessorReset(t *testing.T) {
	for _, pendingEvent := range []bool{false, true} {
		t.Run(map[bool]string{false: "delayed sample", true: "queued event"}[pendingEvent], func(t *testing.T) {
			f := newQuotaFollowFixture(t)
			ctx := context.Background()
			owner, err := NewAccountRepository(integrationEntClient, integrationDB, nil).GetByID(ctx, f.accountID)
			require.NoError(t, err)
			if pendingEvent {
				f.observe(t, 1)
			}
			_, err = integrationDB.Exec(`UPDATE accounts SET credentials='{"chatgpt_account_id":"successor","access_token":"replacement"}'::jsonb WHERE id=$1`, f.accountID)
			require.NoError(t, err)
			if pendingEvent {
				result, err := f.repo.ProcessNextPending(ctx)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.Skipped)
			} else {
				obs := weeklyObservation(f.baseline.Add(7*24*time.Hour), f.baseline, 7)
				obs.Owner = owner
				created, err := f.repo.ObserveWeeklyReset(ctx, f.accountID, obs)
				require.NoError(t, err)
				require.Zero(t, created)
				var events int
				require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM group_quota_follow_reset_events WHERE group_id=$1`, f.groupID).Scan(&events))
				require.Zero(t, events)
			}
			var daily, weekly, monthly, fiveHour float64
			require.NoError(t, integrationDB.QueryRow(`SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd,five_hour_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&daily, &weekly, &monthly, &fiveHour))
			require.Equal(t, []float64{11, 11, 11, 11}, []float64{daily, weekly, monthly, fiveHour})
		})
	}
}
func TestCodexWeeklyPendingCannotCorroborateAcrossTokenRefresh(t *testing.T) {
	f := newQuotaFollowFixture(t)
	ctx := context.Background()
	_, err := integrationDB.Exec(`UPDATE accounts SET credentials='{"chatgpt_account_id":"same-principal","chatgpt_user_id":"verified-user","access_token":"old"}'::jsonb WHERE id=$1`, f.accountID)
	require.NoError(t, err)
	sample := f.now.Add(time.Minute)
	created, err := observeQuotaWeeklyForCurrentOwner(f.repo, ctx, f.accountID, weeklyObservation(f.baseline, sample, 80))
	require.NoError(t, err)
	require.Zero(t, created)
	created, err = observeQuotaWeeklyForCurrentOwner(f.repo, ctx, f.accountID, weeklyObservation(f.baseline, sample.Add(time.Minute), 7))
	require.NoError(t, err)
	require.Zero(t, created)
	_, err = integrationDB.Exec(`UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"refreshed"'::jsonb) WHERE id=$1`, f.accountID)
	require.NoError(t, err)
	created, err = observeQuotaWeeklyForCurrentOwner(f.repo, ctx, f.accountID, weeklyObservation(f.baseline, sample.Add(2*time.Minute), 7))
	require.NoError(t, err)
	require.Zero(t, created, "a fresh grant's first low sample cannot confirm the previous grant")
	created, err = observeQuotaWeeklyForCurrentOwner(f.repo, ctx, f.accountID, weeklyObservation(f.baseline, sample.Add(3*time.Minute), 7))
	require.NoError(t, err)
	require.Equal(t, 1, created, "two fresh real sessions from one grant can confirm the reset")
}

func TestCodexWeeklyBillingRechecksReauthorizationAfterLockWait(t *testing.T) {
	f := newQuotaFollowFixture(t)
	f.observe(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	replacement, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = replacement.Rollback() }()
	_, err = replacement.ExecContext(ctx, `UPDATE accounts SET credentials='{"chatgpt_account_id":"successor","access_token":"replacement"}'::jsonb WHERE id=$1`, f.accountID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 2)
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT platform, type, status, credentials, proxy_id%')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, replacement.Commit())
	require.NoError(t, <-done)
	var usage float64
	require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&usage))
	require.Equal(t, 13.0, usage, "replaced source's queued reset cannot erase the eleven existing units before the new charge")
	result, err := f.repo.ProcessNextPending(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Skipped)
	require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&usage))
	require.Equal(t, 13.0, usage)
}

func TestCodexConfirmedWeeklyResetSurvivesNormalTokenRefresh(t *testing.T) {
	for _, billing := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "billing"}[billing], func(t *testing.T) {
			f := newQuotaFollowFixture(t)
			ctx := context.Background()
			_, err := integrationDB.Exec(`UPDATE accounts SET credentials='{"chatgpt_account_id":"same-principal","chatgpt_user_id":"verified-user","access_token":"old"}'::jsonb WHERE id=$1`, f.accountID)
			require.NoError(t, err)
			created, err := observeQuotaWeeklyForCurrentOwner(f.repo, ctx, f.accountID, weeklyObservation(f.baseline, f.now.Add(time.Minute), 80))
			require.NoError(t, err)
			require.Zero(t, created)
			f.observe(t, 1)
			_, err = integrationDB.Exec(`UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"refreshed"'::jsonb) WHERE id=$1`, f.accountID)
			require.NoError(t, err)
			if billing {
				require.NoError(t, NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 2))
			}
			result, err := f.repo.ProcessNextPending(ctx)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.Skipped, "a confirmed reset belongs to the same verified quota principal after token refresh")
			expected := 0.0
			if billing {
				expected = 2
			}
			var usage float64
			require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&usage))
			require.Equal(t, expected, usage)
		})
	}
}

func TestCodexObservationNewGrantCannotBeBlockedByOldFutureTimestamp(t *testing.T) {
	repo, old := codexObservationFixture(t)
	ctx := context.Background()
	accepted, err := repo.UpdateOpenAICodexUsageIfUnchanged(ctx, old.ID, old, map[string]any{"codex_usage_updated_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano), "codex_7d_used_percent": 99.0, "codex_5h_used_percent": 90.0, "codex_reset_credit_snapshot": map[string]any{"available_count": 5}})
	require.NoError(t, err)
	require.True(t, accepted)
	_, err = integrationDB.Exec(`UPDATE accounts SET credentials='{"access_token":"new","chatgpt_account_id":"principal-B"}'::jsonb WHERE id=$1`, old.ID)
	require.NoError(t, err)
	current, err := repo.GetByID(ctx, old.ID)
	require.NoError(t, err)
	accepted, err = repo.UpdateOpenAICodexUsageIfUnchanged(ctx, current.ID, current, map[string]any{"codex_usage_updated_at": time.Now().UTC().Format(time.RFC3339Nano), "codex_5h_used_percent": 7.0})
	require.NoError(t, err)
	require.True(t, accepted)
	current, err = repo.GetByID(ctx, current.ID)
	require.NoError(t, err)
	require.Equal(t, 7.0, current.Extra["codex_5h_used_percent"])
	require.NotContains(t, current.Extra, "codex_7d_used_percent", "missing windows cannot inherit the predecessor's quota")
	require.NotContains(t, current.Extra, "codex_reset_credit_snapshot", "new principal cannot inherit old reset credits")
	require.Equal(t, "current", current.Extra["keep"])
}

func TestCodexShadowObservationRequiresCurrentParentAndRelation(t *testing.T) {
	for _, operation := range []string{"current parent", "parent reauthorized", "shadow reparented"} {
		t.Run(operation, func(t *testing.T) {
			repo, owner := codexObservationFixture(t)
			ctx := context.Background()
			shadow := &service.Account{Name: "codex-observation-shadow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, ParentAccountID: &owner.ID, QuotaDimension: service.QuotaDimensionSpark}
			require.NoError(t, repo.Create(ctx, shadow))
			t.Cleanup(func() {
				_, _ = integrationDB.Exec(`DELETE FROM scheduler_outbox WHERE account_id=$1`, shadow.ID)
				_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, shadow.ID)
			})
			switch operation {
			case "parent reauthorized":
				_, err := integrationDB.Exec(`UPDATE accounts SET credentials='{"access_token":"replacement"}'::jsonb WHERE id=$1`, owner.ID)
				require.NoError(t, err)
			case "shadow reparented":
				newParent := &service.Account{Name: "codex-observation-new-parent", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "new-parent-grant"}}
				require.NoError(t, repo.Create(ctx, newParent))
				t.Cleanup(func() {
					_, _ = integrationDB.Exec(`DELETE FROM scheduler_outbox WHERE account_id IN ($1,$2)`, shadow.ID, newParent.ID)
					_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, shadow.ID)
					_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, newParent.ID)
				})
				_, err := integrationDB.Exec(`UPDATE accounts SET parent_account_id=$2 WHERE id=$1`, shadow.ID, newParent.ID)
				require.NoError(t, err)
			}
			accepted, err := repo.UpdateOpenAICodexUsageIfUnchanged(ctx, shadow.ID, owner, map[string]any{"codex_usage_updated_at": time.Now().UTC().Format(time.RFC3339Nano), "codex_5h_used_percent": 7.0})
			require.NoError(t, err)
			require.Equal(t, operation == "current parent", accepted)
			target, err := repo.GetByID(ctx, shadow.ID)
			require.NoError(t, err)
			if accepted {
				require.Equal(t, 7.0, target.Extra["codex_5h_used_percent"])
			} else {
				require.NotContains(t, target.Extra, "codex_5h_used_percent")
			}
			parent, err := repo.GetByID(ctx, owner.ID)
			require.NoError(t, err)
			require.NotContains(t, parent.Extra, "codex_5h_used_percent", "Spark windows cannot overwrite the parent's default quota")
		})
	}
}

func TestCodexConfirmedWeeklyResetDoesNotCrossUsersInSameTeam(t *testing.T) {
	for _, billing := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "billing"}[billing], func(t *testing.T) {
			f := newQuotaFollowFixture(t)
			ctx := context.Background()
			_, err := integrationDB.Exec(`UPDATE accounts SET credentials='{"chatgpt_account_id":"same-team","chatgpt_user_id":"user-A","access_token":"old"}'::jsonb WHERE id=$1`, f.accountID)
			require.NoError(t, err)
			created, err := observeQuotaWeeklyForCurrentOwner(f.repo, ctx, f.accountID, weeklyObservation(f.baseline, f.now.Add(time.Minute), 80))
			require.NoError(t, err)
			require.Zero(t, created)
			f.observe(t, 1)
			_, err = integrationDB.Exec(`UPDATE accounts SET credentials='{"chatgpt_account_id":"same-team","chatgpt_user_id":"user-B","access_token":"new"}'::jsonb WHERE id=$1`, f.accountID)
			require.NoError(t, err)
			if billing {
				require.NoError(t, NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 2))
			}
			result, err := f.repo.ProcessNextPending(ctx)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.Skipped)
			expected := 11.0
			if billing {
				expected = 13
			}
			var usage float64
			require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&usage))
			require.Equal(t, expected, usage, "another user on the same team cannot erase prior usage")
		})
	}
}

func TestCodexObservationDuplicateSampleAllowsOnlyConsistentSupplement(t *testing.T) {
	repo, owner := codexObservationFixture(t)
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	accepted, err := repo.UpdateOpenAICodexUsageIfUnchanged(ctx, owner.ID, owner, map[string]any{"codex_usage_updated_at": stamp, "codex_7d_used_percent": 7.0})
	require.NoError(t, err)
	require.True(t, accepted)
	accepted, err = repo.UpdateOpenAICodexUsageIfUnchanged(ctx, owner.ID, owner, map[string]any{"codex_usage_updated_at": stamp, "codex_7d_used_percent": 7.0, "codex_reset_credit_snapshot": map[string]any{"available_count": 1}})
	require.NoError(t, err)
	require.True(t, accepted, "the same acquired payload can append its credit details")
	accepted, err = repo.UpdateOpenAICodexUsageIfUnchanged(ctx, owner.ID, owner, map[string]any{"codex_usage_updated_at": stamp, "codex_7d_used_percent": 99.0, "codex_reset_credit_snapshot": map[string]any{"available_count": 9}})
	require.NoError(t, err)
	require.False(t, accepted, "equal timestamps do not authorize contradictory quota")
	current, err := repo.GetByID(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, 7.0, current.Extra["codex_7d_used_percent"])
	require.Equal(t, map[string]any{"available_count": 1.0}, current.Extra["codex_reset_credit_snapshot"])
}

func TestCodexWeeklyBillingSourceChangeRollsBackChargeForRetry(t *testing.T) {
	f := newQuotaFollowFixture(t)
	f.observe(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	change, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = change.Rollback() }()
	var locked int64
	require.NoError(t, change.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.accountID).Scan(&locked))
	done := make(chan error, 1)
	go func() {
		done <- NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 2)
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT platform, type, status, credentials, proxy_id%')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	_, err = change.ExecContext(ctx, `UPDATE groups SET quota_reset_source_account_id=NULL,quota_reset_config_version=quota_reset_config_version+1 WHERE id=$1`, f.groupID)
	require.NoError(t, err)
	require.NoError(t, change.Commit())
	require.ErrorContains(t, <-done, "source changed", "the first transaction must fail instead of accepting a charge that skipped its verified reset source")
	var usage float64
	require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&usage))
	require.Equal(t, 11.0, usage, "source conflict rolls back both reset and charge")
	require.NoError(t, NewUserSubscriptionRepository(integrationEntClient).IncrementUsage(ctx, f.subscriptionID, 2))
	require.NoError(t, integrationDB.QueryRow(`SELECT weekly_usage_usd FROM user_subscriptions WHERE id=$1`, f.subscriptionID).Scan(&usage))
	require.Equal(t, 13.0, usage, "retry records exactly one charge under the new source policy")
}

func TestCodexPlanTypeMetadataUsesExactOAuthOwner(t *testing.T) {
	for _, change := range []string{"unchanged", "credentials replaced", "proxy changed", "API key"} {
		t.Run(change, func(t *testing.T) {
			repo, owner := codexObservationFixture(t)
			switch change {
			case "credentials replaced":
				_, err := integrationDB.Exec(`UPDATE accounts SET credentials='{"access_token":"new"}'::jsonb WHERE id=$1`, owner.ID)
				require.NoError(t, err)
			case "proxy changed":
				_, err := integrationDB.Exec(`UPDATE proxies SET host='new.invalid' WHERE id=$1`, *owner.ProxyID)
				require.NoError(t, err)
			case "API key":
				_, err := integrationDB.Exec(`UPDATE accounts SET type='apikey' WHERE id=$1`, owner.ID)
				require.NoError(t, err)
				owner, err = repo.GetByID(context.Background(), owner.ID)
				require.NoError(t, err)
			}
			accepted, err := repo.ApplyCredentialStateMutation(context.Background(), owner, service.CredentialStateMutation{OpenAIPlanType: "pro"})
			if change == "API key" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, change == "unchanged", accepted)
			current, err := repo.GetByID(context.Background(), owner.ID)
			require.NoError(t, err)
			if accepted {
				require.Equal(t, "pro", current.Credentials["plan_type"])
				require.Equal(t, "sent", current.Credentials["access_token"])
			} else {
				require.NotContains(t, current.Credentials, "plan_type")
			}
		})
	}
}

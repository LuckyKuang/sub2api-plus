//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type publicSchedulingRecoveryCache struct{ deleted []int64 }

func (*publicSchedulingRecoveryCache) SetTempUnsched(context.Context, int64, *service.TempUnschedState) error {
	return nil
}
func (*publicSchedulingRecoveryCache) GetTempUnsched(context.Context, int64) (*service.TempUnschedState, error) {
	return nil, nil
}
func (c *publicSchedulingRecoveryCache) DeleteTempUnsched(_ context.Context, id int64) error {
	c.deleted = append(c.deleted, id)
	return nil
}

func newPublicSchedulingRecoveryFixture(t *testing.T, platform string) (*accountRepository, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	repo, account, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeAPIKey)
	expires := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET
		platform=$2,status='error',error_message='retained account error',schedulable=FALSE,
		expires_at=$3,auto_pause_on_expired=TRUE,
		rate_limited_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '2 hours',overload_until=NOW()+INTERVAL '3 hours',
		temp_unschedulable_until=NOW()+INTERVAL '4 hours',temp_unschedulable_reason='retained temporary error',
		extra=extra || '{"model_rate_limits":{"m":{"rate_limit_reset_at":"2030-01-01T00:00:00Z","reason":"retained model error"}},"antigravity_quota_scopes":{"scope":"retained"},"runtime_recovery_generation":"historical-runtime-generation"}'::jsonb
		WHERE id=$1`, account.ID, platform, expires)
	require.NoError(t, err)
	before, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	cache.accounts = map[int64]*service.Account{before.ID: before}
	return repo, before, cache
}

func publicSchedulingRecoveryService(repo *accountRepository, cache *publicSchedulingRecoveryCache, runtime *adminRecoveryRuntimeRecorder) *service.RateLimitService {
	s := service.NewRateLimitService(repo, nil, &config.Config{}, nil, cache)
	s.SetAccountRuntimeBlocker(runtime)
	return s
}

func runPublicSchedulingRecovery(ctx context.Context, s *service.RateLimitService, id int64, mode string) error {
	if mode == "rates" {
		return s.ClearRateLimit(ctx, id)
	}
	return s.ClearTempUnschedulable(ctx, id)
}

func assertPublicSchedulingRecoveryPreservesConfiguration(t *testing.T, before, actual *service.Account) {
	t.Helper()
	require.Equal(t, before.Credentials, actual.Credentials)
	require.Equal(t, before.ProxyID, actual.ProxyID)
	require.Equal(t, before.Schedulable, actual.Schedulable)
	require.Equal(t, before.ExpiresAt, actual.ExpiresAt)
	require.Equal(t, before.AutoPauseOnExpired, actual.AutoPauseOnExpired)
	require.Equal(t, "base", actual.Extra["retained"])
}

func assertPublicSchedulingRecoveryState(t *testing.T, expected, actual *service.Account) {
	t.Helper()
	assertPublicSchedulingRecoveryPreservesConfiguration(t, expected, actual)
	require.Equal(t, expected.Status, actual.Status)
	require.Equal(t, expected.ErrorMessage, actual.ErrorMessage)
	require.Equal(t, expected.RateLimitedAt, actual.RateLimitedAt)
	require.Equal(t, expected.RateLimitResetAt, actual.RateLimitResetAt)
	require.Equal(t, expected.OverloadUntil, actual.OverloadUntil)
	require.Equal(t, expected.TempUnschedulableUntil, actual.TempUnschedulableUntil)
	require.Equal(t, expected.TempUnschedulableReason, actual.TempUnschedulableReason)
	require.Equal(t, expected.Extra, actual.Extra)
}

func TestSevenProviderPublicSchedulingRecoveryIsAtomic(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		for _, mode := range []string{"rates", "temp"} {
			for _, scenario := range []string{"success", "account write failure", "outbox failure", "caller rollback"} {
				t.Run(platform+"/"+mode+"/"+scenario, func(t *testing.T) {
					repo, before, scheduler := newPublicSchedulingRecoveryFixture(t, platform)
					cache := &publicSchedulingRecoveryCache{}
					runtime := &adminRecoveryRuntimeRecorder{}
					if scenario == "account write failure" {
						installAdminLifecycleStorageFault(t, "accounts", fmt.Sprintf("NEW.id=%d AND OLD.extra ? 'model_rate_limits' AND NOT (NEW.extra ? 'model_rate_limits')", before.ID))
					} else if scenario == "outbox failure" {
						installAdminLifecycleStorageFault(t, "scheduler_outbox", fmt.Sprintf("NEW.account_id=%d", before.ID))
					}
					var resetErr error
					if scenario == "caller rollback" {
						rollback := errors.New("deliberate public scheduling recovery rollback")
						err := repo.RunOpenAIOAuthPolicyTransaction(context.Background(), func(writeCtx context.Context, repository service.AccountRepository) error {
							writer := repository.(*accountRepository)
							resetErr = runPublicSchedulingRecovery(writeCtx, publicSchedulingRecoveryService(writer, cache, runtime), before.ID, mode)
							require.NoError(t, resetErr)
							require.Empty(t, cache.deleted, "a caller rollback must retain its preexisting temporary-state cache")
							require.Empty(t, runtime.cleared, "uncommitted recovery cannot clear scheduling blocks")
							require.Empty(t, scheduler.setAccounts, "an uncommitted partial account cannot reach the scheduler")
							require.Zero(t, cnControlOutboxCount(t, before.ID))
							stored, err := repo.GetByID(context.Background(), before.ID)
							require.NoError(t, err)
							assertPublicSchedulingRecoveryState(t, before, stored)
							return rollback
						})
						require.ErrorIs(t, err, rollback)
					} else {
						resetErr = runPublicSchedulingRecovery(context.Background(), publicSchedulingRecoveryService(repo, cache, runtime), before.ID, mode)
					}
					stored, err := repo.GetByID(context.Background(), before.ID)
					require.NoError(t, err)
					if scenario != "success" {
						if scenario != "caller rollback" {
							require.Error(t, resetErr)
						}
						assertPublicSchedulingRecoveryState(t, before, stored)
						require.Zero(t, cnControlOutboxCount(t, before.ID))
						require.Empty(t, cache.deleted)
						require.Empty(t, runtime.cleared)
						require.Empty(t, scheduler.setAccounts)
						assertPublicSchedulingRecoveryState(t, before, scheduler.accounts[before.ID])
						return
					}
					require.NoError(t, resetErr)
					assertPublicSchedulingRecoveryPreservesConfiguration(t, before, stored)
					require.Equal(t, before.Status, stored.Status, "scheduling reset does not clear the separate account error")
					require.Equal(t, before.ErrorMessage, stored.ErrorMessage)
					require.Nil(t, stored.TempUnschedulableUntil)
					require.Empty(t, stored.TempUnschedulableReason)
					require.NotContains(t, stored.Extra, "model_rate_limits")
					if mode == "rates" {
						require.Nil(t, stored.RateLimitedAt)
						require.Nil(t, stored.RateLimitResetAt)
						require.Nil(t, stored.OverloadUntil)
						require.NotContains(t, stored.Extra, "antigravity_quota_scopes")
					} else {
						require.Equal(t, before.RateLimitedAt, stored.RateLimitedAt)
						require.Equal(t, before.RateLimitResetAt, stored.RateLimitResetAt)
						require.Equal(t, before.OverloadUntil, stored.OverloadUntil)
						require.Equal(t, before.Extra["antigravity_quota_scopes"], stored.Extra["antigravity_quota_scopes"])
					}
					generation, _ := stored.Extra[service.RuntimeRecoveryGenerationExtraKey].(string)
					if platform == service.PlatformGrok || platform == service.PlatformOpenAI {
						require.NotEmpty(t, generation)
						require.NotEqual(t, oldRuntimeRecoveryGeneration, generation)
					} else {
						require.Equal(t, oldRuntimeRecoveryGeneration, generation)
					}
					require.Equal(t, 1, cnControlOutboxCount(t, before.ID))
					require.Equal(t, []int64{before.ID}, cache.deleted)
					require.Equal(t, []int64{before.ID}, runtime.cleared)
					assertPublicSchedulingRecoveryState(t, stored, requireEvictedSchedulerAccount(t, repo, scheduler, before.ID))
				})
			}
		}
	}
}

func TestSuccessfulTestRecoveryCommitsOnlyRequiredState(t *testing.T) {
	for _, scenario := range []string{"error only", "rates only", "error and rates", "disabled and rates", "no recoverable state", "account write failure", "outbox failure", "caller rollback"} {
		t.Run(scenario, func(t *testing.T) {
			repo, before, scheduler := newPublicSchedulingRecoveryFixture(t, service.PlatformGrok)
			_, err := integrationDB.Exec(`UPDATE accounts SET type='oauth',credentials='{"access_token":"retained-oauth-token"}'::jsonb WHERE id=$1`, before.ID)
			require.NoError(t, err)
			if scenario == "error only" || scenario == "no recoverable state" {
				_, err = integrationDB.Exec(`UPDATE accounts SET rate_limited_at=NULL,rate_limit_reset_at=NULL,overload_until=NULL,temp_unschedulable_until=NULL,temp_unschedulable_reason=NULL,extra=extra-'model_rate_limits'-'antigravity_quota_scopes' WHERE id=$1`, before.ID)
				require.NoError(t, err)
			}
			if scenario == "rates only" || scenario == "no recoverable state" {
				_, err = integrationDB.Exec(`UPDATE accounts SET status='active',error_message=NULL WHERE id=$1`, before.ID)
				require.NoError(t, err)
			} else if scenario == "disabled and rates" {
				_, err = integrationDB.Exec(`UPDATE accounts SET status='inactive',error_message='administrator disabled' WHERE id=$1`, before.ID)
				require.NoError(t, err)
			}
			before, err = repo.GetByID(context.Background(), before.ID)
			require.NoError(t, err)
			scheduler.accounts[before.ID] = before
			cache := &publicSchedulingRecoveryCache{}
			runtime := &adminRecoveryRuntimeRecorder{}
			invalidator := &adminOAuthTokenInvalidator{}
			makeService := func(writer *accountRepository) *service.RateLimitService {
				s := publicSchedulingRecoveryService(writer, cache, runtime)
				s.SetTokenCacheInvalidator(invalidator)
				return s
			}
			if scenario == "account write failure" {
				installAdminLifecycleStorageFault(t, "accounts", fmt.Sprintf("NEW.id=%d AND OLD.extra ? 'model_rate_limits' AND NOT (NEW.extra ? 'model_rate_limits')", before.ID))
			} else if scenario == "outbox failure" {
				installAdminLifecycleStorageFault(t, "scheduler_outbox", fmt.Sprintf("NEW.account_id=%d", before.ID))
			}
			var result *service.SuccessfulTestRecoveryResult
			var recoveryErr error
			if scenario == "caller rollback" {
				rollback := errors.New("deliberate successful test recovery rollback")
				err := repo.RunOpenAIOAuthPolicyTransaction(context.Background(), func(writeCtx context.Context, repository service.AccountRepository) error {
					result, recoveryErr = makeService(repository.(*accountRepository)).RecoverAccountState(writeCtx, before.ID, service.AccountRecoveryOptions{InvalidateToken: true})
					require.NoError(t, recoveryErr)
					require.True(t, result.ClearedError)
					require.True(t, result.ClearedRateLimit)
					require.Empty(t, invalidator.accounts, "uncommitted recovery cannot invalidate credential-owner caches")
					require.Empty(t, cache.deleted)
					require.Empty(t, runtime.cleared)
					require.Empty(t, scheduler.setAccounts)
					require.Zero(t, cnControlOutboxCount(t, before.ID))
					return rollback
				})
				require.ErrorIs(t, err, rollback)
			} else {
				result, recoveryErr = makeService(repo).RecoverAccountState(context.Background(), before.ID, service.AccountRecoveryOptions{InvalidateToken: true})
			}
			stored, err := repo.GetByID(context.Background(), before.ID)
			require.NoError(t, err)
			if scenario == "account write failure" || scenario == "outbox failure" || scenario == "caller rollback" {
				if scenario != "caller rollback" {
					require.Error(t, recoveryErr)
					require.Nil(t, result)
				}
				assertPublicSchedulingRecoveryState(t, before, stored)
				require.Zero(t, cnControlOutboxCount(t, before.ID))
				require.Empty(t, invalidator.accounts)
				require.Empty(t, cache.deleted)
				require.Empty(t, runtime.cleared)
				require.Empty(t, scheduler.setAccounts)
				return
			}
			require.NoError(t, recoveryErr)
			require.NotNil(t, result)
			wantClearError := scenario == "error only" || scenario == "error and rates"
			wantClearRates := scenario == "rates only" || scenario == "error and rates" || scenario == "disabled and rates"
			require.Equal(t, wantClearError, result.ClearedError)
			require.Equal(t, wantClearRates, result.ClearedRateLimit)
			assertPublicSchedulingRecoveryPreservesConfiguration(t, before, stored)
			if scenario == "no recoverable state" {
				assertPublicSchedulingRecoveryState(t, before, stored)
				require.Zero(t, cnControlOutboxCount(t, before.ID))
				require.Empty(t, invalidator.accounts)
				require.Empty(t, cache.deleted)
				require.Empty(t, runtime.cleared)
				require.Empty(t, scheduler.setAccounts)
				return
			}
			if wantClearError {
				require.Equal(t, service.StatusActive, stored.Status)
				require.Empty(t, stored.ErrorMessage)
				require.Len(t, invalidator.accounts, 1)
				require.Equal(t, before.Credentials, invalidator.accounts[0].Credentials)
			} else {
				require.Equal(t, before.Status, stored.Status, "a successful test cannot enable a manually disabled account")
				require.Equal(t, before.ErrorMessage, stored.ErrorMessage)
				require.Empty(t, invalidator.accounts)
			}
			require.Nil(t, stored.RateLimitedAt)
			require.Nil(t, stored.RateLimitResetAt)
			require.Nil(t, stored.OverloadUntil)
			require.Nil(t, stored.TempUnschedulableUntil)
			require.Empty(t, stored.TempUnschedulableReason)
			require.NotContains(t, stored.Extra, "model_rate_limits")
			require.NotContains(t, stored.Extra, "antigravity_quota_scopes")
			require.NotEqual(t, oldRuntimeRecoveryGeneration, stored.Extra[service.RuntimeRecoveryGenerationExtraKey])
			require.Equal(t, 1, cnControlOutboxCount(t, before.ID))
			require.Equal(t, []int64{before.ID}, runtime.cleared)
			if wantClearRates {
				require.Equal(t, []int64{before.ID}, cache.deleted)
			}
			assertPublicSchedulingRecoveryState(t, stored, requireEvictedSchedulerAccount(t, repo, scheduler, before.ID))
		})
	}
}

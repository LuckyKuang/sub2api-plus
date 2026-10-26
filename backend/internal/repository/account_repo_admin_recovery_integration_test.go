//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type adminRecoveryRuntimeRecorder struct{ cleared []int64 }

func (*adminRecoveryRuntimeRecorder) BlockAccountScheduling(*service.Account, time.Time, string) {}
func (r *adminRecoveryRuntimeRecorder) ClearAccountSchedulingBlock(id int64) {
	r.cleared = append(r.cleared, id)
}

func (r *adminRecoveryRuntimeRecorder) PrepareOAuthSchedulingRecovery(account *service.Account) func() {
	return func() { r.ClearAccountSchedulingBlock(account.ID) }
}

func adminRecoveryService(repo *accountRepository, runtime service.AccountRuntimeBlocker) service.AdminService {
	return service.NewAdminService(&config.Config{}, nil, nil, repo, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, integrationEntClient, nil, nil, nil, nil, runtime, nil, nil, nil, nil)
}

func installAdminLifecycleStorageFault(t *testing.T, table, condition string) {
	t.Helper()
	name := fmt.Sprintf("admin_lifecycle_fault_%d", time.Now().UnixNano())
	_, err := integrationDB.ExecContext(context.Background(), fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fault$
		BEGIN IF %s THEN RAISE EXCEPTION 'admin lifecycle storage fault'; END IF; RETURN NEW; END $fault$`, name, condition))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DROP FUNCTION "+name+"() CASCADE")
		require.NoError(t, err)
	})
	_, err = integrationDB.ExecContext(context.Background(), "CREATE TRIGGER "+name+" BEFORE INSERT OR UPDATE ON "+table+" FOR EACH ROW EXECUTE FUNCTION "+name+"()")
	require.NoError(t, err)
}

func TestSevenProviderAdminRecoveryCommitsAllStateOrNothing(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		for _, scenario := range []string{"success", "middle row failure", "final outbox failure"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				ctx := context.Background()
				account := mustCreateAccount(t, integrationEntClient, &service.Account{
					Name: fmt.Sprintf("admin-recovery-%d", time.Now().UnixNano()), Platform: platform, Type: service.AccountTypeAPIKey,
					Status: service.StatusError, ErrorMessage: "retained provider failure", Credentials: map[string]any{"api_key": "retained-credential"},
					Extra: map[string]any{"retained": "base", "model_rate_limits": map[string]any{"grok-4.5": map[string]any{"reason": "old model failure"}}, "antigravity_quota_scopes": map[string]any{"old": true}},
				})
				t.Cleanup(func() {
					_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
					_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
				})
				_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET rate_limited_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '1 hour',overload_until=NOW()+INTERVAL '2 hours',temp_unschedulable_until=NOW()+INTERVAL '3 hours',temp_unschedulable_reason='retained cooldown' WHERE id=$1`, account.ID)
				require.NoError(t, err)
				repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
				before, err := repo.GetByID(ctx, account.ID)
				require.NoError(t, err)
				cache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{account.ID: before}}
				repo.schedulerCache = cache
				runtime := &adminRecoveryRuntimeRecorder{}
				switch scenario {
				case "middle row failure":
					installAdminLifecycleStorageFault(t, "accounts", fmt.Sprintf("NEW.id=%d AND OLD.rate_limit_reset_at IS NOT NULL AND NEW.rate_limit_reset_at IS NULL", account.ID))
				case "final outbox failure":
					installAdminLifecycleStorageFault(t, "scheduler_outbox", fmt.Sprintf("NEW.account_id=%d AND EXISTS(SELECT 1 FROM accounts WHERE id=%d AND temp_unschedulable_until IS NULL)", account.ID, account.ID))
				}
				updated, resetErr := adminRecoveryService(repo, runtime).ClearAccountError(ctx, account.ID)
				after, err := repo.GetByID(ctx, account.ID)
				require.NoError(t, err)
				require.Equal(t, before.Credentials, after.Credentials)
				require.Equal(t, "base", after.Extra["retained"])
				if scenario == "success" {
					require.NoError(t, resetErr)
					require.NotNil(t, updated)
					require.Equal(t, service.StatusActive, after.Status)
					require.Empty(t, after.ErrorMessage)
					require.Nil(t, after.RateLimitedAt)
					require.Nil(t, after.RateLimitResetAt)
					require.Nil(t, after.OverloadUntil)
					require.Nil(t, after.TempUnschedulableUntil)
					require.Empty(t, after.TempUnschedulableReason)
					require.NotContains(t, after.Extra, "model_rate_limits")
					require.NotContains(t, after.Extra, "antigravity_quota_scopes")
					require.Equal(t, []int64{account.ID}, runtime.cleared)
					cached := requireEvictedSchedulerAccount(t, repo, cache, account.ID)
					require.Equal(t, service.StatusActive, cached.Status)
					require.Equal(t, 1, cnControlOutboxCount(t, account.ID))
				} else {
					require.Error(t, resetErr)
					require.Nil(t, updated)
					require.Equal(t, before.Status, after.Status, "a failed reset must not expose a partially recovered account")
					require.Equal(t, before.ErrorMessage, after.ErrorMessage)
					require.Equal(t, before.RateLimitedAt, after.RateLimitedAt)
					require.Equal(t, before.RateLimitResetAt, after.RateLimitResetAt)
					require.Equal(t, before.OverloadUntil, after.OverloadUntil)
					require.Equal(t, before.TempUnschedulableUntil, after.TempUnschedulableUntil)
					require.Equal(t, before.TempUnschedulableReason, after.TempUnschedulableReason)
					require.Equal(t, before.Extra, after.Extra)
					require.Empty(t, runtime.cleared)
					require.Empty(t, cache.setAccounts, "failed reset cannot publish an intermediate cache snapshot")
					require.Equal(t, before, cache.accounts[account.ID])
					require.Zero(t, cnControlOutboxCount(t, account.ID))
				}
			})
		}
	}
}

func newAdminShadowLifecycleFixture(t *testing.T) (*accountRepository, *service.Account, *service.Account, *service.Proxy, *service.Proxy, *schedulerCacheRecorder) {
	t.Helper()
	ctx := context.Background()
	origin := mustCreateProxy(t, integrationEntClient, &service.Proxy{Name: "admin-origin", Protocol: "http", Host: "origin.example", Port: 8080, Status: service.StatusActive})
	backup := mustCreateProxy(t, integrationEntClient, &service.Proxy{Name: "admin-backup", Protocol: "http", Host: "backup.example", Port: 8081, Status: service.StatusActive})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1 OR id=$2", origin.ID, backup.ID)
	})
	parent := mustCreateAccount(t, integrationEntClient, &service.Account{Name: "admin-parent", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ProxyID: &backup.ID, Credentials: map[string]any{"access_token": "parent-login"}})
	shadow := mustCreateAccount(t, integrationEntClient, &service.Account{Name: "admin-shadow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ProxyID: &backup.ID, ParentAccountID: &parent.ID, QuotaDimension: "spark"})
	t.Cleanup(func() {
		for _, id := range []int64{shadow.ID, parent.ID} {
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", id)
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", id)
		}
	})
	_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET proxy_fallback_origin_id=$2 WHERE id=$1", parent.ID, origin.ID)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	parent, err = repo.GetByID(ctx, parent.ID)
	require.NoError(t, err)
	shadow, err = repo.GetByID(ctx, shadow.ID)
	require.NoError(t, err)
	cache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{parent.ID: parent, shadow.ID: shadow}}
	repo.schedulerCache = cache
	return repo, parent, shadow, origin, backup, cache
}

func TestAdminShadowDeletionCommitsWholeCredentialFamilyOrNothing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "parent storage failure"}[fail], func(t *testing.T) {
			repo, parent, shadow, _, _, cache := newAdminShadowLifecycleFixture(t)
			if fail {
				installAdminLifecycleStorageFault(t, "accounts", fmt.Sprintf("NEW.id=%d AND NEW.deleted_at IS NOT NULL", parent.ID))
			}
			err := adminRecoveryService(repo, nil).DeleteAccount(context.Background(), parent.ID)
			if fail {
				require.Error(t, err)
				for _, owner := range []*service.Account{parent, shadow} {
					stored, err := repo.GetByID(context.Background(), owner.ID)
					require.NoError(t, err, "a failed family delete cannot remove an earlier shadow")
					require.Equal(t, owner.Credentials, stored.Credentials)
					require.Equal(t, owner.ProxyID, stored.ProxyID)
					require.Zero(t, cnControlOutboxCount(t, owner.ID))
					require.Contains(t, cache.accounts, owner.ID)
				}
				require.Empty(t, cache.deleteIDs)
			} else {
				require.NoError(t, err)
				for _, owner := range []*service.Account{parent, shadow} {
					_, err := repo.GetByID(context.Background(), owner.ID)
					require.ErrorIs(t, err, service.ErrAccountNotFound)
					require.Equal(t, 1, cnControlOutboxCount(t, owner.ID))
					require.NotContains(t, cache.accounts, owner.ID)
				}
				require.ElementsMatch(t, []int64{parent.ID, shadow.ID}, cache.deleteIDs)
			}
		})
	}
}

func TestAdminShadowProxyRestorationCommitsFamilyBindingOrNothing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "shadow storage failure"}[fail], func(t *testing.T) {
			repo, parent, shadow, origin, backup, cache := newAdminShadowLifecycleFixture(t)
			if fail {
				installAdminLifecycleStorageFault(t, "accounts", fmt.Sprintf("NEW.id=%d AND NEW.proxy_id=%d", shadow.ID, origin.ID))
			}
			err := adminRecoveryService(repo, nil).RevertAccountProxyFallback(context.Background(), parent.ID)
			if fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			for _, owner := range []*service.Account{parent, shadow} {
				stored, err := repo.GetByID(context.Background(), owner.ID)
				require.NoError(t, err)
				wantRoute := &origin.ID
				if fail {
					wantRoute = &backup.ID
				}
				require.Equal(t, wantRoute, stored.ProxyID, "a failed shadow write must roll back the parent's route too")
				require.Equal(t, owner.Credentials, stored.Credentials)
				if fail {
					require.Equal(t, wantRoute, cache.accounts[owner.ID].ProxyID)
					require.Zero(t, cnControlOutboxCount(t, owner.ID))
				} else {
					cached := requireEvictedSchedulerAccount(t, repo, cache, owner.ID)
					require.Equal(t, wantRoute, cached.ProxyID)
					require.Equal(t, 1, cnControlOutboxCount(t, owner.ID))
				}
				if owner.ID == parent.ID {
					if fail {
						require.Equal(t, &origin.ID, stored.ProxyFallbackOriginID)
					} else {
						require.Nil(t, stored.ProxyFallbackOriginID)
					}
				}
			}
			if fail {
				require.Empty(t, cache.setAccounts)
			}
		})
	}
}

func TestAdminRecoveryCallerTransactionPublishesRuntimeOnlyAfterCommit(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			repo, parent, _, _, _, cache := newAdminShadowLifecycleFixture(t)
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET status='error',error_message='retained before commit',temp_unschedulable_until=NOW()+INTERVAL '1 hour' WHERE id=$1", parent.ID)
			require.NoError(t, err)
			before, err := repo.GetByID(context.Background(), parent.ID)
			require.NoError(t, err)
			cache.accounts[parent.ID] = before
			runtime := &adminRecoveryRuntimeRecorder{}
			tx, err := integrationEntClient.Tx(context.Background())
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			updated, err := adminRecoveryService(repo, runtime).ClearAccountError(dbent.NewTxContext(context.Background(), tx), parent.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusActive, updated.Status)
			require.Empty(t, runtime.cleared)
			require.Empty(t, cache.setAccounts)
			require.Zero(t, cnControlOutboxCount(t, parent.ID))
			stored, err := repo.GetByID(context.Background(), parent.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusError, stored.Status)
			if commit {
				require.NoError(t, tx.Commit())
				require.Equal(t, []int64{parent.ID}, runtime.cleared)
				require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
			} else {
				require.NoError(t, tx.Rollback())
				require.Empty(t, runtime.cleared)
				require.Zero(t, cnControlOutboxCount(t, parent.ID))
			}
			stored, err = repo.GetByID(context.Background(), parent.ID)
			require.NoError(t, err)
			require.Equal(t, map[bool]string{false: service.StatusError, true: service.StatusActive}[commit], stored.Status)
		})
	}
}

func TestAdminRecoveryPreservesManualPauseAndExpirationConfiguration(t *testing.T) {
	repo, parent, _, _, _, _ := newAdminShadowLifecycleFixture(t)
	expiry := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET status='error',schedulable=FALSE,expires_at=$2,auto_pause_on_expired=TRUE WHERE id=$1", parent.ID, expiry)
	require.NoError(t, err)
	updated, err := adminRecoveryService(repo, nil).ClearAccountError(context.Background(), parent.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, updated.Status)
	require.False(t, updated.Schedulable, "resetting error state cannot undo the administrator's manual pause")
	require.True(t, updated.AutoPauseOnExpired)
	require.Equal(t, &expiry, updated.ExpiresAt)
	require.False(t, updated.IsSchedulable(), "recovery must not bypass saved expiration admission")
}

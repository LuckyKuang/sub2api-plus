//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

const oldRuntimeRecoveryGeneration = "historical-runtime-generation"

func seedRuntimeRecoveryGeneration(t *testing.T, repo *accountRepository, account *service.Account, generation string) *service.Account {
	t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET extra=extra || jsonb_build_object($2::text,$3::text) WHERE id=$1`, account.ID, service.RuntimeRecoveryGenerationExtraKey, generation)
	require.NoError(t, err)
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	return stored
}

func TestRuntimeRecoveryGenerationResetCommitsOrRollsBackWithAccount(t *testing.T) {
	for _, target := range []struct{ platform, accountType string }{
		{service.PlatformGrok, service.AccountTypeOAuth},
		{service.PlatformGrok, service.AccountTypeAPIKey},
		{service.PlatformOpenAI, service.AccountTypeAPIKey},
	} {
		for _, scenario := range []string{"success", "outbox failure", "caller rollback"} {
			t.Run(target.platform+"/"+target.accountType+"/"+scenario, func(t *testing.T) {
				repo, owner, cache := newGrokRequestOutcomeFixture(t, target.accountType)
				_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET platform=$2 WHERE id=$1", owner.ID, target.platform)
				require.NoError(t, err)
				owner = seedRuntimeRecoveryGeneration(t, repo, owner, oldRuntimeRecoveryGeneration)
				ctx := context.Background()
				var tx *dbent.Tx
				if scenario == "outbox failure" {
					installAdminLifecycleStorageFault(t, "scheduler_outbox", fmt.Sprintf("NEW.account_id=%d", owner.ID))
				} else if scenario == "caller rollback" {
					tx, err = integrationEntClient.Tx(ctx)
					require.NoError(t, err)
					defer func() { _ = tx.Rollback() }()
					ctx = dbent.NewTxContext(ctx, tx)
				}
				resetErr := repo.ClearAccountRecoveryState(ctx, owner.ID)
				if scenario == "outbox failure" {
					require.Error(t, resetErr)
				} else {
					require.NoError(t, resetErr)
				}
				if tx != nil {
					var uncommitted string
					rows, err := tx.Client().QueryContext(ctx, `SELECT extra->>$2 FROM accounts WHERE id=$1`, owner.ID, service.RuntimeRecoveryGenerationExtraKey)
					require.NoError(t, err)
					require.True(t, rows.Next())
					require.NoError(t, rows.Scan(&uncommitted))
					require.NoError(t, rows.Close())
					require.NotEqual(t, oldRuntimeRecoveryGeneration, uncommitted, "the generation changes in the same transaction as recovery")
					require.NotEmpty(t, uncommitted)
					require.Empty(t, cache.setAccounts)
					require.Zero(t, cnControlOutboxCount(t, owner.ID))
					require.NoError(t, tx.Rollback())
				}
				stored, err := repo.GetByID(context.Background(), owner.ID)
				require.NoError(t, err)
				generation, _ := stored.Extra[service.RuntimeRecoveryGenerationExtraKey].(string)
				require.Equal(t, owner.Credentials, stored.Credentials)
				require.Equal(t, "base", stored.Extra["retained"])
				if scenario == "success" {
					require.NotEmpty(t, generation)
					require.NotEqual(t, oldRuntimeRecoveryGeneration, generation)
					require.Equal(t, 1, cnControlOutboxCount(t, owner.ID))
					require.Empty(t, cache.setAccounts)
					require.Equal(t, []int64{owner.ID}, cache.deleteIDs)
				} else {
					require.Equal(t, oldRuntimeRecoveryGeneration, generation)
					require.Zero(t, cnControlOutboxCount(t, owner.ID))
					require.Empty(t, cache.setAccounts)
				}
			})
		}
	}
}

func TestRuntimeRecoveryGenerationResetPreservesUnrelatedProviderState(t *testing.T) {
	for _, target := range []struct{ platform, accountType string }{
		{service.PlatformOpenAI, service.AccountTypeOAuth},
		{service.PlatformDeepseek, service.AccountTypeAPIKey},
		{service.PlatformKimi, service.AccountTypeAPIKey},
		{service.PlatformMiniMax, service.AccountTypeAPIKey},
		{service.PlatformStepFun, service.AccountTypeAPIKey},
		{service.PlatformZhipu, service.AccountTypeAPIKey},
	} {
		t.Run(target.platform, func(t *testing.T) {
			repo, owner, _ := newGrokRequestOutcomeFixture(t, target.accountType)
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET platform=$2 WHERE id=$1", owner.ID, target.platform)
			require.NoError(t, err)
			owner = seedRuntimeRecoveryGeneration(t, repo, owner, oldRuntimeRecoveryGeneration)
			require.NoError(t, repo.ClearAccountRecoveryState(context.Background(), owner.ID))
			stored, err := repo.GetByID(context.Background(), owner.ID)
			require.NoError(t, err)
			require.Equal(t, oldRuntimeRecoveryGeneration, stored.Extra[service.RuntimeRecoveryGenerationExtraKey])
			require.Equal(t, owner.Credentials, stored.Credentials)
		})
	}
}

func TestRuntimeRecoveryGenerationOAuthRecoveryAndRefreshKeepDifferentContracts(t *testing.T) {
	for _, scenario := range []string{"reauthorize", "refresh", "outbox failure", "caller rollback"} {
		t.Run(scenario, func(t *testing.T) {
			repo, owner, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			owner = seedRuntimeRecoveryGeneration(t, repo, owner, oldRuntimeRecoveryGeneration)
			ctx := context.Background()
			var tx *dbent.Tx
			var err error
			if scenario == "outbox failure" {
				installAdminLifecycleStorageFault(t, "scheduler_outbox", fmt.Sprintf("NEW.account_id=%d", owner.ID))
			} else if scenario == "caller rollback" {
				tx, err = integrationEntClient.Tx(ctx)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				ctx = dbent.NewTxContext(ctx, tx)
			}
			applied, writeErr := repo.UpdateOAuthCredentialsIfUnchanged(ctx, owner, owner.Type,
				map[string]any{"access_token": "new-authorization"},
				map[string]any{service.RuntimeRecoveryGenerationExtraKey: "forged-request-generation", "preference": "saved"}, scenario != "refresh")
			if scenario == "outbox failure" {
				require.Error(t, writeErr)
				require.False(t, applied)
			} else {
				require.NoError(t, writeErr)
				require.True(t, applied)
			}
			if tx != nil {
				require.Empty(t, cache.setAccounts)
				require.NoError(t, tx.Rollback())
			}
			stored, err := repo.GetByID(context.Background(), owner.ID)
			require.NoError(t, err)
			generation, _ := stored.Extra[service.RuntimeRecoveryGenerationExtraKey].(string)
			require.NotEqual(t, "forged-request-generation", generation, "client metadata has no authority over runtime recovery")
			if scenario == "reauthorize" {
				require.NotEmpty(t, generation)
				require.NotEqual(t, oldRuntimeRecoveryGeneration, generation)
			} else {
				require.Equal(t, oldRuntimeRecoveryGeneration, generation)
			}
			if scenario == "reauthorize" || scenario == "refresh" {
				require.Equal(t, "new-authorization", stored.GetCredential("access_token"))
				require.Equal(t, "saved", stored.Extra["preference"])
				require.Equal(t, 1, cnControlOutboxCount(t, owner.ID))
			} else {
				require.Equal(t, owner.Credentials, stored.Credentials)
				require.NotContains(t, stored.Extra, "preference")
				require.Zero(t, cnControlOutboxCount(t, owner.ID))
			}
		})
	}
}

func TestRuntimeRecoveryGenerationIsPreservedAcrossOrdinaryMetadataWrites(t *testing.T) {
	for _, method := range []string{"Update", "UpdateExtra", "BulkUpdate"} {
		t.Run(method, func(t *testing.T) {
			repo, stale, _ := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			seedRuntimeRecoveryGeneration(t, repo, stale, "newer-committed-recovery")
			incoming := map[string]any{service.RuntimeRecoveryGenerationExtraKey: "forged-or-stale-generation", "preference": "saved"}
			switch method {
			case "Update":
				stale.Extra = incoming
				require.NoError(t, repo.Update(context.Background(), stale))
			case "UpdateExtra":
				require.NoError(t, repo.UpdateExtra(context.Background(), stale.ID, incoming))
			case "BulkUpdate":
				count, err := repo.BulkUpdate(context.Background(), []int64{stale.ID}, service.AccountBulkUpdate{Extra: incoming})
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
			}
			stored, err := repo.GetByID(context.Background(), stale.ID)
			require.NoError(t, err)
			require.Equal(t, "newer-committed-recovery", stored.Extra[service.RuntimeRecoveryGenerationExtraKey])
			require.Equal(t, "saved", stored.Extra["preference"])
			if method == "Update" {
				require.Equal(t, stored.Extra, stale.Extra, "the account object returned to management callers must use the saved metadata owner")
			}
			require.Equal(t, "forged-or-stale-generation", incoming[service.RuntimeRecoveryGenerationExtraKey], "persistence must not mutate the caller's metadata map")
		})
	}
}

func TestRuntimeRecoveryGenerationCannotBeImportedOnAccountCreation(t *testing.T) {
	for _, method := range []string{"Create", "CreateWithAccountGroups"} {
		t.Run(method, func(t *testing.T) {
			repo, owner, _ := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			account := &service.Account{Name: "new account with imported runtime state", Platform: owner.Platform, Type: owner.Type,
				Status: service.StatusActive, Schedulable: true, Credentials: owner.Credentials,
				Extra: map[string]any{service.RuntimeRecoveryGenerationExtraKey: "imported-other-account-generation", "preference": "saved"}}
			t.Cleanup(func() {
				_, _ = integrationDB.Exec(`DELETE FROM scheduler_outbox WHERE account_id=$1`, account.ID)
				_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, account.ID)
			})
			if method == "Create" {
				require.NoError(t, repo.Create(context.Background(), account))
			} else {
				require.NoError(t, repo.CreateWithAccountGroups(context.Background(), account, nil))
			}
			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.NotContains(t, stored.Extra, service.RuntimeRecoveryGenerationExtraKey)
			require.Equal(t, "saved", stored.Extra["preference"])
			require.NotContains(t, account.Extra, service.RuntimeRecoveryGenerationExtraKey, "the create response cannot claim an owner absent from its saved account")
			require.Equal(t, stored.Extra, account.Extra)
		})
	}
}

type runtimeRecoveryAfterOutcomeQuery struct {
	sqlExecutor
	after    func()
	observed bool
}

func (e *runtimeRecoveryAfterOutcomeQuery) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := e.sqlExecutor.QueryContext(ctx, query, args...)
	if err == nil && strings.Contains(query, "/* grok_request_outcome_cas */") {
		e.observed = true
		e.after()
	}
	return rows, err
}

func TestGrokRequestOutcomeReturnsGenerationOfItsAtomicCommit(t *testing.T) {
	for _, order := range []string{"failure before recovery", "failure after recovery", "recovery after statement before return"} {
		t.Run(order, func(t *testing.T) {
			repo, owner, _ := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			owner = seedRuntimeRecoveryGeneration(t, repo, owner, oldRuntimeRecoveryGeneration)
			reset := func() string {
				base := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
				require.NoError(t, base.ClearAccountRecoveryState(context.Background(), owner.ID))
				stored, err := base.GetByID(context.Background(), owner.ID)
				require.NoError(t, err)
				generation, _ := stored.Extra[service.RuntimeRecoveryGenerationExtraKey].(string)
				require.NotEmpty(t, generation)
				require.NotEqual(t, oldRuntimeRecoveryGeneration, generation)
				return generation
			}
			wantGeneration := oldRuntimeRecoveryGeneration
			if order == "failure after recovery" {
				wantGeneration = reset()
			}
			var queryObserver *runtimeRecoveryAfterOutcomeQuery
			if order == "recovery after statement before return" {
				queryObserver = &runtimeRecoveryAfterOutcomeQuery{sqlExecutor: integrationDB, after: func() { reset() }}
				repo.sql = queryObserver
			}
			result, err := repo.ApplyGrokRequestOutcomeIfUnchanged(context.Background(), owner,
				service.GrokRequestOutcome{Extra: map[string]any{"grok_last_error_reason": "real newly received failure"}})
			require.NoError(t, err)
			require.True(t, result.Applied, "recovery does not revoke a later real upstream failure whose credential/scheduler CAS still matches")
			require.Equal(t, wantGeneration, result.RecoveryGeneration, "publication must use the generation returned by the same account/outbox statement")
			if queryObserver != nil {
				require.True(t, queryObserver.observed)
			}
			if order == "failure before recovery" {
				newGeneration := reset()
				require.NotEqual(t, newGeneration, result.RecoveryGeneration)
			}
			stored, err := repo.GetByID(context.Background(), owner.ID)
			require.NoError(t, err)
			require.Equal(t, "real newly received failure", stored.Extra["grok_last_error_reason"])
		})
	}
}

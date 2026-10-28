//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/adminaccess"
	"github.com/stretchr/testify/require"
)

type scopedRefreshLockRepo struct{ refreshAPIAccountRepo }

func (*scopedRefreshLockRepo) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	return nil, nil
}

// A gateway refresh owns the refresh mutex before persisting its rotated grant.
// A scoped refresh already holds the account row: it must report busy and roll
// back immediately, allowing the original refresh to persist without replay.
func TestScopedAdminRefreshBusyDoesNotWaitWithAccountRowLocked(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		t.Run(platform, func(t *testing.T) {
			account := cnRecoveryAccount(platform)
			account.GroupIDs = []int64{1}
			repo := &scopedRefreshLockRepo{refreshAPIAccountRepo{account: account}}
			api := NewOAuthRefreshAPI(repo, nil)
			issuerStarted, allowIssuerReturn := make(chan struct{}), make(chan struct{})
			ordinary := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{
				"access_token": "rotated-access", "refresh_token": "rotated-refresh",
			}, onRefresh: func() { close(issuerStarted); <-allowIssuerReturn }}
			type outcome struct {
				result *OAuthRefreshResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := api.RefreshIfNeeded(context.Background(), account, ordinary, time.Hour)
				done <- outcome{result, err}
			}()
			<-issuerStarted
			// Always unblock the issuer, including when an assertion fails.
			defer func() {
				select {
				case <-allowIssuerReturn:
				default:
					close(allowIssuerReturn)
				}
			}()
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			defer client.Close()
			admin := &adminServiceImpl{accountRepo: repo, entClient: client}
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs(account.ID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`SELECT id FROM accounts`).WithArgs(account.ID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`SELECT account_id FROM account_groups`).WithArgs(account.ID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectRollback()
			ctx := adminaccess.WithPrincipal(context.Background(), &adminaccess.Principal{Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: []string{"accounts.refresh"}, GroupIDs: []int64{1}, AccountTypes: []string{AccountTypeOAuth}}}}})
			ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
			defer cancel()
			contender := &refreshAPIExecutorStub{needsRefresh: true}
			busy := errors.New("refresh busy")
			_, err = admin.ScopedAdminWrite(ctx, account.ID, "accounts.refresh", nil, func(txctx context.Context, current *Account) (*Account, error) {
				result, err := api.RefreshIfNeeded(txctx, current, contender, time.Hour)
				if err != nil {
					return nil, err
				}
				if result != nil && result.LockHeld {
					return nil, busy
				}
				return nil, errors.New("contending refresh failed to report busy")
			})
			require.ErrorIs(t, err, busy, "the row owner must not wait for the refresh owner to hit its deadline")
			require.NoError(t, ctx.Err())
			require.Zero(t, contender.refreshCalls, "busy scoped refresh must not contact the issuer")
			require.Zero(t, repo.successCASCalls, "busy scoped refresh must not write a grant")
			require.NoError(t, mock.ExpectationsWereMet(), "busy must roll back before the original rotation persists")
			close(allowIssuerReturn)
			completed := <-done
			require.NoError(t, completed.err)
			require.True(t, completed.result.Refreshed)
			require.Equal(t, 1, ordinary.refreshCalls)
			require.Equal(t, 1, repo.successCASCalls)
			require.Equal(t, "rotated-refresh", repo.account.GetCredential("refresh_token"))
		})
	}
}

func TestOAuthRefreshTransactionWithoutContentionPreservesDistributedLockAndCAS(t *testing.T) {
	for _, mode := range []string{"success", "distributed_busy", "distributed_unavailable"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			defer client.Close()
			mock.ExpectBegin()
			tx, err := client.Tx(context.Background())
			require.NoError(t, err)
			ctx := dbent.NewTxContext(context.Background(), tx)
			account := cnRecoveryAccount(PlatformKimi)
			repo := &refreshAPIAccountRepo{account: account}
			cache := &refreshAPICacheStub{lockResult: mode == "success"}
			if mode == "distributed_unavailable" {
				cache.lockErr = errors.New("lock storage unavailable")
			}
			api := NewOAuthRefreshAPI(repo, cache)
			executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new", "refresh_token": "rotated"}}
			result, err := api.RefreshIfNeeded(ctx, account, executor, time.Hour)
			if mode == "success" {
				require.NoError(t, err)
				require.True(t, result.Refreshed)
				require.Equal(t, 1, executor.refreshCalls)
				require.Equal(t, 1, repo.successCASCalls)
				require.Equal(t, 1, cache.releaseCalls)
				mock.ExpectCommit()
				require.NoError(t, tx.Commit())
			} else {
				if mode == "distributed_busy" {
					require.NoError(t, err)
					require.True(t, result.LockHeld)
				} else {
					require.Error(t, err)
				}
				require.Zero(t, executor.refreshCalls)
				require.Zero(t, repo.successCASCalls)
				require.Zero(t, cache.releaseCalls, "never release another refresh owner's distributed lock")
				mock.ExpectRollback()
				require.NoError(t, tx.Rollback())
			}
			require.Zero(t, cache.deleteCalls, "native OAuth must not publish access-token cache state before commit")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

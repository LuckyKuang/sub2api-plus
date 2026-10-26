//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Credential acquisition can fail before inference starts. Its quarantine
// still belongs to the exact login and proxy route that produced that failure.
// These tests exercise the two independent persistence entries, rather than
// treating the already-covered inference-outcome CAS as evidence for them.
func grokCredentialOwnerSnapshotForRepository(t *testing.T, owner *service.Account) service.GrokCredentialMutationSnapshot {
	t.Helper()
	credentials, err := json.Marshal(owner.Credentials)
	require.NoError(t, err)
	return service.GrokCredentialMutationSnapshot{
		CredentialsJSON: string(credentials), AccessToken: owner.GetGrokAccessToken(),
		RefreshToken: owner.GetGrokRefreshToken(), TokenVersion: owner.GetCredentialAsInt64("_token_version"),
		ProxyID: owner.ProxyID, Proxy: owner.Proxy, ProxyMissing: owner.ProxyID != nil && owner.Proxy == nil, Owner: owner,
	}
}

func applyGrokCredentialOwnerFailure(ctx context.Context, repo *accountRepository, owner *service.Account, snapshot service.GrokCredentialMutationSnapshot, mode string, until time.Time) (bool, error) {
	if mode == "permanent" {
		return repo.SetGrokCredentialErrorIfMatch(ctx, owner.ID, snapshot, string(service.GrokCredentialReasonRevoked))
	}
	return repo.SetGrokCredentialTempUnschedulableIfMatch(ctx, owner.ID, snapshot, until, string(service.GrokCredentialReasonRefreshTransient))
}

func grokCredentialOwnerStoredRow(t *testing.T, id int64) string {
	t.Helper()
	var row string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1`, id).Scan(&row))
	return row
}

func assertGrokCredentialOwnerUnchanged(t *testing.T, id int64, before string, cache *schedulerCacheRecorder) {
	t.Helper()
	require.JSONEq(t, before, grokCredentialOwnerStoredRow(t, id), "a rejected credential failure must leave the complete current account row unchanged")
	require.Zero(t, cnControlOutboxCount(t, id))
	require.Empty(t, cache.setAccounts)
	require.Empty(t, cache.deleteIDs)
}

func TestGrokCredentialFailureCASPublishesOnlyCapturedRoute(t *testing.T) {
	for _, mode := range []string{"permanent", "transient"} {
		for _, route := range []string{"direct", "bound", "proxy metadata changed"} {
			t.Run(mode+"/"+route, func(t *testing.T) {
				repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
				if route != "direct" {
					expected = bindCNProxyOwner(t, repo, expected)
				}
				snapshot := grokCredentialOwnerSnapshotForRepository(t, expected)
				if route == "proxy metadata changed" {
					_, err := integrationDB.ExecContext(context.Background(), "UPDATE proxies SET name='new display name' WHERE id=$1", *expected.ProxyID)
					require.NoError(t, err)
				}
				until := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
				applied, err := applyGrokCredentialOwnerFailure(context.Background(), repo, expected, snapshot, mode, until)
				require.NoError(t, err)
				require.True(t, applied, "a matching login and route must still receive its real credential failure")
				got, err := repo.GetByID(context.Background(), expected.ID)
				require.NoError(t, err)
				require.Equal(t, expected.Credentials, got.Credentials)
				require.Equal(t, expected.Extra, got.Extra)
				require.Equal(t, expected.ProxyID, got.ProxyID)
				if mode == "permanent" {
					require.Equal(t, service.StatusError, got.Status)
					require.False(t, got.Schedulable)
					require.Equal(t, string(service.GrokCredentialReasonRevoked), got.ErrorMessage)
					require.Nil(t, got.TempUnschedulableUntil)
				} else {
					require.Equal(t, service.StatusActive, got.Status)
					require.True(t, got.Schedulable)
					require.Empty(t, got.ErrorMessage)
					require.Equal(t, &until, got.TempUnschedulableUntil)
					require.Equal(t, string(service.GrokCredentialReasonRefreshTransient), got.TempUnschedulableReason)
				}
				require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
				require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
			})
		}
	}
}

func TestGrokCredentialFailureCASRejectsSameRowProxyChanges(t *testing.T) {
	for _, mode := range []string{"permanent", "transient"} {
		for _, change := range []struct{ name, sql string }{
			{"host", "host='replacement.example'"}, {"port", "port=9090"},
			{"username", "username='replacement-user'"}, {"password", "password='replacement-secret'"},
			{"protocol", "protocol='socks5'"}, {"disabled", "status='inactive'"},
			{"expired", "expires_at=NOW()"}, {"deleted", "deleted_at=NOW()"},
		} {
			t.Run(mode+"/"+change.name, func(t *testing.T) {
				repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
				expected = bindCNProxyOwner(t, repo, expected)
				snapshot := grokCredentialOwnerSnapshotForRepository(t, expected)
				_, err := integrationDB.ExecContext(context.Background(), "UPDATE proxies SET "+change.sql+" WHERE id=$1", *expected.ProxyID)
				require.NoError(t, err)
				before := grokCredentialOwnerStoredRow(t, expected.ID)
				applied, err := applyGrokCredentialOwnerFailure(context.Background(), repo, expected, snapshot, mode, time.Now().Add(time.Hour))
				require.NoError(t, err)
				require.False(t, applied, "the same proxy ID cannot authorize an old endpoint or authentication failure against its replacement")
				assertGrokCredentialOwnerUnchanged(t, expected.ID, before, cache)
			})
		}
	}
}

func TestGrokCredentialFailureCASWaitsForConcurrentOwnerOrProxyEdit(t *testing.T) {
	for _, mode := range []string{"permanent", "transient"} {
		for _, target := range []string{"account", "proxy"} {
			t.Run(mode+"/"+target, func(t *testing.T) {
				repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
				expected = bindCNProxyOwner(t, repo, expected)
				snapshot := grokCredentialOwnerSnapshotForRepository(t, expected)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				tx, err := integrationDB.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				var holderPID int
				require.NoError(t, tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&holderPID))
				if target == "account" {
					_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"sub":"replacement-owner"}'::jsonb WHERE id=$1`, expected.ID)
				} else {
					_, err = tx.ExecContext(ctx, "UPDATE proxies SET password='replacement-secret' WHERE id=$1", *expected.ProxyID)
				}
				require.NoError(t, err)
				type result struct {
					applied bool
					err     error
				}
				done := make(chan result, 1)
				go func() {
					applied, err := applyGrokCredentialOwnerFailure(ctx, repo, expected, snapshot, mode, time.Now().Add(time.Hour))
					done <- result{applied, err}
				}()
				require.Eventually(t, func() bool {
					var waiting bool
					err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND $1=ANY(pg_blocking_pids(pid)))`, holderPID).Scan(&waiting)
					return err == nil && waiting
				}, 3*time.Second, 10*time.Millisecond, "credential quarantine must wait for the active writer and recheck its committed owner")
				require.NoError(t, tx.Commit())
				select {
				case outcome := <-done:
					require.NoError(t, outcome.err)
					require.False(t, outcome.applied)
				case <-ctx.Done():
					t.Fatal("credential quarantine did not finish after the owner writer committed")
				}
				got, err := repo.GetByID(context.Background(), expected.ID)
				require.NoError(t, err)
				require.Equal(t, service.StatusActive, got.Status)
				require.True(t, got.Schedulable)
				require.Empty(t, got.ErrorMessage)
				require.Nil(t, got.TempUnschedulableUntil)
				require.Equal(t, expected.Extra, got.Extra)
				require.Zero(t, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts)
			})
		}
	}
}

func TestGrokCredentialFailureCASOutboxFailureRollsBackState(t *testing.T) {
	for _, mode := range []string{"permanent", "transient"} {
		t.Run(mode, func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			expected = bindCNProxyOwner(t, repo, expected)
			snapshot := grokCredentialOwnerSnapshotForRepository(t, expected)
			before := grokCredentialOwnerStoredRow(t, expected.ID)
			repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
			applied, err := applyGrokCredentialOwnerFailure(context.Background(), repo, expected, snapshot, mode, time.Now().Add(time.Hour))
			require.Error(t, err)
			require.False(t, applied)
			assertGrokCredentialOwnerUnchanged(t, expected.ID, before, cache)
		})
	}
}

func TestGrokCredentialFailureCASCallerTransactionDoesNotPublishBeforeCommit(t *testing.T) {
	for _, mode := range []string{"permanent", "transient"} {
		for _, boundary := range []string{"context rollback", "managed rollback", "managed commit"} {
			t.Run(mode+"/"+boundary, func(t *testing.T) {
				repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
				expected = bindCNProxyOwner(t, repo, expected)
				snapshot := grokCredentialOwnerSnapshotForRepository(t, expected)
				before := grokCredentialOwnerStoredRow(t, expected.ID)
				rollback := errors.New("deliberate business rollback")
				write := func(writeCtx context.Context, writer *accountRepository) error {
					applied, err := applyGrokCredentialOwnerFailure(writeCtx, writer, expected, snapshot, mode, time.Now().Add(time.Hour))
					require.NoError(t, err)
					require.True(t, applied)
					require.JSONEq(t, before, grokCredentialOwnerStoredRow(t, expected.ID), "uncommitted quarantine must not be visible to another database session")
					require.Zero(t, cnControlOutboxCount(t, expected.ID), "outbox cannot escape the caller's transaction")
					require.Empty(t, cache.setAccounts, "scheduler cache cannot publish uncommitted quarantine")
					if boundary == "managed commit" {
						return nil
					}
					return rollback
				}
				if boundary == "context rollback" {
					tx, err := integrationEntClient.Tx(context.Background())
					require.NoError(t, err)
					defer func() { _ = tx.Rollback() }()
					require.ErrorIs(t, write(dbent.NewTxContext(context.Background(), tx), repo), rollback)
					require.NoError(t, tx.Rollback())
				} else {
					err := repo.RunOpenAIOAuthPolicyTransaction(context.Background(), func(writeCtx context.Context, repository service.AccountRepository) error {
						writer, ok := repository.(*accountRepository)
						require.True(t, ok)
						return write(writeCtx, writer)
					})
					if boundary == "managed commit" {
						require.NoError(t, err)
						require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
						require.Empty(t, cache.setAccounts, "only committed invalidation may be published")
						require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
						return
					}
					require.ErrorIs(t, err, rollback)
				}
				assertGrokCredentialOwnerUnchanged(t, expected.ID, before, cache)
			})
		}
	}
}

func TestGrokCredentialFailureCASMissingProxyQuarantineRejectsRestoration(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "still missing", true: "same ID restored"}[restored], func(t *testing.T) {
			repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
			expected = bindCNProxyOwner(t, repo, expected)
			_, err := integrationDB.ExecContext(context.Background(), "UPDATE proxies SET deleted_at=NOW() WHERE id=$1", *expected.ProxyID)
			require.NoError(t, err)
			// The selected owner retains its configured ID and missing hydrated
			// edge. This structural failure must not authorize a direct request.
			expected.Proxy = nil
			snapshot := grokCredentialOwnerSnapshotForRepository(t, expected)
			if restored {
				_, err = integrationDB.ExecContext(context.Background(), "UPDATE proxies SET deleted_at=NULL WHERE id=$1", *expected.ProxyID)
				require.NoError(t, err)
			}
			before := grokCredentialOwnerStoredRow(t, expected.ID)
			applied, err := repo.SetGrokCredentialErrorIfMatch(context.Background(), expected.ID, snapshot, string(service.GrokCredentialReasonProxyInvalid))
			require.NoError(t, err)
			if restored {
				require.False(t, applied)
				assertGrokCredentialOwnerUnchanged(t, expected.ID, before, cache)
				return
			}
			require.True(t, applied, "a persistently missing configured proxy must still quarantine only its selected account")
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusError, got.Status)
			require.False(t, got.Schedulable)
			require.Equal(t, string(service.GrokCredentialReasonProxyInvalid), got.ErrorMessage)
			require.Equal(t, expected.Credentials, got.Credentials)
			require.Equal(t, expected.ProxyID, got.ProxyID)
			require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
			require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
		})
	}
}

func TestGrokCredentialFailureCASWaitsForConcurrentMissingProxyRestoration(t *testing.T) {
	repo, expected, cache := newGrokRequestOutcomeFixture(t, service.AccountTypeOAuth)
	expected = bindCNProxyOwner(t, repo, expected)
	_, err := integrationDB.ExecContext(context.Background(), "UPDATE proxies SET deleted_at=NOW() WHERE id=$1", *expected.ProxyID)
	require.NoError(t, err)
	expected.Proxy = nil
	snapshot := grokCredentialOwnerSnapshotForRepository(t, expected)
	before := grokCredentialOwnerStoredRow(t, expected.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var holderPID int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&holderPID))
	_, err = tx.ExecContext(ctx, "UPDATE proxies SET deleted_at=NULL WHERE id=$1", *expected.ProxyID)
	require.NoError(t, err)
	type result struct {
		applied bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		applied, err := repo.SetGrokCredentialErrorIfMatch(ctx, expected.ID, snapshot, string(service.GrokCredentialReasonProxyInvalid))
		done <- result{applied, err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND $1=ANY(pg_blocking_pids(pid)))`, holderPID).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "the missing-edge check must wait for the writer restoring that same proxy ID")
	require.NoError(t, tx.Commit())
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.False(t, outcome.applied, "a proxy committed restored before quarantine must win the recheck")
	case <-ctx.Done():
		t.Fatal("missing-proxy quarantine did not finish after restoration committed")
	}
	assertGrokCredentialOwnerUnchanged(t, expected.ID, before, cache)
}

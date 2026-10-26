//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// A proxy binding includes its route and authentication, not just its database
// ID. An old login/control response cannot publish credentials, quota or wallet
// scheduling after that same proxy row is edited. This is the outbound-owner
// snapshot contract in docs/OUTBOUND_IDENTITY.md and DOMESTIC_OAUTH.md.
func bindCNProxyOwner(t *testing.T, repo *accountRepository, expected *service.Account) *service.Account {
	t.Helper()
	proxy := mustCreateProxy(t, integrationEntClient, &service.Proxy{
		Name: fmt.Sprintf("cn-route-owner-%d", expected.ID), Protocol: "http", Host: "original.example", Port: 8080,
		Username: "original-user", Password: "original-password", Status: service.StatusActive,
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "UPDATE accounts SET proxy_id=NULL WHERE id=$1", expected.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM proxies WHERE id=$1", proxy.ID)
	})
	_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET proxy_id=$2 WHERE id=$1", expected.ID, proxy.ID)
	require.NoError(t, err)
	bound, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.NotNil(t, bound.Proxy)
	return bound
}

type integrationCNLoginWriter interface {
	CompleteCNOAuthLoginIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error)
}

func applyCNProxyOwnerResult(t *testing.T, ctx context.Context, repo *accountRepository, expected *service.Account, operation string) (bool, error) {
	t.Helper()
	switch operation {
	case "observation":
		return repo.UpdateCNControlExtraIfUnchanged(ctx, expected, map[string]any{"kimi_5h_used_percent": 23.0})
	case "refresh":
		return repo.UpdateCNOAuthCredentialsIfUnchanged(ctx, expected, map[string]any{"oauth_provider": "kimi", "access_token": "late-rotation"})
	case "wallet":
		until := time.Now().Add(time.Hour)
		return repo.UpdateCNBalanceScheduleIfUnchanged(ctx, expected, &until, "cn_balance_low: old-route result")
	case "login":
		writer, ok := any(repo).(integrationCNLoginWriter)
		require.True(t, ok, "reauthorization needs atomic owner comparison, credential persistence and recovery")
		return writer.CompleteCNOAuthLoginIfUnchanged(ctx, expected, map[string]any{"oauth_provider": "kimi", "access_token": "new-authorized-login"})
	default:
		t.Fatalf("unknown operation %q", operation)
		return false, nil
	}
}

func TestCNProxyOwnerCASRejectsSameRowRouteEditsWithoutSideEffects(t *testing.T) {
	for _, operation := range []string{"observation", "refresh", "wallet", "login"} {
		for _, mutation := range []string{"host='replacement.example'", "port=9090", "username='replacement-user'", "password='replacement-password'", "protocol='socks5'", "status='inactive'", "expires_at=NOW()", "deleted_at=NOW()"} {
			t.Run(operation+"/"+mutation, func(t *testing.T) {
				repo, expected, cache := newCNControlSnapshotFixture(t)
				if operation == "wallet" {
					_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='deepseek', credentials='{"oauth_provider":"deepseek","access_token":"wallet-owner"}'::jsonb,status='active',schedulable=TRUE WHERE id=$1`, expected.ID)
					require.NoError(t, err)
				}
				expected = bindCNProxyOwner(t, repo, expected)
				_, err := integrationDB.ExecContext(context.Background(), "UPDATE proxies SET "+mutation+" WHERE id=$1", *expected.ProxyID)
				require.NoError(t, err)
				applied, err := applyCNProxyOwnerResult(t, context.Background(), repo, expected, operation)
				require.NoError(t, err)
				require.False(t, applied, "the old route cannot authorize a state change for the edited binding")
				got, err := repo.GetByID(context.Background(), expected.ID)
				require.NoError(t, err)
				require.Equal(t, expected.Credentials, got.Credentials)
				require.Equal(t, expected.Extra, got.Extra)
				require.Equal(t, expected.TempUnschedulableUntil, got.TempUnschedulableUntil)
				require.Equal(t, expected.TempUnschedulableReason, got.TempUnschedulableReason)
				require.Zero(t, cnControlOutboxCount(t, expected.ID))
				require.Empty(t, cache.setAccounts)
			})
		}
	}
}

func TestCNProxyOwnerCASWaitsForConcurrentProxyEditAndRechecks(t *testing.T) {
	for _, operation := range []string{"observation", "refresh", "wallet", "login"} {
		t.Run(operation, func(t *testing.T) {
			repo, expected, cache := newCNControlSnapshotFixture(t)
			if operation == "wallet" {
				_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='deepseek', credentials='{"oauth_provider":"deepseek","access_token":"wallet-owner"}'::jsonb,status='active',schedulable=TRUE WHERE id=$1`, expected.ID)
				require.NoError(t, err)
			}
			expected = bindCNProxyOwner(t, repo, expected)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			_, err = tx.ExecContext(ctx, "UPDATE proxies SET password='new-proxy-secret' WHERE id=$1", *expected.ProxyID)
			require.NoError(t, err)
			// Resolve a missing business port before starting a goroutine so a
			// pre-fix failure is reported as an assertion, without orphan work.
			if operation == "login" {
				_, ok := any(repo).(integrationCNLoginWriter)
				require.True(t, ok, "reauthorization must persist through an atomic login owner port")
			}
			type result struct {
				applied bool
				err     error
			}
			done := make(chan result, 1)
			go func() {
				applied, err := applyCNProxyOwnerResult(t, ctx, repo, expected, operation)
				done <- result{applied, err}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%/* cn_%_cas */%')`).Scan(&waiting)
				return err == nil && waiting
			}, time.Second, 10*time.Millisecond, "persistence must wait for the proxy writer rather than use an MVCC snapshot of the old endpoint")
			require.NoError(t, tx.Commit())
			got := <-done
			require.NoError(t, got.err)
			require.False(t, got.applied)
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}

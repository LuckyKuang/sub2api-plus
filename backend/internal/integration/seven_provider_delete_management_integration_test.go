//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Observe the real admin service's already-completed advisory count, then let
// another instance bind the proxy before the production deletion boundary.
// The observer does not replace the repository's delete or binding behavior.
type sevenHeldProxyCount struct {
	service.ProxyRepository
	counted chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *sevenHeldProxyCount) CountAccountsByProxyID(ctx context.Context, id int64) (int64, error) {
	count, err := r.ProxyRepository.CountAccountsByProxyID(ctx, id)
	r.once.Do(func() { close(r.counted) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	return count, err
}

func TestSevenProviderProxyDeletionRechecksBindingAfterAdminPreflight(t *testing.T) {
	services := newSevenBusinessServices(t)
	proxyRepo, accountRepo, proxy, account := sevenExpiryStorageFixture(t, services)
	ctx := context.Background()
	account.ProxyID = nil
	require.NoError(t, accountRepo.Update(ctx, account))
	observed := &sevenHeldProxyCount{ProxyRepository: proxyRepo, counted: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(observed.release) }) }
	t.Cleanup(release)
	admin := service.NewAdminService(&config.Config{}, nil, nil, nil, observed, nil, nil, nil, nil, nil,
		nil, nil, nil, services.ent, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	done := make(chan error, 1)
	go func() { done <- admin.DeleteProxy(ctx, proxy.ID) }()
	select {
	case <-observed.counted:
	case <-time.After(5 * time.Second):
		t.Fatal("admin deletion did not finish its original unbound preflight")
	}
	account.ProxyID = &proxy.ID
	require.NoError(t, accountRepo.Update(ctx, account), "the concurrent binding wins before deletion obtains its durable lock")
	release()
	select {
	case err := <-done:
		require.ErrorIs(t, err, service.ErrProxyInUse, "an old zero-count must not authorize deleting a now-bound route")
	case <-time.After(5 * time.Second):
		t.Fatal("admin deletion did not finish")
	}
	current, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Proxy)
	require.Equal(t, proxy.ID, current.Proxy.ID)
	_, err = proxyRepo.GetByID(ctx, proxy.ID)
	require.NoError(t, err)
}

// A soft-deleted proxy still satisfies the SQL foreign key. Every saved-binding
// writer must therefore lock and recheck the live row, not rely on that FK or
// a UI list captured before deletion. Exercise all shared management writers.
func TestSevenProviderBindingWritersRejectProxyDeletedByConcurrentInstance(t *testing.T) {
	services := newSevenBusinessServices(t)
	for _, operation := range []string{"create", "update", "bulk-update", "revert"} {
		t.Run(operation, func(t *testing.T) {
			proxyRepo, accountRepo, proxy, account := sevenExpiryStorageFixture(t, services)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			account.ProxyID = nil
			require.NoError(t, accountRepo.Update(ctx, account))
			if operation == "revert" {
				_, err := services.db.ExecContext(ctx, "UPDATE accounts SET proxy_fallback_origin_id=$2 WHERE id=$1", account.ID, proxy.ID)
				require.NoError(t, err)
			}
			tx, err := services.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			_, err = tx.ExecContext(ctx, "UPDATE proxies SET deleted_at=NOW() WHERE id=$1", proxy.ID)
			require.NoError(t, err)
			created := &service.Account{Name: "deleted-proxy-create-" + uuid.NewString(), Platform: service.PlatformKimi,
				Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				ProxyID: &proxy.ID, Credentials: map[string]any{"api_key": "cannot-bind-deleted-route"}}
			t.Cleanup(func() {
				if created.ID != 0 {
					_, err := services.db.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", created.ID)
					require.NoError(t, err)
					_, err = services.db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", created.ID)
					require.NoError(t, err)
				}
			})
			done := make(chan error, 1)
			go func() {
				switch operation {
				case "create":
					done <- accountRepo.Create(ctx, created)
				case "update":
					account.ProxyID = &proxy.ID
					done <- accountRepo.Update(ctx, account)
				case "bulk-update":
					_, err := accountRepo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{ProxyID: &proxy.ID})
					done <- err
				case "revert":
					done <- accountRepo.RevertProxyFallback(ctx, account.ID)
				}
			}()
			var early error
			finished := false
			require.Eventually(t, func() bool {
				select {
				case early = <-done:
					finished = true
					return true
				default:
				}
				var waiting bool
				err := services.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%proxies%')`).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond, "binding must either reject or wait for the concurrent proxy deletion")
			require.NoError(t, tx.Commit())
			if !finished {
				early = <-done
			}
			require.ErrorIs(t, early, service.ErrProxyNotFound, "a completed soft deletion must reject the stale binding rather than publish an unusable credential owner")
			_, err = proxyRepo.GetByID(ctx, proxy.ID)
			require.ErrorIs(t, err, service.ErrProxyNotFound)
			current, err := accountRepo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Nil(t, current.ProxyID, "failed binding must preserve the original explicit direct route")
			if operation == "revert" {
				require.Equal(t, &proxy.ID, current.ProxyFallbackOriginID, "failed restore must retain the original recovery record")
			}
		})
	}
}

func TestSevenProviderAccountDeletionOutboxFailurePreservesAccountAndCache(t *testing.T) {
	services := newSevenBusinessServices(t)
	_, accountRepo, _, account := sevenExpiryStorageFixture(t, services)
	ctx := context.Background()
	prefix := "seven-delete-fault:" + uuid.NewString() + ":"
	t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
	rdb := services.namespacedRedis(t, prefix)
	cache := repository.NewSchedulerCache(rdb)
	owner, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NoError(t, cache.SetAccount(ctx, owner))
	writer := repository.NewAccountRepository(services.ent, services.db, cache)
	function := "delete_outbox_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = services.db.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fault$
		BEGIN
			IF NEW.account_id=%d THEN RAISE EXCEPTION 'delete outbox storage fault'; END IF;
			RETURN NEW;
		END $fault$`, function, account.ID))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DROP FUNCTION "+function+"() CASCADE")
		require.NoError(t, err)
	})
	_, err = services.db.ExecContext(ctx, "CREATE TRIGGER "+function+" BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION "+function+"()")
	require.NoError(t, err)
	err = writer.Delete(ctx, account.ID)
	require.ErrorContains(t, err, "delete outbox storage fault")
	stored, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, owner.Credentials, stored.Credentials)
	require.Equal(t, owner.ProxyID, stored.ProxyID)
	cached, err := cache.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, cached, "a failed transaction must not publish a premature cache deletion")
	require.Equal(t, owner.Credentials, cached.Credentials)
}

func TestSevenProviderAccountDeletionRespectsCallerTransactionRollback(t *testing.T) {
	services := newSevenBusinessServices(t)
	ctx := context.Background()
	_, accountRepo, _, account := sevenExpiryStorageFixture(t, services)
	prefix := "seven-delete-rollback:" + uuid.NewString() + ":"
	t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
	rdb := services.namespacedRedis(t, prefix)
	cache := repository.NewSchedulerCache(rdb)
	owner, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NoError(t, cache.SetAccount(ctx, owner))
	writer := repository.NewAccountRepository(services.ent, services.db, cache)
	tx, err := services.ent.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	require.NoError(t, writer.Delete(dbent.NewTxContext(ctx, tx), account.ID))
	_, err = accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err, "a caller's uncommitted deletion cannot become visible to another instance")
	cached, err := cache.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, cached, "cache invalidation cannot precede caller commit")
	require.NoError(t, tx.Rollback())
	stored, err := accountRepo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, owner.Credentials, stored.Credentials)
	require.Equal(t, owner.ProxyID, stored.ProxyID)
}

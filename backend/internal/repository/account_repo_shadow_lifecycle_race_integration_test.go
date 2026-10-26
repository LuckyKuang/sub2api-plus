//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func shadowLifecycleAdmin(repo service.AdminAccountRepository) service.AdminService {
	return service.NewAdminService(&config.Config{}, nil, nil, repo, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, integrationEntClient, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func newShadowRaceFixture(t *testing.T) (*accountRepository, *service.Account, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	parent := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: fmt.Sprintf("shadow-race-parent-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI,
		Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "preserved-parent-token"},
	})
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE id=$1 OR parent_account_id=$1)`, parent.ID)
		_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE parent_account_id=$1`, parent.ID)
		_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, parent.ID)
	})
	cache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{parent.ID: parent}}
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, cache)
	storedParent, err := repo.GetByID(context.Background(), parent.ID)
	require.NoError(t, err)
	parent = storedParent
	cache.accounts[parent.ID] = parent
	shadow := &service.Account{
		Name: "concurrent spark shadow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, ParentAccountID: &parent.ID,
		QuotaDimension: service.QuotaDimensionSpark, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
	}
	return repo, parent, shadow, cache
}

func createShadowRaceRecord(ctx context.Context, repo *accountRepository, method string, shadow *service.Account) error {
	switch method {
	case "Create":
		return repo.Create(ctx, shadow)
	case "CreateWithAccountGroups":
		return repo.CreateWithAccountGroups(ctx, shadow, nil)
	case "AdminCreateShadow":
		created, err := shadowLifecycleAdmin(repo).CreateShadow(ctx, *shadow.ParentAccountID, service.ShadowOptions{})
		if err == nil {
			*shadow = *created
		}
		return err
	default:
		return fmt.Errorf("unknown shadow creation method %q", method)
	}
}

func TestShadowCreationRejectsDeletedCredentialParentWithoutSideEffects(t *testing.T) {
	for _, method := range []string{"Create", "CreateWithAccountGroups"} {
		t.Run(method, func(t *testing.T) {
			repo, parent, shadow, cache := newShadowRaceFixture(t)
			require.NoError(t, shadowLifecycleAdmin(repo).DeleteAccount(context.Background(), parent.ID))
			deleteIDs := append([]int64(nil), cache.deleteIDs...)
			err := createShadowRaceRecord(context.Background(), repo, method, shadow)
			require.ErrorIs(t, err, service.ErrAccountNotFound, "a soft-deleted parent is no longer a usable credential owner")
			var liveChildren, childOutbox int
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM accounts WHERE parent_account_id=$1 AND deleted_at IS NULL`, parent.ID).Scan(&liveChildren))
			require.Zero(t, liveChildren)
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE parent_account_id=$1)`, parent.ID).Scan(&childOutbox))
			require.Zero(t, childOutbox)
			require.Equal(t, deleteIDs, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
			require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
		})
	}
}

// Hold the production family enumeration after its actual PostgreSQL read. The
// service and repository still perform all mutations and transaction management.
type shadowDeletionEnumerationHold struct {
	service.AdminAccountRepository
	listed chan struct{}
	resume chan struct{}
}

func (r *shadowDeletionEnumerationHold) RunOpenAIOAuthPolicyTransaction(ctx context.Context, fn func(context.Context, service.AccountRepository) error) error {
	writer := r.AdminAccountRepository.(service.OpenAIOAuthPolicyTransactionRepository)
	return writer.RunOpenAIOAuthPolicyTransaction(ctx, func(writeCtx context.Context, repo service.AccountRepository) error {
		return fn(writeCtx, &shadowDeletionEnumerationHold{AdminAccountRepository: repo.(service.AdminAccountRepository), listed: r.listed, resume: r.resume})
	})
}

func (r *shadowDeletionEnumerationHold) LockAccountCredentialFamily(ctx context.Context, id int64) error {
	writer, ok := r.AdminAccountRepository.(interface {
		LockAccountCredentialFamily(context.Context, int64) error
	})
	if !ok {
		return errors.New("credential family locker is unavailable")
	}
	return writer.LockAccountCredentialFamily(ctx, id)
}

func (r *shadowDeletionEnumerationHold) ListShadowsByParent(ctx context.Context, id int64) ([]*service.Account, error) {
	shadows, err := r.AdminAccountRepository.ListShadowsByParent(ctx, id)
	close(r.listed)
	select {
	case <-r.resume:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return shadows, err
}

func shadowRaceBlockedQuery(ctx context.Context) bool {
	var waiting bool
	err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'
		AND (query LIKE '%/* account_group_account_lock */%' OR query LIKE '%/* account_credential_family_lock */%' OR query LIKE '%/* account_live_credential_parent_lock */%' OR query LIKE '%INSERT INTO "accounts"%')
	)`).Scan(&waiting)
	return err == nil && waiting
}

func TestAdminShadowDeletionPreventsCreationAfterFamilyEnumeration(t *testing.T) {
	for _, method := range []string{"Create", "CreateWithAccountGroups", "AdminCreateShadow"} {
		t.Run(method, func(t *testing.T) {
			repo, parent, shadow, cache := newShadowRaceFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			held := &shadowDeletionEnumerationHold{AdminAccountRepository: repo, listed: make(chan struct{}), resume: make(chan struct{})}
			deleteDone := make(chan error, 1)
			go func() { deleteDone <- shadowLifecycleAdmin(held).DeleteAccount(ctx, parent.ID) }()
			select {
			case <-held.listed:
			case <-ctx.Done():
				t.Fatal("family enumeration did not reach the controlled boundary")
			}
			createDone := make(chan error, 1)
			go func() { createDone <- createShadowRaceRecord(ctx, repo, method, shadow) }()
			var createErr error
			completed := false
			require.Eventually(t, func() bool {
				select {
				case createErr = <-createDone:
					completed = true
					return true
				default:
					return shadowRaceBlockedQuery(ctx)
				}
			}, 3*time.Second, 10*time.Millisecond, "creation must either reach the database lock or complete")
			close(held.resume)
			require.NoError(t, <-deleteDone)
			if !completed {
				createErr = <-createDone
			}
			require.ErrorIs(t, createErr, service.ErrAccountNotFound, "a deletion that has enumerated its family cannot permit a new credential-dependent child")
			var orphaned int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE parent_account_id=$1 AND deleted_at IS NULL`, parent.ID).Scan(&orphaned))
			require.Zero(t, orphaned)
			require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
			require.Equal(t, []int64{parent.ID}, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestAdminShadowDeletionRechecksFamilyAfterInFlightCreation(t *testing.T) {
	for _, method := range []string{"Create", "CreateWithAccountGroups"} {
		for _, commit := range []bool{false, true} {
			t.Run(method+"/"+map[bool]string{false: "creation rollback", true: "creation commit"}[commit], func(t *testing.T) {
				repo, parent, shadow, cache := newShadowRaceFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				tx, err := integrationEntClient.Tx(ctx)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				writeCtx := dbent.NewTxContext(ctx, tx)
				createRepo := newAccountRepositoryWithSQL(tx.Client(), tx.Client(), nil)
				require.NoError(t, createShadowRaceRecord(writeCtx, createRepo, method, shadow))
				creationRows, err := tx.Client().QueryContext(writeCtx, `SELECT count(*) FROM scheduler_outbox WHERE account_id=$1 AND event_type=$2`, shadow.ID, service.SchedulerOutboxEventAccountChanged)
				require.NoError(t, err)
				require.True(t, creationRows.Next())
				var creationEvents int
				require.NoError(t, creationRows.Scan(&creationEvents))
				require.NoError(t, creationRows.Close())
				require.Equal(t, 1, creationEvents, "creation must enqueue its invalidation inside the owning transaction")
				deleteDone := make(chan error, 1)
				go func() { deleteDone <- shadowLifecycleAdmin(repo).DeleteAccount(ctx, parent.ID) }()
				require.Eventually(t, func() bool { return shadowRaceBlockedQuery(ctx) }, 3*time.Second, 10*time.Millisecond, "deletion must wait for the uncommitted parent/child relationship")
				if commit {
					require.NoError(t, tx.Commit())
				} else {
					require.NoError(t, tx.Rollback())
				}
				require.NoError(t, <-deleteDone)
				_, err = repo.GetByID(ctx, parent.ID)
				require.ErrorIs(t, err, service.ErrAccountNotFound)
				_, err = repo.GetByID(ctx, shadow.ID)
				require.ErrorIs(t, err, service.ErrAccountNotFound, "a child committed while deletion waits must be included in the final family scan")
				var orphaned int
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE parent_account_id=$1 AND deleted_at IS NULL`, parent.ID).Scan(&orphaned))
				require.Zero(t, orphaned)
				require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
				if commit {
					require.Equal(t, 1, cnControlOutboxCount(t, shadow.ID), "identical pending account invalidations coalesce while still reloading the committed deleted state")
					var eventType string
					require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT event_type FROM scheduler_outbox WHERE account_id=$1`, shadow.ID).Scan(&eventType))
					require.Equal(t, service.SchedulerOutboxEventAccountChanged, eventType)
					require.ElementsMatch(t, []int64{parent.ID, shadow.ID}, cache.deleteIDs)
				} else {
					require.Zero(t, cnControlOutboxCount(t, shadow.ID))
					require.Equal(t, []int64{parent.ID}, cache.deleteIDs)
				}
				require.Empty(t, cache.setAccounts)
			})
		}
	}
}

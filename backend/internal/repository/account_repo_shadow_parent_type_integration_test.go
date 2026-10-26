//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func commitShadowParentSetupToken(ctx context.Context, repo service.AdminAccountRepository, parent *service.Account) (*service.Account, error) {
	return shadowLifecycleAdmin(repo).(service.OAuthCredentialCommitter).CommitOAuthCredentials(
		ctx, parent, service.AccountTypeSetupToken, map[string]any{"access_token": "new-setup-credential"}, nil, false)
}

func TestOAuthCredentialTypeChangeRejectsShadowCreatedAfterPreflight(t *testing.T) {
	repo, parent, _, cache := newShadowRaceFixture(t)
	held := &adminOAuthHeldRepository{AdminAccountRepository: repo}
	var shadow *service.Account
	held.before = func() {
		var err error
		shadow, err = shadowLifecycleAdmin(repo).CreateShadow(context.Background(), parent.ID, service.ShadowOptions{})
		require.NoError(t, err)
	}
	updated, err := commitShadowParentSetupToken(context.Background(), held, parent)
	require.Equal(t, int32(1), held.observed.Load(), "the real type write must reach the post-preflight boundary")
	require.Error(t, err, "a new shadow still requires its parent to remain OpenAI OAuth")
	require.Nil(t, updated)
	stored, err := repo.GetByID(context.Background(), parent.ID)
	require.NoError(t, err)
	require.Equal(t, service.AccountTypeOAuth, stored.Type)
	require.Equal(t, parent.Credentials, stored.Credentials)
	children, err := repo.ListShadowsByParent(context.Background(), parent.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	require.Equal(t, shadow.ID, children[0].ID)
	require.Zero(t, cnControlOutboxCount(t, parent.ID))
	require.Equal(t, 1, cnControlOutboxCount(t, shadow.ID))
	require.Empty(t, cache.setAccounts)
	require.Empty(t, cache.deleteIDs)
}

type shadowCreationParentTypeChange struct {
	service.AdminAccountRepository
	before func()
}

func (r *shadowCreationParentTypeChange) CreateWithAccountGroups(ctx context.Context, account *service.Account, groups []service.AccountGroup) error {
	r.before()
	return r.AdminAccountRepository.CreateWithAccountGroups(ctx, account, groups)
}

func TestAdminShadowCreationRechecksParentTypeAtPersistence(t *testing.T) {
	repo, parent, _, cache := newShadowRaceFixture(t)
	held := &shadowCreationParentTypeChange{AdminAccountRepository: repo}
	held.before = func() {
		changed, err := commitShadowParentSetupToken(context.Background(), repo, parent)
		require.NoError(t, err)
		require.Equal(t, service.AccountTypeSetupToken, changed.Type)
	}
	shadow, createErr := shadowLifecycleAdmin(held).CreateShadow(context.Background(), parent.ID, service.ShadowOptions{})
	require.Error(t, createErr, "a parent that stopped being OpenAI OAuth cannot gain a new Spark shadow")
	require.Nil(t, shadow)
	stored, err := repo.GetByID(context.Background(), parent.ID)
	require.NoError(t, err)
	require.Equal(t, service.AccountTypeSetupToken, stored.Type)
	require.Equal(t, "new-setup-credential", stored.GetCredential("access_token"))
	children, err := repo.ListShadowsByParent(context.Background(), parent.ID)
	require.NoError(t, err)
	require.Empty(t, children)
	var childEvents int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE parent_account_id=$1)`, parent.ID).Scan(&childEvents))
	require.Zero(t, childEvents)
	require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
	require.Equal(t, service.AccountTypeSetupToken, requireEvictedSchedulerAccount(t, repo, cache, parent.ID).Type)
}

func TestOAuthCredentialTypeChangeRechecksShadowCommittedWhileWaiting(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "creation rollback", true: "creation commit"}[commit], func(t *testing.T) {
			repo, parent, shadow, cache := newShadowRaceFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := integrationEntClient.Tx(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			createRepo := newAccountRepositoryWithSQL(tx.Client(), tx.Client(), nil)
			require.NoError(t, createRepo.CreateWithAccountGroups(dbent.NewTxContext(ctx, tx), shadow, nil))
			type outcome struct {
				account *service.Account
				err     error
			}
			done := make(chan outcome, 1)
			go func() {
				account, err := commitShadowParentSetupToken(ctx, repo, parent)
				done <- outcome{account: account, err: err}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'
					AND (query LIKE '%/* oauth_credential_owner_cas */%' OR query LIKE '%/* account_live_credential_parent_lock */%')
				)`).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			if commit {
				require.NoError(t, tx.Commit())
			} else {
				require.NoError(t, tx.Rollback())
			}
			result := <-done
			stored, err := repo.GetByID(ctx, parent.ID)
			require.NoError(t, err)
			children, err := repo.ListShadowsByParent(ctx, parent.ID)
			require.NoError(t, err)
			if commit {
				require.Error(t, result.err, "type changes must recheck children committed while waiting for the parent lock")
				require.Nil(t, result.account)
				require.Equal(t, service.AccountTypeOAuth, stored.Type)
				require.Equal(t, parent.Credentials, stored.Credentials)
				require.Len(t, children, 1)
				require.Equal(t, shadow.ID, children[0].ID)
				require.Zero(t, cnControlOutboxCount(t, parent.ID))
				require.Equal(t, 1, cnControlOutboxCount(t, shadow.ID))
				require.Empty(t, cache.setAccounts)
			} else {
				require.NoError(t, result.err)
				require.NotNil(t, result.account)
				require.Equal(t, service.AccountTypeSetupToken, stored.Type)
				require.Equal(t, "new-setup-credential", stored.GetCredential("access_token"))
				require.Empty(t, children)
				require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
				require.Zero(t, cnControlOutboxCount(t, shadow.ID))
				require.Equal(t, service.AccountTypeSetupToken, requireEvictedSchedulerAccount(t, repo, cache, parent.ID).Type)
			}
			if commit {
				require.Empty(t, cache.deleteIDs)
			}
		})
	}
}

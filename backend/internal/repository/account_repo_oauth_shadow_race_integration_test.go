//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// A Spark child requires an OpenAI OAuth credential parent. Changing that
// parent's type and creating a child must serialize across both public creation
// and repository creation entrypoints. A rolled-back operation has no authority
// to prevent the other valid operation or publish a scheduler snapshot.
func TestOAuthParentTypeConversionSerializesWithShadowCreation(t *testing.T) {
	for _, method := range []string{"Create", "CreateWithAccountGroups", "AdminCreateShadow"} {
		for _, commit := range []bool{false, true} {
			t.Run(method+"/"+map[bool]string{false: "conversion rollback", true: "conversion commit"}[commit], func(t *testing.T) {
				base, original, shadow, _ := newShadowRaceFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				parent, err := base.GetByID(ctx, original.ID)
				require.NoError(t, err)
				conversionCache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{parent.ID: parent}}
				creationCache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{parent.ID: parent}}
				conversionRepo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, conversionCache)
				creationRepo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, creationCache)
				rollback := errors.New("deliberate OAuth type conversion rollback")
				created := make(chan error, 1)
				err = conversionRepo.RunOpenAIOAuthPolicyTransaction(ctx, func(writeCtx context.Context, repository service.AccountRepository) error {
					writer := repository.(service.OAuthCredentialOwnerRepository)
					applied, err := writer.UpdateOAuthCredentialsIfUnchanged(writeCtx, parent, service.AccountTypeSetupToken,
						map[string]any{"access_token": "converted-setup-credential"}, map[string]any{"conversion": "committed"}, true)
					require.NoError(t, err)
					require.True(t, applied, "the type conversion must reach its real uncommitted row write")
					require.Empty(t, conversionCache.setAccounts, "an uncommitted conversion cannot publish its credential owner")
					visible, err := base.GetByID(ctx, parent.ID)
					require.NoError(t, err)
					require.Equal(t, service.AccountTypeOAuth, visible.Type)
					require.Equal(t, parent.Credentials, visible.Credentials)
					go func() {
						created <- creationRepo.RunOpenAIOAuthPolicyTransaction(ctx, func(createCtx context.Context, repository service.AccountRepository) error {
							return createShadowRaceRecord(createCtx, repository.(*accountRepository), method, shadow)
						})
					}()
					require.Eventually(t, func() bool { return oauthShadowTypeWriterBlocked(ctx) }, 3*time.Second, 10*time.Millisecond,
						"shadow creation must reach the locked parent before the conversion is released")
					if !commit {
						return rollback
					}
					return nil
				})
				if commit {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, rollback)
				}
				var createErr error
				select {
				case createErr = <-created:
				case <-ctx.Done():
					t.Fatal("shadow creation did not finish after the conversion transaction ended")
				}
				stored, err := base.GetByID(ctx, parent.ID)
				require.NoError(t, err)
				if commit {
					require.Error(t, createErr, "a committed setup-token parent cannot acquire an OAuth Spark child")
					require.Equal(t, service.AccountTypeSetupToken, stored.Type)
					require.Equal(t, "converted-setup-credential", stored.GetCredential("access_token"))
					require.Equal(t, "committed", stored.Extra["conversion"])
					require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
					require.Equal(t, service.AccountTypeSetupToken, requireEvictedSchedulerAccount(t, base, conversionCache, parent.ID).Type)
					require.Empty(t, creationCache.setAccounts)
					require.NotContains(t, creationCache.accounts, shadow.ID)
				} else {
					require.NoError(t, createErr, "a rolled-back conversion must leave a valid OAuth parent available")
					require.Equal(t, parent.Type, stored.Type)
					require.Equal(t, parent.Credentials, stored.Credentials)
					require.Equal(t, parent.Extra, stored.Extra)
					require.Zero(t, cnControlOutboxCount(t, parent.ID))
					require.Empty(t, conversionCache.setAccounts)
					require.Equal(t, shadow.ID, requireEvictedSchedulerAccount(t, base, creationCache, shadow.ID).ID)
				}
				assertOAuthShadowTypeFamily(t, parent.ID, shadow, !commit)
			})
		}
	}
}

func TestOAuthParentTypeConversionRechecksShadowCreationBeforeCommit(t *testing.T) {
	for _, method := range []string{"Create", "CreateWithAccountGroups", "AdminCreateShadow"} {
		for _, commit := range []bool{false, true} {
			t.Run(method+"/"+map[bool]string{false: "creation rollback", true: "creation commit"}[commit], func(t *testing.T) {
				base, original, shadow, _ := newShadowRaceFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				parent, err := base.GetByID(ctx, original.ID)
				require.NoError(t, err)
				conversionCache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{parent.ID: parent}}
				creationCache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{parent.ID: parent}}
				conversionRepo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, conversionCache)
				creationRepo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, creationCache)
				committer := shadowLifecycleAdmin(conversionRepo).(service.OAuthCredentialCommitter)
				type conversionResult struct {
					account *service.Account
					err     error
				}
				converted := make(chan conversionResult, 1)
				rollback := errors.New("deliberate Spark shadow creation rollback")
				err = creationRepo.RunOpenAIOAuthPolicyTransaction(ctx, func(writeCtx context.Context, repository service.AccountRepository) error {
					require.NoError(t, createShadowRaceRecord(writeCtx, repository.(*accountRepository), method, shadow))
					require.Empty(t, creationCache.setAccounts)
					var visibleChildren int
					require.NoError(t, integrationDB.QueryRowContext(ctx,
						`SELECT count(*) FROM accounts WHERE parent_account_id=$1 AND deleted_at IS NULL`, parent.ID).Scan(&visibleChildren))
					require.Zero(t, visibleChildren, "the competing conversion must begin before the shadow is committed")
					go func() {
						account, err := committer.CommitOAuthCredentials(ctx, parent, service.AccountTypeSetupToken,
							map[string]any{"access_token": "converted-setup-credential"}, map[string]any{"conversion": "committed"}, true)
						converted <- conversionResult{account: account, err: err}
					}()
					require.Eventually(t, func() bool { return oauthShadowTypeWriterBlocked(ctx) }, 3*time.Second, 10*time.Millisecond,
						"type conversion must contend with the in-flight child before creation is released")
					if !commit {
						return rollback
					}
					return nil
				})
				if commit {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, rollback)
				}
				var result conversionResult
				select {
				case result = <-converted:
				case <-ctx.Done():
					t.Fatal("type conversion did not finish after shadow creation ended")
				}
				stored, err := base.GetByID(ctx, parent.ID)
				require.NoError(t, err)
				if commit {
					require.Error(t, result.err, "the newly committed shadow makes its credential parent's type immutable")
					require.Equal(t, "SPARK_SHADOW_PARENT_IMMUTABLE_TYPE", infraerrors.Reason(result.err))
					require.Nil(t, result.account)
					require.Equal(t, parent.Type, stored.Type)
					require.Equal(t, parent.Credentials, stored.Credentials)
					require.Equal(t, parent.Extra, stored.Extra)
					require.Equal(t, parent.Status, stored.Status)
					require.Equal(t, parent.ErrorMessage, stored.ErrorMessage)
					require.Zero(t, cnControlOutboxCount(t, parent.ID))
					require.Empty(t, conversionCache.setAccounts)
					require.Equal(t, shadow.ID, requireEvictedSchedulerAccount(t, base, creationCache, shadow.ID).ID)
				} else {
					require.NoError(t, result.err, "rolled-back shadow creation cannot freeze the valid parent's type")
					require.NotNil(t, result.account)
					require.Equal(t, service.AccountTypeSetupToken, stored.Type)
					require.Equal(t, "converted-setup-credential", stored.GetCredential("access_token"))
					require.Equal(t, "committed", stored.Extra["conversion"])
					require.Equal(t, 1, cnControlOutboxCount(t, parent.ID))
					require.Equal(t, service.AccountTypeSetupToken, requireEvictedSchedulerAccount(t, base, conversionCache, parent.ID).Type)
					require.Empty(t, creationCache.setAccounts)
					require.NotContains(t, creationCache.accounts, shadow.ID)
				}
				assertOAuthShadowTypeFamily(t, parent.ID, shadow, commit)
			})
		}
	}
}

func oauthShadowTypeWriterBlocked(ctx context.Context) bool {
	var waiting bool
	err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'
		AND (query LIKE '%/* oauth_credential_owner_cas */%'
			OR query LIKE '%/* account_group_account_lock */%'
			OR query LIKE '%/* account_credential_family_lock */%'
			OR query LIKE '%/* account_live_credential_parent_lock */%')
	)`).Scan(&waiting)
	return err == nil && waiting
}

func assertOAuthShadowTypeFamily(t *testing.T, parentID int64, shadow *service.Account, exists bool) {
	t.Helper()
	var children, outbox int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM accounts WHERE parent_account_id=$1 AND deleted_at IS NULL`, parentID).Scan(&children))
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE parent_account_id=$1)`, parentID).Scan(&outbox))
	if !exists {
		require.Zero(t, children, "a rejected or rolled-back creation cannot leave a credential-dependent orphan")
		require.Zero(t, outbox)
		return
	}
	require.Equal(t, 1, children)
	require.Equal(t, 1, outbox, "only the committed child creation publishes an event")
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	stored, err := repo.GetByID(context.Background(), shadow.ID)
	require.NoError(t, err)
	require.Equal(t, service.AccountTypeOAuth, stored.Type)
	require.Equal(t, &parentID, stored.ParentAccountID)
	require.Equal(t, service.QuotaDimensionSpark, stored.QuotaDimension)
	require.NotContains(t, stored.Credentials, "access_token")
	require.NotContains(t, stored.Credentials, "refresh_token")
}

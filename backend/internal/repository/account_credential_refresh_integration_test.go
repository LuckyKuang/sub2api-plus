//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func (s *AccountRepoSuite) TestCNOAuthRefreshCASAppliesAndPublishesDurableState() {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		account := mustCreateAccount(s.T(), s.client, &service.Account{
			Name: "native-refresh-success-" + platform, Platform: platform, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"oauth_provider": platform, "access_token": "attempted", "refresh_token": "old", "_token_version": 7, "model_mapping": map[string]any{"requested": "upstream"}},
		})
		observed, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		replacement := map[string]any{"oauth_provider": platform, "access_token": "new", "refresh_token": "rotated", "_token_version": 8, "model_mapping": map[string]any{"requested": "upstream"}}
		cache := &schedulerCacheRecorder{}
		s.repo.schedulerCache = cache
		_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
		s.Require().NoError(err)
		applied, err := s.repo.UpdateOAuthRefreshCredentialsIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: platform, Credentials: observed.Credentials, ProxyID: observed.ProxyID, Type: service.AccountTypeOAuth, Proxy: observed.Proxy, Status: service.StatusActive}, replacement)
		s.Require().NoError(err)
		s.Require().True(applied, platform)
		fresh, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Equal("new", fresh.GetCredential("access_token"))
		s.Require().Equal("rotated", fresh.GetCredential("refresh_token"))
		s.Require().Equal(service.StatusActive, fresh.Status)
		s.Require().True(fresh.Schedulable)
		s.Require().Empty(cache.setAccounts, "refresh must not publish a snapshot that can race a newer login")
		s.Require().Equal([]int64{account.ID}, cache.deleteIDs)
		var count int
		err = scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE event_type=$1 AND account_id=$2", []any{service.SchedulerOutboxEventAccountChanged, account.ID}, &count)
		s.Require().NoError(err)
		s.Require().Equal(1, count)
	}
}

func (s *AccountRepoSuite) TestCNOAuthRefreshCASRejectsConcurrentOwnerAndGrantChanges() {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		for _, change := range []string{"reauthorization", "metadata", "proxy", "type", "platform", "status"} {
			account := mustCreateAccount(s.T(), s.client, &service.Account{Name: "native-refresh-fence-" + platform + "-" + change, Platform: platform, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"oauth_provider": platform, "access_token": "observed", "refresh_token": "observed-rt", "_token_version": 7}})
			observed, err := s.repo.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			switch change {
			case "reauthorization":
				err = s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"oauth_provider": platform, "access_token": "new-login", "refresh_token": "new-rt", "_token_version": 8})
			case "metadata":
				err = s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"oauth_provider": platform, "access_token": "observed", "refresh_token": "observed-rt", "_token_version": 7, "model_mapping": map[string]any{"a": "b"}})
			case "proxy":
				proxy := mustCreateProxy(s.T(), s.client, &service.Proxy{Name: "native-refresh-proxy-" + platform})
				_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET proxy_id=$1 WHERE id=$2", proxy.ID, account.ID)
			case "type":
				_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET type=$1 WHERE id=$2", service.AccountTypeAPIKey, account.ID)
			case "platform":
				replacementPlatform := service.PlatformDeepseek
				if platform == replacementPlatform {
					replacementPlatform = service.PlatformOpenAI
				}
				_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET platform=$1 WHERE id=$2", replacementPlatform, account.ID)
			case "status":
				_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET status=$1,schedulable=FALSE WHERE id=$2", service.StatusError, account.ID)
			}
			s.Require().NoError(err)
			current, err := s.repo.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			cache := &schedulerCacheRecorder{}
			s.repo.schedulerCache = cache
			_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
			s.Require().NoError(err)
			applied, err := s.repo.UpdateOAuthRefreshCredentialsIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: platform, Credentials: observed.Credentials, ProxyID: observed.ProxyID, Type: service.AccountTypeOAuth, Proxy: observed.Proxy, Status: service.StatusActive}, map[string]any{"access_token": "obsolete-response", "refresh_token": "obsolete-response"})
			s.Require().NoError(err)
			s.Require().False(applied, platform+"/"+change)
			fresh, err := s.repo.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			s.Require().Equal(current.Credentials, fresh.Credentials)
			s.Require().Equal(current.Platform, fresh.Platform)
			s.Require().Equal(current.Type, fresh.Type)
			s.Require().Equal(current.ProxyID, fresh.ProxyID)
			s.Require().Equal(current.Status, fresh.Status)
			s.Require().Empty(cache.setAccounts)
			s.Require().Empty(cache.deleteIDs)
			var count int
			err = scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count)
			s.Require().NoError(err)
			s.Require().Zero(count, "stale refresh must neither persist nor publish scheduler state")
		}
	}
}

func TestCNOAuthRefreshCASRollsBackWhenOutboxInsertFails(t *testing.T) {
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: "native-refresh-atomic-outbox-failure", Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "observed", "refresh_token": "old"}})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_ = client.Account.DeleteOneID(account.ID).Exec(context.Background())
	})
	_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
	require.NoError(t, err)
	cache := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(client, &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}, cache)
	observed, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	applied, err := repo.UpdateOAuthRefreshCredentialsIfUnchanged(context.Background(), &service.Account{ID: account.ID, Platform: service.PlatformKimi, Credentials: observed.Credentials, ProxyID: observed.ProxyID, Type: service.AccountTypeOAuth, Proxy: observed.Proxy, Status: service.StatusActive}, map[string]any{"access_token": "must-roll-back", "refresh_token": "must-roll-back"})
	require.Error(t, err)
	require.False(t, applied)
	fresh, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, observed.Credentials, fresh.Credentials)
	require.Empty(t, cache.setAccounts)
	require.Empty(t, cache.deleteIDs)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&count))
	require.Zero(t, count)
}

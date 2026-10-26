//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func (s *AccountRepoSuite) TestCNQuotaCASAppliesExactOwnerAndEvictsSchedulerState() {
	for _, platform := range []string{service.PlatformKimi, service.PlatformZhipu, service.PlatformMiniMax} {
		for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
			account := mustCreateAccount(s.T(), s.client, &service.Account{Name: "quota-cas-" + platform + accountType, Platform: platform, Type: accountType, Status: service.StatusActive, Credentials: map[string]any{"access_token": "actual-query-grant", "api_key": "actual-query-key"}, Extra: map[string]any{"unrelated": "preserved"}})
			expected, err := s.repo.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
			s.Require().NoError(err)
			cache := &schedulerCacheRecorder{}
			s.repo.schedulerCache = cache
			applied, err := s.repo.UpdateCNProviderQuotaIfUnchanged(s.ctx, expected, map[string]any{platform + "_5h_used_percent": 100, platform + "_quota_generation": "query-generation"})
			s.Require().NoError(err)
			s.Require().True(applied)
			saved, err := s.repo.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			s.Require().Equal("preserved", saved.Extra["unrelated"])
			s.Require().Equal(float64(100), saved.Extra[platform+"_5h_used_percent"])
			s.Require().Equal(expected.Credentials, saved.Credentials)
			s.Require().Empty(cache.setAccounts, "a quota observation cannot publish a snapshot that a concurrent login could supersede")
			s.Require().Equal([]int64{account.ID}, cache.deleteIDs)
			var count int
			s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
			s.Require().Equal(1, count)
		}
	}
}

func (s *AccountRepoSuite) TestCNQuotaCASRejectsEveryChangedOwnerField() {
	for _, change := range []string{"grant", "metadata", "platform", "type", "status", "proxy assignment", "proxy endpoint", "proxy credentials", "proxy status"} {
		proxy := mustCreateProxy(s.T(), s.client, &service.Proxy{Name: "quota-proxy-" + change, Protocol: "http", Host: "127.0.0.1", Port: 8111, Status: service.StatusActive})
		account := mustCreateAccount(s.T(), s.client, &service.Account{Name: "quota-fence-" + change, Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, Status: service.StatusActive, ProxyID: &proxy.ID, Credentials: map[string]any{"access_token": "old-grant", "refresh_token": "old-refresh"}, Extra: map[string]any{"kimi_5h_used_percent": 10, "kimi_quota_generation": "healthy-new-owner"}})
		expected, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().NotNil(expected.Proxy)
		switch change {
		case "grant":
			err = s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"access_token": "new-grant", "refresh_token": "new-refresh"})
		case "metadata":
			err = s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"access_token": "old-grant", "refresh_token": "old-refresh", "model_mapping": map[string]any{"a": "b"}})
		case "platform":
			_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET platform=$1 WHERE id=$2", service.PlatformZhipu, account.ID)
		case "type":
			_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET type=$1 WHERE id=$2", service.AccountTypeAPIKey, account.ID)
		case "status":
			_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET status=$1 WHERE id=$2", service.StatusError, account.ID)
		case "proxy assignment":
			_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE accounts SET proxy_id=NULL WHERE id=$1", account.ID)
		case "proxy endpoint":
			_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE proxies SET host='changed.invalid' WHERE id=$1", proxy.ID)
		case "proxy credentials":
			_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE proxies SET username='replacement' WHERE id=$1", proxy.ID)
		case "proxy status":
			_, err = s.repo.sql.ExecContext(s.ctx, "UPDATE proxies SET status='error' WHERE id=$1", proxy.ID)
		}
		s.Require().NoError(err)
		_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
		s.Require().NoError(err)
		cache := &schedulerCacheRecorder{}
		s.repo.schedulerCache = cache
		applied, err := s.repo.UpdateCNProviderQuotaIfUnchanged(s.ctx, expected, map[string]any{"kimi_5h_used_percent": 100, "kimi_quota_generation": "stale-owner"})
		s.Require().NoError(err, change)
		s.Require().False(applied, change)
		saved, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Equal(float64(10), saved.Extra["kimi_5h_used_percent"], change)
		s.Require().Equal("healthy-new-owner", saved.Extra["kimi_quota_generation"], change)
		s.Require().Empty(cache.setAccounts, change)
		s.Require().Empty(cache.deleteIDs, change)
		var count int
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Zero(count, change)
	}
}

type failCNQuotaOutboxExecutor struct{ sqlExecutor }

func (e *failCNQuotaOutboxExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "cn_control_snapshot_cas") && strings.Contains(query, "INSERT INTO scheduler_outbox") {
		args = append([]any(nil), args...)
		args[len(args)-1] = nil // event_type's NOT NULL failure must roll back the quota merge.
	}
	return e.sqlExecutor.ExecContext(ctx, query, args...)
}

func TestCNQuotaCASRollsBackWhenDurableInvalidationFails(t *testing.T) {
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: "quota-atomic-rollback", Platform: service.PlatformKimi, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "actual-query-key"}, Extra: map[string]any{"kimi_5h_used_percent": 10}})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_ = client.Account.DeleteOneID(account.ID).Exec(context.Background())
	})
	repo := newAccountRepositoryWithSQL(client, &failCNQuotaOutboxExecutor{sqlExecutor: integrationDB}, nil)
	cache := &schedulerCacheRecorder{}
	repo.schedulerCache = cache
	expected, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	applied, err := repo.UpdateCNProviderQuotaIfUnchanged(context.Background(), expected, map[string]any{"kimi_5h_used_percent": 100})
	require.Error(t, err)
	require.False(t, applied)
	require.Empty(t, cache.setAccounts)
	require.Empty(t, cache.deleteIDs)
	saved, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, float64(10), saved.Extra["kimi_5h_used_percent"])
}

func (s *AccountRepoSuite) TestProxyIdentityChangeInvalidatesCNQuotaBindingAndPublishesAccounts() {
	for _, change := range []string{"name", "endpoint", "status"} {
		proxy := mustCreateProxy(s.T(), s.client, &service.Proxy{Name: "quota-proxy-invalidate-" + change, Protocol: "http", Host: "127.0.0.1", Port: 8111, Status: service.StatusActive})
		var accounts []*service.Account
		for _, platform := range []string{service.PlatformKimi, service.PlatformZhipu, service.PlatformMiniMax} {
			accounts = append(accounts, mustCreateAccount(s.T(), s.client, &service.Account{Name: "quota-proxy-" + change + platform, Platform: platform, Type: service.AccountTypeOAuth, Status: service.StatusActive, ProxyID: &proxy.ID, Credentials: map[string]any{"access_token": "selected"}, Extra: map[string]any{platform + "_5h_used_percent": 100, platform + "_quota_generation": "old-route-generation", "unrelated": "retained"}}))
		}
		_, err := s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
		s.Require().NoError(err)
		switch change {
		case "name":
			proxy.Name = "cosmetic"
		case "endpoint":
			proxy.Host = "new-route.invalid"
		case "status":
			proxy.Status = service.StatusError
		}
		proxyRepo := newProxyRepositoryWithSQL(s.client, s.repo.sql)
		s.Require().NoError(proxyRepo.Update(s.ctx, proxy))
		for _, account := range accounts {
			saved, err := s.repo.GetByID(s.ctx, account.ID)
			s.Require().NoError(err)
			s.Require().Equal("retained", saved.Extra["unrelated"])
			s.Require().Equal(float64(100), saved.Extra[account.Platform+"_5h_used_percent"])
			if change == "name" {
				s.Require().Equal("old-route-generation", saved.Extra[account.Platform+"_quota_generation"])
			} else {
				s.Require().NotContains(saved.Extra, account.Platform+"_quota_generation")
			}
		}
		var count int
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE event_type=$1", []any{service.SchedulerOutboxEventAccountBulkChanged}, &count))
		// Every proxy edit refreshes the embedded scheduler proxy. A name-only
		// edit preserves quota ownership while still updating that snapshot.
		s.Require().Equal(1, count)
	}
}

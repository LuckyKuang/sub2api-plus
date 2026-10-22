//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Exact credentials, platform/type and proxy are the authority for a failed
// acquisition. Exercise the PostgreSQL predicate and durable outbox together.
func (s *AccountRepoSuite) TestCredentialFailureQuarantinePreservesConcurrentReauthorization() {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu, service.PlatformDeepseek} {
		account := mustCreateAccount(s.T(), s.client, &service.Account{
			Name: "credential-fence-" + platform, Platform: platform, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"access_token": "observed", "_token_version": 7},
		})
		observed, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().NoError(s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"access_token": "reauthorized", "_token_version": 8}))
		_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
		s.Require().NoError(err)
		applied, err := s.repo.SetCredentialErrorIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: platform, Type: observed.Type, Credentials: observed.Credentials, ProxyID: observed.ProxyID, Proxy: observed.Proxy, Status: service.StatusActive}, "grant_revoked")
		s.Require().NoError(err)
		s.Require().False(applied, platform)
		fresh, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Equal(service.StatusActive, fresh.Status)
		s.Require().True(fresh.Schedulable)
		s.Require().Equal("reauthorized", fresh.GetCredential("access_token"))
		var count int
		err = scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1", []any{account.ID}, &count)
		s.Require().NoError(err)
		s.Require().Zero(count, "a stale failure must not publish a disabled snapshot")
		applied, err = s.repo.SetCredentialErrorIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: platform, Type: fresh.Type, Credentials: fresh.Credentials, ProxyID: fresh.ProxyID, Proxy: fresh.Proxy, Status: service.StatusActive}, "grant_revoked")
		s.Require().NoError(err)
		s.Require().True(applied)
		fresh, err = s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Equal(service.StatusError, fresh.Status)
		s.Require().False(fresh.Schedulable)
		err = scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1", []any{account.ID}, &count)
		s.Require().NoError(err)
		s.Require().Equal(1, count)
	}
}

func (s *AccountRepoSuite) TestCredentialFailureCooldownPreservesConcurrentReauthorization() {
	until := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu, service.PlatformDeepseek} {
		account := mustCreateAccount(s.T(), s.client, &service.Account{
			Name: "credential-cooldown-" + platform, Platform: platform, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"access_token": "sent", "refresh_token": "sent-refresh", "_token_version": 7},
		})
		observed, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().NoError(s.repo.UpdateCredentials(s.ctx, account.ID, map[string]any{"access_token": "new-login", "refresh_token": "new-login-refresh", "_token_version": 8}))
		_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
		s.Require().NoError(err)
		applied, err := s.repo.SetCredentialTempUnschedulableIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: platform, Type: observed.Type, Credentials: observed.Credentials, ProxyID: observed.ProxyID, Proxy: observed.Proxy, Status: service.StatusActive}, until, "old-401")
		s.Require().NoError(err)
		s.Require().False(applied, platform)
		fresh, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().Nil(fresh.TempUnschedulableUntil, platform)
		s.Require().Equal(service.StatusActive, fresh.Status)
		s.Require().True(fresh.Schedulable)
		s.Require().Equal("new-login", fresh.GetCredential("access_token"))
		var count int
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Zero(count, platform)
		applied, err = s.repo.SetCredentialTempUnschedulableIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: platform, Type: fresh.Type, Credentials: fresh.Credentials, ProxyID: fresh.ProxyID, Proxy: fresh.Proxy, Status: service.StatusActive}, until, "current-401")
		s.Require().NoError(err)
		s.Require().True(applied, platform)
		fresh, err = s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().NotNil(fresh.TempUnschedulableUntil)
		s.Require().True(until.Equal(*fresh.TempUnschedulableUntil))
		s.Require().Equal("current-401", fresh.TempUnschedulableReason)
		s.Require().Equal(service.StatusActive, fresh.Status, "refresh worker must still recover this account")
		applied, err = s.repo.SetCredentialTempUnschedulableIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: platform, Type: fresh.Type, Credentials: fresh.Credentials, ProxyID: fresh.ProxyID, Proxy: fresh.Proxy, Status: service.StatusActive}, until.Add(-time.Minute), "shorter-window")
		s.Require().NoError(err)
		s.Require().False(applied)
		fresh, err = s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Require().True(until.Equal(*fresh.TempUnschedulableUntil))
		s.Require().Equal("current-401", fresh.TempUnschedulableReason)
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Equal(1, count)
	}
}

func (s *AccountRepoSuite) TestCredentialFailureCooldownRejectsChangedOwnerFields() {
	proxy := mustCreateProxy(s.T(), s.client, &service.Proxy{Name: "credential-cooldown-route", Protocol: "http", Host: "127.0.0.1", Port: 8111, Status: service.StatusActive})
	for _, field := range []string{"platform", "type", "status", "proxy", "deleted", "refresh-token"} {
		account := mustCreateAccount(s.T(), s.client, &service.Account{Name: "credential-cooldown-" + field, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "same-access", "refresh_token": "sent-refresh"}})
		expected, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		var query string
		args := []any{account.ID}
		switch field {
		case "platform":
			query = "UPDATE accounts SET platform='kimi' WHERE id=$1"
		case "type":
			query = "UPDATE accounts SET type='apikey' WHERE id=$1"
		case "status":
			query = "UPDATE accounts SET status='error' WHERE id=$1"
		case "proxy":
			query = "UPDATE accounts SET proxy_id=$2 WHERE id=$1"
			args = append(args, proxy.ID)
		case "deleted":
			query = "UPDATE accounts SET deleted_at=NOW() WHERE id=$1"
		case "refresh-token":
			query = `UPDATE accounts SET credentials=jsonb_set(credentials,'{refresh_token}','"new-login-refresh"'::jsonb) WHERE id=$1`
		}
		_, err = s.repo.sql.ExecContext(s.ctx, query, args...)
		s.Require().NoError(err, field)
		_, err = s.repo.sql.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
		s.Require().NoError(err)
		applied, err := s.repo.SetCredentialTempUnschedulableIfUnchanged(s.ctx, &service.Account{ID: account.ID, Platform: expected.Platform, Type: expected.Type, Credentials: expected.Credentials, ProxyID: expected.ProxyID, Proxy: expected.Proxy, Status: service.StatusActive}, time.Now().Add(time.Minute), "stale-owner")
		s.Require().NoError(err, field)
		s.Require().False(applied, field)
		var cooldown sql.NullTime
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT temp_unschedulable_until FROM accounts WHERE id=$1", []any{account.ID}, &cooldown))
		s.Require().False(cooldown.Valid, field)
		var count int
		s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", []any{account.ID}, &count))
		s.Require().Zero(count, field)
	}
}

type failCredentialCooldownOutboxExecutor struct{ sqlExecutor }

func (e *failCredentialCooldownOutboxExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "temp_unschedulable_until = $1") && strings.Contains(query, "INSERT INTO scheduler_outbox") {
		args = append([]any(nil), args...)
		args[8] = nil
	}
	return e.sqlExecutor.ExecContext(ctx, query, args...)
}

func TestCredentialFailureCooldownRollsBackWhenOutboxInsertFails(t *testing.T) {
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: "credential-cooldown-rollback", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "sent", "refresh_token": "sent-refresh"}})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_ = client.Account.DeleteOneID(account.ID).Exec(context.Background())
	})
	repo := newAccountRepositoryWithSQL(client, &failCredentialCooldownOutboxExecutor{sqlExecutor: integrationDB}, nil)
	expected, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	applied, err := repo.SetCredentialTempUnschedulableIfUnchanged(context.Background(), &service.Account{ID: account.ID, Platform: expected.Platform, Type: expected.Type, Credentials: expected.Credentials, ProxyID: expected.ProxyID, Proxy: expected.Proxy, Status: service.StatusActive}, time.Now().Add(time.Minute), "must-roll-back")
	require.Error(t, err)
	require.False(t, applied)
	saved, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Nil(t, saved.TempUnschedulableUntil)
	require.Empty(t, saved.TempUnschedulableReason)
	require.Equal(t, service.StatusActive, saved.Status)
}

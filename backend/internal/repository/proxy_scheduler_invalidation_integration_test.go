//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func (s *ProxyRepoSuite) proxyOutboxAccountIDs() []int64 {
	s.T().Helper()
	rows, err := s.tx.QueryContext(s.ctx, "SELECT event_type, payload FROM scheduler_outbox ORDER BY id")
	s.Require().NoError(err)
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var eventType string
		var raw []byte
		s.Require().NoError(rows.Scan(&eventType, &raw))
		s.Require().Equal(service.SchedulerOutboxEventAccountBulkChanged, eventType, "proxy edits must target their bound accounts rather than rebuild unrelated groups")
		var payload struct {
			AccountIDs []int64 `json:"account_ids"`
		}
		s.Require().NoError(json.Unmarshal(raw, &payload))
		ids = append(ids, payload.AccountIDs...)
	}
	s.Require().NoError(rows.Err())
	return ids
}

func (s *ProxyRepoSuite) TestUpdatesPublishEveryBoundAccountWithoutQuotaSnapshots() {
	proxy := s.mustCreateProxy(&service.Proxy{Name: "all-platform-proxy", Protocol: "http", Host: "old.invalid", Port: 8080, Status: service.StatusActive})
	other := s.mustCreateProxy(&service.Proxy{Name: "other-proxy", Protocol: "http", Host: "other.invalid", Port: 8080, Status: service.StatusActive})
	var wantIDs []int64
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
			account := mustCreateAccount(s.T(), s.tx.Client(), &service.Account{
				Name: "proxy-no-quota-" + platform + kind, Platform: platform, Type: kind,
				Status: service.StatusActive, Schedulable: true, ProxyID: &proxy.ID,
				Credentials: map[string]any{"access_token": "fixture", "api_key": "fixture"},
				Extra:       map[string]any{"keep": "unchanged"},
			})
			wantIDs = append(wantIDs, account.ID)
		}
	}
	paused := mustCreateAccount(s.T(), s.tx.Client(), &service.Account{Name: "paused-bound", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusDisabled, ProxyID: &proxy.ID})
	wantIDs = append(wantIDs, paused.ID)
	mustCreateAccount(s.T(), s.tx.Client(), &service.Account{Name: "unbound", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
	mustCreateAccount(s.T(), s.tx.Client(), &service.Account{Name: "other-bound", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ProxyID: &other.ID})
	deleted := mustCreateAccount(s.T(), s.tx.Client(), &service.Account{Name: "deleted-bound", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ProxyID: &proxy.ID})
	_, err := s.tx.ExecContext(s.ctx, "UPDATE accounts SET deleted_at=NOW() WHERE id=$1", deleted.ID)
	s.Require().NoError(err)

	changes := []struct {
		name  string
		apply func()
	}{
		{"endpoint", func() { proxy.Host = "new.invalid" }},
		{"authentication", func() { proxy.Username, proxy.Password = "new-user", "new-password" }},
		{"protocol", func() { proxy.Protocol = "socks5" }},
		{"status", func() { proxy.Status = service.StatusDisabled }},
		{"egress timezone", func() { proxy.EgressTimezone = "Europe/Amsterdam" }},
		{"egress country", func() { proxy.EgressCountry = "NL" }},
		{"fallback", func() { proxy.FallbackMode = service.FallbackModeDirect }},
		{"expiry", func() { value := time.Now().Add(time.Hour); proxy.ExpiresAt = &value }},
		{"name", func() { proxy.Name = "renamed-proxy" }},
	}
	for _, change := range changes {
		s.Run(change.name, func() {
			_, err := s.tx.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
			s.Require().NoError(err)
			change.apply()
			s.Require().NoError(s.repo.Update(s.ctx, proxy))
			s.Require().Equal(wantIDs, s.proxyOutboxAccountIDs())
		})
	}
}

func (s *ProxyRepoSuite) TestDeleteRejectsLiveBindingsAndPreservesDanglingBinding() {
	proxy := s.mustCreateProxy(&service.Proxy{Name: "deleted-proxy", Protocol: "http", Host: "old.invalid", Port: 8080, Status: service.StatusActive})
	accountRepo := newAccountRepositoryWithSQL(s.tx.Client(), s.tx, nil)
	var wantIDs []int64
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformDeepseek, service.PlatformKimi, service.PlatformMiniMax, service.PlatformStepFun, service.PlatformZhipu} {
		for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
			extra := map[string]any{"keep": "unchanged"}
			if platform == service.PlatformKimi {
				extra["kimi_quota_generation"] = "old-route"
			}
			account := mustCreateAccount(s.T(), s.tx.Client(), &service.Account{
				Name: "delete-proxy-" + platform + kind, Platform: platform, Type: kind, ProxyID: &proxy.ID,
				Extra: extra,
			})
			wantIDs = append(wantIDs, account.ID)
		}
	}
	ollama := mustCreateAccount(s.T(), s.tx.Client(), &service.Account{
		Name: "delete-proxy-ollama", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, ProxyID: &proxy.ID,
		Credentials: map[string]any{"api_key": "fixture", "base_url": "https://ollama.com"},
		Extra: map[string]any{
			"keep":                                   "unchanged",
			service.OllamaCloudUsageSnapshotExtraKey: map[string]any{"status": service.OllamaCloudUsageStatusOK},
			service.OllamaCloudUsageSessionExtraKey:  "cipher:fixture",
		},
	})
	wantIDs = append(wantIDs, ollama.ID)
	mustCreateAccount(s.T(), s.tx.Client(), &service.Account{Name: "delete-unbound", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
	_, err := s.tx.ExecContext(s.ctx, "TRUNCATE scheduler_outbox")
	s.Require().NoError(err)

	// Live bindings prevent deletion even when the repository is called without
	// the admin service's advisory count. Rejection must not invalidate snapshots
	// or publish a scheduler change for an operation that never committed.
	s.Require().ErrorIs(s.repo.Delete(s.ctx, proxy.ID), service.ErrProxyInUse)
	s.Require().Empty(s.proxyOutboxAccountIDs())
	savedProxy, err := s.repo.GetByID(s.ctx, proxy.ID)
	s.Require().NoError(err)
	s.Require().Equal("old.invalid", savedProxy.Host)
	for _, id := range wantIDs {
		account, err := accountRepo.GetByID(s.ctx, id)
		s.Require().NoError(err)
		s.Require().Equal(&proxy.ID, account.ProxyID)
		s.Require().NotNil(account.Proxy)
		route, err := account.StrictProxyURL()
		s.Require().NoError(err)
		s.Require().Equal("http://old.invalid:8080", route)
		s.Require().Equal("unchanged", account.Extra["keep"])
		if account.Platform == service.PlatformKimi {
			s.Require().Equal("old-route", account.Extra["kimi_quota_generation"])
		}
		if account.ID == ollama.ID {
			s.Require().Equal(map[string]any{"status": service.OllamaCloudUsageStatusOK}, account.Extra[service.OllamaCloudUsageSnapshotExtraKey])
			s.Require().Equal("cipher:fixture", account.Extra[service.OllamaCloudUsageSessionExtraKey])
		}
	}

	// Simulate a historical or out-of-band soft deletion. Normal deletion cannot
	// produce this state, but hydration must retain the configured binding so a
	// corrupt route is never interpreted as permission for direct egress.
	_, err = s.tx.ExecContext(s.ctx, "UPDATE proxies SET deleted_at=NOW() WHERE id=$1", proxy.ID)
	s.Require().NoError(err)
	_, err = s.repo.GetByID(s.ctx, proxy.ID)
	s.Require().ErrorIs(err, service.ErrProxyNotFound)
	for _, id := range wantIDs {
		account, err := accountRepo.GetByID(s.ctx, id)
		s.Require().NoError(err)
		s.Require().Equal(&proxy.ID, account.ProxyID)
		s.Require().Nil(account.Proxy)
		route, err := account.StrictProxyURL()
		s.Require().ErrorContains(err, "configured proxy snapshot is unavailable")
		s.Require().Empty(route)
	}
	s.Require().NoError(s.repo.Delete(s.ctx, proxy.ID), "deleting a missing proxy remains idempotent")
	s.Require().Empty(s.proxyOutboxAccountIDs())
}

func TestProxyWriteFailurePreservesBoundAccountState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation string
	}{
		{name: "update_outbox_failure", operation: "update"},
		{name: "delete_rejects_binding_before_outbox", operation: "delete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := testEntClient(t)
			repo := newProxyRepositoryWithSQL(client, integrationDB)
			proxy := mustCreateProxy(t, client, &service.Proxy{Name: "proxy-write-atomic-" + tc.operation, Protocol: "http", Host: "old.invalid", Port: 8080, Status: service.StatusActive})
			account := mustCreateAccount(t, client, &service.Account{Name: "proxy-write-atomic-" + tc.operation, Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, ProxyID: &proxy.ID, Extra: map[string]any{"kimi_quota_generation": "old-route", "keep": "unchanged"}})
			functionName := fmt.Sprintf("fail_proxy_outbox_%d", account.ID)
			triggerName := functionName + "_trigger"
			t.Cleanup(func() {
				_, _ = integrationDB.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+triggerName+" ON scheduler_outbox")
				_, _ = integrationDB.ExecContext(ctx, "DROP FUNCTION IF EXISTS "+functionName+"()")
				_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1 OR payload->'account_ids' @> jsonb_build_array($1::bigint)", account.ID)
				_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
				_, _ = integrationDB.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", proxy.ID)
			})
			_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
				CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
					IF NEW.event_type='account_bulk_changed' AND NEW.payload->'account_ids' @> jsonb_build_array(%d::bigint) THEN
						RAISE EXCEPTION 'forced proxy outbox failure';
					END IF;
					RETURN NEW;
				END;
				$$`, functionName, account.ID))
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "CREATE TRIGGER "+triggerName+" BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION "+functionName+"()")
			require.NoError(t, err)
			if tc.operation == "update" {
				proxy.Host = "new.invalid"
				err = repo.Update(ctx, proxy)
				require.ErrorContains(t, err, "forced proxy outbox failure")
			} else {
				err = repo.Delete(ctx, proxy.ID)
				// A live binding must reject deletion before any snapshot or
				// outbox mutation, even with a failing outbox installed.
				require.ErrorIs(t, err, service.ErrProxyInUse)
			}
			saved, err := repo.GetByID(ctx, proxy.ID)
			require.NoError(t, err, "a failed update or rejected deletion must preserve the proxy")
			require.Equal(t, "old.invalid", saved.Host)
			accountRepo := newAccountRepositoryWithSQL(client, integrationDB, nil)
			savedAccount, err := accountRepo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, &proxy.ID, savedAccount.ProxyID)
			require.NotNil(t, savedAccount.Proxy)
			route, err := savedAccount.StrictProxyURL()
			require.NoError(t, err)
			require.Equal(t, "http://old.invalid:8080", route)
			require.Equal(t, map[string]any{"kimi_quota_generation": "old-route", "keep": "unchanged"}, savedAccount.Extra)
			var count int
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE payload->'account_ids' @> jsonb_build_array($1::bigint)", account.ID).Scan(&count))
			require.Zero(t, count)
		})
	}
}

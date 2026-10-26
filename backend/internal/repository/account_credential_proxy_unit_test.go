//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialProxyCASRequiresHydratedMatchingEvidence(t *testing.T) {
	for _, operation := range credentialProxyMutationCases() {
		for _, missing := range []string{"missing-hydration", "wrong-proxy-id", "unexpected-proxy"} {
			t.Run(operation.name+"/"+missing, func(t *testing.T) {
				proxyID := int64(17)
				owner := &service.Account{ID: 42, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "sent"}, ProxyID: &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: "route.invalid", Port: 8080, Status: service.StatusActive}}
				switch missing {
				case "missing-hydration":
					owner.Proxy = nil
				case "wrong-proxy-id":
					owner.Proxy.ID++
				case "unexpected-proxy":
					owner.ProxyID = nil
				}
				exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
				cache := &oauthRefreshSchedulerCacheRecorder{}
				repo := newAccountRepositoryWithSQL(nil, exec, cache)

				applied, err := operation.apply(context.Background(), repo, owner)

				require.Error(t, err)
				require.False(t, applied)
				require.Empty(t, exec.execQueries, "incomplete route proof must not authorize any write")
				require.Empty(t, cache.deleteIDs)
				require.Empty(t, cache.setAccounts)
			})
		}
	}
}

func TestCredentialProxyCASGrokRefreshRequiresGrokOAuthOwner(t *testing.T) {
	for _, operation := range credentialProxyMutationCases() {
		if operation.name != "grok-missing-refresh" && operation.name != "grok-refresh-success" && operation.name != "grok-refresh-error" && operation.name != "grok-refresh-cooldown" {
			continue
		}
		for _, invalid := range []string{"platform", "type"} {
			t.Run(operation.name+"/"+invalid, func(t *testing.T) {
				owner := &service.Account{ID: 42, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "sent"}}
				if invalid == "platform" {
					owner.Platform = service.PlatformOpenAI
				} else {
					owner.Type = service.AccountTypeAPIKey
				}
				exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
				repo := newAccountRepositoryWithSQL(nil, exec, nil)
				applied, err := operation.apply(context.Background(), repo, owner)
				require.Error(t, err)
				require.False(t, applied)
				require.Empty(t, exec.execQueries, "a specialized port must not replace the evidence's provider or credential type")
			})
		}
	}
}

func TestCredentialProxyCASLocksCompleteObservedRoute(t *testing.T) {
	for _, operation := range credentialProxyMutationCases() {
		t.Run(operation.name, func(t *testing.T) {
			proxyID := int64(17)
			owner := &service.Account{ID: 42, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "sent"}, ProxyID: &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: "route.invalid", Port: 8080, Username: "observed-user", Password: "observed-password", Status: service.StatusActive}}
			exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
			repo := newAccountRepositoryWithSQL(nil, exec, nil)

			applied, err := operation.apply(context.Background(), repo, owner)

			require.NoError(t, err)
			require.False(t, applied)
			require.Len(t, exec.execQueries, 1)
			query := normalizeSQLWhitespace(exec.execQueries[0])
			for _, predicate := range []string{"p.deleted_at IS NULL", "p.protocol =", "p.host =", "p.port =", "COALESCE(p.username, '') =", "COALESCE(p.password, '') =", "p.status =", "p.expires_at IS NOT DISTINCT FROM", "FOR SHARE", "INSERT INTO scheduler_outbox"} {
				require.Contains(t, query, predicate)
			}
			for _, expected := range []any{"route.invalid", 8080, "observed-user", "observed-password", service.StatusActive} {
				require.Contains(t, exec.execArgs[0], expected)
			}
		})
	}
}

func TestGrokMissingProxyCASRequiresExplicitInvalidStateProof(t *testing.T) {
	for _, proof := range []string{"missing-marker", "wrong-reason", "hydrated-proxy", "no-binding", "confirmed-missing"} {
		t.Run(proof, func(t *testing.T) {
			proxyID := int64(17)
			snapshot := service.GrokCredentialMutationSnapshot{CredentialsJSON: `{"access_token":"sent"}`, ProxyID: &proxyID, ProxyMissing: true}
			reason := string(service.GrokCredentialReasonProxyInvalid)
			switch proof {
			case "missing-marker":
				snapshot.ProxyMissing = false
			case "wrong-reason":
				reason = "revoked"
			case "hydrated-proxy":
				snapshot.Proxy = &service.Proxy{ID: proxyID}
			case "no-binding":
				snapshot.ProxyID = nil
			}
			snapshot.Owner = &service.Account{ID: 42, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "sent"}, ProxyID: snapshot.ProxyID, Proxy: snapshot.Proxy}
			exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
			repo := newAccountRepositoryWithSQL(nil, exec, nil)

			applied, err := repo.SetGrokCredentialErrorIfMatch(context.Background(), 42, snapshot, reason)

			require.False(t, applied)
			if proof == "confirmed-missing" {
				require.NoError(t, err)
				require.Len(t, exec.execQueries, 1)
				require.Contains(t, exec.execQueries[0], "WITH captured_proxy AS MATERIALIZED")
				require.Contains(t, exec.execQueries[0], "FOR SHARE")
				require.Contains(t, exec.execQueries[0], "captured_proxy.deleted_at IS NULL")
			} else {
				require.Error(t, err)
				require.Empty(t, exec.execQueries)
			}
		})
	}
}

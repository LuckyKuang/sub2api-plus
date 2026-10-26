//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func credentialProxyEvidenceOwner(platform, kind string) *Account {
	proxyID := int64(117)
	return &Account{ID: 681, Platform: platform, Type: kind, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "sent-token", "api_key": "sent-token", "refresh_token": "sent-refresh"},
		ProxyID:     &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "sent.invalid", Port: 8080, Username: "sent-user", Password: "sent-password", Status: StatusActive}}
}

func TestHTTP401CredentialMutationCarriesFrozenProxyEvidence(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
			for _, repaired := range []bool{false, true} {
				for _, cooldown := range []bool{false, true} {
					if cooldown && kind != AccountTypeOAuth {
						continue
					}
					name := platform + "/" + kind + map[bool]string{false: "/error", true: "/cooldown"}[cooldown] + map[bool]string{false: "/unchanged", true: "/proxy-repaired"}[repaired]
					t.Run(name, func(t *testing.T) {
						owner := credentialProxyEvidenceOwner(platform, kind)
						if platform == PlatformZhipu && kind == AccountTypeOAuth {
							owner.Credentials["oauth_provider"] = "bigmodel"
							owner.Credentials["plan_kind"] = ZhipuPlanStartPlan
							owner.Credentials["account_mode"] = AccountModeCoding
							owner.Credentials["api_protocol"] = APIProtocolAdaptive
							owner.Credentials["zcode_jwt_token"] = "sent-token"
							owner.Credentials[credentialAPIBaseURLs] = map[string]any{APIProtocolAnthropic: "https://zcode.z.ai/api/v1/zcode-plan/anthropic"}
							owner.Credentials[anthropicAPIKeyAuthSchemeExtraKey] = AnthropicAPIKeyAuthSchemeAuthorizationBearer
						}
						selected := snapshotRequestCredentialAccount(owner)
						ctx := WithOutboundIdentityScope(context.Background(), nil)
						recordRequestCredentialOwner(ctx, owner, "sent-token")
						ctx = withRequestCredentialOwner(ctx, selected, "sent-token")
						// Selection remains mutable after acquisition; none of these
						// newer fields can replace the route actually sent.
						selected.Proxy.Protocol, selected.Proxy.Host, selected.Proxy.Port = "socks5", "selected.invalid", 9090
						selected.Proxy.Username, selected.Proxy.Password, selected.Proxy.Status = "selected-user", "selected-password", StatusDisabled
						repo := &http401CredentialCooldownRepo{http401CredentialFenceRepo: http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(owner)}}
						if repaired {
							repo.current.Proxy.Status = StatusDisabled
						}
						blocker := &runtimeBlockRecorder{}
						invalidator := &tokenCacheInvalidatorRecorder{}
						svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
						svc.SetAccountRuntimeBlocker(blocker)
						svc.SetTokenCacheInvalidator(invalidator)
						if cooldown {
							svc.handleHTTP401CredentialCooldown(ctx, selected, selected, time.Now().Add(time.Minute), "rejected")
							require.Equal(t, 1, repo.cooldownCalls)
						} else {
							svc.handleHTTP401CredentialError(ctx, selected, selected, "rejected")
							require.Equal(t, 1, repo.casCalls)
						}
						require.Equal(t, owner.ProxyID, repo.attempted.ProxyID)
						require.Equal(t, owner.Proxy, repo.attempted.Proxy, "CAS must carry every frozen proxy field, including status")
						require.Zero(t, repo.unconditionalCalls+repo.unconditionalCooldowns)
						if repaired {
							require.Equal(t, StatusActive, repo.current.Status)
							require.Nil(t, repo.current.TempUnschedulableUntil)
							require.Empty(t, blocker.accounts)
							require.Empty(t, invalidator.accounts)
						} else {
							require.Len(t, blocker.accounts, 1)
							require.Equal(t, owner.Proxy, blocker.accounts[0].Proxy)
							if cooldown {
								require.NotNil(t, repo.current.TempUnschedulableUntil)
							} else {
								require.Equal(t, StatusError, repo.current.Status)
							}
						}
					})
				}
			}
		}
	}
}

func TestRuntimeCredentialBlocksIsolateSameProxyRowStatus(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
			t.Run(platform+"/"+kind, func(t *testing.T) {
				old := credentialProxyEvidenceOwner(platform, kind)
				old.Proxy.Status = StatusDisabled
				current := snapshotRequestCredentialAccount(old)
				current.Proxy.Status = StatusActive
				svc := &OpenAIGatewayService{}
				until := time.Now().Add(time.Minute)
				svc.BlockAccountScheduling(old, until, "failed-disabled-proxy")
				oldBlock := svc.peekOpenAIAccountRuntimeBlock(old)
				require.True(t, oldBlock.blocked)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(current), "repairing status on the same proxy row starts a new runtime owner")
				svc.BlockAccountScheduling(current, until, "new-failure")
				currentBlock := svc.peekOpenAIAccountRuntimeBlock(current)
				svc.clearOpenAIAccountRuntimeBlockIfUnchanged(old.ID, oldBlock)
				require.True(t, svc.isOpenAIAccountRuntimeBlocked(current), "cleanup of the old status cannot clear the new proxy's block")
				require.Equal(t, currentBlock.generation, svc.peekOpenAIAccountRuntimeBlock(current).generation)
			})
		}
	}
}

type credentialProxyRefreshRepo struct {
	refreshCleanupRepo
	attempted *Account
}

func (r *credentialProxyRefreshRepo) UpdateOAuthRefreshCredentialsIfUnchanged(ctx context.Context, owner *Account, credentials map[string]any) (bool, error) {
	r.attempted = snapshotRequestCredentialAccount(owner)
	return r.refreshCleanupRepo.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, owner, credentials)
}

func (r *credentialProxyRefreshRepo) UpdateGrokOAuthCredentialsIfUnchanged(ctx context.Context, owner *Account, credentials map[string]any) (bool, error) {
	return r.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, owner, credentials)
}

func (r *credentialProxyRefreshRepo) UpdateCNOAuthCredentialsIfUnchanged(ctx context.Context, owner *Account, credentials map[string]any) (bool, error) {
	return r.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, owner, credentials)
}

func TestRefreshCredentialMutationCarriesAttemptedProxyEvidence(t *testing.T) {
	// These are the official grant types that rotate through OAuthRefreshAPI's
	// conditional success ports; the other official grants do not use this flow.
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformKimi, PlatformMiniMax} {
		for _, repaired := range []bool{false, true} {
			t.Run(platform+map[bool]string{false: "/unchanged", true: "/proxy-repaired"}[repaired], func(t *testing.T) {
				owner := credentialProxyEvidenceOwner(platform, AccountTypeOAuth)
				if platform == PlatformKimi || platform == PlatformMiniMax {
					owner.Credentials["oauth_provider"] = platform
				}
				repo := &credentialProxyRefreshRepo{refreshCleanupRepo: refreshCleanupRepo{current: snapshotRequestCredentialAccount(owner)}}
				executor := &dynamicRefreshExecutor{canRefresh: true, cacheKey: "proxy-evidence", needsRefreshFunc: func() bool { return true },
					refreshFunc: func(_ context.Context, fresh *Account) (map[string]any, error) {
						require.Equal(t, owner.Proxy, fresh.Proxy)
						if repaired {
							repo.current.Proxy.Status = StatusDisabled
						}
						// Upstream enrichers may mutate their local account. The CAS
						// must still compare the pre-attempt route.
						fresh.Proxy.Host, fresh.Proxy.Password = "enriched.invalid", "enriched-password"
						return map[string]any{"access_token": "rotated", "refresh_token": "rotated-refresh"}, nil
					}}
				api := NewOAuthRefreshAPI(repo, nil)
				result, err := api.RefreshIfNeeded(context.Background(), owner, executor, time.Minute)
				require.Equal(t, owner.ProxyID, repo.attempted.ProxyID)
				require.Equal(t, owner.Proxy, repo.attempted.Proxy)
				require.Equal(t, 1, repo.persistCalls)
				if repaired {
					require.Equal(t, "sent-token", repo.current.GetCredential("access_token"))
					if err == nil {
						require.False(t, result.Refreshed)
					}
				} else {
					require.NoError(t, err)
					require.True(t, result.Refreshed)
					require.Equal(t, "rotated", repo.current.GetCredential("access_token"))
				}
			})
		}
	}
}

//go:build unit

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCNQuotaProjectionRetainsObservedWindowsAndOwner(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(time.Hour)
	for _, platform := range []string{service.PlatformKimi, service.PlatformZhipu, service.PlatformMiniMax} {
		for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
			t.Run(platform+"/"+kind, func(t *testing.T) {
				proxyID := int64(8)
				account := service.Account{ID: 71, Platform: platform, Type: kind, Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"access_token": "access-secret", "refresh_token": "refresh-secret", "api_key": "api-secret", "oauth_provider": platform, "oauth_region": "cn", "account_mode": service.AccountModeCoding},
					ProxyID:     &proxyID, Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Host: "proxy.test", Port: 8080, Username: "user", Password: "proxy-secret", Status: service.StatusActive},
					Extra: map[string]any{platform + "_5h_used_percent": 100.0, platform + "_5h_reset_at": reset.Format(time.RFC3339), platform + "_weekly_unlimited": true, platform + "_weekly_used_percent": 100.0, platform + "_weekly_reset_at": now.Add(7 * 24 * time.Hour).Format(time.RFC3339)},
				}
				account.Extra[platform+"_quota_generation"] = service.CNQuotaGeneration(&account)
				require.True(t, service.EvaluateAccountSchedulingThreshold(&account, map[string]int{platform: 80}, now).ShouldPause)
				full, metadata, err := marshalSchedulerCacheAccount(account)
				require.NoError(t, err)
				require.NotContains(t, string(metadata), "access-secret")
				require.NotContains(t, string(metadata), "refresh-secret")
				require.NotContains(t, string(metadata), "proxy-secret")
				require.NotContains(t, string(full), "cn_quota_generation", "full hydration must recompute from its actual grant")
				var decoded service.Account
				require.NoError(t, json.Unmarshal(metadata, &decoded))
				decision := service.EvaluateAccountSchedulingThreshold(&decoded, map[string]int{platform: 80}, now)
				require.True(t, decision.ShouldPause)
				require.Equal(t, "5h", decision.Window)
				require.Equal(t, 100.0, decision.UsedPercent)
				require.NotNil(t, decision.Until)
				require.True(t, reset.Equal(*decision.Until))
				if platform == service.PlatformMiniMax && kind == service.AccountTypeOAuth {
					require.False(t, decoded.IsSchedulable(), "observed native plan exhaustion still blocks when early pause is disabled")
				}
				_, _, err = marshalSchedulerCacheAccount(decoded)
				require.Error(t, err, "a slim row cannot populate the hydrated cache")
				credential := "access_token"
				if kind == service.AccountTypeAPIKey {
					credential = "api_key"
				}
				account.Credentials[credential] = "replacement-secret"
				_, replacedMetadata, err := marshalSchedulerCacheAccount(account)
				require.NoError(t, err)
				decoded = service.Account{}
				require.NoError(t, json.Unmarshal(replacedMetadata, &decoded))
				require.False(t, service.EvaluateAccountSchedulingThreshold(&decoded, map[string]int{platform: 80}, now).ShouldPause, "old observation cannot follow replacement grant through projection")
			})
		}
	}
}

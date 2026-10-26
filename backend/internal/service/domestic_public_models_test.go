//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

func TestDomesticPublicModelsUsesNativeControlAuthAndSeparatesOwners(t *testing.T) {
	for _, tc := range []struct{ platform, region, target, userAgent string }{
		{"kimi", "cn", "https://api.kimi.com/coding/v1/models", "kimi-code-cli/2.3.1"},
		{"kimi", "global", "https://api.kimi.ai/coding/v1/models", "kimi-code-cli/2.3.1"},
		{"deepseek", "cn", "https://api.deepseek.com/models", "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)"},
		{"stepfun", "cn", "https://api.stepfun.com/step_plan/v1/models", "stepcode (linux 6.8.0-31-generic; x64)"},
		{"stepfun", "global", "https://api.stepfun.ai/step_plan/v1/models", "stepcode (linux 6.8.0-31-generic; x64)"},
	} {
		t.Run(tc.platform+"/"+tc.region, func(t *testing.T) {
			a := &Account{ID: 174, Platform: tc.platform, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: tc.platform, Region: tc.region}, &cnoauth.Grant{AccessToken: "native-owner-token", RefreshToken: "native-refresh", ExpiresAt: time.Now().Add(time.Hour)})}
			a.Credentials["base_url"] = "https://relay.attacker.example"
			a.Credentials["api_key"] = "copied-secret"
			a.Credentials["header_override_enabled"] = true
			a.Credentials["header_overrides"] = map[string]any{"Authorization": "Bearer untrusted", "X-Dsh-Auth-Token": "untrusted", "User-Agent": "untrusted"}
			proxyID := int64(8)
			a.ProxyID, a.Proxy = &proxyID, &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080}
			repo := &zhipuProjectQuotaRepo{account: a}
			upstream := &zhipuProjectQuotaUpstream{status: 200, body: `{"data":[{"id":"old-owner-model"}]}`}
			if tc.platform == PlatformDeepseek {
				upstream.body = `{"data":[{"id":"old-owner-model","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"0.2.0-rc.2"}}}]}`
			}
			gateway := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream, cfg: cnProbeAllowlistConfig("api.kimi.com", "api.kimi.ai", "api.deepseek.com", "api.stepfun.com", "api.stepfun.ai"), cnOAuthService: NewCNOAuthService(nil, nil, nil)}
			first, err := gateway.FetchOpenAIModelsList(context.Background(), a)
			require.NoError(t, err)
			require.Contains(t, string(first.Body), "old-owner-model")
			require.Equal(t, tc.target, upstream.request.URL.String())
			if tc.platform == "deepseek" {
				require.Empty(t, upstream.request.Header.Get("Authorization"))
				require.Equal(t, "native-owner-token", upstream.request.Header.Get("X-Dsh-Auth-Token"))
			} else {
				require.Equal(t, "Bearer native-owner-token", upstream.request.Header.Get("Authorization"))
				require.Empty(t, upstream.request.Header.Get("X-Dsh-Auth-Token"))
			}
			require.Equal(t, tc.userAgent, upstream.request.UserAgent())
			require.Equal(t, "http://proxy.example:8080", upstream.proxy)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
			a.Credentials["access_token"] = "replacement-native-owner"
			upstream.body = `{"data":[{"id":"replacement-owner-model"}]}`
			if tc.platform == PlatformDeepseek {
				upstream.body = `{"data":[{"id":"replacement-owner-model","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"0.2.0-rc.2"}}}]}`
			}
			second, err := gateway.FetchOpenAIModelsList(context.Background(), a)
			require.NoError(t, err)
			require.Contains(t, string(second.Body), "replacement-owner-model", "a replacement login must not receive the earlier owner's cached list")
			require.Equal(t, 2, upstream.calls)
		})
	}
}

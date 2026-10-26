//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStepFunCapabilityLate401CannotQuarantineReplacementOwner(t *testing.T) {
	for _, profile := range stepCapabilityProfiles {
		for _, protocol := range []string{"chat", "responses", "messages"} {
			for _, change := range []string{"unchanged", "credential_replaced", "proxy_rebound", "same_proxy_edited", "region_changed"} {
				t.Run(profile.name+"/"+protocol+"/"+change, func(t *testing.T) {
					account := stepFunTestAccount(profile.kind, profile.region)
					account.Credentials["account_mode"] = profile.mode
					account.Status, account.Schedulable = StatusActive, true
					proxyID := int64(883301)
					account.ProxyID = &proxyID
					account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "captured.invalid", Port: 8080, Username: "owner", Password: "captured-password", Status: StatusActive}
					captured := snapshotRequestCredentialAccount(account)
					repo := &http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(account)}
					blocker := &runtimeBlockRecorder{}
					rl := NewRateLimitService(repo, nil, rawChatCompletionsTestConfig(), nil, nil)
					rl.SetAccountRuntimeBlocker(blocker)
					requests := 0
					upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, route string, accountID int64, _ int) (*http.Response, error) {
						requests++
						require.Equal(t, account.ID, accountID)
						require.Equal(t, profile.endpoint, request.URL.String())
						require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
						require.Equal(t, "http://owner:captured-password@captured.invalid:8080", route)
						requireStepFunWire(t, request, true)
						switch change {
						case "credential_replaced":
							if profile.kind == "oauth" {
								repo.current.Credentials["access_token"] = "replacement-key"
							} else {
								repo.current.Credentials["api_key"] = "replacement-key"
							}
						case "proxy_rebound":
							id := int64(883302)
							repo.current.ProxyID = &id
							repo.current.Proxy.ID = id
						case "same_proxy_edited":
							repo.current.Proxy.Password = "replacement-password"
						case "region_changed":
							key := "region"
							if profile.kind == "oauth" {
								key = "oauth_region"
							}
							repo.current.Credentials[key] = map[string]string{"cn": "global", "global": "cn"}[profile.region]
						}
						return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_api_key","message":"invalid credential"}}`))}, nil
					}}
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil), accountRepo: repo, rateLimitService: rl}
					result, _, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, false, 1))
					require.Error(t, err)
					require.Nil(t, result)
					require.Equal(t, 1, requests)
					require.Equal(t, 1, repo.casCalls)
					require.Zero(t, repo.unconditionalCalls)
					require.Equal(t, captured.Credentials, repo.attempted.Credentials, "mutation must belong to the actual sent model key/profile")
					require.Equal(t, captured.ProxyID, repo.attempted.ProxyID)
					require.Equal(t, captured.Proxy, repo.attempted.Proxy, "same proxy ID still requires its complete captured route")
					if change == "unchanged" {
						require.Equal(t, StatusError, repo.current.Status)
						require.False(t, repo.current.Schedulable)
						require.Len(t, blocker.accounts, 1)
					} else {
						require.Equal(t, StatusActive, repo.current.Status)
						require.True(t, repo.current.Schedulable)
						require.Empty(t, blocker.accounts, "late failure must not poison replacement owner's runtime eligibility")
					}
				})
			}
		}
	}
}

//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

// The official ZCode project-access-token contract applies at every physical
// send, including probes, discovery, retries and a new failover credential owner.
func TestZhipuProjectPhysicalTransportMatrix(t *testing.T) {
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, plan := range []string{ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan} {
			for _, path := range []string{"gateway-chat", "gateway-messages", "gateway-count", "probe", "probe-tls", "discovery"} {
				t.Run(provider+"/"+plan+"/"+path, func(t *testing.T) {
					a := projectTokenAccount()
					a.Credentials["oauth_provider"], a.Credentials["plan_kind"] = provider, plan
					proxyID := int64(8)
					a.ProxyID = &proxyID
					a.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080}
					a.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "0.16.9"}
					issuer := &projectIssuerStub{issue: func(ctx context.Context, login string) (*zcode.ProjectAccessToken, error) {
						identity, ok := outboundidentity.FromContext(ctx)
						require.True(t, ok)
						require.Equal(t, "account", identity.Source)
						require.Equal(t, "0.16.9", identity.Version)
						require.Equal(t, a.ID, identity.AccountID)
						require.Equal(t, "owner-login", login)
						return &zcode.ProjectAccessToken{Token: "physical-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
					}}
					tokens := NewZhipuProjectTokenManager(issuer, nil)
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}}
					gateway := &OpenAIGatewayService{httpUpstream: upstream, projectTokens: tokens}
					tester := &AccountTestService{httpUpstream: upstream, projectTokens: tokens}
					method, target := http.MethodPost, zhipuCodingBaseURL(provider)+"/chat/completions"
					switch path {
					case "gateway-messages":
						target = zhipuAnthropicBaseURL(provider) + "/v1/messages"
					case "gateway-count":
						target = zhipuAnthropicBaseURL(provider) + "/v1/messages/count_tokens"
					case "discovery":
						method, target = http.MethodGet, zhipuCodingBaseURL(provider)+"/models"
					}
					req, err := http.NewRequest(method, target, strings.NewReader(`{"model":"glm-test"}`))
					require.NoError(t, err)
					if path == "discovery" {
						ctx, snapshotErr := withCNControlRequestOwner(req.Context(), a)
						require.NoError(t, snapshotErr)
						req = req.WithContext(ctx)
					}
					req.Header.Set("Authorization", "Bearer copied-secret")
					req.Header.Set("X-Api-Key", "public-key-id")
					req.Header.Set("User-Agent", "inbound/999")
					req.Header.Set("X-ZCode-App-Version", "999.0.0")
					req.Header.Set("bigmodel-organization", "untrusted-org")
					req.Header.Set("bigmodel-project", "untrusted-project")
					var response *http.Response
					switch path {
					case "probe", "probe-tls":
						response, err = tester.doOpenAIAccountTestUpstream(req, "http://proxy.example:8080", a, path == "probe-tls")
					case "discovery":
						response, err = tester.doUpstreamModelsRequest(req, a)
					default:
						response, err = gateway.doOpenAIUpstream(req, "http://proxy.example:8080", a)
					}
					require.NoError(t, err)
					require.NotNil(t, response)
					require.NoError(t, response.Body.Close())
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "Bearer physical-pat", upstream.lastReq.Header.Get("Authorization"))
					require.Empty(t, upstream.lastReq.Header.Get("X-Api-Key"))
					// Oracle: OUTBOUND_IDENTITY.md's reviewed per-protocol SDK
					// wire experiment; discovery carries the product UA alone.
					wantUA := "ZCode/0.16.9 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22"
					if path == "gateway-messages" || path == "gateway-count" {
						wantUA = "ZCode/0.16.9 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22"
					} else if path == "discovery" {
						wantUA = "ZCode/0.16.9"
					}
					require.Equal(t, wantUA, upstream.lastReq.UserAgent())
					require.Equal(t, "0.16.9", upstream.lastReq.Header.Get("X-ZCode-App-Version"))
					require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
					require.Equal(t, "http://proxy.example:8080", upstream.lastProxyURL)
					if plan == ZhipuPlanTeamCodingPlan {
						require.Equal(t, "org", upstream.lastReq.Header.Get("bigmodel-organization"))
						require.Equal(t, "project", upstream.lastReq.Header.Get("bigmodel-project"))
					} else {
						require.Empty(t, upstream.lastReq.Header.Get("bigmodel-organization"), "personal requests must omit team scope declarations")
						require.Empty(t, upstream.lastReq.Header.Get("bigmodel-project"))
					}
				})
			}
		}
	}
}

func TestZhipuProjectTransportRetryAndFailoverResolveOwner(t *testing.T) {
	a := projectTokenAccount()
	issuer := &projectIssuerStub{}
	issuer.issue = func(_ context.Context, login string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: fmt.Sprintf("%s-pat-%d", login, issuer.calls.Load()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	tokens := NewZhipuProjectTokenManager(issuer, nil)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))},
		{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))},
	}}
	service := &OpenAIGatewayService{httpUpstream: upstream, projectTokens: tokens}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	for attempt := 0; attempt < 3; attempt++ {
		owner := a
		if attempt == 2 {
			owner = snapshotOAuthRefreshAccount(a)
			owner.ID = 95
			owner.Credentials["access_token"] = "failover-login"
			owner.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "0.16.9"}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, zhipuCodingBaseURL("bigmodel")+"/chat/completions", nil)
		require.NoError(t, err)
		resp, err := service.doOpenAIUpstream(req, "", owner)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NoError(t, resp.Body.Close())
		identity, ok := outboundidentity.FromContext(upstream.lastReq.Context())
		require.True(t, ok)
		require.Equal(t, owner.ID, identity.AccountID)
	}
	require.EqualValues(t, 3, issuer.calls.Load())
	require.Len(t, upstream.requests, 4, "the initial 401 recovery, subsequent turn and failover are distinct physical sends")
	require.Equal(t, "Bearer owner-login-pat-1", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(t, "Bearer owner-login-pat-2", upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, "Bearer owner-login-pat-2", upstream.requests[2].Header.Get("Authorization"), "the next turn retains the recovered owner's valid PAT")
	require.Equal(t, "Bearer failover-login-pat-3", upstream.requests[3].Header.Get("Authorization"))
	require.Equal(t, "ZCode/0.16.9 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22", upstream.requests[3].UserAgent())
}

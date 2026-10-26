//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokOAuthSelectedProxyClient struct {
	grokOAuthClientStub
	sends  int
	routes []string
}

func (c *grokOAuthSelectedProxyClient) ExchangeCode(_ context.Context, _, _, _, proxy, _ string) (*xai.TokenResponse, error) {
	c.sends++
	c.routes = append(c.routes, proxy)
	return &xai.TokenResponse{AccessToken: "fake-exchange-token", IDToken: "verified-id-token"}, nil
}

func (c *grokOAuthSelectedProxyClient) RefreshToken(_ context.Context, _, proxy, _ string) (*xai.TokenResponse, error) {
	c.sends++
	c.routes = append(c.routes, proxy)
	return &xai.TokenResponse{AccessToken: "fake-refreshed-token"}, nil
}

func (c *grokOAuthSelectedProxyClient) ConvertSSOToBuild(_ context.Context, _, proxy string) (*xai.TokenResponse, error) {
	c.sends++
	c.routes = append(c.routes, proxy)
	return &xai.TokenResponse{AccessToken: "fake-converted-token"}, nil
}

func TestGrokOAuthExplicitProxyMustResolveBeforeAnyCredentialSend(t *testing.T) {
	for _, invalid := range []string{"no_repository", "deleted", "lookup_failed", "zero_id", "mismatched", "invalid_protocol", "empty_host", "invalid_port"} {
		t.Run(invalid, func(t *testing.T) {
			id := int64(121)
			proxy := &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 8080}
			repo := &openAISelectedProxyRepo{proxy: proxy}
			errorCode := "GROK_OAUTH_PROXY_INVALID"
			var selectedRepo ProxyRepository = repo
			switch invalid {
			case "no_repository":
				selectedRepo = nil
				errorCode = "GROK_OAUTH_PROXY_NOT_AVAILABLE"
			case "deleted":
				repo.proxy = nil
				errorCode = "GROK_OAUTH_PROXY_NOT_FOUND"
			case "lookup_failed":
				repo.err = errors.New("proxy database unavailable")
				errorCode = "GROK_OAUTH_PROXY_LOOKUP_FAILED"
			case "zero_id":
				id = 0
			case "mismatched":
				proxy.ID++
			case "invalid_protocol":
				proxy.Protocol = "file"
			case "empty_host":
				proxy.Host = ""
			case "invalid_port":
				proxy.Port = 65536
			}
			client := &grokOAuthSelectedProxyClient{}
			svc := NewGrokOAuthService(selectedRepo, client)
			t.Cleanup(svc.Stop)
			_, err := svc.GenerateAuthURL(context.Background(), &id, "")
			require.ErrorContains(t, err, errorCode)
			_, err = svc.ValidateSSOToken(context.Background(), "fake-sso-token", &id)
			require.ErrorContains(t, err, errorCode)
			_, err = svc.ValidateRefreshToken(context.Background(), "fake-refresh-token", &id)
			require.ErrorContains(t, err, errorCode)
			// A callback cannot add a proxy to a genuine session that began
			// without one. Route ownership is pinned before the browser opens.
			auth, err := svc.GenerateAuthURL(context.Background(), nil, "")
			require.NoError(t, err)
			_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{SessionID: auth.SessionID, State: auth.State, Code: "fake-code", ProxyID: &id})
			require.ErrorContains(t, err, "GROK_OAUTH_PROXY_MISMATCH")
			_, exists := svc.sessionStore.Get(auth.SessionID)
			require.True(t, exists, "invalid proxy preparation must not consume the usable OAuth session")
			require.Zero(t, client.sends)
		})
	}
}

func TestGrokOAuthExplicitValidProxyIsUsedForEveryCredentialFlow(t *testing.T) {
	proxy := &Proxy{ID: 122, Protocol: "http", Host: "127.0.0.1", Port: 8080}
	client := &grokOAuthSelectedProxyClient{}
	svc := NewGrokOAuthService(&openAISelectedProxyRepo{proxy: proxy}, client)
	t.Cleanup(svc.Stop)
	auth, err := svc.GenerateAuthURL(context.Background(), &proxy.ID, "")
	require.NoError(t, err)
	session, exists := svc.sessionStore.Get(auth.SessionID)
	require.True(t, exists)
	require.Equal(t, proxy.URL(), session.ProxyURL)
	_, err = svc.ValidateSSOToken(context.Background(), "fake-sso-token", &proxy.ID)
	require.NoError(t, err)
	_, err = svc.ValidateRefreshToken(context.Background(), "fake-refresh-token", &proxy.ID)
	require.NoError(t, err)
	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{SessionID: auth.SessionID, State: auth.State, Code: "fake-code", ProxyID: &proxy.ID})
	require.NoError(t, err)
	require.Equal(t, 3, client.sends)
	require.Equal(t, []string{proxy.URL(), proxy.URL(), proxy.URL()}, client.routes)
}

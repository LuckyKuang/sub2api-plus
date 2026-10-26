//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type openAICallbackProxyClient struct {
	OpenAIOAuthClient
	calls int
	proxy string
}

func (c *openAICallbackProxyClient) ExchangeCodeWithIdentity(_ context.Context, _ string, _ string, _ string, proxyURL string, _ string, _ string, _ string, _ string) (*openai.TokenResponse, error) {
	c.calls++
	c.proxy = proxyURL
	return &openai.TokenResponse{AccessToken: "issued-access", ExpiresIn: 3600}, nil
}

func TestOpenAIOAuthCallbackRetainsOriginalProxy(t *testing.T) {
	for _, mode := range []string{"no-override", "same-id-edited-row", "different-id", "bind-mid-login"} {
		t.Run(mode, func(t *testing.T) {
			id := int64(51)
			repo := &grokOIDCSessionProxyRepo{proxy: &Proxy{ID: id, Protocol: "http", Host: "captured.example", Port: 8080}}
			client := &openAICallbackProxyClient{}
			svc := NewOpenAIOAuthService(repo, client)
			t.Cleanup(svc.Stop)
			svc.SetPrivacyClientFactory(func(string) (*req.Client, error) { return nil, errors.New("skip optional enrichment") })
			var selected *int64 = &id
			if mode == "bind-mid-login" {
				selected = nil
			}
			start, err := svc.GenerateAuthURL(context.Background(), selected, "", PlatformOpenAI)
			require.NoError(t, err)
			session, ok := svc.sessionStore.Get(start.SessionID)
			require.True(t, ok)
			input := &OpenAIExchangeCodeInput{SessionID: start.SessionID, State: session.State, Code: "private-code"}
			switch mode {
			case "same-id-edited-row":
				repo.proxy.Host = "replacement.example"
				input.ProxyID = &id
			case "different-id":
				other := int64(52)
				repo.proxy.ID = other
				input.ProxyID = &other
			case "bind-mid-login":
				input.ProxyID = &id
			}
			info, err := svc.ExchangeCode(context.Background(), input)
			if mode == "different-id" || mode == "bind-mid-login" {
				require.Error(t, err)
				require.Nil(t, info)
				require.Zero(t, client.calls)
				_, ok := svc.sessionStore.Get(start.SessionID)
				require.True(t, ok)
			} else {
				require.NoError(t, err)
				require.Equal(t, "http://captured.example:8080", client.proxy)
			}
			expected := 1
			if selected == nil {
				expected = 0
			}
			require.Equal(t, expected, repo.calls, "callback must retain the login route without re-reading a changed row")
		})
	}
}

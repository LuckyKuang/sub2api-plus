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

// A credential owner retains its hydrated route across refresh/revoke; only an
// account with no binding authorizes direct egress (OUTBOUND_IDENTITY.md).
type openAIProxySnapshotClient struct {
	OpenAIOAuthClient
	calls int
	proxy string
}

func (c *openAIProxySnapshotClient) RefreshTokenWithClientIDAndIdentity(_ context.Context, _ string, proxyURL string, _ string, _ string, _ string, _ string) (*openai.TokenResponse, error) {
	c.calls++
	c.proxy = proxyURL
	return &openai.TokenResponse{AccessToken: "rotated-access", ExpiresIn: 3600}, nil
}
func (c *openAIProxySnapshotClient) RevokeToken(_ context.Context, _ string, _ string, _ string, proxyURL string, _ string, _ string) error {
	c.calls++
	c.proxy = proxyURL
	return nil
}

func TestOpenAIOAuthAccountOperationsRetainStrictProxySnapshot(t *testing.T) {
	for _, operation := range []string{"refresh", "revoke"} {
		for _, route := range []string{"captured", "unbound", "missing", "different-id", "invalid"} {
			t.Run(operation+"/"+route, func(t *testing.T) {
				id := int64(53)
				account := &Account{ID: 54, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "captured.example", Port: 8080}, Credentials: map[string]any{"access_token": "old-access", "refresh_token": "private-refresh"}}
				switch route {
				case "unbound":
					account.ProxyID = nil
				case "missing":
					account.Proxy = nil
				case "different-id":
					account.Proxy.ID++
				case "invalid":
					account.Proxy.Protocol = "file"
				}
				repo := &grokOIDCSessionProxyRepo{proxy: &Proxy{ID: id, Protocol: "http", Host: "replacement.example", Port: 8888}}
				client := &openAIProxySnapshotClient{}
				svc := NewOpenAIOAuthService(repo, client)
				t.Cleanup(svc.Stop)
				svc.SetPrivacyClientFactory(func(string) (*req.Client, error) { return nil, errors.New("no optional enrichment in route contract") })
				var err error
				if operation == "refresh" {
					_, err = svc.RefreshAccountToken(context.Background(), account)
				} else {
					err = svc.RevokeAccountTokens(context.Background(), account)
				}
				if route == "captured" || route == "unbound" {
					require.NoError(t, err)
					require.Equal(t, 1, client.calls)
					expected := "http://captured.example:8080"
					if route == "unbound" {
						expected = ""
					}
					require.Equal(t, expected, client.proxy)
				} else {
					require.Error(t, err)
					require.Zero(t, client.calls)
				}
				require.Zero(t, repo.calls, "an account-owned operation cannot hydrate its route from a newer proxy row")
			})
		}
	}
}

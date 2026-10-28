//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type countTokensOwnerCache struct {
	GeminiTokenCache
	reads, writes int
	beforeRead    func()
}

func (c *countTokensOwnerCache) GetAccessToken(context.Context, string) (string, error) {
	c.reads++
	if c.beforeRead != nil {
		c.beforeRead()
	}
	return "captured-count-token", nil
}

func (c *countTokensOwnerCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	c.writes++
	return nil
}

func countTokensOwnerAccount() *Account {
	proxyID := int64(7981)
	return &Account{ID: 7982, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3,
		Credentials: map[string]any{
			"access_token": "captured-count-token", "refresh_token": "captured-count-refresh",
			"user_agent": "codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color",
		},
		ProxyID: &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "captured.count.proxy.example", Port: 8080,
			Username: "captured-count-user", Password: "captured-count-password", Status: StatusActive},
	}
}

func runCountTokensOwnerEntry(t *testing.T, gateway *OpenAIGatewayService, account *Account, entry string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	response := httptest.NewRecorder()
	inbound, _ := gin.CreateTestContext(response)
	path := "/v1/responses/input_tokens"
	if entry == "messages-count" {
		path = "/v1/messages/count_tokens"
	}
	inbound.Request = httptest.NewRequest(http.MethodPost, path, nil)
	inbound.Request.Header.Set("User-Agent", "untrusted-inbound/9.9")
	inbound.Request.Header.Set("Originator", "untrusted-client")
	inbound.Request.Header.Set("Version", "9.9")
	if entry == "messages-count" {
		return response, gateway.ForwardCountTokensAsAnthropic(context.Background(), inbound, account,
			[]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello world"}]}`), "gpt-5.4")
	}
	return response, gateway.ForwardResponsesInputTokens(context.Background(), inbound, account,
		[]byte(`{"model":"gpt-5.4","input":[{"role":"user","content":"hello world"}]}`))
}

func countTokensOwnerGateway(cache *countTokensOwnerCache, send func(*http.Request, string, int64, int) (*http.Response, error)) *OpenAIGatewayService {
	return &OpenAIGatewayService{cfg: &config.Config{},
		openAITokenProvider: NewOpenAITokenProvider(nil, cache, nil),
		httpUpstream:        &codexModelsHTTPUpstreamStub{do: send},
	}
}

func assertCountTokensCapturedSend(t *testing.T, req *http.Request, proxy string, accountID int64, concurrency int, unbound bool) {
	t.Helper()
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "https://api.openai.com/v1/responses/input_tokens", req.URL.String())
	require.Equal(t, "Bearer captured-count-token", req.Header.Get("Authorization"))
	require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", req.UserAgent())
	require.Equal(t, "codex_cli_rs", req.Header.Get("Originator"))
	require.Equal(t, "0.165.0", req.Header.Get("Version"))
	require.Empty(t, req.Header.Get("Agent-Authorization"))
	require.Equal(t, int64(7982), accountID)
	require.Equal(t, 3, concurrency)
	if unbound {
		require.Empty(t, proxy, "an explicit unbinding ignores the residual proxy object")
	} else {
		require.Equal(t, "http://captured-count-user:captured-count-password@captured.count.proxy.example:8080", proxy)
	}
}

// The common sender already rejects malformed routes. This independently pins
// the earlier boundary: a token-count request that needs upstream HTTP must
// reject its broken binding before token cache/issuer work or state mutation.
func TestCountTokensUpstreamRejectsBrokenProxyBeforeCredentialLookup(t *testing.T) {
	for _, entry := range []string{"responses-native", "messages-count"} {
		for _, binding := range openAIStrictPreflightProxyModes {
			t.Run(entry+"/"+binding, func(t *testing.T) {
				account := countTokensOwnerAccount()
				setOpenAIStrictPreflightProxyMode(account, binding)
				before, err := json.Marshal(account.Credentials)
				require.NoError(t, err)
				cache := &countTokensOwnerCache{}
				sends := 0
				gateway := countTokensOwnerGateway(cache, func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
					sends++
					assertCountTokensCapturedSend(t, req, proxy, id, concurrency, binding == "explicitly-unbound")
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(strings.NewReader(`{"object":"response.input_tokens","input_tokens":42}`))}, nil
				})
				response, err := runCountTokensOwnerEntry(t, gateway, account, entry)
				if binding == "valid-bound" || binding == "explicitly-unbound" {
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, response.Code)
					require.Contains(t, response.Body.String(), `"input_tokens":42`)
					require.Equal(t, 1, cache.reads)
					require.Equal(t, 1, sends)
				} else {
					require.Error(t, err)
					require.Zero(t, cache.reads, "a broken bound route must not consume or refresh an OAuth credential")
					require.Zero(t, sends, "the existing final sender still must reject the broken route")
					for _, secret := range []string{"captured-count-token", "captured-count-refresh", "captured-count-user", "captured-count-password"} {
						require.NotContains(t, err.Error(), secret)
						require.NotContains(t, response.Body.String(), secret)
					}
				}
				after, err := json.Marshal(account.Credentials)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
				require.Zero(t, cache.writes)
			})
		}
	}
}

// Lookup may overlap another management operation on the same cached row.
// Its mutation must not combine a prior bearer with the new route, identity or
// authentication mode. The original captured request remains independently valid.
func TestCountTokensUpstreamKeepsCapturedOwnerDuringTokenLookup(t *testing.T) {
	for _, entry := range []string{"responses-native", "messages-count"} {
		for _, change := range []string{"proxy-route", "credential-relink", "identity", "saved-unbinding", "saved-binding"} {
			t.Run(entry+"/"+change, func(t *testing.T) {
				account := countTokensOwnerAccount()
				if change == "saved-binding" {
					account.ProxyID = nil
				}
				cache := &countTokensOwnerCache{beforeRead: func() {
					switch change {
					case "proxy-route":
						account.Proxy.Host, account.Proxy.Password = "replacement.count.proxy.example", "replacement-route-password"
					case "credential-relink":
						account.Credentials = map[string]any{"access_token": "replacement-count-token", "auth_mode": OpenAIAuthModeAgentIdentity}
					case "identity":
						account.Credentials["user_agent"] = "codex-tui/9.9.9 (Mac OS X 14.0; arm64) iTerm (codex-tui; 9.9.9)"
					case "saved-unbinding":
						account.ProxyID = nil
					case "saved-binding":
						id := int64(7983)
						account.ProxyID = &id
						account.Proxy = &Proxy{ID: id, Protocol: "http", Host: "replacement.count.proxy.example", Port: 9090, Status: StatusActive}
					}
				}}
				sends := 0
				gateway := countTokensOwnerGateway(cache, func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
					sends++
					assertCountTokensCapturedSend(t, req, proxy, id, concurrency, change == "saved-binding")
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(strings.NewReader(`{"object":"response.input_tokens","input_tokens":42}`))}, nil
				})
				response, err := runCountTokensOwnerEntry(t, gateway, account, entry)
				require.NoError(t, err, "a same-row edit cannot replace the captured request's credential owner")
				require.Equal(t, http.StatusOK, response.Code)
				require.Equal(t, 1, cache.reads)
				require.Zero(t, cache.writes)
				require.Equal(t, 1, sends)
			})
		}
	}
}

// These routes are deliberately local. A broken saved proxy does not create a
// reason to consume credentials, refresh a grant or send any provider request.
func TestCountTokensLocalEstimatesKeepZeroCredentialAndNetworkEffects(t *testing.T) {
	for _, entry := range []string{"responses-native", "messages-count"} {
		providers := []string{"deepseek", "kimi", "minimax", "stepfun", "zhipu"}
		if entry == "responses-native" {
			// Grok Messages count_tokens is dispatched to its own local handler;
			// the OpenAI Messages bridge still sends for a custom OpenAI relay.
			providers = append(providers, "grok", "custom-relay")
		}
		for _, provider := range providers {
			t.Run(entry+"/"+provider, func(t *testing.T) {
				account := countTokensOwnerAccount()
				setOpenAIStrictPreflightProxyMode(account, "missing-bound-snapshot")
				if provider == "custom-relay" {
					account.Type = AccountTypeAPIKey
					account.Credentials = map[string]any{"api_key": "local-only-key", "base_url": "https://relay.count.example/v1"}
				} else {
					account.Platform = provider
					if provider != "grok" {
						account.Type = AccountTypeAPIKey
						account.Credentials = map[string]any{"api_key": "local-only-key", "api_protocol": "anthropic"}
					}
				}
				cache := &countTokensOwnerCache{}
				sends := 0
				gateway := countTokensOwnerGateway(cache, func(*http.Request, string, int64, int) (*http.Response, error) {
					sends++
					return nil, errors.New("local count_tokens unexpectedly selected an upstream sender")
				})
				gateway.grokTokenProvider = NewGrokTokenProvider(nil, cache)
				response, err := runCountTokensOwnerEntry(t, gateway, account, entry)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.Code)
				var result struct {
					InputTokens int `json:"input_tokens"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.Positive(t, result.InputTokens)
				require.Zero(t, cache.reads, "a local estimate must not select an issuer or token cache")
				require.Zero(t, cache.writes)
				require.Zero(t, sends)
			})
		}
	}
}

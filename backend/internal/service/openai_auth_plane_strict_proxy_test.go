//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestAgentIdentityTaskProxyFailuresNeverDialDirect(t *testing.T) {
	_, privateKey := newTestAgentIdentityKey(t)
	for _, route := range []string{"missing", "mismatched", "invalid", "timeout", "refused", "authentication_required", "proxy", "unbound"} {
		t.Run(route, func(t *testing.T) {
			var destinationCalls, destinationConnections, proxyCalls atomic.Int32
			destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				destinationCalls.Add(1)
				_, _ = w.Write([]byte(`{"task_id":"direct-task"}`))
			}))
			destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					destinationConnections.Add(1)
				}
			}
			destination.Start()
			t.Cleanup(destination.Close)
			oldBase := openAIAgentIdentityAuthAPIBaseURL
			openAIAgentIdentityAuthAPIBaseURL = destination.URL
			t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = oldBase })
			proxyID := int64(91)
			account := &Account{ID: 92, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &proxyID, Credentials: map[string]any{
				"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-test", "agent_private_key": privateKey,
			}}
			switch route {
			case "missing":
			case "mismatched":
				account.Proxy = &Proxy{ID: 999, Protocol: "http", Host: "127.0.0.1", Port: 80}
			case "invalid":
				account.Proxy = &Proxy{ID: proxyID, Protocol: "file", Host: "127.0.0.1", Port: 80}
			case "refused":
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				account.Proxy = openAIAuthPlaneStrictProxy(t, "http://"+listener.Addr().String(), proxyID)
				require.NoError(t, listener.Close())
			case "unbound":
				account.ProxyID = nil
			default:
				release := make(chan struct{})
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxyCalls.Add(1)
					if route == "timeout" {
						<-release
						return
					}
					if route == "authentication_required" {
						w.WriteHeader(http.StatusProxyAuthRequired)
						return
					}
					_, _ = w.Write([]byte(`{"task_id":"proxied-task"}`))
				}))
				t.Cleanup(proxy.Close)
				t.Cleanup(func() { close(release) })
				account.Proxy = openAIAuthPlaneStrictProxy(t, proxy.URL, proxyID)
			}
			// Use the production gateway credential acquisition entrypoint. Task
			// bootstrap happens before the eventual inference send boundary.
			repo := &agentIdentityCredentialsRepo{account: snapshotRequestCredentialAccount(account)}
			svc := &OpenAIGatewayService{accountRepo: repo}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			err := svc.ensureAgentIdentityTask(ctx, account, "")
			if route == "proxy" || route == "unbound" {
				require.NoError(t, err)
				require.NotEmpty(t, repo.credentials["task_id"])
			} else {
				require.Error(t, err)
				require.Empty(t, repo.credentials, "failed proxy attempts cannot persist a task")
			}
			if route == "unbound" {
				require.Equal(t, int32(1), destinationCalls.Load())
			} else {
				require.Zero(t, destinationCalls.Load())
				require.Zero(t, destinationConnections.Load())
			}
			if route == "proxy" || route == "timeout" || route == "authentication_required" {
				require.Positive(t, proxyCalls.Load())
			}
		})
	}
}

func TestOpenAIPATAccountRefreshMissingProxyStopsBeforeWhoami(t *testing.T) {
	var calls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"email":"test@example.com"}`))
	}))
	t.Cleanup(destination.Close)
	oldURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = destination.URL
	t.Cleanup(func() { openAICodexPATWhoamiURL = oldURL })
	proxyID := int64(91)
	svc := NewOpenAIOAuthService(nil, nil)
	t.Cleanup(svc.Stop)
	_, err := svc.RefreshAccountToken(context.Background(), &Account{ID: 93, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &proxyID, Credentials: map[string]any{
		"auth_mode": OpenAIAuthModePersonalAccessToken, "access_token": "at-test",
	}})
	require.Error(t, err)
	require.Zero(t, calls.Load())
}

func TestOpenAIPATRefreshRetainsCapturedCredentialAndProxy(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(destination.Close)
	oldURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = destination.URL
	t.Cleanup(func() { openAICodexPATWhoamiURL = oldURL })
	sentAuthorization := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		sentAuthorization <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"test@example.com","chatgpt_user_id":"user","chatgpt_account_id":"account","chatgpt_plan_type":"plus","chatgpt_account_is_fedramp":false}`))
	}))
	t.Cleanup(proxy.Close)
	selected := openAIAuthPlaneStrictProxy(t, proxy.URL, 94)
	account := &Account{ID: 95, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &selected.ID, Proxy: selected, Credentials: map[string]any{
		"auth_mode": OpenAIAuthModePersonalAccessToken, "access_token": "at-captured",
	}}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, account, "at-captured")
	ctx = withRequestCredentialOwner(ctx, account, "at-captured")
	account.ProxyID, account.Proxy = nil, nil
	account.Credentials["access_token"] = "at-replaced"
	svc := NewOpenAIOAuthService(nil, nil)
	t.Cleanup(svc.Stop)
	info, err := svc.RefreshAccountToken(ctx, account)
	require.NoError(t, err)
	require.Equal(t, "at-captured", info.AccessToken)
	require.Equal(t, "Bearer at-captured", <-sentAuthorization)
	require.Equal(t, int32(1), proxyCalls.Load())
	require.Zero(t, directCalls.Load())
}

func TestOpenAIReauthorizationWithoutProxySelectionKeepsAccountRoute(t *testing.T) {
	proxy := &Proxy{ID: 96, Protocol: "http", Host: "127.0.0.1", Port: 1096}
	account := &Account{ID: 97, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &proxy.ID, Proxy: proxy}
	svc := NewOpenAIOAuthService(nil, nil)
	t.Cleanup(svc.Stop)
	result, err := svc.GenerateAuthURLForAccount(context.Background(), account, nil, "", PlatformOpenAI)
	require.NoError(t, err)
	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Equal(t, proxy.URL(), session.ProxyURL)
	account.Proxy = nil
	_, err = svc.GenerateAuthURLForAccount(context.Background(), account, nil, "", PlatformOpenAI)
	require.Error(t, err, "an unavailable existing route cannot silently become direct during reauthorization")
}

type openAISelectedProxyRepo struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (r *openAISelectedProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	return r.proxy, r.err
}

type openAISelectedProxyClient struct {
	openaiOAuthClientRefreshStub
	calls int
}

func (c *openAISelectedProxyClient) StartDeviceCode(context.Context, string, string) (*openai.DeviceUserCodeResponse, error) {
	c.calls++
	return nil, errors.New("unexpected start")
}

func (c *openAISelectedProxyClient) ExchangeCodeWithIdentity(context.Context, string, string, string, string, string, string, string, string) (*openai.TokenResponse, error) {
	c.calls++
	return nil, errors.New("unexpected exchange")
}

func TestOpenAIOAuthExplicitProxyMustResolveBeforeStartingOrExchanging(t *testing.T) {
	proxyID := int64(101)
	for _, invalid := range []string{"no_repository", "deleted", "lookup_failed", "mismatched", "invalid"} {
		t.Run(invalid, func(t *testing.T) {
			var repo ProxyRepository
			if invalid != "no_repository" {
				selected := &openAISelectedProxyRepo{}
				switch invalid {
				case "lookup_failed":
					selected.err = errors.New("database unavailable")
				case "mismatched":
					selected.proxy = &Proxy{ID: 999, Protocol: "http", Host: "127.0.0.1", Port: 80}
				case "invalid":
					selected.proxy = &Proxy{ID: proxyID, Protocol: "file", Host: "127.0.0.1", Port: 80}
				}
				repo = selected
			}
			client := &openAISelectedProxyClient{}
			svc := NewOpenAIOAuthService(repo, client)
			t.Cleanup(svc.Stop)
			_, err := svc.GenerateAuthURL(context.Background(), &proxyID, "", PlatformOpenAI)
			require.Error(t, err)
			_, err = svc.StartDeviceCode(context.Background(), &proxyID, PlatformOpenAI)
			require.Error(t, err)
			svc.sessionStore.Set("test-session", &openai.OAuthSession{State: "test-state", CreatedAt: time.Now()})
			_, err = svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: "test-session", State: "test-state", Code: "code", ProxyID: &proxyID})
			require.Error(t, err)
			require.ErrorContains(t, err, "OPENAI_OAUTH_PROXY_MISMATCH", "a callback cannot replace the route captured in its authorization session")
			require.Zero(t, client.calls, "selected-proxy failures must stop before any credential client call")
		})
	}
}

func openAIAuthPlaneStrictProxy(t *testing.T, address string, id int64) *Proxy {
	t.Helper()
	u, err := url.Parse(address)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &Proxy{ID: id, Protocol: u.Scheme, Host: u.Hostname(), Port: port, Status: StatusActive}
}

//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokRealtimeHandlerTokenCache struct {
	service.GeminiTokenCache
	reads, writes, deletes int
	onRead                 func()
}

func (c *grokRealtimeHandlerTokenCache) GetAccessToken(context.Context, string) (string, error) {
	c.reads++
	if c.onRead != nil {
		c.onRead()
	}
	return "handler-original-token", nil
}

func (c *grokRealtimeHandlerTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	c.writes++
	return nil
}

func (c *grokRealtimeHandlerTokenCache) DeleteAccessToken(context.Context, string) error {
	c.deletes++
	return nil
}

func (*grokRealtimeHandlerTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (*grokRealtimeHandlerTokenCache) ReleaseRefreshLock(context.Context, string) error { return nil }

func grokRealtimePreflightHandler(t *testing.T, account service.Account, cache *grokRealtimeHandlerTokenCache) (*OpenAIGatewayHandler, *grokCredentialHandlerRepo, *grokMediaSlotsCache) {
	t.Helper()
	repo := &grokCredentialHandlerRepo{accounts: []service.Account{account}, missingOnGet: map[int64]bool{}}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	provider := service.NewGrokTokenProvider(repo, cache)
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, nil, nil, nil, nil, nil, nil, provider, nil, nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	return h, repo, slots
}

func grokRealtimePreflightAccount(proxy *service.Proxy) service.Account {
	proxyID := proxy.ID
	return service.Account{ID: 7952, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{24},
		ProxyID: &proxyID, Proxy: proxy, Credentials: map[string]any{"access_token": "handler-original-token", "refresh_token": "handler-original-refresh", "expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339)}}
}

func grokRealtimePreflightContext() (*gin.Context, *httptest.ResponseRecorder) {
	c, recorder := grokMediaSlotContext(context.Background(), false)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime?model=grok-voice-latest", nil)
	c.Request.Header.Set("Connection", "Upgrade")
	c.Request.Header.Set("Upgrade", "websocket")
	c.Request.Header.Set("Sec-WebSocket-Version", "13")
	c.Request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return c, recorder
}

func grokRealtimePreflightProxy(t *testing.T, calls *atomic.Int64) *service.Proxy {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	return &service.Proxy{ID: 7951, Protocol: "http", Host: parsed.Hostname(), Port: port, Username: "handler-proxy-user", Password: "handler-proxy-password", Status: service.StatusActive}
}

// The real HTTP handler acquires credentials separately from opening the WS.
// Its request entrance must validate the captured route before that credential
// lookup; fixing only OpenGrokRealtime is too late to enforce this boundary.
func TestGrokRealtimeHandlerRejectsBrokenProxyBeforeCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"missing", "mismatched", "malformed", "incomplete-auth"} {
		t.Run(mode, func(t *testing.T) {
			var proxyCalls atomic.Int64
			account := grokRealtimePreflightAccount(grokRealtimePreflightProxy(t, &proxyCalls))
			switch mode {
			case "missing":
				account.Proxy = nil
			case "mismatched":
				account.Proxy.ID++
			case "malformed":
				account.Proxy.Host = "invalid proxy host"
			case "incomplete-auth":
				account.Proxy.Password = ""
			}
			cache := &grokRealtimeHandlerTokenCache{}
			h, repo, slots := grokRealtimePreflightHandler(t, account, cache)
			c, recorder := grokRealtimePreflightContext()
			h.GrokRealtime(c)
			require.Zero(t, cache.reads, "the actual handler must reject the configured route before acquiring its bearer")
			require.Zero(t, cache.writes)
			require.Zero(t, cache.deletes, "invalid routing must not invalidate otherwise healthy credentials")
			require.Zero(t, proxyCalls.Load())
			require.Equal(t, http.StatusBadGateway, recorder.Code)
			require.Empty(t, repo.setErrorIDs)
			require.Empty(t, repo.setTempIDs)
			require.Empty(t, repo.updateExtraIDs)
			slots.assertReleased(t)
		})
	}
}

func TestGrokRealtimeHandlerRetainsRouteAcrossCredentialLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var originalCalls, replacementCalls atomic.Int64
	account := grokRealtimePreflightAccount(grokRealtimePreflightProxy(t, &originalCalls))
	replacement := grokRealtimePreflightProxy(t, &replacementCalls)
	cache := &grokRealtimeHandlerTokenCache{onRead: func() {
		account.Proxy.Host, account.Proxy.Port, account.Proxy.Password = replacement.Host, replacement.Port, "replacement-password"
	}}
	h, _, slots := grokRealtimePreflightHandler(t, account, cache)
	c, recorder := grokRealtimePreflightContext()
	h.GrokRealtime(c)
	require.Equal(t, 1, cache.reads, "the healthy token cache is exercised before the real WS handshake")
	require.Equal(t, int64(1), originalCalls.Load(), "the request retains the route captured before credential lookup")
	require.Zero(t, replacementCalls.Load(), "a route edit during credential lookup cannot receive the acquired bearer")
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	slots.assertReleased(t)
}

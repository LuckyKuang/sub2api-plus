//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	boundRetryUpstream429 = `{"error":{"code":429,"message":"Resource exhausted. Please try again later.","status":"RESOURCE_EXHAUSTED"}}`
	boundRetryUpstream502 = `{"error":{"code":502,"message":"Upstream service temporarily unavailable","status":"UNAVAILABLE"}}`
)

// boundRetryUpstream 前 failures 次请求返回 failStatus（默认 429），之后返回成功的生成响应。
type boundRetryUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	failures   int
	failStatus int
	bodies     []string
	onRequest  func(*http.Request, string, int)
}

func (u *boundRetryUpstream) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	raw, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	u.bodies = append(u.bodies, string(raw))
	n := len(u.bodies)
	u.mu.Unlock()
	if u.onRequest != nil {
		u.onRequest(req, proxyURL, n)
	}
	header := http.Header{"Content-Type": []string{"application/json"}}
	if n <= u.failures {
		if u.failStatus == http.StatusBadGateway {
			return &http.Response{StatusCode: http.StatusBadGateway, Header: header, Body: io.NopCloser(strings.NewReader(boundRetryUpstream502))}, nil
		}
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: header, Body: io.NopCloser(strings.NewReader(boundRetryUpstream429))}, nil
	}
	body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],` +
		`"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":8,"candidatesTokenCount":1,"totalTokenCount":11}}`
	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (u *boundRetryUpstream) requestBodies() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.bodies...)
}

// newBoundRetryEnv 构造绑定在池模式上游网关账号 900 上的显式缓存，账号对 retryCodes 同账号重试 2 次。
func newBoundRetryEnv(t *testing.T, upstream *boundRetryUpstream, retryCodes ...int) *cachedBillingEnv {
	t.Helper()
	env := newCachedBillingEnv(t, 500, 900)
	creds := env.accounts[900].Credentials
	creds["base_url"] = "https://gw.example.com"
	creds[service.GeminiExplicitCacheUpstreamCredentialKey] = true
	creds["pool_mode"] = true
	creds["pool_mode_retry_count"] = float64(2)
	codes := make([]any, 0, len(retryCodes))
	for _, code := range retryCodes {
		codes = append(codes, float64(code))
	}
	creds["pool_mode_retry_status_codes"] = codes
	env.seed(time.Now().Add(time.Hour))

	env.h.geminiCompatService = service.NewGeminiMessagesCompatService(&cachedBillingAccountRepo{accounts: env.accounts},
		nil, nil, nil, nil, nil, upstream, nil, &config.Config{})
	return env
}

func (e *cachedBillingEnv) generateWithCache() *httptest.ResponseRecorder {
	return e.serve(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent",
		`{"cachedContent":"cachedContents/abc","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
		gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}, e.h.GeminiV1BetaModels)
}

func TestGeminiV1BetaModels_BoundCachePoolModeRetriesUpstream429(t *testing.T) {
	upstream := &boundRetryUpstream{failures: 1}
	env := newBoundRetryEnv(t, upstream, 429)

	rec := env.generateWithCache()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	bodies := upstream.requestBodies()
	require.Len(t, bodies, 2, "429 后在持有缓存的账号上重试一次")
	for _, body := range bodies {
		require.Contains(t, body, `"cachedContents/up1"`, "每次重试都引用上游缓存名")
	}
}

func TestGeminiV1BetaModels_BoundCacheRetryExhaustedPassesThroughUpstream429(t *testing.T) {
	upstream := &boundRetryUpstream{failures: 10}
	env := newBoundRetryEnv(t, upstream, 429)

	rec := env.generateWithCache()

	require.Len(t, upstream.requestBodies(), 3, "首发 + pool_mode_retry_count 次同账号重试")
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.JSONEq(t, boundRetryUpstream429, rec.Body.String(), "重试用尽后原样回写上游网关的响应")
}

// 502 在池模式重试状态码内时同样同账号重试；用尽后原样回写，不按空响应临时封禁本账号。
func TestGeminiV1BetaModels_BoundCacheRetryExhaustedOn502KeepsAccountSchedulable(t *testing.T) {
	upstream := &boundRetryUpstream{failures: 10, failStatus: http.StatusBadGateway}
	env := newBoundRetryEnv(t, upstream, 429, 502)

	rec := env.generateWithCache()

	require.Len(t, upstream.requestBodies(), 3)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.JSONEq(t, boundRetryUpstream502, rec.Body.String())
}

// OUTBOUND_IDENTITY.md requires one credential/identity/proxy snapshot for a
// same-account retry. Replacing a saved row must not lend its new grant to an
// already accepted request referring to the old grant's cached content.
func TestGeminiV1BetaModels_BoundCacheRetryRetainsAdmittedOwner(t *testing.T) {
	for _, mutation := range []string{"replace credential and proxy", "edit shared credential and proxy"} {
		t.Run(mutation, func(t *testing.T) {
			upstream := &boundRetryUpstream{failures: 1}
			env := newBoundRetryEnv(t, upstream, 429)
			account := env.accounts[900]
			proxyID := int64(71)
			account.ProxyID = &proxyID
			account.Proxy = &service.Proxy{ID: 71, Protocol: "http", Host: "old-proxy.example", Port: 8080,
				Username: "old-user", Password: "old-password", Status: service.StatusActive}
			account.Credentials["outbound_identity"] = service.OutboundIdentitySelection{Preset: "gemini", Version: "3.9.1"}
			var urls, proxies, keys, agents []string
			upstream.onRequest = func(req *http.Request, proxyURL string, attempt int) {
				urls = append(urls, req.URL.String())
				proxies = append(proxies, proxyURL)
				keys = append(keys, req.Header.Get("x-goog-api-key"))
				agents = append(agents, req.Header.Get("User-Agent"))
				if attempt != 1 {
					return
				}
				if mutation == "replace credential and proxy" {
					replacement := make(map[string]any, len(account.Credentials))
					for key, value := range account.Credentials {
						replacement[key] = value
					}
					account.Credentials = replacement
					newProxyID := int64(72)
					account.ProxyID = &newProxyID
					account.Proxy = &service.Proxy{ID: 72, Protocol: "http", Port: 8080, Status: service.StatusActive}
				}
				account.Credentials["api_key"] = "replacement-key"
				account.Credentials["base_url"] = "https://new-gateway.example"
				account.Credentials["outbound_identity"] = service.OutboundIdentitySelection{Preset: "gemini", Version: "3.9.7"}
				account.Proxy.Host = "new-proxy.example"
				account.Proxy.Username, account.Proxy.Password = "new-user", "new-password"
				// Retail settings changed after audit must not reprice the retry.
				env.group.RateMultiplier = 9
			}

			rec := env.generateWithCache()

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, []string{
				"https://gw.example.com/v1beta/models/gemini-2.5-pro:generateContent",
				"https://gw.example.com/v1beta/models/gemini-2.5-pro:generateContent",
			}, urls)
			require.Equal(t, []string{"http://old-user:old-password@old-proxy.example:8080", "http://old-user:old-password@old-proxy.example:8080"}, proxies)
			require.Equal(t, []string{"k", "k"}, keys)
			require.Equal(t, []string{"GeminiCLI/3.9.1 (Linux; x64)", "GeminiCLI/3.9.1 (Linux; x64)"}, agents)
			for _, body := range upstream.requestBodies() {
				require.Contains(t, body, `"cachedContents/up1"`)
			}
			require.Len(t, env.waitForUsage(t, 1), 1, "only the successful response can create a usage charge")
			require.Equal(t, 1.0, env.usage.first().RateMultiplier, "settlement keeps the generation admitted before the first send")

			// The retained owner belongs to one accepted request, not the row or
			// its cache resource. A new request uses the committed replacement.
			rec = env.generateWithCache()
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, urls, 3)
			require.Equal(t, "https://new-gateway.example/v1beta/models/gemini-2.5-pro:generateContent", urls[2])
			require.Equal(t, "http://new-user:new-password@new-proxy.example:8080", proxies[2])
			require.Equal(t, "replacement-key", keys[2])
			require.Equal(t, "GeminiCLI/3.9.7 (Linux; x64)", agents[2])
			require.Len(t, env.waitForUsage(t, 2), 2)
		})
	}
}

// A real scheduler bucket drops an account after its group membership changes.
// The shared fake intentionally ignores buckets, so model that contract here
// without filtering status: disabling must still be rejected by the scheduler.
type boundRetryGroupSnapshotCache struct {
	*fakeSchedulerCache
}

func (c *boundRetryGroupSnapshotCache) GetSnapshot(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	var accounts []*service.Account
	for _, account := range c.accounts {
		for _, membership := range account.AccountGroups {
			if membership.GroupID == bucket.GroupID {
				accounts = append(accounts, account)
				break
			}
		}
	}
	return accounts, true, nil
}

func TestGeminiV1BetaModels_BoundCacheRetryRechecksCurrentEligibility(t *testing.T) {
	for _, mutation := range []string{"disabled", "moved to another group"} {
		t.Run(mutation, func(t *testing.T) {
			upstream := &boundRetryUpstream{failures: 10}
			env := newBoundRetryEnv(t, upstream, 429)
			account := env.accounts[900]
			cache := &boundRetryGroupSnapshotCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account}}}
			snapshot := service.NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
			// This failure-only fixture needs scheduling and admission, not a
			// successful settlement service. Every attempted send returns 429.
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			env.h.gatewayService = service.NewGatewayService(nil, &fakeGroupRepo{group: env.group}, env.usage, nil, nil,
				nil, nil, nil, cfg, snapshot, nil, nil, nil, &service.BillingCacheService{}, nil, nil,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			upstream.onRequest = func(_ *http.Request, _ string, attempt int) {
				if attempt != 1 {
					return
				}
				if mutation == "disabled" {
					account.Status = service.StatusDisabled
				} else {
					account.AccountGroups = []service.AccountGroup{{AccountID: account.ID, GroupID: env.group.ID + 1}}
				}
			}

			rec := env.generateWithCache()

			require.Len(t, upstream.requestBodies(), 1, "the retained grant must not bypass current scheduling eligibility")
			require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
			require.JSONEq(t, boundRetryUpstream429, rec.Body.String())
			require.Empty(t, env.usage.totals(), "an unsuccessful response cannot create a usage charge")
		})
	}
}

func TestGeminiV1BetaModels_BoundCacheMissingProxySnapshotCannotSend(t *testing.T) {
	upstream := &boundRetryUpstream{}
	env := newBoundRetryEnv(t, upstream, 429)
	var acquiredUserSlots int32
	slots := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
		atomic.AddInt32(&acquiredUserSlots, 1)
		return true, nil
	}}
	env.h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(slots), SSEPingFormatNone, 0)
	proxyID := int64(71)
	env.accounts[900].ProxyID = &proxyID
	env.accounts[900].Proxy = nil

	rec := env.generateWithCache()

	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Empty(t, upstream.requestBodies(), "a configured proxy without its snapshot cannot authorize direct egress")
	require.Empty(t, env.usage.totals(), "rejection before sending cannot create a usage charge")
	require.Equal(t, int32(1), atomic.LoadInt32(&acquiredUserSlots), "the request entered user admission before resolving its proxy")
	require.Equal(t, int32(1), atomic.LoadInt32(&slots.releaseUserCalled), "proxy rejection must release its acquired user slot exactly once")
}
